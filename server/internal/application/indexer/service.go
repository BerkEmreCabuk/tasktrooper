package indexer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/makifbaysal/tasktrooper/server/internal/application/chunker"
	"github.com/makifbaysal/tasktrooper/server/internal/application/graph"
	"github.com/makifbaysal/tasktrooper/server/internal/application/mapper"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

const (
	perChunkEmbedTimeout = 90 * time.Second
	// defaultEmbedRetryWait mirrors the LLM client's cap on a single retry wait,
	// used when the config leaves it unset.
	defaultEmbedRetryWait = 60 * time.Second
)

const (
	// defaultEmbedConcurrency bounds embedding calls across every index job of
	// the service. File workers still read and parse in parallel and only wait
	// here for a slot. It protects a local CPU model: two repositories indexed
	// at once used to send eight requests into one embedder that answers them
	// all slower, not more of them.
	defaultEmbedConcurrency = 2
	// maxTransientEmbedAttempts is how often one chunk is tried when the
	// embedder was briefly unreachable (a reset or refused connection, a 502,
	// 503 or 504), and transientEmbedBackoff the first wait between tries.
	maxTransientEmbedAttempts = 4
	transientEmbedBackoff     = time.Second
	// maxConsecutiveEmbedFailures is how many chunks in a row may fail before
	// the pass gives up on an embedder that is evidently not coming back.
	maxConsecutiveEmbedFailures = 10
)

type Service struct {
	store          port.IndexStore
	llm            port.LLMClient
	mapper         *mapper.Service
	chunkers       *chunker.Registry
	cfg            domain.IndexerConfig
	graphCfg       domain.GraphConfig
	embeddingModel string
	// embedBudget is the deadline for embedding one chunk. It covers the
	// throttling and retry waits the LLM client may add, otherwise a retry of a
	// rate-limited call would be cut off by the deadline that was sized for a
	// single request.
	embedBudget time.Duration
	// embedSlots is the shared limit on concurrent embedding calls; nil means
	// unlimited, which is what a Service built by hand in a test gets.
	embedSlots chan struct{}
	projectMu  sync.Mutex
	// projectRunning maps a project to the cancel func of its running pass, so
	// an operator can stop an index that is chewing through a huge repository
	// instead of waiting it out. A stopped pass keeps every file it already
	// finished — persistFile stamps each one — so stopping costs the remaining
	// files, not the work already done.
	projectRunning map[uuid.UUID]context.CancelFunc
	branchRunning  map[string]context.CancelFunc

	// mirrors restores the repository clone this process indexes FROM, before
	// a pass walks it. Optional: nil is a deployment whose working copies are
	// not a cache (a desktop build indexing the folder the human opened), where
	// there is nothing to restore and a missing folder is already an error from
	// the walk. See MirrorRestorer.
	mirrors MirrorRestorer
	// embeddings answers what this install's embed calls resolve to right now,
	// which is what a pass STAMPS onto the index it builds and what makes a
	// model change detectable afterwards. Optional; without it a pass records
	// the model it was configured with (s.embeddingModel), which is the same
	// answer wherever "auto" is not in play.
	embeddings port.EmbeddingProvenanceResolver
}

// MirrorRestorer puts a repository's code back on this process's filesystem
// before an index pass reads it.
//
// It exists because in the cloud the checkout an index pass walks is a CACHE on
// a pod disk, not a durable working copy: the index rows live in Postgres and
// are shared by every replica, but the tree they were built from belongs to
// whichever pod happened to serve the import. A restart, a rescheduled replica
// or simply a pass that lands on a different replica finds nothing there — and
// an index pass over an empty directory does not fail, it completes, reports
// "completed 100%" and leaves a repository that answers every code search with
// silence.
//
// Implemented by repository.Service (ensureIndexMirror), which owns the record
// that says where the code came from.
type MirrorRestorer interface {
	// EnsureIndexMirror returns nil only when rootPath now holds a usable git
	// working copy of this repository. Any other outcome is an error naming
	// what is missing, and the pass fails on it rather than indexing whatever
	// happens to be there.
	EnsureIndexMirror(ctx context.Context, repositoryID uuid.UUID, rootPath string) error
}

// SetMirrorRestorer wires the restore step. Wired late (platform/runtime) for
// the usual reason: the thing that can restore a repository is the repository
// service, which is built after the indexer it is being handed to.
func (s *Service) SetMirrorRestorer(m MirrorRestorer) {
	if s == nil {
		return
	}
	s.mirrors = m
}

// SetEmbeddingResolver tells this service — and, through it, its store — what
// the embedding calls resolve to.
//
// The forward to the store is the point of doing it here. Three layers compare
// against "the configured model" for three different jobs (this one stamps a
// finished pass and decides whether a pass must re-embed everything; the store
// refuses a search; the status endpoint warns a person), and they must reach
// the SAME answer — a card that says an index is unusable while search happily
// serves it, or the reverse, is worse than either behaviour on its own. Wiring
// them from one place is what makes that hold.
func (s *Service) SetEmbeddingResolver(r port.EmbeddingProvenanceResolver) {
	if s == nil {
		return
	}
	s.setEmbeddingResolver(fallbackEmbeddingResolver{inner: r, model: s.embeddingModel})
}

func (s *Service) setEmbeddingResolver(r port.EmbeddingProvenanceResolver) {
	s.embeddings = r
	if aware, ok := s.store.(embeddingResolverAware); ok {
		aware.SetEmbeddingResolver(r)
	}
}

// embeddingResolverAware is the optional half of port.IndexStore: a store that
// can answer "is this index still comparable" needs the same resolver. The
// in-memory and test stores do not implement it and are unaffected.
type embeddingResolverAware interface {
	SetEmbeddingResolver(r port.EmbeddingProvenanceResolver)
}

