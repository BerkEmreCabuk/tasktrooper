package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// BoardSeedStore runs the default board seed and records in install_state
// that it ran, so a board the user has since edited is never seeded again.
type BoardSeedStore struct {
	pool *DB
}

func NewBoardSeedStore(pool *DB) *BoardSeedStore { return &BoardSeedStore{pool: pool} }

// SeedBoardOnce runs sql unless install_state says the board is already seeded,
// and stamps board_seeded_at in the same transaction, so a half-seeded board
// never counts as seeded. It reports whether the seed ran.
//
// The conditional upsert doubles as the lock: a concurrent caller waits on the
// row, then finds board_seeded_at set and skips.
func (s *BoardSeedStore) SeedBoardOnce(ctx context.Context, sql string) (bool, error) {
	var ran bool
	err := s.pool.InTx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO install_state (id, board_seeded_at) VALUES (1, now())
			ON CONFLICT (id) DO UPDATE SET board_seeded_at = EXCLUDED.board_seeded_at
			WHERE install_state.board_seeded_at IS NULL
			RETURNING true
		`).Scan(&ran)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, sql)
		return err
	})
	if err != nil {
		return false, err
	}
	return ran, nil
}
