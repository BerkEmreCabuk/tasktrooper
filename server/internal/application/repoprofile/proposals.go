package repoprofile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/repofacts"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func (s *Service) syncProposals(ctx context.Context, repo domain.Repository, facts repofacts.Facts) {
	if s.profiles == nil {
		return
	}
	proposals := repofacts.Proposals(facts)
	if len(proposals) == 0 {
		return
	}

	pending := make([]domain.ProfileProposal, 0, len(proposals))
	for _, p := range proposals {
		p.RepositoryID = repo.ID
		p.Slot = slotOf(p)
		current, err := s.currentValue(ctx, repo, p)
		if err != nil {
			log.Debug().Err(err).Str("field", p.Field).Msg("profile proposal: could not read current setting")
			continue
		}
		p.Current = current

		proposedText := valueText(p)
		switch {
		case proposedText == current:
			continue
		case current == "":
			if err := s.applyProposalValue(ctx, repo, p); err != nil {
				log.Warn().Err(err).Str("field", p.Field).Str("repository", repo.Name).Msg("profile proposal auto-apply failed")
				p.Status = domain.ProposalPending
				pending = append(pending, p)
				continue
			}
			now := facts.CollectedAt
			p.Status = domain.ProposalApplied
			p.AppliedAt = &now
			log.Info().Str("repository", repo.Name).Str("field", p.Field).Str("value", proposedText).
				Msg("profile proposal applied to an empty setting")
			pending = append(pending, p)
		default:
			p.Status = domain.ProposalPending
			pending = append(pending, p)
		}
	}

	if err := s.profiles.ReplaceProposals(ctx, repo.ID, pending); err != nil {
		log.Warn().Err(err).Str("repository", repo.Name).Msg("storing profile proposals failed")
	}
}

func slotOf(p domain.ProfileProposal) string {
	if p.Field != domain.ProposalFieldPipelineJob {
		return ""
	}
	var job domain.PipelineJobProposal
	if err := json.Unmarshal(p.Value, &job); err != nil {
		return ""
	}
	return job.Category + ":" + job.SubRepoKind
}

func valueText(p domain.ProfileProposal) string {
	switch p.Field {
	case domain.ProposalFieldSubRepoKinds:
		var kinds []string
		if err := json.Unmarshal(p.Value, &kinds); err != nil {
			return ""
		}
		return strings.Join(kinds, ",")
	case domain.ProposalFieldPipelineJob:
		var job domain.PipelineJobProposal
		if err := json.Unmarshal(p.Value, &job); err != nil {
			return ""
		}
		return job.TargetRef
	default:
		var v string
		if err := json.Unmarshal(p.Value, &v); err != nil {
			return ""
		}
		return v
	}
}

func (s *Service) currentValue(ctx context.Context, repo domain.Repository, p domain.ProfileProposal) (string, error) {
	switch p.Field {
	case domain.ProposalFieldRepoKind:
		return repo.Kind, nil
	case domain.ProposalFieldSubRepoKinds:
		return strings.Join(repo.SubRepoKinds, ","), nil
	case domain.ProposalFieldBuildCommand:
		return repo.BuildCommand, nil
	case domain.ProposalFieldTestCommand:
		return repo.TestCommand, nil
	case domain.ProposalFieldVerifyCommand:
		return repo.VerifyCommand, nil
	case domain.ProposalFieldPipelineJob:
		if s.pipelines == nil {
			return "", errors.New("pipeline settings are not wired")
		}
		var job domain.PipelineJobProposal
		if err := json.Unmarshal(p.Value, &job); err != nil {
			return "", err
		}
		jobs, err := s.pipelines.ListByRepository(ctx, repo.ID)
		if err != nil {
			return "", err
		}
		for _, j := range jobs {
			if j.Category == job.Category && j.SubRepoKind == job.SubRepoKind {
				return j.TargetRef, nil
			}
		}
		return "", nil
	}
	return "", fmt.Errorf("unknown proposal field %q", p.Field)
}