// fallbackEmbeddingResolver is the one definition of "the model this index
// would be compared against".
//
// A wired resolver wins when it names a model, because that is what the
// embed calls will actually reach — including whatever "auto" currently means.
// It answers blank on a deployment with no live embedding setting to read,
// and there the model this service was CONSTRUCTED with is the honest answer:
// an operator who changes it in config.yml has changed what every future query
// is embedded by, and the indexes built before that change are exactly as stale
// as they would be after an operator switched providers.
//
// An error is passed through rather than swallowed into the fallback. It means
// "the setting could not be read just now", which is not evidence of anything,
// and each caller decides what to do with that — none of them treat it as a
// mismatch.
type fallbackEmbeddingResolver struct {
	inner port.EmbeddingProvenanceResolver
	model string
}

func (r fallbackEmbeddingResolver) ResolvedEmbedding(ctx context.Context) (string, int, error) {
	if r.inner != nil {
		model, dims, err := r.inner.ResolvedEmbedding(ctx)
		if err != nil {
			return "", 0, err
		}
		if model != "" {
			return model, dims, nil
		}
	}
	// The constructed model's dimension is not knowable from a name, so 0:
	// domain.EmbeddingProvenanceStale reads that as "unknown size" and compares
	// on the name alone, which is the only fact available here.
	return r.model, 0, nil
}

func NewService(
	store port.IndexStore,
	llm port.LLMClient,
	mapperSvc *mapper.Service,
	chunkers *chunker.Registry,
	cfg domain.IndexerConfig,
	graphCfg domain.GraphConfig,
	embeddingModel string,
) *Service {
	svc := &Service{
		store:          store,
		llm:            llm,
		mapper:         mapperSvc,
		chunkers:       chunkers,
		cfg:            cfg,
		graphCfg:       graphCfg,
		embeddingModel: embeddingModel,
		embedBudget:    perChunkEmbedTimeout,
		embedSlots:     make(chan struct{}, embedConcurrency(cfg)),
		projectRunning: make(map[uuid.UUID]context.CancelFunc),
	}
	// Installed here, not only in SetEmbeddingResolver, so a deployment that
	// never wires a live resolver still has ONE answer to "what would a
	// query be embedded by" — the configured model — shared by the pass, the
	// store's search guard and the status warning. Without it those three would
	// disagree on exactly the deployments least able to notice.
	svc.setEmbeddingResolver(fallbackEmbeddingResolver{model: embeddingModel})
	return svc
}

// SetEmbeddingLimits widens the per-chunk deadline to cover the retry waits the
// LLM client performs when the embedding provider rate-limits us.
func (s *Service) SetEmbeddingLimits(cfg domain.EmbeddingConfig) {
	request := cfg.RequestTimeout
	if request <= 0 {
		request = perChunkEmbedTimeout
	}
	retries := cfg.MaxRetries
	if retries < 0 {
		retries = 0
	}
	wait := cfg.MaxRetryWait
	if wait <= 0 {
		wait = defaultEmbedRetryWait
	}
	s.embedBudget = request*time.Duration(retries+1) + wait*time.Duration(retries)
}

func (s *Service) GetProjectStatus(ctx context.Context, projectID uuid.UUID) (domain.WorkspaceIndex, error) {
	return s.store.GetIndexByProject(ctx, projectID)
}

func (s *Service) SearchProject(ctx context.Context, projectID uuid.UUID, query string, topK int) ([]domain.WorkspaceChunk, error) {
	idx, err := s.store.GetIndexByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("index not found")
	}
	if idx.Status != domain.IndexStatusCompleted {
		return nil, fmt.Errorf("index not ready: %s", idx.Status)
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}
	if topK <= 0 {
		topK = s.cfg.TopK
	}
	if topK <= 0 {
		topK = 10
	}
	if topK > 50 {
		topK = 50
	}
	embedding, err := s.llm.Embed(ctx, query, s.embeddingModel)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	chunks, err := s.store.SearchChunksHybrid(ctx, idx.ID, query, embedding, topK)
	if err != nil {
		return nil, fmt.Errorf("search chunks: %w", err)
	}
	for i := range chunks {
		chunks[i].Embedding = nil
	}
	return chunks, nil
}

func (s *Service) IsProjectIndexActive(projectID uuid.UUID) bool {
	s.projectMu.Lock()
	defer s.projectMu.Unlock()
	_, ok := s.projectRunning[projectID]
	return ok
}

// StopIndexProject cancels the running pass of one project (and of any branch
// index of it) and reports whether anything was actually stopped.
//
// Stopping is cheap and safe now that each file is persisted as it finishes:
// the index keeps every file already done, its hashes say so, and the next pass
// picks up exactly where this one was interrupted. Before that it would have
// meant throwing the whole run away, which is why the only option used to be
// waiting for a repository-sized job to end.
func (s *Service) StopIndexProject(projectID uuid.UUID) bool {
	if s == nil {
		return false
	}
	s.projectMu.Lock()
	defer s.projectMu.Unlock()
	stopped := false
	if cancel, ok := s.projectRunning[projectID]; ok {
		cancel()
		stopped = true
	}
	prefix := projectID.String() + "|"
	for key, cancel := range s.branchRunning {
		if strings.HasPrefix(key, prefix) {
			cancel()
			stopped = true
		}
	}
	return stopped
}

// RestartIndexProject forces a full re-index: it bypasses the change-detection
// short-circuit so an explicit "reindex" always re-processes and re-embeds even
// when no source files changed (e.g. to recover from a prior embedding failure).
func (s *Service) RestartIndexProject(ctx context.Context, projectID uuid.UUID, rootPath string, onDone func()) {
	s.projectMu.Lock()
	delete(s.projectRunning, projectID)
	s.projectMu.Unlock()
	s.startIndexProject(ctx, projectID, rootPath, onDone, true)
}

func (s *Service) StartIndexProject(ctx context.Context, projectID uuid.UUID, rootPath string, onDone func()) {
	s.startIndexProject(ctx, projectID, rootPath, onDone, false)
}

