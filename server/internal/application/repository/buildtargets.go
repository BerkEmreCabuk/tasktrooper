package repository

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func DetectBuildTargets(rootPath string) domain.BuildTargets {
	root := strings.TrimSpace(rootPath)
	if root == "" {
		return domain.BuildTargets{}
	}
	return domain.BuildTargets{
		XcodeScheme:  detectXcodeScheme(root),
		GradleModule: detectGradleModule(root),
	}
}

func platformDirs(root, platform string) []string {
	return []string{root, filepath.Join(root, platform)}
}

var (
	schemeRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,99}$`)
	moduleRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(:[A-Za-z0-9][A-Za-z0-9._-]*)*$`)
)

func detectXcodeScheme(root string) string {
	for _, dir := range platformDirs(root, "ios") {
		containers := xcodeContainers(dir)
		if len(containers) == 0 {
			continue
		}
		if shared := sharedSchemes(containers); len(shared) > 0 {
			return pickScheme(shared)
		}
		return pickScheme(containerNames(containers))
	}
	return ""
}

func xcodeContainers(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".xcworkspace") || strings.HasSuffix(e.Name(), ".xcodeproj") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

func sharedSchemes(containers []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, container := range containers {
		entries, err := os.ReadDir(filepath.Join(container, "xcshareddata", "xcschemes"))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".xcscheme") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".xcscheme")
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}

func containerNames(containers []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, container := range containers {
		name := strings.TrimSuffix(filepath.Base(container), filepath.Ext(container))
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

func pickScheme(candidates []string) string {
	var primary []string
	for _, c := range candidates {
		name := strings.TrimSpace(c)
		if name == "" || auxiliaryTarget(name) || !schemeRe.MatchString(name) {
			continue
		}
		primary = append(primary, name)
	}
	if len(primary) != 1 {
		return ""
	}
	return primary[0]
}

func detectGradleModule(root string) string {
	for _, dir := range platformDirs(root, "android") {
		var apps []string
		for _, module := range includedModules(dir) {
			if appliesAndroidApplication(moduleDir(dir, module)) {
				apps = append(apps, module)
			}
		}
		if len(apps) == 1 {
			return apps[0]
		}
		if len(apps) > 1 {
			return ""
		}
	}
	return ""
}

func includedModules(dir string) []string {
	body := ""
	for _, name := range []string{"settings.gradle", "settings.gradle.kts"} {
		if text, ok := readIdentityFile(filepath.Join(dir, name)); ok {
			body += "\n" + text
		}
	}
	if strings.TrimSpace(body) == "" {
		return []string{"app"}
	}
	modules := parseIncludes(body)
	if len(modules) == 0 {
		return []string{"app"}
	}
	return modules
}

var (
	blockCommentRe = regexp.MustCompile(`(?s)/\*.*?\*/`)

	includeRe = regexp.MustCompile(`(?m)include\s*\(([^)]*)\)|^\s*include\s+([^\n]*)`)
	quotedRe  = regexp.MustCompile(`["']([^"']*)["']`)
)

func parseIncludes(body string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range includeRe.FindAllStringSubmatch(stripGradleComments(body), -1) {
		args := m[1] + m[2]
		for _, q := range quotedRe.FindAllStringSubmatch(args, -1) {
			module := strings.Trim(strings.TrimSpace(q[1]), ":")
			if module == "" || seen[module] || !moduleRe.MatchString(module) {
				continue
			}
			seen[module] = true
			out = append(out, module)
		}
	}
	return out
}

func stripGradleComments(body string) string {
	body = blockCommentRe.ReplaceAllString(body, " ")
	var b strings.Builder
	for _, line := range strings.Split(body, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func moduleDir(dir, module string) string {
	return filepath.Join(dir, filepath.FromSlash(strings.ReplaceAll(module, ":", "/")))
}

var androidAppPluginRe = regexp.MustCompile(`com\.android\.application\b|plugins\.android[._]?[Aa]pplication\b`)

func appliesAndroidApplication(dir string) bool {
	for _, name := range []string{"build.gradle", "build.gradle.kts"} {
		body, ok := readIdentityFile(filepath.Join(dir, name))
		if !ok {
			continue
		}
		if androidAppPluginRe.MatchString(stripGradleComments(body)) {
			return true
		}
	}
	return false
}
