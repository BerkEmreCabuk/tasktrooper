package localpreview

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/application/workspace"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type TaskReader interface {
	Get(ctx context.Context, repositoryID, taskID uuid.UUID) (domain.BoardTask, error)
}

type RepoRootResolver interface {
	ResolveRootPath(ctx context.Context, repositoryID uuid.UUID) (string, error)
}

type GitWorkspacer interface {
	HasGit(rootPath string) bool
	EnsureTaskWorkspace(ctx context.Context, projectRoot, workspacePath, branch string) error
}

const stopGrace = 10 * time.Second

const logTailLines = 200

var urlPattern = regexp.MustCompile(`https?://(?:localhost|127\.0\.0\.1|0\.0\.0\.0)(?::\d+)?[^\s"'<>]*`)

type Deps struct {
	Tasks         TaskReader
	Repositories  RepoRootResolver
	Git           GitWorkspacer
	WorkspaceRoot string
}

type Service struct {
	tasks         TaskReader
	repos         RepoRootResolver
	git           GitWorkspacer
	workspaceRoot string

	mu     sync.Mutex
	active map[uuid.UUID]*process
}

func NewService(deps Deps) *Service {
	s := &Service{
		tasks:         deps.Tasks,
		repos:         deps.Repositories,
		git:           deps.Git,
		workspaceRoot: deps.WorkspaceRoot,
	}
	s.reapStale()
	return s
}

func (s *Service) reapStale() {
	if s.workspaceRoot == "" {
		return
	}
	for _, e := range loadState(s.workspaceRoot) {
		if e.PID <= 0 {
			continue
		}
		terminateProcessGroup(e.PID)
		go func(pid int) {
			time.Sleep(stopGrace)
			killProcessGroup(pid)
		}(e.PID)
	}

	saveState(s.workspaceRoot, nil)
}

type process struct {
	preview domain.LocalPreview
	cmd     *exec.Cmd
	done    chan struct{}

	mu      sync.Mutex
	status  domain.LocalPreviewStatus
	url     string
	detail  string
	logTail []string
}

func (p *process) snapshot() domain.LocalPreview {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.preview
	out.Status = p.status
	out.URL = p.url
	out.Detail = p.detail
	out.LogTail = append([]string(nil), p.logTail...)
	return out
}

func (p *process) appendLine(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.logTail = append(p.logTail, line)
	if len(p.logTail) > logTailLines {
		p.logTail = p.logTail[len(p.logTail)-logTailLines:]
	}
	if p.url == "" {
		if m := urlPattern.FindString(line); m != "" {
			p.url = strings.Replace(m, "0.0.0.0", "127.0.0.1", 1)
			p.status = domain.LocalPreviewRunning
		}
	}
}

func (p *process) setDone(status domain.LocalPreviewStatus, detail string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.status = status
	if detail != "" {
		p.detail = detail
	}
}