// detachIndexContext is the context a background pass runs on.
//
// context.WithoutCancel, not context.Background: the pass outlives the request
// that started it (an HTTP handler that only kicks it off), but it must keep
// that request's values, including the identity the workspace path helpers
// still read.
func detachIndexContext(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	// Background priority: nobody is waiting on an index pass (it is started by
	// a webhook, a freshness check or a settings page nobody is watching), so
	// the provider limiter may put a person's chat request in front of the tens
	// of thousands of embedding calls this is about to make.
	return domain.WithBackgroundLLM(context.WithoutCancel(ctx))
}

func (s *Service) startIndexProject(ctx context.Context, projectID uuid.UUID, rootPath string, onDone func(), force bool) {
	if !s.cfg.Enabled {
		if onDone != nil {
			onDone()
		}
		return
	}
	s.projectMu.Lock()
	if _, running := s.projectRunning[projectID]; running {
		s.projectMu.Unlock()
		if onDone != nil {
			onDone()
		}
		return
	}
	passCtx, cancel := context.WithCancel(detachIndexContext(ctx))
	s.projectRunning[projectID] = cancel
	s.projectMu.Unlock()

	go func() {
		defer func() {
			cancel()
			s.projectMu.Lock()
			delete(s.projectRunning, projectID)
			s.projectMu.Unlock()
			if onDone != nil {
				onDone()
			}
		}()
		if _, err := s.indexProject(passCtx, projectID, rootPath, force); err != nil {
			log.Warn().Err(err).Str("project_id", projectID.String()).Msg("project index failed")
		}
	}()
}

func (s *Service) GetStatus(ctx context.Context, sessionID uuid.UUID) (domain.WorkspaceIndex, error) {
	return s.store.GetIndexBySession(ctx, sessionID)
}

func (s *Service) IndexSession(ctx context.Context, sessionID uuid.UUID, rootPath string) (domain.WorkspaceIndex, error) {
	if !s.cfg.Enabled {
		return domain.WorkspaceIndex{}, fmt.Errorf("indexer disabled")
	}

	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return domain.WorkspaceIndex{}, fmt.Errorf("resolve root path: %w", err)
	}

	paths, err := WalkIndexableFiles(absRoot)
	if err != nil {
		return domain.WorkspaceIndex{}, err
	}

	treeText, err := s.mapper.BuildTree(absRoot)
	if err != nil {
		return domain.WorkspaceIndex{}, fmt.Errorf("build tree: %w", err)
	}

	existing, existingErr := s.store.GetIndexBySession(ctx, sessionID)
	var index domain.WorkspaceIndex
	isNew := existingErr != nil
	if isNew {
		index, err = s.store.CreateIndex(ctx, sessionID, absRoot, treeText)
		if err != nil {
			return domain.WorkspaceIndex{}, fmt.Errorf("create index: %w", err)
		}
	} else {
		index = existing
		index.RootPath = absRoot
		index.TreeText = treeText
	}

	// Session indexes take the same pass as project and branch ones. They used
	// to have their own copy of it, which detected the changed files and then
	// re-indexed the whole tree anyway — the detection only ever decided
	// between "nothing to do" and "everything again".
	return s.runIndexPass(ctx, index, absRoot, paths, treeText, isNew, false)
}

func (s *Service) failIndex(ctx context.Context, indexID uuid.UUID, err error) {
	if indexID == uuid.Nil || err == nil {
		return
	}
	_ = s.store.UpdateIndexStatus(ctx, indexID, domain.IndexStatusFailed, 0, 0, 0, err.Error())
}

func (s *Service) indexProject(ctx context.Context, projectID uuid.UUID, rootPath string, force bool) (index domain.WorkspaceIndex, retErr error) {
	var indexID uuid.UUID
	defer func() {
		if retErr != nil {
			s.failIndex(ctx, indexID, retErr)
		}
	}()

	if !s.cfg.Enabled {
		return domain.WorkspaceIndex{}, fmt.Errorf("indexer disabled")
	}

	absRoot, err := filepath.Abs(rootPath)
	if err != nil {
		return domain.WorkspaceIndex{}, fmt.Errorf("resolve root path: %w", err)
	}

	// The existing row is resolved BEFORE anything that can fail, so that
	// whatever fails lands on the index card a person is looking at.
	//
	// It used to be read after the walk and the tree build, which meant every
	// failure before that point — a missing checkout above all — had no index
	// id to attach itself to: failIndex was called with uuid.Nil, wrote
	// nothing, and the only trace was one log line on a pod nobody was
	// tailing, while the settings page went on showing the last successful
	// pass.
	existing, existingErr := s.store.GetIndexByProject(ctx, projectID)
	if existingErr == nil {
		indexID = existing.ID
	}

	// The tree this pass is about to walk is a CACHE, not a working copy, and
	// this is where that is made true or the pass stops.
	//
	// In the cloud the index rows live in shared Postgres while the checkout
	// they were built from sits on one replica's pod disk. A restart, a
	// rescheduled pod, or a pass that lands on a replica which never served the
	// import all find root_path empty while the index rows look perfectly
	// healthy — and a walk over an empty directory does not fail. It returns no
	// files, embeds nothing, and finalizeIndex writes "completed": a silently
	// empty index, which is worse than a failed one, because nothing looks
	// wrong until an agent's code search comes back empty and the agent
	// concludes the code does not exist.
	//
	// So the answer is restore-or-fail, never index-nothing-and-succeed. Why
	// this server holds a clone at all, why the content comes from GitHub
	// rather than through the tunnel to the user's Mac, and which refusals
	// apply are all at the restore itself:
	// repository.Service.EnsureIndexMirror.
	if s.mirrors != nil {
		if err := s.mirrors.EnsureIndexMirror(ctx, projectID, absRoot); err != nil {
			return domain.WorkspaceIndex{}, fmt.Errorf("the code to index is not on this server and could not be restored, so nothing was indexed: %w", err)
		}
	}

	paths, err := WalkIndexableFiles(absRoot)
	if err != nil {
		return domain.WorkspaceIndex{}, err
	}

	treeText, err := s.mapper.BuildTree(absRoot)
	if err != nil {
		return domain.WorkspaceIndex{}, fmt.Errorf("build tree: %w", err)
	}

	isNew := existingErr != nil
	if isNew {
		index, err = s.store.CreateProjectIndex(ctx, projectID, absRoot, treeText)
		if err != nil {
			return domain.WorkspaceIndex{}, fmt.Errorf("create project index: %w", err)
		}
	} else {
		index = existing
		index.RootPath = absRoot
		index.TreeText = treeText
	}
	indexID = index.ID
	return s.runIndexPass(ctx, index, absRoot, paths, treeText, isNew, force)
}

