package toolchain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Pin is one version declaration a checkout makes, with the file that made it.
type Pin struct {
	// Language is normalised and lowercase: go, node, python, ruby, java, rust,
	// flutter, dart — or whatever a `.tool-versions` line named, unchanged.
	Language string
	// Version is what the file says, verbatim. A range stays a range.
	Version string
	// Exact is false for a constraint: ">=3.11", "^20" and "lts/hydrogen" name
	// a set of runtimes, and passing a set where a version is expected is a
	// guess dressed as a fact.
	Exact bool
	// Source is the file this came from, relative to the checkout.
	Source string
}

// pinFileLimit bounds one pin file, so a file in the checkout cannot be an
// allocation primitive. The largest of them is a package.json.
const pinFileLimit = 1024 * 1024

const pinValueLimit = 128

// maxPins bounds the answer: `.tool-versions` may name any tool, so the count
// is the repository's to choose.
const maxPins = 128

var exactVersion = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*([.-][A-Za-z0-9][A-Za-z0-9._+-]*)?$`)

// rustChannel is the other shape of an exact answer: rustup takes a channel
// name, so `stable` and a dated nightly are pins in rustup's own terms.
var rustChannel = regexp.MustCompile(`^(stable|beta|nightly)(-[0-9]{4}-[0-9]{2}-[0-9]{2})?$`)

var toolAliases = map[string]string{
	"nodejs": "node",
	"golang": "go",
	"rustc":  "rust",
	"jdk":    "java",
}

// ReadPins reports what the checkout at dir DECLARES, in precedence order per
// language: a version manager's own file first, then the language's dotfile,
// then a manifest's constraint. Two files pinning one language both appear, so
// a disagreement is visible rather than silently resolved.
//
// Only files whose purpose is to state a version are read. A directory full of
// `.py` says somebody wrote Python, not which Python, and a version invented
// here would be passed on as a pin. Absence is absence: a language with no pin
// file does not appear at all. Only dir itself is read, never below it — a
// monorepo pins per package, and walking would mean choosing which answer is
// the repository's.
func ReadPins(dir string) []Pin {
	r := pinReader{dir: dir}
	var pins []Pin
	for _, source := range pinSources {
		pins = append(pins, source(r)...)
		if len(pins) > maxPins {
			return pins[:maxPins]
		}
	}
	return pins
}

type pinReader struct{ dir string }

// text reads one pin file. A symlink anywhere on the way is skipped rather than
// followed: the names are this package's own, so a link at one of them was put
// there by the checkout, and following it would read a file outside the
// checkout through a name that looks like it is inside.
func (r pinReader) text(name string) (string, bool) {
	path := r.dir
	parts := strings.Split(filepath.ToSlash(name), "/")
	for i, part := range parts {
		path = filepath.Join(path, part)
		info, err := os.Lstat(path)
		if err != nil {
			return "", false
		}
		if i < len(parts)-1 {
			if !info.IsDir() {
				return "", false
			}
			continue
		}
		if !info.Mode().IsRegular() || info.Size() > pinFileLimit {
			return "", false
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(raw), true
}

func pin(language, version, source string) []Pin {
	language = strings.ToLower(strings.TrimSpace(language))
	if alias, ok := toolAliases[language]; ok {
		language = alias
	}
	version = strings.TrimSpace(version)
	if language == "" || version == "" || len(version) > pinValueLimit {
		return nil
	}
	exact := exactVersion.MatchString(version) || (language == "rust" && rustChannel.MatchString(version))
	return []Pin{{Language: language, Version: version, Exact: exact, Source: source}}
}

var pinSources = []func(pinReader) []Pin{
	toolVersionsPins,
	misePins,
	goPins,
	nodePins,
	pythonPins,
	rubyPins,
	javaPins,
	rustPins,
	flutterPins,
	pubspecPins,
	packageJSONPins,
	pyprojectPins,
	gemfilePins,
}

// toolVersionsPins reads asdf's and mise's shared format. Extra versions on a
// line are fallbacks, and the first is the one that would be used.
func toolVersionsPins(r pinReader) []Pin {
	text, ok := r.text(".tool-versions")
	if !ok {
		return nil
	}
	var out []Pin
	for _, line := range strings.Split(text, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		out = append(out, pin(fields[0], fields[1], ".tool-versions")...)
	}
	return out
}

func misePins(r pinReader) []Pin {
	for _, name := range []string{"mise.toml", ".mise.toml"} {
		text, ok := r.text(name)
		if !ok {
			continue
		}
		var out []Pin
		for _, entry := range tomlTable(text, "tools") {
			out = append(out, pin(entry.key, firstTOMLValue(entry.value), name)...)
		}
		return out
	}
	return nil
}

// goPins reads go.mod, then `.go-version`. go.mod's `toolchain` line is already
// a toolchain name (`go1.24.3`) and is the most precise statement a go.mod can
// make, so it is kept exact even though its shape is not exactVersion's.
func goPins(r pinReader) []Pin {
	var out []Pin
	if text, ok := r.text("go.mod"); ok {
		var toolchainLine, languageLine string
		for _, line := range strings.Split(text, "\n") {
			fields := strings.Fields(line)
			if len(fields) != 2 {
				continue
			}
			switch fields[0] {
			case "toolchain":
				toolchainLine = fields[1]
			case "go":
				languageLine = fields[1]
			}
		}
		if strings.HasPrefix(toolchainLine, "go") && exactVersion.MatchString(strings.TrimPrefix(toolchainLine, "go")) {
			out = append(out, Pin{Language: "go", Version: toolchainLine, Exact: true, Source: "go.mod"})
		} else {
			out = append(out, pin("go", languageLine, "go.mod")...)
		}
	}
	if text, ok := r.text(".go-version"); ok {
		out = append(out, pin("go", firstLine(text), ".go-version")...)
	}
	return out
}

func nodePins(r pinReader) []Pin {
	var out []Pin
	for _, name := range []string{".nvmrc", ".node-version"} {
		if text, ok := r.text(name); ok {
			out = append(out, pin("node", firstLine(text), name)...)
		}
	}
	return out
}

func pythonPins(r pinReader) []Pin {
	if text, ok := r.text(".python-version"); ok {
		return pin("python", firstLine(text), ".python-version")
	}
	return nil
}

func rubyPins(r pinReader) []Pin {
	if text, ok := r.text(".ruby-version"); ok {
		return pin("ruby", firstLine(text), ".ruby-version")
	}
	return nil
}

func javaPins(r pinReader) []Pin {
	var out []Pin
	if text, ok := r.text(".java-version"); ok {
		out = append(out, pin("java", firstLine(text), ".java-version")...)
	}
	if text, ok := r.text(".sdkmanrc"); ok {
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") {
				continue
			}
			key, value, found := strings.Cut(line, "=")
			if found && strings.TrimSpace(key) == "java" {
				out = append(out, pin("java", value, ".sdkmanrc")...)
			}
		}
	}
	return out
}

func rustPins(r pinReader) []Pin {
	if text, ok := r.text("rust-toolchain.toml"); ok {
		for _, entry := range tomlTable(text, "toolchain") {
			if entry.key == "channel" {
				return pin("rust", firstTOMLValue(entry.value), "rust-toolchain.toml")
			}
		}
		return nil
	}
	if text, ok := r.text("rust-toolchain"); ok {
		return pin("rust", firstLine(text), "rust-toolchain")
	}
	return nil
}

func flutterPins(r pinReader) []Pin {
	var out []Pin
	if text, ok := r.text(".fvmrc"); ok {
		var doc struct {
			Flutter string `json:"flutter"`
		}
		if json.Unmarshal([]byte(text), &doc) == nil {
			out = append(out, pin("flutter", doc.Flutter, ".fvmrc")...)
		}
	} else if text, ok := r.text(".fvm/fvm_config.json"); ok {
		var doc struct {
			SDKVersion string `json:"flutterSdkVersion"`
		}
		if json.Unmarshal([]byte(text), &doc) == nil {
			out = append(out, pin("flutter", doc.SDKVersion, ".fvm/fvm_config.json")...)
		}
	}
	if text, ok := r.text(".flutter-version"); ok {
		out = append(out, pin("flutter", firstLine(text), ".flutter-version")...)
	}
	return out
}

// pubspecPins reads the `environment` block, where both SDK constraints live.
// A top-level key ends the block, which is what keeps an `sdk:` under
// `dependencies:` from being read as the Dart version.
func pubspecPins(r pinReader) []Pin {
	text, ok := r.text("pubspec.yaml")
	if !ok {
		return nil
	}
	var out []Pin
	inEnvironment := false
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			inEnvironment = strings.HasPrefix(trimmed, "environment:")
			continue
		}
		if !inEnvironment {
			continue
		}
		key, value, found := strings.Cut(trimmed, ":")
		if !found {
			continue
		}
		version := strings.Trim(strings.TrimSpace(value), `"'`)
		switch strings.TrimSpace(key) {
		case "sdk":
			out = append(out, pin("dart", version, "pubspec.yaml")...)
		case "flutter":
			out = append(out, pin("flutter", version, "pubspec.yaml")...)
		}
	}
	return out
}

