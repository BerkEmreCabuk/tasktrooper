package toolchain

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Requirements struct {
	Go           string
	GoExact      bool
	Node         string
	NodeSource   string
	Python       string
	PythonSource string

	Flutter       string
	FlutterSource string
}

type Overlay struct {
	Env      []string
	Warnings []string
}

var requirementSources = map[string][]string{
	"go":      {"go.mod", ".tool-versions"},
	"node":    {".nvmrc", ".node-version", ".tool-versions", "package.json"},
	"python":  {".python-version", ".tool-versions"},
	"flutter": {".tool-versions", ".flutter-version"},
}

func Detect(dir string) Requirements {
	pins := ReadPins(dir)
	var req Requirements
	if p, ok := firstPin(pins, requirementSources["go"], "go"); ok {
		req.Go = normalizeVersion(p.Version)
		req.GoExact = p.Source != "go.mod" || strings.HasPrefix(p.Version, "go")
	}
	if p, ok := firstPin(pins, requirementSources["node"], "node"); ok {
		req.Node, req.NodeSource = normalizeVersion(p.Version), p.Source
	}
	if p, ok := firstPin(pins, requirementSources["python"], "python"); ok {
		req.Python, req.PythonSource = normalizeVersion(p.Version), p.Source
	}
	if p, ok := firstPin(pins, requirementSources["flutter"], "flutter", "dart"); ok {
		req.Flutter, req.FlutterSource = normalizeVersion(p.Version), p.Source
	}
	return req
}

func firstPin(pins []Pin, sources []string, languages ...string) (Pin, bool) {
	for _, source := range sources {
		for _, language := range languages {
			for _, p := range pins {
				if p.Source == source && p.Language == language && normalizeVersion(p.Version) != "" {
					return p, true
				}
			}
		}
	}
	return Pin{}, false
}

type Resolver struct {
	Home     string
	Environ  func() []string
	LookPath func(string) (string, error)

	Version func(bin string, args ...string) string

	mu    sync.Mutex
	cache map[string]cachedOverlay
}

type cachedOverlay struct {
	overlay Overlay
	expires time.Time
}

var Default = New()