func (s *Service) Start(ctx context.Context, repositoryID, taskID uuid.UUID, commandOverride string) (domain.LocalPreview, error) {
	task, err := s.tasks.Get(ctx, repositoryID, taskID)
	if err != nil {
		return domain.LocalPreview{}, fmt.Errorf("read task: %w", err)
	}
	root, err := s.repos.ResolveRootPath(ctx, repositoryID)
	if err != nil {
		return domain.LocalPreview{}, fmt.Errorf("resolve repository: %w", err)
	}
	if s.git == nil || s.workspaceRoot == "" || !s.git.HasGit(root) {
		return domain.LocalPreview{}, fmt.Errorf("this repository has no git working copy to check a branch out of")
	}
	branch := domain.TaskBranchName(task)
	workspacePath, err := workspace.TaskDir(s.workspaceRoot, taskID)
	if err != nil {
		return domain.LocalPreview{}, err
	}
	if err := s.git.EnsureTaskWorkspace(ctx, root, workspacePath, branch); err != nil {
		return domain.LocalPreview{}, fmt.Errorf("check out branch %s: %w", branch, err)
	}

	command := strings.TrimSpace(commandOverride)
	if command == "" {
		command = DetectRunCommand(workspacePath)
	}
	if command == "" {
		return domain.LocalPreview{}, fmt.Errorf("could not detect a way to run this repository locally (looked for an npm dev/start script, a Makefile dev target, or a Go module)")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil {
		s.active = make(map[uuid.UUID]*process)
	}
	if existing, ok := s.active[repositoryID]; ok {
		s.stopLocked(existing)
	}

	cmd := shellCommand(command)
	cmd.Dir = workspacePath
	cmd.Env = os.Environ()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return domain.LocalPreview{}, fmt.Errorf("open preview output: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return domain.LocalPreview{}, fmt.Errorf("open preview error output: %w", err)
	}

	p := &process{
		preview: domain.LocalPreview{
			RepositoryID: repositoryID,
			TaskID:       taskID,
			Branch:       branch,
			Command:      command,
			StartedAt:    time.Now(),
		},
		cmd:    cmd,
		done:   make(chan struct{}),
		status: domain.LocalPreviewStarting,
	}

	if err := cmd.Start(); err != nil {
		return domain.LocalPreview{}, fmt.Errorf("start %q: %w", command, err)
	}

	s.active[repositoryID] = p
	s.persistLocked()
	go pumpLines(stdout, p.appendLine)
	go pumpLines(stderr, p.appendLine)
	go s.wait(repositoryID, p)

	log.Info().Str("repository_id", repositoryID.String()).Str("task_id", taskID.String()).
		Str("branch", branch).Str("command", command).Msg("local preview started")
	return p.snapshot(), nil
}

func pumpLines(r io.Reader, onLine func(string)) {
	scanner := bufio.NewScanner(r)

	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		onLine(scanner.Text())
	}
}

func (s *Service) wait(repositoryID uuid.UUID, p *process) {
	err := p.cmd.Wait()
	close(p.done)
	p.mu.Lock()
	stopped := p.status == domain.LocalPreviewStopped
	p.mu.Unlock()
	switch {
	case stopped:

	case err != nil:
		p.setDone(domain.LocalPreviewFailed, err.Error())
	default:
		p.setDone(domain.LocalPreviewFailed, "the command exited on its own")
	}
	s.mu.Lock()
	if s.active[repositoryID] == p {
		delete(s.active, repositoryID)
		s.persistLocked()
	}
	s.mu.Unlock()
}

func (s *Service) Status(repositoryID uuid.UUID) (domain.LocalPreview, bool) {
	s.mu.Lock()
	p, ok := s.active[repositoryID]
	s.mu.Unlock()
	if !ok {
		return domain.LocalPreview{}, false
	}
	return p.snapshot(), true
}

func (s *Service) Stop(repositoryID uuid.UUID) {
	s.mu.Lock()
	p, ok := s.active[repositoryID]
	if ok {
		delete(s.active, repositoryID)
		s.persistLocked()
	}
	s.mu.Unlock()
	if ok {
		s.stopProcess(p)
	}
}

func (s *Service) stopLocked(p *process) {
	delete(s.active, p.preview.RepositoryID)
	go s.stopProcess(p)
}

func (s *Service) persistLocked() {
	entries := make([]persistedEntry, 0, len(s.active))
	for repositoryID, p := range s.active {
		if p.cmd.Process == nil {
			continue
		}
		entries = append(entries, persistedEntry{RepositoryID: repositoryID, PID: p.cmd.Process.Pid})
	}
	saveState(s.workspaceRoot, entries)
}

func (s *Service) stopProcess(p *process) {
	p.mu.Lock()
	p.status = domain.LocalPreviewStopped
	p.mu.Unlock()
	if p.cmd.Process == nil {
		return
	}
	pgid := p.cmd.Process.Pid
	terminateProcessGroup(pgid)
	select {
	case <-p.done:
		return
	case <-time.After(stopGrace):
	}
	killProcessGroup(pgid)
	<-p.done
}

func DetectRunCommand(dir string) string {
	if hasNPMScript(dir, "dev") {
		return "npm run dev"
	}
	if hasNPMScript(dir, "start") {
		return "npm start"
	}
	if fileExists(filepath.Join(dir, "Makefile")) && makeHasTarget(dir, "dev") {
		return "make dev"
	}
	if fileExists(filepath.Join(dir, "go.mod")) {
		return "go run ."
	}
	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func hasNPMScript(dir, script string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return false
	}

	return strings.Contains(string(data), `"`+script+`":`)
}

func makeHasTarget(dir, target string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "Makefile"))
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, target+":") {
			return true
		}
	}
	return false
}
