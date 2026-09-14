package domain_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/stretchr/testify/assert"
)

// The failure this exists to prevent: a byte slice through a multi-byte rune
// makes invalid UTF-8, Postgres rejects the write, and the row that was meant
// to record a finished run never lands. Turkish is the default language here,
// so multi-byte runes are the norm, not the exception.
func TestTruncateNeverSplitsARune(t *testing.T) {
	turkish := strings.Repeat("çğışöü", 200)

	for budget := 1; budget < 40; budget++ {
		assert.True(t, utf8.ValidString(domain.TruncateHead(turkish, budget)), "head budget %d", budget)
		assert.True(t, utf8.ValidString(domain.TruncateTail(turkish, budget)), "tail budget %d", budget)
	}
}

func TestTruncateStaysWithinBudget(t *testing.T) {
	s := strings.Repeat("çğışöü", 200)

	assert.LessOrEqual(t, len(domain.TruncateHead(s, 100)), 100)
	assert.LessOrEqual(t, len(domain.TruncateTail(s, 100)), 100)
}

func TestTruncateKeepsTheRightEnd(t *testing.T) {
	s := "START" + strings.Repeat("x", 100) + "END"

	assert.True(t, strings.HasPrefix(domain.TruncateHead(s, 20), "START"))
	assert.True(t, strings.HasSuffix(domain.TruncateTail(s, 20), "END"))
}

func TestTruncateLeavesShortStringsAlone(t *testing.T) {
	assert.Equal(t, "kısa", domain.TruncateHead("kısa", 100))
	assert.Equal(t, "kısa", domain.TruncateTail("kısa", 100))
	assert.Equal(t, "", domain.TruncateHead("kısa", 0))
	assert.Equal(t, "", domain.TruncateTail("kısa", -1))
}
