package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

// Deploy packages: the release path for repositories that turned per-task auto
// release off.
//
// Such a repository has, without this, no release path whatsoever —
// trigger_release refuses it with ErrReleaseDisabled ("uses batched release")
// and nothing else dispatches a prod deploy. The batch that error names is this
// file.
//
// WHY RELEASE IS NOT A LOOP
// A release is not something that finishes inside a request. TriggerRelease
// only DISPATCHES a workflow; the deploy runs for minutes on GitHub Actions and
// the pipeline runner finalizes it later, out of band. So ReleasePackage cannot
// walk its members in order and wait — it dispatches the first wave (members
// whose dependencies are already satisfied) and returns with the package in
// `releasing`. Every subsequent read of the package advances it: members that
// have landed are marked, members whose dependencies just became satisfied are
// dispatched. That advancement is AdvancePackage, and it is deliberately the
// only place package state changes after the initial dispatch.

// SetDeployPackages wires the release-train store. Nil leaves every package
// endpoint answering "unavailable" rather than panicking.
func (s *Service) SetDeployPackages(store port.DeployPackageStore) {
	s.deployPackages = store
}

// ListDeployPackages returns the repository's release trains, each advanced
// first: a package's stored status is only as fresh as the last time somebody
// looked at it, because deploys land out of band and nothing else polls them.
func (s *Service) ListDeployPackages(ctx context.Context, repositoryID uuid.UUID) ([]domain.DeployPackage, error) {
	if s.deployPackages == nil {
		return nil, fmt.Errorf("deploy package store unavailable")
	}
	packages, err := s.deployPackages.ListByRepository(ctx, repositoryID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.DeployPackage, 0, len(packages))
	for _, pkg := range packages {
		advanced, aerr := s.AdvancePackage(ctx, repositoryID, pkg)
		if aerr != nil {
			// One unreadable package must not blank the whole list: return it
			// as stored, with its membership, so the UI can still show it.
			log.Warn().Err(aerr).Str("package_id", pkg.ID.String()).Msg("advance deploy package failed")
			pkg.Tasks, _ = s.deployPackages.ListTasks(ctx, pkg.ID)
			out = append(out, pkg)
			continue
		}
		out = append(out, advanced)
	}
	return out, nil
}

// GetDeployPackage returns one release train, advanced.
func (s *Service) GetDeployPackage(ctx context.Context, repositoryID, packageID uuid.UUID) (domain.DeployPackage, error) {
	if s.deployPackages == nil {
		return domain.DeployPackage{}, fmt.Errorf("deploy package store unavailable")
	}
	pkg, err := s.deployPackages.Get(ctx, repositoryID, packageID)
	if err != nil {
		return domain.DeployPackage{}, err
	}
	return s.AdvancePackage(ctx, repositoryID, pkg)
}

// CreateDeployPackage opens an empty draft train.
func (s *Service) CreateDeployPackage(ctx context.Context, repositoryID uuid.UUID, name, description string) (domain.DeployPackage, error) {
	if s.deployPackages == nil {
		return domain.DeployPackage{}, fmt.Errorf("deploy package store unavailable")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.DeployPackage{}, fmt.Errorf("deploy package name is required")
	}
	if _, err := s.repos.Get(ctx, repositoryID); err != nil {
		return domain.DeployPackage{}, err
	}
	return s.deployPackages.Create(ctx, domain.DeployPackage{
		RepositoryID: repositoryID,
		Name:         name,
		Description:  strings.TrimSpace(description),
		Status:       domain.DeployPackageStatusDraft,
	})
}

// UpdateDeployPackage renames a train or cancels it.
//
// cancelled is the ONLY status an API caller may set. Every other transition is
// evidence-driven (a member landed, a member's release was refused), and a
// client that could write `released` directly would be asserting a production
// state nothing verified — which is the exact failure the lifecycle gates exist
// to prevent.
func (s *Service) UpdateDeployPackage(ctx context.Context, repositoryID, packageID uuid.UUID, name, description, status *string) (domain.DeployPackage, error) {
	if s.deployPackages == nil {
		return domain.DeployPackage{}, fmt.Errorf("deploy package store unavailable")
	}
	if status != nil {
		if *status != domain.DeployPackageStatusCancelled {
			return domain.DeployPackage{}, fmt.Errorf(
				"a deploy package's status is set by its release, not by the client; only %q may be requested",
				domain.DeployPackageStatusCancelled)
		}
		current, err := s.deployPackages.Get(ctx, repositoryID, packageID)
		if err != nil {
			return domain.DeployPackage{}, err
		}
		if current.Status == domain.DeployPackageStatusReleased {
			return domain.DeployPackage{}, fmt.Errorf("this package is already released; cancelling it would not un-deploy anything")
		}
	}
	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if trimmed == "" {
			return domain.DeployPackage{}, fmt.Errorf("deploy package name is required")
		}
		name = &trimmed
	}
	return s.deployPackages.Update(ctx, repositoryID, packageID, name, description, status, nil)
}

