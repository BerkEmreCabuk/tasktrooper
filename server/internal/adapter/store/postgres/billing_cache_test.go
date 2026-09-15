package postgres_test

import (
	"context"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/suite"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/store/postgres"
	"github.com/makifbaysal/tasktrooper/server/internal/application/tenantboot"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/database"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/tenant"
)

// BillingCacheSuite covers the cost arithmetic, which lives entirely in SQL:
// cached tokens are a subset of prompt_tokens priced at their own rate, and an
// unpriced cache column must fall back to the FULL prompt price rather than
// quietly discounting the spend the budget gate is supposed to catch.
type BillingCacheSuite struct {
	suite.Suite
	ctx    context.Context
	cancel context.CancelFunc
	pg     *database.Embedded
	pool   *pgxpool.Pool
	db     *postgres.DB
	store  *postgres.BillingStore
	usage  *postgres.LLMUsageStore
	// seeded is the tenant's starting price list, captured before the per-test
	// wipe so the seed itself can still be asserted on.
	//
	// It comes from tenantboot rather than from a migration now: migration 114
	// moved every per-install seed to a per-TENANT one, because a migration
	// runs once for the whole shared database and its rows would belong to
	// nobody.
	seeded []domain.ModelPrice
}

func TestBillingCacheSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded postgres integration test in short mode")
	}
	suite.Run(t, new(BillingCacheSuite))
}

func (s *BillingCacheSuite) SetupSuite() {
	s.ctx, s.cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	tmp := s.T().TempDir()
	pg, err := database.StartEmbedded(s.ctx, database.EmbeddedConfig{
		DataDir:     filepath.Join(tmp, "postgres"),
		RuntimePath: filepath.Join(tmp, "runtime"),
	})
	s.Require().NoError(err)
	s.pg = pg
	pool, err := pgxpool.New(s.ctx, pg.DSN())
	s.Require().NoError(err)
	s.pool = pool
	s.db = postgres.NewDB(pool)
	s.store = postgres.NewBillingStore(s.db)
	s.usage = postgres.NewLLMUsageStore(s.db)

	// Seed the default board exactly as boot does.
	s.Require().NoError(tenantboot.NewService(postgres.NewTenantSeedStore(s.db)).
		Sight(s.ctx, tenant.Identity{TenantID: tenant.LocalTenantID, Role: tenant.RoleOwner}))

	s.seeded, err = s.store.ListModelPrices(s.ctx)
	s.Require().NoError(err)
}

func (s *BillingCacheSuite) TearDownSuite() {
	if s.pool != nil {
		s.pool.Close()
	}
	if s.pg != nil {
		_ = s.pg.Stop()
	}
	if s.cancel != nil {
		s.cancel()
	}
}

// Each case starts from a clean slate: UsdSpentSince sums the whole period, so
// a leftover row from another case would land in the total.
func (s *BillingCacheSuite) SetupTest() {
	_, err := s.db.Exec(s.ctx, `DELETE FROM llm_usage`)
	s.Require().NoError(err)
	_, err = s.db.Exec(s.ctx, `DELETE FROM model_prices`)
	s.Require().NoError(err)
}

func usd(v float64) *float64 { return &v }

func (s *BillingCacheSuite) spent() float64 {
	total, err := s.store.UsdSpentSince(s.ctx, time.Now().Add(-time.Hour))
	s.Require().NoError(err)
	return total
}

func (s *BillingCacheSuite) assertUSD(got, want float64) {
	s.T().Helper()
	if math.Abs(got-want) > 1e-9 {
		s.Failf("wrong spend", "usd = %.10f, want %.10f", got, want)
	}
}

// The headline case: a turn where most of the prompt came from cache must cost
// far less than the same turn priced flat, and the base rate must apply only to
// the uncached remainder.
func (s *BillingCacheSuite) TestMixedCacheUsageIsPricedPerRate() {
	_, err := s.store.UpsertModelPrice(s.ctx, domain.ModelPrice{
		Model:              "claude-opus-5",
		UsdPer1MPrompt:     5.0,
		UsdPer1MCompletion: 25.0,
		UsdPer1MCacheRead:  usd(0.5),  // 0.10x prompt
		UsdPer1MCacheWrite: usd(6.25), // 1.25x prompt
	})
	s.Require().NoError(err)

	s.Require().NoError(s.usage.Record(s.ctx, domain.LLMUsageRecord{
		Model:            "claude-opus-5",
		PromptTokens:     1_000_000,
		CompletionTokens: 100_000,
		CacheReadTokens:  800_000,
		CacheWriteTokens: 100_000,
	}))

	// uncached 100_000 * 5.0/1e6 = 0.50
	// read     800_000 * 0.5/1e6 = 0.40
	// write    100_000 * 6.25/1e6 = 0.625
	// output   100_000 * 25.0/1e6 = 2.50
	s.assertUSD(s.spent(), 0.50+0.40+0.625+2.50)
}

