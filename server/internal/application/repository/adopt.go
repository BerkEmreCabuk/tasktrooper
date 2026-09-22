package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (s *Service) assertSameRepo(ctx context.Context, dir, wantRemote string) error {
	if s.git == nil {
		return nil
	}
	return assertCheckoutIsRepo(s.git.OriginURL(ctx, dir), dir, wantRemote)
}

func assertCheckoutIsRepo(foundRemote, dir, wantRemote string) error {
	want := strings.TrimSpace(wantRemote)
	found := strings.TrimSpace(foundRemote)
	if want == "" || found == "" || domain.SameGitRemote(found, want) {
		return nil
	}

	return fmt.Errorf(
		"the working copy at %s is a checkout of a different repository than the one on record (%s); "+
			"nothing was changed or deleted — move it aside, or re-import this repository, before it can be used",
		dir, domain.NormalizeGitRemote(want))
}