func New() *Resolver {
	return &Resolver{
		Home:     firstNonEmpty(os.Getenv("HOME"), homeDir()),
		Environ:  os.Environ,
		LookPath: exec.LookPath,
		Version: func(bin string, args ...string) string {
			out, _ := exec.Command(bin, args...).CombinedOutput()
			return strings.TrimSpace(string(out))
		},
		cache: map[string]cachedOverlay{},
	}
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

const cacheTTL = 30 * time.Second

func (r *Resolver) Overlay(dir string) Overlay {
	r.mu.Lock()
	if c, ok := r.cache[dir]; ok && time.Now().Before(c.expires) {
		r.mu.Unlock()
		return c.overlay
	}
	r.mu.Unlock()

	ov := r.resolve(dir)

	r.mu.Lock()
	r.cache[dir] = cachedOverlay{overlay: ov, expires: time.Now().Add(cacheTTL)}
	r.mu.Unlock()
	return ov
}

func (r *Resolver) resolve(dir string) Overlay {
	var ov Overlay
	req := Detect(dir)

	basePath := envValue(r.Environ(), "PATH")
	newPath := ensurePathDirs(basePath, "/opt/homebrew/bin", "/usr/local/bin")

	if req.Go != "" {
		if req.GoExact {

			ov.Env = append(ov.Env, "GOTOOLCHAIN=go"+req.Go+"+auto")
		} else {

			ov.Env = append(ov.Env, "GOTOOLCHAIN=auto")
		}
	}

	if req.Node != "" {
		if binDir := r.findInstall(nodeInstallRoots(r.Home), req.Node, "node"); binDir != "" {
			newPath = binDir + string(os.PathListSeparator) + newPath
		} else if w := r.hostMismatch("node", []string{"--version"}, req.Node, req.NodeSource, 1); w != "" {
			ov.Warnings = append(ov.Warnings, w)
		}
	}

	if req.Python != "" {
		if binDir := r.findInstall(pythonInstallRoots(r.Home), req.Python, "python3", "python"); binDir != "" {
			newPath = binDir + string(os.PathListSeparator) + newPath
		} else if w := r.hostMismatch("python3", []string{"--version"}, req.Python, req.PythonSource, 2); w != "" {
			ov.Warnings = append(ov.Warnings, w)
		}
	}

	if req.Flutter != "" {
		if binDir := r.findInstall(flutterInstallRoots(r.Home), req.Flutter, "flutter"); binDir != "" {
			newPath = binDir + string(os.PathListSeparator) + newPath
		} else if w := r.hostMismatch("flutter", []string{"--version"}, req.Flutter, req.FlutterSource, 2); w != "" {
			ov.Warnings = append(ov.Warnings, w)
		}
	}

	if newPath != basePath {
		ov.Env = append(ov.Env, "PATH="+newPath)
	}
	return ov
}

func (r *Resolver) hostMismatch(bin string, args []string, want, source string, matchSegs int) string {
	path, err := r.LookPath(bin)
	if err != nil {
		return fmt.Sprintf("repo declares %s %s (%s) but no %s binary is on PATH", bin, want, source, bin)
	}
	raw := r.Version(path, args...)
	host := normalizeVersion(raw)
	if host == "" {
		return ""
	}
	if !segsMatch(host, want, matchSegs) {
		return fmt.Sprintf("repo declares %s %s (%s) but PATH resolves %s %s and no managed install matches (searched mise/asdf/nvm/pyenv/homebrew); builds may behave differently", bin, want, source, bin, host)
	}
	return ""
}

func flutterInstallRoots(home string) []installRoot {
	return []installRoot{
		{dir: filepath.Join(home, ".local/share/mise/installs/flutter"), binSubdir: "bin"},
		{dir: filepath.Join(home, ".asdf/installs/flutter"), binSubdir: "bin"},
	}
}

func nodeInstallRoots(home string) []installRoot {
	return []installRoot{
		{dir: filepath.Join(home, ".local/share/mise/installs/node"), binSubdir: "bin"},
		{dir: filepath.Join(home, ".local/share/mise/installs/nodejs"), binSubdir: "bin"},
		{dir: filepath.Join(home, ".asdf/installs/nodejs"), binSubdir: "bin"},
		{dir: filepath.Join(home, ".nvm/versions/node"), binSubdir: "bin"},
		{dir: "/opt/homebrew/opt", namePrefix: "node@", binSubdir: "bin"},
		{dir: "/usr/local/opt", namePrefix: "node@", binSubdir: "bin"},
	}
}

func pythonInstallRoots(home string) []installRoot {
	return []installRoot{
		{dir: filepath.Join(home, ".pyenv/versions"), binSubdir: "bin"},
		{dir: filepath.Join(home, ".local/share/mise/installs/python"), binSubdir: "bin"},
		{dir: filepath.Join(home, ".asdf/installs/python"), binSubdir: "bin"},
	}
}

type installRoot struct {
	dir        string
	namePrefix string
	binSubdir  string
}

func (r *Resolver) findInstall(roots []installRoot, want string, bins ...string) string {
	for _, root := range roots {
		entries, err := os.ReadDir(root.dir)
		if err != nil {
			continue
		}
		best := ""
		for _, e := range entries {
			name := e.Name()
			if root.namePrefix != "" {
				if !strings.HasPrefix(name, root.namePrefix) {
					continue
				}
			}
			ver := normalizeVersion(strings.TrimPrefix(name, root.namePrefix))
			if !segsMatch(ver, want, len(versionSegs(want))) {
				continue
			}
			if best == "" || compareVersions(verOfName(name, root.namePrefix), verOfName(best, root.namePrefix)) < 0 {
				best = name
			}
		}
		if best == "" {
			continue
		}
		binDir := filepath.Join(root.dir, best, root.binSubdir)
		for _, b := range bins {
			if _, err := os.Stat(filepath.Join(binDir, b)); err == nil {
				return binDir
			}
		}
	}
	return ""
}

func verOfName(name, prefix string) string {
	return normalizeVersion(strings.TrimPrefix(name, prefix))
}

var versionRe = regexp.MustCompile(`[0-9]+(\.[0-9]+)*`)

func normalizeVersion(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	if m := versionRe.FindString(s); m != "" {
		return m
	}
	return ""
}

func versionSegs(v string) []int {
	parts := strings.Split(v, ".")
	segs := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			break
		}
		segs = append(segs, n)
	}
	return segs
}

func segsMatch(got, want string, n int) bool {
	g, w := versionSegs(got), versionSegs(want)
	if len(w) < n {
		n = len(w)
	}
	if len(g) < n {
		return false
	}
	for i := 0; i < n; i++ {
		if g[i] != w[i] {
			return false
		}
	}
	return true
}

func compareVersions(a, b string) int {
	as, bs := versionSegs(a), versionSegs(b)
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] != bs[i] {
			return bs[i] - as[i]
		}
	}
	return len(bs) - len(as)
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return env[i][len(prefix):]
		}
	}
	return ""
}

func ensurePathDirs(path string, dirs ...string) string {
	existing := strings.Split(path, string(os.PathListSeparator))
	present := map[string]bool{}
	for _, e := range existing {
		present[e] = true
	}
	for _, d := range dirs {
		if !present[d] {
			path += string(os.PathListSeparator) + d
		}
	}
	return path
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