// The safety property: an operator who prices a model but leaves the cache
// columns NULL must be charged the full prompt rate on every token. Charging
// less would let a tenant spend past the budget for free.
func (s *BillingCacheSuite) TestNullCacheRatesChargeTheFullPromptPrice() {
	_, err := s.store.UpsertModelPrice(s.ctx, domain.ModelPrice{
		Model:              "gpt-oss",
		UsdPer1MPrompt:     5.0,
		UsdPer1MCompletion: 25.0,
	})
	s.Require().NoError(err)

	s.Require().NoError(s.usage.Record(s.ctx, domain.LLMUsageRecord{
		Model:            "gpt-oss",
		PromptTokens:     1_000_000,
		CompletionTokens: 100_000,
		CacheReadTokens:  800_000,
		CacheWriteTokens: 100_000,
	}))

	// Every prompt token at 5.0 — no discount whatsoever.
	withCache := s.spent()
	s.assertUSD(withCache, 5.0+2.50)

	// And it matches the same call recorded with no cache split at all.
	_, err = s.db.Exec(s.ctx, `DELETE FROM llm_usage`)
	s.Require().NoError(err)
	s.Require().NoError(s.usage.Record(s.ctx, domain.LLMUsageRecord{
		Model:            "gpt-oss",
		PromptTokens:     1_000_000,
		CompletionTokens: 100_000,
	}))
	s.assertUSD(withCache, s.spent())
}

// The '*' catch-all still prices unknown models, and its own cache columns
// apply when it is the row that matched.
func (s *BillingCacheSuite) TestCatchAllFallbackStillPricesUnknownModels() {
	_, err := s.store.UpsertModelPrice(s.ctx, domain.ModelPrice{
		Model:              "*",
		UsdPer1MPrompt:     10.0,
		UsdPer1MCompletion: 30.0,
		UsdPer1MCacheRead:  usd(1.0),
	})
	s.Require().NoError(err)

	s.Require().NoError(s.usage.Record(s.ctx, domain.LLMUsageRecord{
		Model:            "some-unlisted-model",
		PromptTokens:     1_000_000,
		CompletionTokens: 100_000,
		CacheReadTokens:  600_000,
	}))

	// uncached 400_000 * 10.0/1e6 = 4.0
	// read     600_000 *  1.0/1e6 = 0.6
	// output   100_000 * 30.0/1e6 = 3.0
	s.assertUSD(s.spent(), 4.0+0.6+3.0)
}

// A model priced by name but with no cache rate must not borrow the catch-all
// row's cache discount — that would price one model's cache off another's.
func (s *BillingCacheSuite) TestExactRowDoesNotBorrowTheCatchAllCacheRate() {
	_, err := s.store.UpsertModelPrice(s.ctx, domain.ModelPrice{
		Model:              "*",
		UsdPer1MPrompt:     10.0,
		UsdPer1MCompletion: 30.0,
		UsdPer1MCacheRead:  usd(0.01),
	})
	s.Require().NoError(err)
	_, err = s.store.UpsertModelPrice(s.ctx, domain.ModelPrice{
		Model:              "priced-model",
		UsdPer1MPrompt:     5.0,
		UsdPer1MCompletion: 25.0,
	})
	s.Require().NoError(err)

	s.Require().NoError(s.usage.Record(s.ctx, domain.LLMUsageRecord{
		Model:           "priced-model",
		PromptTokens:    1_000_000,
		CacheReadTokens: 1_000_000,
	}))

	// Its own full prompt rate, not the '*' row's 0.01.
	s.assertUSD(s.spent(), 5.0)
}

// A model with no price row at all still costs nothing — the pre-existing
// behaviour the budget gate documents — and cache columns must not change that.
func (s *BillingCacheSuite) TestUnpricedModelStillCostsNothing() {
	s.Require().NoError(s.usage.Record(s.ctx, domain.LLMUsageRecord{
		Model:            "(default)",
		PromptTokens:     1_000_000,
		CompletionTokens: 100_000,
		CacheReadTokens:  900_000,
	}))
	s.assertUSD(s.spent(), 0)
}

// Defensive: a provider reporting more cached tokens than prompt tokens must
// not mint negative spend that offsets other rows.
func (s *BillingCacheSuite) TestCacheTokensExceedingPromptNeverGoNegative() {
	_, err := s.store.UpsertModelPrice(s.ctx, domain.ModelPrice{
		Model:              "weird-model",
		UsdPer1MPrompt:     5.0,
		UsdPer1MCompletion: 0,
		UsdPer1MCacheRead:  usd(0),
	})
	s.Require().NoError(err)

	s.Require().NoError(s.usage.Record(s.ctx, domain.LLMUsageRecord{
		Model:           "weird-model",
		PromptTokens:    1_000,
		CacheReadTokens: 5_000,
	}))
	s.assertUSD(s.spent(), 0)
}

// A fresh TENANT must account for cache spend without an operator touching
// anything, so the seed prices every Claude row off that row's own prompt rate
// (0.10x read, 1.25x write) the way migration 094 did when the seed lived in a
// migration.
func (s *BillingCacheSuite) TestMigrationSeedsClaudeCacheRates() {
	var checked int
	for _, p := range s.seeded {
		if p.UsdPer1MCacheRead == nil && p.UsdPer1MCacheWrite == nil {
			continue // an unseeded model correctly left at full price
		}
		checked++
		s.Require().NotNil(p.UsdPer1MCacheRead, "%s: read rate", p.Model)
		s.Require().NotNil(p.UsdPer1MCacheWrite, "%s: write rate", p.Model)
		s.assertUSD(*p.UsdPer1MCacheRead, 0.10*p.UsdPer1MPrompt)
		s.assertUSD(*p.UsdPer1MCacheWrite, 1.25*p.UsdPer1MPrompt)
	}
	s.Require().NotZero(checked, "tenantboot seeds Claude rows; they must carry cache rates")
}
