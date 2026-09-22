package toolchain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Pin struct {
	Language string

	Version string

	Exact bool

	Source string
}

const pinFileLimit = 1024 * 1024

const pinValueLimit = 128

const maxPins = 128

var exactVersion = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*([.-][A-Za-z0-9][A-Za-z0-9._+-]*)?$`)

var rustChannel = regexp.MustCompile(`^(stable|beta|nightly)(-[0-9]{4}-[0-9]{2}-[0-9]{2})?$`)

var toolAliases = map[string]string{
	"nodejs": "node",
	"golang": "go",
	"rustc":  "rust",
	"jdk":    "java",
}

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
