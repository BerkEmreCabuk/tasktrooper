package port

import (
	"context"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type SettingsStore interface {
	Get(ctx context.Context) (domain.AppSettings, error)
	Update(ctx context.Context, req domain.UpdateSettingsRequest) (domain.AppSettings, error)
}

type GitHubTokenStore interface {
	GitHubToken(ctx context.Context) (string, error)
	SetGitHubToken(ctx context.Context, token string) error
	DeleteGitHubToken(ctx context.Context) error
}