// IndexBranch builds or refreshes the index of one task branch from its
// workspace clone. A brand-new branch index is seeded by copying the
// default-branch index, so only the branch's own diff is re-chunked and
// re-embedded instead of the whole repository.
func (s *Service) IndexBranch(ctx context.Context, projectID uuid.UUID, branch, workspacePath string) (index domain.WorkspaceIndex, retErr error) {
	var indexID uuid.UUID
	defer func() {
		if retErr != nil {
			s.failIndex(ctx, indexID, retErr)
		}
	}()

	if !s.cfg.Enabled {
		return domain.WorkspaceIndex{}, fmt.Errorf("indexer disabled")
	}
	if branch == "" {
		return domain.WorkspaceIndex{}, fmt.Errorf("branch is required")
	}

	absRoot, err := filepath.Abs(workspacePath)
	if err != nil {
		return domain.WorkspaceIndex{}, fmt.Errorf("resolve workspace path: %w", err)
	}
	paths, err := WalkIndexableFiles(absRoot)
	if err != nil {
		return domain.WorkspaceIndex{}, err
	}
	treeText, err := s.mapper.BuildTree(absRoot)
	if err != nil {
		return domain.WorkspaceIndex{}, fmt.Errorf("build tree: %w", err)
	}

	existing, existingErr := s.store.GetIndexByProjectBranch(ctx, projectID, branch)
	isNew := existingErr != nil
	if isNew {
		index, err = s.store.CreateProjectBranchIndex(ctx, projectID, branch, absRoot, treeText)
		if err != nil {
			return domain.WorkspaceIndex{}, fmt.Errorf("create branch index: %w", err)
		}
		if base, baseErr := s.store.GetIndexByProject(ctx, projectID); baseErr == nil && base.Status == domain.IndexStatusCompleted {
			if copyErr := s.store.CopyIndexData(ctx, base.ID, index.ID); copyErr != nil {
				log.Warn().Err(copyErr).Str("branch", branch).Msg("seed branch index from base failed; full pass")
			} else {
				// Seeded: the copied hashes make the pass incremental.
				isNew = false
			}
		}
	} else {
		index = existing
		index.RootPath = absRoot
		index.TreeText = treeText
	}
	indexID = index.ID
	return s.runIndexPass(ctx, index, absRoot, paths, treeText, isNew, false)
}

// StartIndexBranch refreshes a branch index asynchronously. Concurrent
// refreshes of the same (project, branch) coalesce into one.
func (s *Service) StartIndexBranch(ctx context.Context, projectID uuid.UUID, branch, workspacePath string) {
	if s == nil || !s.cfg.Enabled || branch == "" || projectID == uuid.Nil {
		return
	}
	key := projectID.String() + "|" + branch
	s.projectMu.Lock()
	if s.branchRunning == nil {
		s.branchRunning = make(map[string]context.CancelFunc)
	}
	if _, busy := s.branchRunning[key]; busy {
		s.projectMu.Unlock()
		return
	}
	passCtx, cancel := context.WithTimeout(detachIndexContext(ctx), 30*time.Minute)
	s.branchRunning[key] = cancel
	s.projectMu.Unlock()

	go func() {
		defer func() {
			cancel()
			s.projectMu.Lock()
			delete(s.branchRunning, key)
			s.projectMu.Unlock()
		}()
		if _, err := s.IndexBranch(passCtx, projectID, branch, workspacePath); err != nil {
			log.Warn().Err(err).Str("project_id", projectID.String()).Str("branch", branch).Msg("branch index refresh failed")
		}
	}()
}

