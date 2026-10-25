package board

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// mergedFilesCap is the GitHub adapter's ListPullRequestFiles ceiling
// (5 pages of 100). A list that reaches it may be cut short, and a cut list
// could make a merge look as if it touched no component when its later
// files did, so it is not used to decide anything.
const mergedFilesCap = 500

var errMergedFilesTruncated = errors.New("the pull request changed too many files to list them all")

// MergedTaskFiles is the release service's view of what a task's merged pull
// request changed, read from GitHub so it works the same with or without a
// checkout on this machine. A renamed file counts at both paths: moving a
// file out of a component changes that component too.
func (s *TaskPRService) MergedTaskFiles(ctx context.Context, repositoryID uuid.UUID, task domain.BoardTask) ([]string, error) {
	number, prURL := taskPRRef(task)
	if prURL == "" || number <= 0 {
		return nil, fmt.Errorf("task %s has no pull request recorded", taskLabel(task))
	}
	if s.prs == nil {
		return nil, errors.New("the pull request client is not configured")
	}
	token := s.token(ctx)
	if token == "" {
		return nil, errors.New("GitHub is not connected")
	}
	owner, repo, err := s.ownerRepo(ctx, repositoryID, task.ID)
	if err != nil {
		return nil, err
	}
	files, err := s.prs.ListPullRequestFiles(ctx, token, owner, repo, number)
	if err != nil {
		return nil, err
	}
	if len(files) >= mergedFilesCap {
		return nil, errMergedFilesTruncated
	}
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
		if f.PreviousPath != "" {
			paths = append(paths, f.PreviousPath)
		}
	}
	return paths, nil
}