// DeleteDeployPackage drops a train. Membership goes with it (ON DELETE
// CASCADE); the tasks themselves are untouched.
func (s *Service) DeleteDeployPackage(ctx context.Context, repositoryID, packageID uuid.UUID) error {
	if s.deployPackages == nil {
		return fmt.Errorf("deploy package store unavailable")
	}
	return s.deployPackages.Delete(ctx, repositoryID, packageID)
}

// SetDeployPackageTasks replaces the membership. Array order becomes position.
//
// Membership is only editable while the train has not shipped: adding a task to
// a package that is already releasing would produce a member nothing ever
// dispatched, and to one that is released would make the package's own status a
// lie.
func (s *Service) SetDeployPackageTasks(ctx context.Context, repositoryID, packageID uuid.UUID, taskIDs []uuid.UUID) (domain.DeployPackage, error) {
	if s.deployPackages == nil {
		return domain.DeployPackage{}, fmt.Errorf("deploy package store unavailable")
	}
	pkg, err := s.deployPackages.Get(ctx, repositoryID, packageID)
	if err != nil {
		return domain.DeployPackage{}, err
	}
	if pkg.Status == domain.DeployPackageStatusReleasing || pkg.Status == domain.DeployPackageStatusReleased {
		return domain.DeployPackage{}, fmt.Errorf("cannot change the membership of a %s package", pkg.Status)
	}
	// Every member must belong to this repository: a package releases through
	// this repository's workflows, so a foreign task in it would dispatch the
	// wrong pipeline.
	for _, taskID := range taskIDs {
		if _, terr := s.tasks.Get(ctx, repositoryID, taskID); terr != nil {
			return domain.DeployPackage{}, fmt.Errorf("task %s does not belong to this repository", taskID)
		}
	}
	if err := s.deployPackages.ReplaceTasks(ctx, packageID, taskIDs); err != nil {
		return domain.DeployPackage{}, err
	}
	return s.AdvancePackage(ctx, repositoryID, pkg)
}

// ReleasePackage starts the train.
//
// It dispatches only the FIRST wave — members whose deploy dependencies are
// already satisfied — and leaves the package in `releasing`. Later waves go out
// of AdvancePackage as earlier members land. Attempting the whole train here
// would mean releasing a dependent task while its dependency's deploy is still
// running, which deployDependencyGate would (correctly) refuse.
func (s *Service) ReleasePackage(ctx context.Context, repositoryID, packageID uuid.UUID) (domain.DeployPackage, error) {
	if s.deployPackages == nil {
		return domain.DeployPackage{}, fmt.Errorf("deploy package store unavailable")
	}
	pkg, err := s.deployPackages.Get(ctx, repositoryID, packageID)
	if err != nil {
		return domain.DeployPackage{}, err
	}
	// draft is the ordinary start; failed is a retry after the operator fixed
	// whatever the note named. releasing would double-dispatch, and
	// released/cancelled are terminal.
	if pkg.Status != domain.DeployPackageStatusDraft && pkg.Status != domain.DeployPackageStatusFailed {
		return domain.DeployPackage{}, fmt.Errorf("a %s deploy package cannot be released", pkg.Status)
	}
	members, err := s.deployPackages.ListTasks(ctx, packageID)
	if err != nil {
		return domain.DeployPackage{}, err
	}
	if len(members) == 0 {
		return domain.DeployPackage{}, fmt.Errorf("this deploy package has no tasks")
	}
	// Ordering is checked before anything is dispatched: a cycle has no valid
	// release order, and discovering that half-way through would leave the
	// train part-shipped with no way to finish it.
	if _, err := s.orderMembers(ctx, members); err != nil {
		note := err.Error()
		return s.failPackage(ctx, repositoryID, packageID, note)
	}

	updated, err := s.deployPackages.Update(ctx, repositoryID, packageID,
		nil, nil, ptr(domain.DeployPackageStatusReleasing), ptr(""))
	if err != nil {
		return domain.DeployPackage{}, err
	}
	return s.AdvancePackage(ctx, repositoryID, updated)
}

