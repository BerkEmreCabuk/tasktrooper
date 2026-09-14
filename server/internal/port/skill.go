package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type SkillRetriever interface {
	SearchSkills(ctx context.Context, query string, topK int) ([]domain.Skill, error)
}