// runIndexPass is the shared indexing core: decide between a full rebuild and
// an incremental diff pass, process the work list, then finalize.
func (s *Service) runIndexPass(ctx context.Context, index domain.WorkspaceIndex, absRoot string, paths []string, treeText string, isNew, force bool) (domain.WorkspaceIndex, error) {
	if err := s.store.UpdateIndexStatus(ctx, index.ID, domain.IndexStatusRunning, 0, 0, 0, ""); err != nil {
		return domain.WorkspaceIndex{}, err
	}
	_ = s.store.UpdateIndexProgress(ctx, index.ID, 0, len(paths))

	// What this pass will embed with, resolved once. It is the value stamped on
	// the index at the end (finalizeIndex) and, right here, the reason a pass
	// may have to redo work it would otherwise have skipped.
	prov := s.newPassProvenance(ctx)

	// A stale index is re-embedded WHOLE, never incrementally.
	//
	// The incremental path exists to skip files whose content did not change —
	// but "did not change" is a statement about the file, not about the vector
	// stored for it. When the embedding model has changed underneath, skipping
	// a file leaves its old model's vectors in place next to the new model's,
	// and one index then holds two incomparable coordinate systems. Every
	// search over it silently ranks part of the repository against the wrong
	// space, and the provenance stamped at the end would be a lie about half
	// the rows. So the change-detection short-circuit is bypassed, exactly as
	// an explicit reindex bypasses it.
	if prov.staleAgainst(index) {
		log.Info().
			Str("index_id", index.ID.String()).
			Str("indexed_with", index.EmbeddingModel).
			Int("indexed_dims", index.EmbeddingDims).
			Str("configured", prov.model).
			Msg("index was built with a different embedding model; re-embedding every file instead of only the changed ones")
		force = true
	}

	if len(paths) == 0 {
		return s.finalizeIndex(ctx, index, absRoot, paths, treeText, prov)
	}

	workList := paths
	incremental := false
	if s.cfg.ReindexOnChange && !isNew {
		storedHashes, err := s.store.GetFileHashes(ctx, index.ID)
		if err == nil && len(storedHashes) > 0 {
			changes := DetectChangedFiles(absRoot, paths, storedHashes)
			if force {
				// An explicit reindex re-reads every file, but still file by
				// file: the point of "force" is distrusting the hashes, not
				// throwing away an index that is about to be rebuilt from the
				// same tree.
				changes.Changed = append(changes.Changed, changes.Unchanged...)
				changes.Unchanged = nil
			}
			if len(changes.Added) == 0 && len(changes.Changed) == 0 && len(changes.Removed) == 0 {
				// Up to date — still refresh the tree, which UpdateIndexStatus
				// never writes and which used to stay frozen at first index.
				_ = s.store.UpdateIndexTree(ctx, index.ID, absRoot, treeText)
				_ = s.store.UpdateIndexProgress(ctx, index.ID, len(paths), len(paths))
				if err := s.store.UpdateIndexStatus(ctx, index.ID, domain.IndexStatusCompleted, len(paths), index.ChunkCount, index.SymbolCount, ""); err != nil {
					return domain.WorkspaceIndex{}, err
				}
				index.Status = domain.IndexStatusCompleted
				index.FileCount = len(paths)
				index.FilesProcessed = len(paths)
				index.FilesTotal = len(paths)
				return index, nil
			}
			// Incremental: drop derived rows of changed+removed files, keep
			// every other file's chunks and embeddings. Added files are cleared
			// too — an "added" file is also a file a stopped pass wrote half of
			// and never stamped, and re-inserting over those rows would double
			// every chunk it had already stored.
			stale := append(append([]string{}, changes.Changed...), changes.Removed...)
			stale = append(stale, changes.Added...)
			if err := s.store.DeleteFileData(ctx, index.ID, stale); err != nil {
				return domain.WorkspaceIndex{}, fmt.Errorf("clear stale file data: %w", err)
			}
			workList = append(append([]string{}, changes.Added...), changes.Changed...)
			incremental = true
		}
	}

	if !incremental && !isNew {
		// A refresh with no usable hashes (hashing disabled, an index from
		// before hashes existed, a pass that died before stamping them) still
		// re-reads every file — but it clears them one file at a time, as each
		// is rewritten. DeleteIndexData here is what made an interrupted run
		// catastrophic: the index was emptied in the first second and, if the
		// process died at file 900 of 1000, the next run found no rows AND no
		// hashes and started over from zero. Every run did.
		if err := s.store.DeleteFileData(ctx, index.ID, workList); err != nil {
			return domain.WorkspaceIndex{}, fmt.Errorf("clear stale file data: %w", err)
		}
	}
	// Files that left the tree, in every mode: their rows would otherwise
	// survive as a searchable ghost of code that no longer exists.
	if !isNew {
		if err := s.store.DeleteFilesNotIn(ctx, index.ID, paths); err != nil {
			return domain.WorkspaceIndex{}, fmt.Errorf("drop removed file data: %w", err)
		}
	}

	progress := func(processed, total int) {
		_ = s.store.UpdateIndexProgress(ctx, index.ID, processed, total)
	}

	if workers := s.indexConcurrency(); workers > 1 && len(workList) > 1 {
		return s.runWorkList(ctx, index, absRoot, paths, treeText, workList, workers, prov)
	}

	for i, rel := range workList {
		if err := ctx.Err(); err != nil {
			// Stopped (or shut down) mid-pass. The files already done keep their
			// rows and hashes, so this is recorded as an interruption with a
			// count rather than as a failure with a stack of nothing.
			msg := fmt.Sprintf("indexing stopped after %d/%d files; the next run continues from here", i, len(workList))
			// Detached: the write must survive the cancellation it is reporting.
			stopCtx := context.WithoutCancel(ctx)
			_ = s.store.UpdateIndexStatus(stopCtx, index.ID, domain.IndexStatusFailed, 0, 0, 0, msg)
			log.Info().Str("index_id", index.ID.String()).Int("done", i).Int("total", len(workList)).Msg("index pass stopped")
			return domain.WorkspaceIndex{}, fmt.Errorf("%s: %w", msg, err)
		}
		progress(i, len(workList))
		fileChunks, fileSymbols, fileEdges, err := s.processFile(absRoot, rel)
		if err != nil {
			_ = s.store.UpdateIndexStatus(ctx, index.ID, domain.IndexStatusFailed, 0, 0, 0, err.Error())
			return domain.WorkspaceIndex{}, fmt.Errorf("process %s: %w", rel, err)
		}
		// Persisted per file, hash last: the hash is the receipt that this
		// file's rows are complete, so an interrupted pass resumes at the first
		// file without one instead of redoing the whole tree.
		if err := s.persistFile(ctx, index.ID, absRoot, rel, fileChunks, fileSymbols, fileEdges, prov); err != nil {
			_ = s.store.UpdateIndexStatus(ctx, index.ID, domain.IndexStatusFailed, 0, 0, 0, err.Error())
			return domain.WorkspaceIndex{}, err
		}
		progress(i+1, len(workList))
	}

	return s.finalizeIndex(ctx, index, absRoot, paths, treeText, prov)
}

// defaultIndexConcurrency is the worker count when the config leaves it unset.
// Four is deliberately modest: it is a clear multiple of the old serial pass
// while staying well inside what a single local OpenAI-compatible server answers
// without queueing, and hosted endpoints are paced by the embedding rate
// limiter regardless of how many workers ask.
const defaultIndexConcurrency = 4

// maxIndexConcurrency caps whatever the config asks for. Past this the
// bottleneck is the embedding server, and the only thing more workers add is
// 429s and a thread pile-up.
const maxIndexConcurrency = 16