func (s *Service) applyProposalValue(ctx context.Context, repo domain.Repository, p domain.ProfileProposal) error {
	switch p.Field {
	case domain.ProposalFieldRepoKind:
		kind := valueText(p)
		if !domain.ValidRepoKind(kind) {
			return fmt.Errorf("invalid repository kind %q", kind)
		}
		_, err := s.repos.UpdateMeta(ctx, repo.ID, &kind, nil, nil)
		return err

	case domain.ProposalFieldSubRepoKinds:
		var kinds []string
		if err := json.Unmarshal(p.Value, &kinds); err != nil {
			return err
		}
		for _, k := range kinds {
			if !domain.ValidSubRepoKind(k) {
				return fmt.Errorf("invalid sub-repo kind %q", k)
			}
		}
		_, err := s.repos.UpdateMeta(ctx, repo.ID, nil, &kinds, nil)
		return err

	case domain.ProposalFieldBuildCommand:
		cmd := valueText(p)
		_, err := s.repos.Update(ctx, repo.ID, repo.Name, repo.Description, nil, &cmd, nil, nil)
		return err

	case domain.ProposalFieldTestCommand:
		cmd := valueText(p)
		_, err := s.repos.Update(ctx, repo.ID, repo.Name, repo.Description, nil, nil, &cmd, nil)
		return err

	case domain.ProposalFieldVerifyCommand:
		cmd := valueText(p)
		_, err := s.repos.Update(ctx, repo.ID, repo.Name, repo.Description, &cmd, nil, nil, nil)
		return err

	case domain.ProposalFieldPipelineJob:
		return s.applyPipelineJob(ctx, repo, p)
	}
	return fmt.Errorf("unknown proposal field %q", p.Field)
}

func (s *Service) applyPipelineJob(ctx context.Context, repo domain.Repository, p domain.ProfileProposal) error {
	if s.pipelines == nil {
		return errors.New("pipeline settings are not wired")
	}
	var job domain.PipelineJobProposal
	if err := json.Unmarshal(p.Value, &job); err != nil {
		return err
	}
	existing, err := s.pipelines.ListByRepository(ctx, repo.ID)
	if err != nil {
		return err
	}
	merged := make([]domain.RepositoryPipelineJob, 0, len(existing)+1)
	replaced := false
	for _, j := range existing {
		if j.Category == job.Category && j.SubRepoKind == job.SubRepoKind {
			j.TargetKind = job.TargetKind
			j.TargetRef = job.TargetRef
			replaced = true
		}
		merged = append(merged, j)
	}
	if !replaced {
		merged = append(merged, domain.RepositoryPipelineJob{
			RepositoryID: repo.ID,
			SubRepoKind:  job.SubRepoKind,
			Category:     job.Category,
			TargetKind:   job.TargetKind,
			TargetRef:    job.TargetRef,
		})
	}
	_, err = s.pipelines.ReplaceForRepository(ctx, repo.ID, merged)
	return err
}

func (s *Service) ApplyProposal(ctx context.Context, repositoryID, proposalID uuid.UUID) (domain.ProfileProposal, error) {
	if s.profiles == nil {
		return domain.ProfileProposal{}, errors.New("profile proposals are not available on this deployment")
	}
	p, err := s.profiles.GetProposal(ctx, proposalID)
	if err != nil {
		return domain.ProfileProposal{}, err
	}
	if p.RepositoryID != repositoryID {
		return domain.ProfileProposal{}, errors.New("proposal belongs to a different repository")
	}
	repo, err := s.repos.Get(ctx, repositoryID)
	if err != nil {
		return domain.ProfileProposal{}, err
	}
	if err := s.applyProposalValue(ctx, repo, p); err != nil {
		return domain.ProfileProposal{}, err
	}
	return s.profiles.SetProposalStatus(ctx, proposalID, domain.ProposalApplied)
}

func (s *Service) DismissProposal(ctx context.Context, repositoryID, proposalID uuid.UUID) (domain.ProfileProposal, error) {
	if s.profiles == nil {
		return domain.ProfileProposal{}, errors.New("profile proposals are not available on this deployment")
	}
	p, err := s.profiles.GetProposal(ctx, proposalID)
	if err != nil {
		return domain.ProfileProposal{}, err
	}
	if p.RepositoryID != repositoryID {
		return domain.ProfileProposal{}, errors.New("proposal belongs to a different repository")
	}
	return s.profiles.SetProposalStatus(ctx, proposalID, domain.ProposalDismissed)
}
