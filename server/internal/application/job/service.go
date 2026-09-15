package job

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/agent"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/urlguard"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
)

// callbackTimeout budgets one callback POST end to end. There was no client
// timeout at all before — http.DefaultClient has none — so a callback host that
// accepted the connection and then said nothing held a goroutine forever, and
// every completed job leaked another one.
const callbackTimeout = 30 * time.Second

type Service struct {
	store         port.JobStore
	agentLoop     *agent.Loop
	rag           RAGInjector
	maxWorkers    int
	timeout       time.Duration
	defaultPolicy domain.ToolPolicy
	// urlPolicy vets callback_url. It comes straight off the request body and is
	// POSTed to with the job's result, so it is an attacker-chosen destination
	// with a body attached — and loopback, which is what makes it interesting,
	// is invisible to the cluster NetworkPolicy.
	urlPolicy urlguard.Policy
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

type RAGInjector interface {
	InjectContext(ctx context.Context, messages []domain.Message, fileIDs []string) ([]domain.Message, error)
}

func NewService(store port.JobStore, agentLoop *agent.Loop, rag RAGInjector, maxWorkers int, timeout time.Duration, defaultPolicy domain.ToolPolicy) *Service {
	return &Service{
		store:         store,
		agentLoop:     agentLoop,
		rag:           rag,
		maxWorkers:    maxWorkers,
		timeout:       timeout,
		defaultPolicy: defaultPolicy,
		urlPolicy:     urlguard.Default(),
	}
}

// SetURLPolicy overrides what counts as an acceptable callback_url. Unset means
// urlguard.Default: the public internet, plus loopback when a self-hosted
// operator has set ALLOW_LOOPBACK_TOOL_URLS.
func (s *Service) SetURLPolicy(p urlguard.Policy) { s.urlPolicy = p }

func (s *Service) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	for i := 0; i < s.maxWorkers; i++ {
		s.wg.Add(1)
		go s.worker(ctx)
	}
}

func (s *Service) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

func (s *Service) Create(ctx context.Context, req domain.JobRequest, policy domain.ToolPolicy) (domain.Job, error) {
	// Reject a callback pointing at ourselves or at the cluster before the job
	// is queued, so the caller finds out now rather than by watching what the
	// worker does minutes later. Offline only — the resolving check happens in
	// fireCallback, which is where the connection is actually made and the only
	// place a rebind can be caught.
	req.CallbackURL = strings.TrimSpace(req.CallbackURL)
	if req.CallbackURL != "" {
		if _, err := s.urlPolicy.Precheck(req.CallbackURL); err != nil {
			log.Warn().Err(err).Msg("refused a job callback_url")
			return domain.Job{}, fmt.Errorf("callback_url is not an allowed destination")
		}
	}

	merged := domain.MergeToolPolicy(s.defaultPolicy, policy)
	merged = domain.MergeToolPolicy(merged, req.ToolPolicy)

	payload := struct {
		domain.JobRequest
		ToolPolicy domain.ToolPolicy `json:"tool_policy"`
	}{
		JobRequest: req,
		ToolPolicy: merged,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return domain.Job{}, fmt.Errorf("marshal job request: %w", err)
	}
	return s.store.Create(ctx, raw, req.CallbackURL)
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (domain.Job, error) {
	return s.store.Get(ctx, id)
}

func (s *Service) List(ctx context.Context, status string, limit int) ([]domain.Job, error) {
	return s.store.List(ctx, status, limit)
}

func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	job, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if job.Status == domain.JobStatusPending || job.Status == domain.JobStatusRunning {
		return s.store.UpdateStatus(ctx, id, domain.JobStatusCancelled, nil, "cancelled by user")
	}
	return s.store.Delete(ctx, id)
}

func (s *Service) worker(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.processOne(ctx)
		}
	}
}

func (s *Service) processOne(ctx context.Context) {
	job, err := s.store.ClaimPending(ctx)
	if err != nil {
		log.Error().Err(err).Msg("claim job failed")
		return
	}
	if job == nil {
		return
	}

	runCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	var payload struct {
		domain.JobRequest
		ToolPolicy domain.ToolPolicy `json:"tool_policy"`
	}
	if err := json.Unmarshal(job.Request, &payload); err != nil {
		_ = s.store.UpdateStatus(ctx, job.ID, domain.JobStatusFailed, nil, err.Error())
		return
	}

	messages := payload.Messages
	if s.rag != nil && len(payload.FileIDs) > 0 {
		messages, err = s.rag.InjectContext(runCtx, messages, payload.FileIDs)
		if err != nil {
			_ = s.store.UpdateStatus(ctx, job.ID, domain.JobStatusFailed, nil, err.Error())
			return
		}
	}

	resp, err := s.agentLoop.Run(runCtx, messages, payload.Model, "", payload.ToolPolicy)
	if err != nil {
		_ = s.store.UpdateStatus(ctx, job.ID, domain.JobStatusFailed, nil, err.Error())
		s.fireCallback(ctx, job.CallbackURL, job.ID, domain.JobStatusFailed, nil, err.Error())
		return
	}

	result := domain.JobResult{Message: resp.Message, Usage: resp.Usage}
	raw, _ := json.Marshal(result)
	_ = s.store.UpdateStatus(ctx, job.ID, domain.JobStatusCompleted, raw, "")
	s.fireCallback(ctx, job.CallbackURL, job.ID, domain.JobStatusCompleted, raw, "")
}

// ctx is carried for its VALUES, not its lifetime: the callback outlives the
// job whose result it reports. Nothing here reads a tenant row today, and that
// is exactly why it is worth carrying — the next thing added to this path would
// otherwise inherit a context with no identity and fail silently.
func (s *Service) fireCallback(ctx context.Context, url string, jobID uuid.UUID, status domain.JobStatus, result []byte, errMsg string) {
	if url == "" {
		return
	}
	payload := map[string]interface{}{
		"job_id": jobID.String(),
		"status": status,
	}
	if len(result) > 0 {
		payload["result"] = json.RawMessage(result)
	}
	if errMsg != "" {
		payload["error"] = errMsg
	}
	body, _ := json.Marshal(payload)
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), callbackTimeout)
		defer cancel()

		// Resolved and checked here, not just at Create: the row was written
		// minutes ago and the name is free to answer something else now.
		target, err := s.urlPolicy.Validate(ctx, url)
		if err != nil {
			log.Warn().Err(err).Str("url", urlguard.LogRaw(url)).Msg("job callback destination refused")
			return
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.URL.String(), bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")

		client := s.urlPolicy.ClientFor(target, callbackTimeout)
		// A callback is a delivery, not a fetch: there is nothing to read at the
		// other end of a redirect, and following one only widens where the job
		// result — which contains the model's output — can be posted. Take the
		// 3xx as the answer and stop.
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

		resp, err := client.Do(req)
		if err != nil {
			// LogValue, not the raw URL: a callback URL routinely carries the
			// caller's own signature or token in its query string, and this log
			// line used to write the whole thing out on every failure.
			log.Warn().Err(err).Str("url", urlguard.LogValue(target.URL)).Msg("job callback failed")
			return
		}
		resp.Body.Close()
	}()
}
