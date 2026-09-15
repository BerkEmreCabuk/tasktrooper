package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// assertSameRepo is the check every path that ADOPTS an existing checkout has
// to pass before it acts on one.
//
// There are four such paths — the GitHub import skipping the clone, the
// explicit restore, the index mirror, and the board runner's ensureWorkingCopy
// — and all four used to reduce to "is there a .git here?". That question was
// sufficient while a directory could only have been put there by this
// repository. It stops being sufficient once a repository is deleted and its
// directory name reused by a later import, where "yes, a repository" and
// "yes, THIS repository" come apart.
//
// The comparison is on the origin URL because that is the only identity a
// checkout carries that this process did not itself write into the row. A
// positive mismatch is a refusal; a missing value on either side is not,
// because a repository created here by `git init` legitimately has no origin
// yet, and `repositories.root_path` being UNIQUE is what rules out a
// collision in that case. This check is the second lock, not the first.
func (s *Service) assertSameRepo(ctx context.Context, dir, wantRemote string) error {
	if s.git == nil {
		return nil
	}
	return assertCheckoutIsRepo(s.git.OriginURL(ctx, dir), dir, wantRemote)
}

// assertCheckoutIsRepo is the comparison itself, taking the origin the caller
// already read. Separate from the method so the board runner — which cannot
// import this package — applies the identical rule and the identical sentence.
func assertCheckoutIsRepo(foundRemote, dir, wantRemote string) error {
	want := strings.TrimSpace(wantRemote)
	found := strings.TrimSpace(foundRemote)
	if want == "" || found == "" || domain.SameGitRemote(found, want) {
		return nil
	}
	// The found repository's URL is not echoed back: on a shared volume that
	// would name a checkout the caller may have no business knowing exists.
	return fmt.Errorf(
		"the working copy at %s is a checkout of a different repository than the one on record (%s); "+
			"nothing was changed or deleted — move it aside, or re-import this repository, before it can be used",
		dir, domain.NormalizeGitRemote(want))
}