// packageJSONPins reads `engines.node` only: `npm`, `pnpm` and `yarn` there are
// package managers, not runtimes.
func packageJSONPins(r pinReader) []Pin {
	text, ok := r.text("package.json")
	if !ok {
		return nil
	}
	var doc struct {
		Engines map[string]string `json:"engines"`
	}
	if json.Unmarshal([]byte(text), &doc) != nil {
		return nil
	}
	if version, has := doc.Engines["node"]; has {
		return pin("node", version, "package.json")
	}
	return nil
}

func pyprojectPins(r pinReader) []Pin {
	text, ok := r.text("pyproject.toml")
	if !ok {
		return nil
	}
	for _, entry := range tomlTable(text, "project") {
		if entry.key == "requires-python" {
			return pin("python", firstTOMLValue(entry.value), "pyproject.toml")
		}
	}
	for _, entry := range tomlTable(text, "tool.poetry.dependencies") {
		if entry.key == "python" {
			return pin("python", firstTOMLValue(entry.value), "pyproject.toml")
		}
	}
	return nil
}

var gemfileRuby = regexp.MustCompile(`(?m)^\s*ruby\s+["']([^"']+)["']`)

func gemfilePins(r pinReader) []Pin {
	text, ok := r.text("Gemfile")
	if !ok {
		return nil
	}
	if m := gemfileRuby.FindStringSubmatch(text); m != nil {
		return pin("ruby", m[1], "Gemfile")
	}
	return nil
}

