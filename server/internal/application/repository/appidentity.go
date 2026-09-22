package repository

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func DetectAppIdentity(rootPath string) domain.AppIdentity {
	root := strings.TrimSpace(rootPath)
	if root == "" {
		return domain.AppIdentity{}
	}
	return domain.AppIdentity{
		BundleID:    detectBundleID(root),
		PackageName: detectPackageName(root),
	}
}

const maxIdentityFileSize = 8 << 20

func detectPackageName(root string) string {
	for _, rel := range []string{
		"android/app/build.gradle", "android/app/build.gradle.kts",
		"app/build.gradle", "app/build.gradle.kts",
		"android/build.gradle", "android/build.gradle.kts",
		"build.gradle", "build.gradle.kts",
	} {
		body, ok := readIdentityFile(filepath.Join(root, filepath.FromSlash(rel)))
		if !ok {
			continue
		}
		if id := gradleApplicationID(body); id != "" {
			return id
		}
	}
	for _, rel := range []string{
		"android/app/src/main/AndroidManifest.xml",
		"app/src/main/AndroidManifest.xml",
		"src/main/AndroidManifest.xml",
		"AndroidManifest.xml",
	} {
		body, ok := readIdentityFile(filepath.Join(root, filepath.FromSlash(rel)))
		if !ok {
			continue
		}
		if id := manifestPackage(body); id != "" {
			return id
		}
	}
	return ""
}

func detectBundleID(root string) string {
	for _, dir := range []string{root, filepath.Join(root, "ios")} {
		for _, proj := range xcodeProjects(dir) {
			body, ok := readIdentityFile(filepath.Join(proj, "project.pbxproj"))
			if !ok {
				continue
			}
			if id := pickBundleID(pbxBundleIDs(body)); id != "" {
				return id
			}
		}
	}
	for _, path := range infoPlistPaths(root) {
		body, ok := readIdentityFile(path)
		if !ok {
			continue
		}
		if id := plistBundleID(body); id != "" {
			return id
		}
	}
	return ""
}

func xcodeProjects(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && strings.HasSuffix(e.Name(), ".xcodeproj") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

func infoPlistPaths(root string) []string {
	paths := []string{filepath.Join(root, "ios", "Runner", "Info.plist")}
	for _, dir := range []string{filepath.Join(root, "ios"), root} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || skipDir(e.Name()) {
				continue
			}
			paths = append(paths, filepath.Join(dir, e.Name(), "Info.plist"))
		}
	}
	return append(paths, filepath.Join(root, "Info.plist"))
}

var applicationIDPattern = regexp.MustCompile(`applicationId\s*=?\s*["']([^"']*)["']`)

func gradleApplicationID(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		m := applicationIDPattern.FindStringSubmatch(trimmed)
		if m != nil && plausibleAppID(m[1]) {
			return m[1]
		}
	}
	return ""
}

var (
	manifestTagPattern     = regexp.MustCompile(`(?s)<manifest\b[^>]*>`)
	manifestPackagePattern = regexp.MustCompile(`(?:^|\s)package\s*=\s*"([^"]*)"`)
)

func manifestPackage(body string) string {
	tag := manifestTagPattern.FindString(body)
	if tag == "" {
		return ""
	}
	m := manifestPackagePattern.FindStringSubmatch(tag)
	if m == nil || !plausibleAppID(m[1]) {
		return ""
	}
	return m[1]
}

var pbxBundleIDPattern = regexp.MustCompile(`PRODUCT_BUNDLE_IDENTIFIER\s*=\s*("[^"\n]*"|[^;\n]*);`)

func pbxBundleIDs(body string) []string {
	var out []string
	for _, m := range pbxBundleIDPattern.FindAllStringSubmatch(body, -1) {
		value := strings.Trim(strings.TrimSpace(m[1]), `"`)
		if plausibleAppID(value) {
			out = append(out, value)
		}
	}
	return out
}

func pickBundleID(candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	primary := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if extendsAnother(c, candidates) || auxiliaryTarget(c) {
			continue
		}
		primary = append(primary, c)
	}
	if len(primary) == 0 {
		primary = candidates
	}
	best := primary[0]
	for _, c := range primary[1:] {
		if len(c) < len(best) {
			best = c
		}
	}
	return best
}

func extendsAnother(candidate string, all []string) bool {
	for _, other := range all {
		if other != candidate && strings.HasPrefix(candidate, other+".") {
			return true
		}
	}
	return false
}

var auxiliarySuffixes = []string{
	"test", "tests", "uitest", "uitests", "testing",
	"widget", "widgets", "widgetextension", "extension",
	"notificationservice", "notificationcontent", "shareextension", "todayextension",
	"watchkitapp", "watchkitextension", "intents", "intentsui", "clip",
}

func auxiliaryTarget(id string) bool {
	last := id
	if i := strings.LastIndex(id, "."); i >= 0 {
		last = id[i+1:]
	}
	last = strings.ToLower(last)
	for _, suffix := range auxiliarySuffixes {
		if strings.HasSuffix(last, suffix) {
			return true
		}
	}
	return false
}

var plistStringPattern = regexp.MustCompile(`(?s)<string>(.*?)</string>`)

func plistBundleID(body string) string {
	idx := strings.Index(body, "<key>CFBundleIdentifier</key>")
	if idx < 0 {
		return ""
	}
	m := plistStringPattern.FindStringSubmatch(body[idx:])
	if m == nil {
		return ""
	}
	value := strings.TrimSpace(m[1])
	if !plausibleAppID(value) {
		return ""
	}
	return value
}

func plausibleAppID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) < 3 || !strings.Contains(value, ".") {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return !strings.HasPrefix(value, ".") && !strings.HasSuffix(value, ".")
}

func readIdentityFile(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > maxIdentityFileSize {
		return "", false
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(body), true
}