// AdvancePackage is the single place a package's state moves after its initial
// dispatch. Called on every read and by ReleasePackage itself, it:
//
//  1. reads each member's production evidence,
//  2. marks the package released when all of them have it,
//  3. dispatches any member whose dependencies just became satisfied and which
//     does not already have a deploy in flight,
//  4. fails the package (with the offending task key and the reason) on the
//     first hard refusal.
//
// It is idempotent by construction: dispatch is skipped for a member with a
// pending/running/successful deploy pipeline, so calling it twice in a row
// cannot double-deploy anything.
//
// A non-releasing package is only enriched, never advanced: a draft has not
// been started, and released/failed/cancelled are the operator's to move.
func (s *Service) AdvancePackage(ctx context.Context, repositoryID uuid.UUID, pkg domain.DeployPackage) (domain.DeployPackage, error) {
	if s.deployPackages == nil {
		return pkg, fmt.Errorf("deploy package store unavailable")
	}
	members, err := s.deployPackages.ListTasks(ctx, pkg.ID)
	if err != nil {
		return pkg, err
	}
	released, err := s.markReleasedMembers(ctx, repositoryID, members)
	if err != nil {
		return pkg, err
	}
	pkg.Tasks = members

	if pkg.Status != domain.DeployPackageStatusReleasing {
		return pkg, nil
	}
	if len(members) == 0 {
		return pkg, nil
	}
	if len(released) == len(members) {
		done, uerr := s.deployPackages.Update(ctx, repositoryID, pkg.ID,
			nil, nil, ptr(domain.DeployPackageStatusReleased), ptr(""))
		if uerr != nil {
			return pkg, uerr
		}
		done.Tasks = members
		return done, nil
	}

	ordered, err := s.orderMembers(ctx, members)
	if err != nil {
		failed, ferr := s.failPackage(ctx, repositoryID, pkg.ID, err.Error())
		if ferr != nil {
			return pkg, ferr
		}
		failed.Tasks = members
		return failed, nil
	}

	for _, member := range ordered {
		if released[member.TaskID] {
			continue
		}
		// A member still waiting on an in-package dependency is not skipped —
		// it is simply not this wave. Waves after the first go out of a later
		// AdvancePackage call, once the dependency has landed.
		ready, rerr := s.memberDependenciesSatisfied(ctx, repositoryID, member, released)
		if rerr != nil {
			return pkg, rerr
		}
		if !ready {
			continue
		}
		inFlight, ierr := s.hasDeployInFlight(ctx, member.TaskID)
		if ierr != nil {
			return pkg, ierr
		}
		if inFlight {
			continue
		}
		if _, derr := s.triggerRelease(ctx, repositoryID, member.TaskID, releaseOptions{FromPackage: true}); derr != nil {
			note := fmt.Sprintf("%s: %v", memberLabel(member), derr)
			failed, ferr := s.failPackage(ctx, repositoryID, pkg.ID, note)
			if ferr != nil {
				return pkg, ferr
			}
			failed.Tasks = members
			return failed, nil
		}
	}
	return pkg, nil
}

// markReleasedMembers stamps each member with its production evidence and
// returns the set that has it. Mutates members in place so the caller's slice
// is the enriched one the API returns.
func (s *Service) markReleasedMembers(ctx context.Context, repositoryID uuid.UUID, members []domain.DeployPackageTask) (map[uuid.UUID]bool, error) {
	released := make(map[uuid.UUID]bool, len(members))
	for i := range members {
		task, err := s.tasks.Get(ctx, repositoryID, members[i].TaskID)
		if err != nil {
			return nil, fmt.Errorf("read package member %s: %w", memberLabel(members[i]), err)
		}
		live, lerr := s.taskIsLive(ctx, repositoryID, task)
		if lerr != nil {
			return nil, fmt.Errorf("read deploy history of %s: %w", memberLabel(members[i]), lerr)
		}
		members[i].Released = live
		members[i].Column = task.Column
		if live {
			released[members[i].TaskID] = true
		}
	}
	return released, nil
}

// memberDependenciesSatisfied reports whether this member may be dispatched
// now. In-package dependencies must have landed (the released set); dependencies
// outside the package are left to deployDependencyGate, which is the authority
// on them and posts its own explanation on the task.
func (s *Service) memberDependenciesSatisfied(ctx context.Context, repositoryID uuid.UUID, member domain.DeployPackageTask, released map[uuid.UUID]bool) (bool, error) {
	if s.relations == nil {
		return true, nil
	}
	rels, err := s.relations.ListBySource(ctx, member.TaskID)
	if err != nil {
		return false, err
	}
	for _, rel := range rels {
		if rel.RelationType != domain.TaskRelationDeployDependsOn {
			continue
		}
		if released[rel.TargetTaskID] {
			continue
		}
		// Not a member of this package (or a member that has not landed):
		// either way, only "already live" clears it.
		target, terr := s.tasks.Get(ctx, repositoryID, rel.TargetTaskID)
		if terr != nil {
			return false, nil
		}
		live, lerr := s.taskIsLive(ctx, repositoryID, target)
		if lerr != nil {
			return false, lerr
		}
		if !live {
			return false, nil
		}
	}
	return true, nil
}

