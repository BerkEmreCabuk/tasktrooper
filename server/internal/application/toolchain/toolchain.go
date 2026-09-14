// Package toolchain resolves the tool versions a repository declares in its
// own files (go.mod, .nvmrc, .node-version, .tool-versions, package.json
// engines, .python-version) into an environment overlay for commands executed
// in that repository's workspace. Two concurrent tasks whose repos pin
// different versions of the same tool each get their own resolution instead of
// silently sharing whatever binary the host PATH finds first.
package toolchain

import (
	"encoding/json"
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

// Requirements are the versions a repository declares for itself. Empty fields
// mean the repo declares nothing for that tool.
type Requirements struct {
	Go           string // "1.23.4" (exact) or "1.23" (minimum)
	GoExact      bool   // true when go.mod carries a toolchain directive or .tool-versions pins golang
	Node         string
	NodeSource   string
	Python       string
	PythonSource string
	// Flutter is what a mobile repository declares, either in .tool-versions or
	// as the SDK constraint in pubspec.yaml. It is resolved the same way node
	// and python are — an install root on disk, not a download — because the
	// verify gate has to judge a diff with the analyzer the repo expects, and
	// two Flutter minors disagree about what is a lint and what is an error.
	Flutter       string
	FlutterSource string
}

// Overlay is what a command executed in the workspace must add on top of the
// process environment. Entries appended after os.Environ() win, so PATH and
// GOTOOLCHAIN here override the inherited values.
type Overlay struct {
	Env      []string
	Warnings []string
}

// Detect reads the repository's own version declarations. It never guesses:
// no declaration, no requirement.
func Detect(dir string) Requirements {
	var req Requirements
	tools := parseToolVersions(filepath.Join(dir, ".tool-versions"))

	if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
		goDirective, toolchainDirective := parseGoMod(string(data))
		switch {
		case toolchainDirective != "":
			req.Go, req.GoExact = toolchainDirective, true
		case goDirective != "":
			req.Go = goDirective
		}
	}
	if req.Go == "" {
		if v := tools["golang"]; v != "" {
			req.Go, req.GoExact = v, true
		}
	}

	nodeSources := []struct {
		source string
		value  string
	}{
		{".nvmrc", readVersionFile(filepath.Join(dir, ".nvmrc"))},
		{".node-version", readVersionFile(filepath.Join(dir, ".node-version"))},
		{".tool-versions", firstNonEmpty(tools["nodejs"], tools["node"])},
		{"package.json engines.node", enginesNode(dir)},
	}
	for _, s := range nodeSources {
		if s.value != "" {
			req.Node, req.NodeSource = s.value, s.source
			break
		}
	}

	pySources := []struct {
		source string
		value  string
	}{
		{".python-version", readVersionFile(filepath.Join(dir, ".python-version"))},
		{".tool-versions", tools["python"]},
	}
	for _, s := range pySources {
		if s.value != "" {
			req.Python, req.PythonSource = s.value, s.source
			break
		}
	}

	flutterSources := []struct {
		source string
		value  string
	}{
		{".tool-versions", firstNonEmpty(tools["flutter"], tools["dart"])},
		{".flutter-version", readVersionFile(filepath.Join(dir, ".flutter-version"))},
	}
	for _, s := range flutterSources {
		if s.value != "" {
			req.Flutter, req.FlutterSource = s.value, s.source
			break
		}
	}
	return req
}

// Resolver turns Requirements into an Overlay using the version-manager
// installs present on this machine. All lookups are injectable for tests.
type Resolver struct {
	Home     string
	Environ  func() []string
	LookPath func(string) (string, error)
	// Version runs a binary with args and returns its trimmed combined output
	// ("v22.1.0", "Python 3.12.1").
	Version func(bin string, args ...string) string

	mu    sync.Mutex
	cache map[string]cachedOverlay
}

type cachedOverlay struct {
	overlay Overlay
	expires time.Time
}

// Default is the process-wide resolver used by the shell tool and the board
// verify gate.
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

// Overlay resolves the env overlay for commands running in dir. Results are
// cached briefly because agent loops run many commands in the same workspace.
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
			// Pin the exact toolchain but allow go.mod to upgrade further —
			// mirrors what `go` does when the directive is honored locally.
			ov.Env = append(ov.Env, "GOTOOLCHAIN=go"+req.Go+"+auto")
		} else {
			// A bare `go 1.x` directive: let the go command pick/download a
			// satisfying toolchain even when the host env pinned GOTOOLCHAIN.
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

// hostMismatch compares the requirement against whatever the current PATH
// resolves; matchSegs says how many leading version segments must agree.
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

// flutterInstallRoots covers the two ways a Flutter SDK lands on a machine that
// is not a developer laptop: mise (what the tools image uses) and a plain
// unpacked SDK under the home directory, which is what every CI recipe on the
// internet tells people to do.
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
	namePrefix string // e.g. "node@" for homebrew's /opt/homebrew/opt/node@18
	binSubdir  string
}

// findInstall returns the bin directory of the best installed version
// matching want, or "" when nothing matches. bins are candidate binary names
// that must exist inside the returned directory.
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

// --- parsing helpers ---

func parseGoMod(content string) (goDirective, toolchainDirective string) {
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "go":
			goDirective = fields[1]
		case "toolchain":
			toolchainDirective = strings.TrimPrefix(fields[1], "go")
		}
	}
	return goDirective, toolchainDirective
}

func parseToolVersions(path string) map[string]string {
	out := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			out[fields[0]] = normalizeVersion(fields[1])
		}
	}
	return out
}

// readVersionFile reads single-line version files like .nvmrc. Alias values
// ("lts/iron", "system") carry no comparable version and are ignored.
func readVersionFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	v := normalizeVersion(strings.TrimSpace(strings.SplitN(string(data), "\n", 2)[0]))
	if v == "" || !isDigit(v[0]) {
		return ""
	}
	return v
}

var versionRe = regexp.MustCompile(`[0-9]+(\.[0-9]+)*`)

func enginesNode(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return ""
	}
	var pkg struct {
		Engines struct {
			Node string `json:"node"`
		} `json:"engines"`
	}
	if json.Unmarshal(data, &pkg) != nil || pkg.Engines.Node == "" {
		return ""
	}
	// Ranges like ">=18.17 <19" or "^20.x": the leading concrete version is
	// the intent; exact range semantics are not needed to pick a runtime.
	return versionRe.FindString(pkg.Engines.Node)
}

// --- version helpers ---

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

// segsMatch reports whether the first n segments of got equal want's leading
// segments (bounded by how many segments want actually has).
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

// compareVersions returns <0 when a is newer than b (sort-best-first order).
func compareVersions(a, b string) int {
	as, bs := versionSegs(a), versionSegs(b)
	for i := 0; i < len(as) && i < len(bs); i++ {
		if as[i] != bs[i] {
			return bs[i] - as[i]
		}
	}
	return len(bs) - len(as)
}

// --- env helpers ---

func envValue(env []string, key string) string {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return env[i][len(prefix):]
		}
	}
	return ""
}

// ensurePathDirs appends dirs missing from path — the bridge may run from a
// GUI launch context whose PATH lacks the usual install locations (mirrors
// the git adapter's PATH patch, applied consistently here).
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

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
