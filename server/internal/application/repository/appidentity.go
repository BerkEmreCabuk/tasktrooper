package repository

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// DetectAppIdentity reads a mobile working copy's store identifiers off disk —
// the iOS bundle id and the Android package name — the same way DetectRepoKind
// and DetectMobilePlatform read its shape: marker files, no network, no LLM.
//
// It exists so the deploy settings screen can PREFILL the store identity
// fields instead of asking a human to retype what the build files already say;
// a wrong prefill is worse than none, so every rule below refuses rather than
// guesses, and both halves answer "" independently.
//
// Never returns an error: a repository with an unparseable, half-written or
// unreadable build file is not a failed import, it is a repository whose
// identifiers nobody could read.
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

// maxIdentityFileSize caps what this file will read into memory. A project.pbxproj
// of a large app runs into the low megabytes; anything past this is not one.
const maxIdentityFileSize = 8 << 20

// detectPackageName answers the Android half: applicationId from Gradle, and
// only then the manifest's package attribute. That order is the shipping
// order — applicationId is what lands on Play, while the manifest package is
// the code's namespace and merely coincides with it on most projects (and is
// absent entirely on AGP 8 projects, which moved it to `namespace`).
//
// Both the Flutter layout (android/app/…) and a plain Android repo (app/… at
// the root) are covered, most specific first.
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

// detectBundleID answers the Apple half: PRODUCT_BUNDLE_IDENTIFIER from the
// Xcode project, and only then Info.plist — which on every modern project
// holds $(PRODUCT_BUNDLE_IDENTIFIER) rather than a literal, and is therefore
// the fallback for the hand-written projects that still spell it out.
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

// infoPlistPaths lists where an Info.plist worth reading lives: Flutter's
// ios/Runner first, then any other target folder under ios/, then the target
// folders of a native iOS repo whose project sits at the root. One level down
// only — deeper is Pods/, build output and vendored copies.
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

// applicationIDPattern matches `applicationId "com.x"` (Groovy) and
// `applicationId = "com.x"` (Kotlin DSL). applicationIdSuffix cannot match it:
// the quote has to follow the name, and there a letter does.
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

// manifestPackage reads the package attribute off the <manifest> element and
// nowhere else: <queries> ships <package android:name="…"/> children, and
// answering with one of those would name somebody else's app.
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

// pbxBundleIDs collects every literal PRODUCT_BUNDLE_IDENTIFIER in a
// project.pbxproj, in file order. There is one per build configuration per
// target, so a Flutter project alone yields six — pickBundleID decides which
// of them is the app.
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

// pickBundleID chooses the app's own identifier out of every target's. Two
// rules, in order: an id that extends another one belongs to something the app
// ships INSIDE it (com.acme.app.RunnerTests, .widget, .NotificationService),
// and a target whose last segment names a test bundle or an app extension is
// not the app either. Shortest wins among what is left, which is the app on
// every layout where the extensions were named independently.
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

// auxiliarySuffixes are the last-segment names Xcode's own templates give to
// something that is not the app: test bundles, app extensions, watch targets,
// app clips.
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

// plausibleAppID rejects everything that is not a literal identifier: a build
// variable ($(PRODUCT_BUNDLE_IDENTIFIER), ${applicationId}), an interpolated
// flavour, a placeholder with spaces in it, or a bare word with no dot. A
// prefill the user has to delete is worse than an empty field.
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