// hasDeployInFlight is the idempotency guard: a member whose prod/preprod
// pipeline is pending or running has already been dispatched by an earlier
// AdvancePackage call, and dispatching again would deploy the same task twice.
func (s *Service) hasDeployInFlight(ctx context.Context, taskID uuid.UUID) (bool, error) {
	if s.pipelineStore == nil {
		return false, nil
	}
	runs, err := s.pipelineStore.ListByTask(ctx, taskID)
	if err != nil {
		return false, err
	}
	for _, run := range runs {
		if run.Trigger != domain.PipelineTriggerProdDeploy && run.Trigger != domain.PipelineTriggerPreProdDeploy {
			continue
		}
		if run.Status == domain.PipelineStatusPending || run.Status == domain.PipelineStatusRunning {
			return true, nil
		}
	}
	return false, nil
}

// failPackage records which member stopped the train and why. A failed package
// with no note is indistinguishable from a stuck one, so the note is not
// optional.
func (s *Service) failPackage(ctx context.Context, repositoryID, packageID uuid.UUID, note string) (domain.DeployPackage, error) {
	log.Warn().Str("package_id", packageID.String()).Str("note", note).Msg("deploy package failed")
	return s.deployPackages.Update(ctx, repositoryID, packageID,
		nil, nil, ptr(domain.DeployPackageStatusFailed), &note)
}

// orderMembers topologically sorts the package's tasks so a dependency is
// always released before whatever depends on it.
//
// Only in-package edges are considered: a dependency outside the package is not
// something this ordering can influence, and deployDependencyGate refuses the
// dispatch anyway if it has not shipped. Ties are broken by the human's
// declared position, so a package with no dependencies at all releases in
// exactly the order it was assembled.
//
// A cycle is a hard error. There is no order that satisfies it, and picking an
// arbitrary one would dispatch a task whose own dependency gate would refuse it.
func (s *Service) orderMembers(ctx context.Context, members []domain.DeployPackageTask) ([]domain.DeployPackageTask, error) {
	inPackage := make(map[uuid.UUID]bool, len(members))
	for _, m := range members {
		inPackage[m.TaskID] = true
	}
	// deps[task] = the in-package tasks it must deploy after.
	deps := make(map[uuid.UUID]map[uuid.UUID]bool, len(members))
	for _, m := range members {
		deps[m.TaskID] = map[uuid.UUID]bool{}
	}
	if s.relations != nil {
		for _, m := range members {
			rels, err := s.relations.ListBySource(ctx, m.TaskID)
			if err != nil {
				return nil, err
			}
			for _, rel := range rels {
				if rel.RelationType != domain.TaskRelationDeployDependsOn {
					continue
				}
				if !inPackage[rel.TargetTaskID] || rel.TargetTaskID == m.TaskID {
					continue
				}
				deps[m.TaskID][rel.TargetTaskID] = true
			}
		}
	}

	// Kahn's algorithm, taking the lowest-position ready task each round so the
	// output is deterministic and matches the assembled order where the
	// dependencies allow it.
	pending := make([]domain.DeployPackageTask, len(members))
	copy(pending, members)
	sort.SliceStable(pending, func(i, j int) bool { return pending[i].Position < pending[j].Position })

	done := make(map[uuid.UUID]bool, len(members))
	ordered := make([]domain.DeployPackageTask, 0, len(members))
	for len(ordered) < len(pending) {
		progressed := false
		for _, m := range pending {
			if done[m.TaskID] {
				continue
			}
			ready := true
			for dep := range deps[m.TaskID] {
				if !done[dep] {
					ready = false
					break
				}
			}
			if !ready {
				continue
			}
			done[m.TaskID] = true
			ordered = append(ordered, m)
			progressed = true
			break
		}
		if !progressed {
			var stuck []string
			for _, m := range pending {
				if !done[m.TaskID] {
					stuck = append(stuck, memberLabel(m))
				}
			}
			return nil, fmt.Errorf("%w: %s", domain.ErrDeployPackageCycle, strings.Join(stuck, " ↔ "))
		}
	}
	return ordered, nil
}

// memberLabel names a member the way the board does, falling back to the id.
func memberLabel(m domain.DeployPackageTask) string {
	if strings.TrimSpace(m.Key) != "" {
		return m.Key
	}
	return m.TaskID.String()
}

// ptr is the one-line "give me an optional field" helper the store's partial
// Update needs at half a dozen call sites.
func ptr[T any](v T) *T { return &v }

// IsDeployPackageNotFound reports whether an error is a missing package, so the
// HTTP layer can answer 404 instead of 400.
func IsDeployPackageNotFound(err error) bool {
	return errors.Is(err, port.ErrNotFound)
}
