package postgres

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type LLMUsageStore struct {
	pool *DB
}

func NewLLMUsageStore(pool *DB) *LLMUsageStore {
	return &LLMUsageStore{pool: pool}
}

func (s *LLMUsageStore) Record(ctx context.Context, rec domain.LLMUsageRecord) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO llm_usage (model, prompt_tokens, completion_tokens, cache_read_tokens, cache_write_tokens)
		VALUES ($1, $2, $3, $4, $5)`,
		rec.Model, rec.PromptTokens, rec.CompletionTokens, rec.CacheReadTokens, rec.CacheWriteTokens)
	return err
}

func (s *LLMUsageStore) Summary(ctx context.Context, days int) (domain.LLMUsageSummary, error) {
	if days <= 0 {
		days = 30
	}
	sum := domain.LLMUsageSummary{Days: days}

	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(prompt_tokens), 0), COALESCE(SUM(completion_tokens), 0),
		       COALESCE(SUM(cache_read_tokens), 0), COALESCE(SUM(cache_write_tokens), 0)
		FROM llm_usage
		WHERE created_at >= now() - make_interval(days => $1)`, days).
		Scan(&sum.Total.Calls, &sum.Total.PromptTokens, &sum.Total.CompletionTokens,
			&sum.Total.CacheReadTokens, &sum.Total.CacheWriteTokens)
	if err != nil {
		return domain.LLMUsageSummary{}, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT model, COUNT(*), SUM(prompt_tokens), SUM(completion_tokens),
		       SUM(cache_read_tokens), SUM(cache_write_tokens)
		FROM llm_usage
		WHERE created_at >= now() - make_interval(days => $1)
		GROUP BY model
		ORDER BY SUM(prompt_tokens + completion_tokens) DESC`, days)
	if err != nil {
		return domain.LLMUsageSummary{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var m domain.LLMUsageByModel
		if err := rows.Scan(&m.Model, &m.Calls, &m.PromptTokens, &m.CompletionTokens,
			&m.CacheReadTokens, &m.CacheWriteTokens); err != nil {
			return domain.LLMUsageSummary{}, err
		}
		sum.ByModel = append(sum.ByModel, m)
	}
	if err := rows.Err(); err != nil {
		return domain.LLMUsageSummary{}, err
	}

	dayRows, err := s.pool.Query(ctx, `
		SELECT to_char(date_trunc('day', created_at AT TIME ZONE 'UTC'), 'YYYY-MM-DD'),
		       COUNT(*), SUM(prompt_tokens), SUM(completion_tokens),
		       SUM(cache_read_tokens), SUM(cache_write_tokens)
		FROM llm_usage
		WHERE created_at >= now() - make_interval(days => $1)
		GROUP BY 1
		ORDER BY 1`, days)
	if err != nil {
		return domain.LLMUsageSummary{}, err
	}
	defer dayRows.Close()
	for dayRows.Next() {
		var d domain.LLMUsageByDay
		if err := dayRows.Scan(&d.Day, &d.Calls, &d.PromptTokens, &d.CompletionTokens,
			&d.CacheReadTokens, &d.CacheWriteTokens); err != nil {
			return domain.LLMUsageSummary{}, err
		}
		sum.Daily = append(sum.Daily, d)
	}
	return sum, dayRows.Err()
}