func (s *Service) indexConcurrency() int {
	n := s.cfg.Concurrency
	if n <= 0 {
		n = defaultIndexConcurrency
	}
	if n > maxIndexConcurrency {
		n = maxIndexConcurrency
	}
	return n
}

// runWorkList processes the files concurrently.
//
// Indexing is dominated by one HTTP round trip per chunk: a serial pass spends
// nearly all of its wall clock waiting for the embedding endpoint with one
// request outstanding, which is why a repository took as long as it did. The
// files are independent — each one's rows and hash are written by persistFile
// on its own — so they parallelize without any ordering to preserve.
//
// The first failure cancels the rest and is returned. Whatever the other
// workers finished before that stays: their hashes are stamped, so the next
// pass skips them. Partial progress is the point, not a side effect.
func (s *Service) runWorkList(
	ctx context.Context,
	index domain.WorkspaceIndex,
	absRoot string,
	paths []string,
	treeText string,
	workList []string,
	workers int,
	prov *passProvenance,
) (domain.WorkspaceIndex, error) {
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(workers)

	var done int64
	total := len(workList)
	for _, rel := range workList {
		rel := rel
		group.Go(func() error {
			if err := groupCtx.Err(); err != nil {
				return err
			}
			fileChunks, fileSymbols, fileEdges, err := s.processFile(absRoot, rel)
			if err != nil {
				return fmt.Errorf("process %s: %w", rel, err)
			}
			if err := s.persistFile(groupCtx, index.ID, absRoot, rel, fileChunks, fileSymbols, fileEdges, prov); err != nil {
				return err
			}
			// Progress is written by whichever worker just finished; the count
			// is what the UI shows, and with workers in flight it is a count of
			// completed files rather than a position in the list.
			n := atomic.AddInt64(&done, 1)
			_ = s.store.UpdateIndexProgress(ctx, index.ID, int(n), total)
			return nil
		})
	}

	if err := group.Wait(); err != nil {
		// Detached: the status write must survive the cancellation that a
		// stopped pass performs on ctx.
		stopCtx := context.WithoutCancel(ctx)
		msg := err.Error()
		if ctx.Err() != nil {
			msg = fmt.Sprintf("indexing stopped after %d/%d files; the next run continues from here", atomic.LoadInt64(&done), total)
		}
		_ = s.store.UpdateIndexStatus(stopCtx, index.ID, domain.IndexStatusFailed, 0, 0, 0, msg)
		return domain.WorkspaceIndex{}, fmt.Errorf("%s", msg)
	}
	return s.finalizeIndex(ctx, index, absRoot, paths, treeText, prov)
}

// rateLimitBackoff is the wait after a rate-limited embedding call when the
// provider did not say how long to wait, doubling up to rateLimitMaxBackoff.
const (
	rateLimitBackoff    = 5 * time.Second
	rateLimitMaxBackoff = 2 * time.Minute
)

// isRateLimited reports the provider saying "not now" rather than "no".
//
// It matches on text as well as on the LLM client's typed error because the
// limit reaches here through several shapes — a 429 body from an OpenAI-
// compatible proxy, Vertex's RESOURCE_EXHAUSTED, the client's own exhausted-
// retries wrapper. Being wrong in the permissive direction costs one extra
// wait; being wrong the other way fails an index over a queue.
func isRateLimited(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "429") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "rate_limited") ||
		strings.Contains(msg, "resource_exhausted") ||
		strings.Contains(msg, "too many requests")
}

// embedWithBackoff embeds one chunk, waiting out rate limits instead of failing
// on them.
//
// A rate limit is the provider pacing us, not a problem with the work: failing
// the pass over one meant an index that stopped at 9% and a person having to
// press re-index. Here the pass simply waits and carries on from the same
// chunk. It waits indefinitely by design — the loop's only exit besides success
// is the run's own context, so stopping the index (or shutting the pod down)
// still ends it immediately, and everything already indexed is stamped and
// kept.
func (s *Service) embedWithBackoff(ctx context.Context, input string, budget time.Duration) ([]float32, error) {
	wait := rateLimitBackoff
	transient := 0
	for attempt := 1; ; attempt++ {
		embedCtx, cancel := context.WithTimeout(ctx, budget)
		emb, err := s.llm.Embed(embedCtx, input, s.embeddingModel)
		cancel()
		if err == nil {
			return emb, nil
		}
		// A cancelled RUN is final; a per-chunk deadline that expired is not,
		// and telling them apart is what stops a stopped index from looking
		// like a rate limit and retrying forever.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !isRateLimited(err) {
			if !isTransientEmbedError(err) || transient >= maxTransientEmbedAttempts-1 {
				return nil, err
			}
			transient++
			backoff := transientEmbedBackoff << (transient - 1)
			log.Info().Err(err).Dur("wait", backoff).Int("attempt", transient+1).
				Msg("embedder briefly unreachable; retrying the same chunk")
			retry := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				retry.Stop()
				return nil, ctx.Err()
			case <-retry.C:
			}
			continue
		}
		sleep := wait
		if hinted := retryAfterHint(err); hinted > 0 {
			sleep = hinted
		}
		log.Info().Err(err).Dur("wait", sleep).Int("attempt", attempt).
			Msg("embedding rate limited; waiting and continuing from the same chunk")
		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		if wait < rateLimitMaxBackoff {
			wait *= 2
			if wait > rateLimitMaxBackoff {
				wait = rateLimitMaxBackoff
			}
		}
	}
}

// retryAfterHint reads the "retry after N seconds" a provider states in its
// message. The typed error carrying it lives in the LLM adapter, so what
// reaches here is text.
func retryAfterHint(err error) time.Duration {
	msg := strings.ToLower(err.Error())
	idx := strings.Index(msg, "retry after ")
	if idx < 0 {
		return 0
	}
	rest := msg[idx+len("retry after "):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}
	secs, err2 := strconv.Atoi(rest[:end])
	if err2 != nil || secs <= 0 {
		return 0
	}
	if d := time.Duration(secs) * time.Second; d < rateLimitMaxBackoff {
		return d
	}
	return rateLimitMaxBackoff
}