type tomlEntry struct{ key, value string }

// tomlTable returns the `key = value` lines inside one table and only that
// table. Not a TOML parser: it understands headers, `key = value` and comments,
// which is all these files use for the keys read here. It is table-aware so a
// `node = "20"` under `[env]` is not read as a tool pin, and anything more
// structured falls out as a value firstTOMLValue does not recognise — no pin
// rather than a wrong one.
func tomlTable(text, want string) []tomlEntry {
	var out []tomlEntry
	current := ""
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			current = strings.TrimSpace(strings.Trim(line, "[]"))
			continue
		}
		if current != want {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		if i := strings.Index(value, "#"); i >= 0 && !strings.Contains(value[:i], `"`) {
			value = value[:i]
		}
		out = append(out, tomlEntry{key: strings.Trim(strings.TrimSpace(key), `"'`), value: strings.TrimSpace(value)})
	}
	return out
}

// firstTOMLValue unwraps a quoted string, or the first element of an array of
// them: mise writes `node = ["20", "18"]` for a version and its fallbacks.
func firstTOMLValue(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "[") {
		value = strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")
		if comma := strings.Index(value, ","); comma >= 0 {
			value = value[:comma]
		}
		value = strings.TrimSpace(value)
	}
	return strings.Trim(value, `"'`)
}

// firstLine is a whole-file pin: the first non-blank, non-comment line. An
// `.nvmrc` is sometimes written `v20.11.0`; the `v` is nvm's prefix and no other
// tool accepts it.
func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		return strings.TrimPrefix(trimmed, "v")
	}
	return ""
}