// persistFile embeds and stores one file's derived rows, then stamps its hash.
//
// The order is the whole point. Rows first, hash last: a crash between them
// costs one file's work on the next pass, whereas stamping first would leave a
// file marked indexed with nothing indexed — a hole no later run would ever
// notice, because the hash says it is done.
func (s *Service) persistFile(
	ctx context.Context,
	indexID uuid.UUID,
	absRoot, relPath string,
	chunks []domain.WorkspaceChunk,
	symbols []domain.WorkspaceSymbol,
	edges []domain.WorkspaceEdge,
	prov *passProvenance,
) error {
	unembedded := 0
	for i := range chunks {
		input := FormatEmbedInput(chunks[i].FilePath, chunks[i].SymbolName, chunks[i].Signature, capEmbedContent(chunks[i].Content))
		budget := s.embedBudget
		if budget <= 0 {
			budget = perChunkEmbedTimeout
		}
		emb, err := s.embedChunk(ctx, input, budget)
		switch {
		case err == nil:
			prov.embedSucceeded()
			prov.observe(emb)
			chunks[i].Embedding = emb
		case ctx.Err() != nil:
			return fmt.Errorf("embed chunk: %w", err)
		default:
			// One chunk the embedder cannot take is stored without a vector:
			// search skips it, and the rest of the repository still gets
			// indexed. Only a long run of failures stops the pass.
			if streak := prov.embedFailed(); streak >= maxConsecutiveEmbedFailures {
				return fmt.Errorf("embed chunk: %d chunks in a row could not be embedded: %w", streak, err)
			}
			unembedded++
			log.Warn().Err(err).Str("file", chunks[i].FilePath).Str("symbol", chunks[i].SymbolName).
				Msg("chunk stored without an embedding")
		}
		chunks[i].IndexID = indexID
	}
	for i := range symbols {
		symbols[i].IndexID = indexID
	}
	for i := range edges {
		edges[i].IndexID = indexID
	}
	if len(symbols) > 0 {
		if err := s.store.SaveSymbols(ctx, indexID, symbols); err != nil {
			return fmt.Errorf("save symbols: %w", err)
		}
	}
	if len(chunks) > 0 {
		if err := s.store.SaveChunks(ctx, indexID, chunks); err != nil {
			return fmt.Errorf("save chunks: %w", err)
		}
	}
	if len(edges) > 0 {
		if err := s.store.SaveEdges(ctx, indexID, edges); err != nil {
			return fmt.Errorf("save edges: %w", err)
		}
	}
	if unembedded > 0 {
		// No hash for a file that has a chunk without a vector: the next pass
		// sees the file as new, clears its rows and embeds it again, so a
		// failure does not leave the chunk unsearchable for good.
		return nil
	}
	hash, err := HashFile(absRoot, relPath)
	if err != nil {
		return fmt.Errorf("hash %s: %w", relPath, err)
	}
	return s.store.SaveFileHashes(ctx, indexID, []domain.WorkspaceFileHash{{FilePath: relPath, Hash: hash}})
}

func (s *Service) finalizeIndex(
	ctx context.Context,
	index domain.WorkspaceIndex,
	absRoot string,
	paths []string,
	treeText string,
	prov *passProvenance,
) (domain.WorkspaceIndex, error) {
	// Chunks, symbols, edges and hashes are already stored — persistFile wrote
	// each file's rows as it went. What is left is the pass-level bookkeeping.

	// Persist the fresh tree — UpdateIndexStatus never writes tree_text, which
	// is how the injected "Repository structure" used to freeze at first index.
	if err := s.store.UpdateIndexTree(ctx, index.ID, absRoot, treeText); err != nil {
		log.Warn().Err(err).Str("index_id", index.ID.String()).Msg("update index tree failed")
	}

	// The totals are whatever is actually stored — a pass only ever writes the
	// files it touched, so counting its own work would report the diff as the
	// size of the index.
	chunkCount, symbolCount := index.ChunkCount, index.SymbolCount
	if storedChunks, storedSymbols, err := s.store.CountIndexData(ctx, index.ID); err == nil {
		chunkCount, symbolCount = storedChunks, storedSymbols
	}

	now := time.Now()
	index.Status = domain.IndexStatusCompleted
	index.FileCount = len(paths)
	index.ChunkCount = chunkCount
	index.SymbolCount = symbolCount
	index.TreeText = treeText
	index.IndexedAt = &now

	if err := s.store.UpdateIndexStatus(ctx, index.ID, domain.IndexStatusCompleted, index.FileCount, index.ChunkCount, index.SymbolCount, ""); err != nil {
		return domain.WorkspaceIndex{}, err
	}
	_ = s.store.UpdateIndexProgress(ctx, index.ID, index.FileCount, index.FileCount)

	if sha := GitHeadSHA(absRoot); sha != "" {
		if err := s.store.UpdateIndexCommit(ctx, index.ID, sha); err != nil {
			log.Warn().Err(err).Str("index_id", index.ID.String()).Msg("stamp index commit failed")
		} else {
			index.CommitSHA = sha
		}
	}

	// The commit says WHICH CODE this index describes; this says WHAT PRODUCED
	// its vectors. Both are stamped here, at the end, because both are only
	// true of a pass that finished — and a pass that did not finish keeps the
	// previous stamp, which correctly still describes the rows that are
	// actually stored.
	//
	// Logged rather than fatal on failure: the index itself is complete and
	// usable, and refusing to report a finished pass because one bookkeeping
	// column would not write would throw away a repository's worth of
	// embeddings. The cost of the miss is that this index reads as stale later
	// and is rebuilt — the safe direction.
	index.EmbeddingModel = prov.modelName()
	index.EmbeddingDims = prov.dimensions()
	if err := s.store.UpdateIndexEmbedding(ctx, index.ID, index.EmbeddingModel, index.EmbeddingDims); err != nil {
		log.Warn().Err(err).Str("index_id", index.ID.String()).Msg("stamp index embedding provenance failed")
	}

	return index, nil
}

// GitHeadSHA returns the HEAD commit of root, or "" when root is not a git
// work tree (session uploads index fine without a commit baseline).
func GitHeadSHA(root string) string {
	out, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (s *Service) processFile(root, relPath string) ([]domain.WorkspaceChunk, []domain.WorkspaceSymbol, []domain.WorkspaceEdge, error) {
	fullPath := filepath.Join(root, relPath)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, nil, nil, err
	}

	rawChunks, err := s.chunkers.Chunk(relPath, content)
	if err != nil {
		return nil, nil, nil, err
	}

	chunks := make([]domain.WorkspaceChunk, 0, len(rawChunks))
	symbols := make([]domain.WorkspaceSymbol, 0, len(rawChunks))
	seenSymbols := make(map[string]struct{})

	for _, ch := range rawChunks {
		chunkID := uuid.New()
		chunks = append(chunks, domain.WorkspaceChunk{
			ID:         chunkID,
			FilePath:   relPath,
			SymbolName: ch.SymbolName,
			Kind:       ch.Kind,
			StartLine:  ch.StartLine,
			EndLine:    ch.EndLine,
			Language:   ch.Language,
			Signature:  ch.Signature,
			Content:    ch.Content,
		})

		if ch.SymbolName == "" || ch.Kind == "block" {
			continue
		}
		key := relPath + ":" + ch.SymbolName
		if _, ok := seenSymbols[key]; ok {
			continue
		}
		seenSymbols[key] = struct{}{}
		symbols = append(symbols, domain.WorkspaceSymbol{
			ID:        uuid.New(),
			FilePath:  relPath,
			Kind:      ch.Kind,
			Name:      ch.SymbolName,
			Signature: ch.Signature,
			StartLine: ch.StartLine,
			EndLine:   ch.EndLine,
		})
	}

	edges := s.buildEdges(relPath, content, rawChunks)
	return chunks, symbols, edges, nil
}

func (s *Service) buildEdges(relPath string, content []byte, chunks []chunker.Chunk) []domain.WorkspaceEdge {
	return BuildFileEdges(relPath, content, chunks)
}

// BuildFileEdges extracts import/call edges for one file. Shared with the
// query-time overlay so live (unindexed) edits produce the same graph shape.
func BuildFileEdges(relPath string, content []byte, chunks []chunker.Chunk) []domain.WorkspaceEdge {
	var edges []domain.WorkspaceEdge
	lower := strings.ToLower(relPath)

	switch {
	case strings.HasSuffix(lower, ".go"):
		importEdges, err := graph.ExtractGoImports(relPath, content)
		if err == nil {
			for _, e := range importEdges {
				edges = append(edges, domainEdgeFromGraph(e))
			}
		}
		callEdges, err := graph.BuildGoCallGraph(relPath, content)
		if err == nil {
			for _, e := range callEdges {
				edges = append(edges, domainEdgeFromGraph(e))
			}
		}
	case graph.HasGrammarGraph(relPath):
		// TypeScript, JavaScript, Python, Java, Kotlin and Swift all go through
		// the grammar-backed extractors. Calls are scoped by each chunk's line
		// range: members are named Class.method here, and looking that name up
		// in the grammar finds nothing.
		for _, e := range graph.ExtractImports(relPath, content) {
			edges = append(edges, domainEdgeFromGraph(e))
		}
		for _, ch := range chunks {
			if !callableChunkKinds[ch.Kind] {
				continue
			}
			for _, e := range graph.BuildCallGraphInRange(relPath, ch.SymbolName, content, ch.StartLine, ch.EndLine) {
				edges = append(edges, domainEdgeFromGraph(e))
			}
		}
	}

	return edges
}

// callableChunkKinds are the chunk kinds that can contain call sites. Arrow
// functions assigned to a const land as "const" or "property" depending on the
// language, so those count too — a React component's body is where its hooks
// and handlers are called. "block" is deliberately absent: those are the
// synthetic slices of an oversized symbol, and they have no row in
// workspace_symbols for an edge to point at.
var callableChunkKinds = map[string]bool{
	"function": true,
	"method":   true,
	"const":    true,
	"property": true,
}

func domainEdgeFromGraph(e graph.Edge) domain.WorkspaceEdge {
	return domain.WorkspaceEdge{
		FromFile:   e.From.FilePath,
		FromSymbol: e.From.SymbolName,
		ToFile:     e.To.FilePath,
		ToSymbol:   e.To.SymbolName,
		EdgeKind:   string(e.Kind),
	}
}

func DefaultChunkerRegistry() *chunker.Registry {
	return chunker.DefaultRegistry()
}

func NewInjectorFromService(s *Service, mapperSvc *mapper.Service) *Injector {
	return NewInjector(s.store, s.llm, mapperSvc, s.embeddingModel, s.graphCfg)
}

// isTransientEmbedError reports an embedder that did not answer this time but
// may on the next try: a connection reset or refused (the local embedder's
// socket closed under a request), or a gateway status. A 500 or a 4xx is an
// answer about this input and is not retried.
func isTransientEmbedError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"embeddings unreachable", "connection reset", "connection refused", "broken pipe",
		"unexpected eof", "embeddings returned 502", "embeddings returned 503", "embeddings returned 504",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

func embedConcurrency(cfg domain.IndexerConfig) int {
	if cfg.EmbedConcurrency > 0 {
		return cfg.EmbedConcurrency
	}
	return defaultEmbedConcurrency
}

// embedChunk embeds one chunk inside the service-wide embedding limit.
func (s *Service) embedChunk(ctx context.Context, input string, budget time.Duration) ([]float32, error) {
	if s.embedSlots != nil {
		select {
		case s.embedSlots <- struct{}{}:
			defer func() { <-s.embedSlots }()
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return s.embedWithBackoff(ctx, input, budget)
}
