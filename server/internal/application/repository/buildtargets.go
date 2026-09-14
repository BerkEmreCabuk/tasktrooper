package repository

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// DetectBuildTargets reads what a mobile working copy calls the thing that
// produces its release artifact — the Xcode scheme to archive and the Gradle
// module to bundle — the same way DetectAppIdentity reads its store
// identifiers: marker files, no network, no LLM.
//
// It is read at import, and stored, for the reason DetectAppIdentity is: this
// is the one moment the code is guaranteed to be on the disk of the host doing
// the reading, while the release that needs the answer is started from a
// shared agent-server that may hold no working copy at all.
//
// Every rule below refuses rather than guesses, and it refuses harder than
// DetectAppIdentity does. An unread bundle id leaves a form field empty for a
// human to fill; an unread scheme has no form behind it — the value goes
// straight into `xcodebuild -scheme` in the generated release script, so the
// only alternative to "" is a convention presented as a fact. "The scheme is
// named after the app" and "the module is called app" are true often enough to
// look right and wrong often enough to archive a target that does not exist,
// which fails a hundred lines into a build log with nothing naming the cause.
//
// Never returns an error, for the same reason DetectAppIdentity does not: a
// repository whose Xcode or Gradle files cannot be read is not a failed
// import, it is a repository whose build targets nobody could read.
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

// platformDirs lists where a mobile project keeps one platform's build files:
// at the root for a native repo, and under ios/ or android/ for the
// cross-platform layout Flutter, React Native, Capacitor and Cordova all ship.
// Most specific first is meaningless here — the two are mutually exclusive in
// practice — but the root is tried first so a native repo never has a stray
// ios/ folder consulted ahead of its own project.
func platformDirs(root, platform string) []string {
	return []string{root, filepath.Join(root, platform)}
}

// schemeRe and moduleRe are the shapes these two values must have to survive
// the generator, which matches rather than escapes them (see
// pipeline.Render): a scheme name that needs shell escaping is a mis-read
// scheme, and answering with it would only move the surprise to the build.
// Kept as a local copy rather than imported — application/repository must not
// depend on the release generator to know what a scheme looks like.
var (
	schemeRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 ._-]{0,99}$`)
	moduleRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(:[A-Za-z0-9][A-Za-z0-9._-]*)*$`)
)

// detectXcodeScheme answers the Apple half. The SHARED schemes are the
// preferred source and not merely the first one tried: a scheme that is not
// shared lives in the developer's own xcuserdata and is not in the repository
// at all, so CI cannot archive it however confidently it was named. Where the
// project shares schemes, the answer comes from that list alone — falling back
// to the project's name there would answer with a scheme the repository does
// not have.
//
// The container's own name is consulted only when nothing shares a scheme,
// which is Xcode's default state for a project nobody has ticked the box on.
// It is a weaker answer, but it is the name Xcode itself generates the scheme
// from, so it is a reading of the tree rather than a convention imposed on it.
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

// xcodeContainers lists the .xcworkspace and .xcodeproj directories directly
// inside dir. One level only: deeper is Pods/, Carthage checkouts and build
// output, none of which is the app.
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

// sharedSchemes collects the scheme names checked into the repository, which
// is exactly what xcshareddata/xcschemes holds — the file name is the scheme
// name. Deduplicated because a workspace and the project inside it routinely
// share one.
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

// containerNames is the .xcworkspace / .xcodeproj base names, deduplicated —
// a project and the workspace wrapping it almost always share one, and where
// they do that single name is the answer.
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

// pickScheme narrows a list of candidate scheme names to the one that archives
// the app, and answers "" the moment it cannot. The only narrowing rule is
// auxiliaryTarget's: a name ending in a test-bundle or app-extension suffix is
// not the app, which is what Xcode's own templates produce beside every app
// scheme (RunnerTests, MyAppUITests, MyAppWidgetExtension).
//
// Anything still ambiguous after that is left unanswered on purpose. Two
// shared schemes with unrelated names are two apps, or an app and something
// nobody labelled, and picking either would be a coin flip whose losing side
// is a silent build failure.
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

// detectGradleModule answers the Android half: the module that applies
// com.android.application, since that plugin is what produces an AAB and
// nothing else in a Gradle build does. The module list comes from
// settings.gradle rather than from a directory scan because Gradle itself
// takes it from there — a folder holding a build.gradle that settings.gradle
// never includes is not part of the build.
//
// Two candidates is an honest "" and not a tie to break: a tree with an app
// and a Wear app, or two flavour modules, has two things that both bundle, and
// the release script can only run one.
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

// includedModules lists the Gradle modules settings.gradle declares, and falls
// back to the single conventional one when there is no settings file to read.
// That fallback is not a guess about naming: `app` is the only directory
// detectGradleModule will then look at, and it still has to apply the
// application plugin to be answered with.
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
	// includeRe matches `include ':app', ':wear'` and `include(":app")`,
	// including the parenthesised form spread over several lines. `includeBuild`
	// cannot match it: a letter follows `include` there, and neither branch
	// allows one.
	includeRe = regexp.MustCompile(`(?m)include\s*\(([^)]*)\)|^\s*include\s+([^\n]*)`)
	quotedRe  = regexp.MustCompile(`["']([^"']*)["']`)
)

// parseIncludes reads the module paths out of a settings file, normalised to
// the form the release script interpolates: no leading colon, colons kept
// between the segments of a nested module (`:apps:android` → `apps:android`,
// which is the <module> in :<module>:bundleRelease).
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

// stripGradleComments blanks out what Gradle would not execute, so a module
// somebody commented out is not read back as included.
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

// moduleDir maps a Gradle module path to the directory it builds from, which
// is Gradle's own default. A settings file that re-points a module with
// project(':x').projectDir is not followed: that is a rare enough layout that
// mis-reading it is likelier than reading it, and the answer this produces is
// then simply a directory with no application plugin in it, i.e. "".
func moduleDir(dir, module string) string {
	return filepath.Join(dir, filepath.FromSlash(strings.ReplaceAll(module, ":", "/")))
}

// androidAppPluginRe matches the four ways a module declares itself an Android
// APPLICATION: the Groovy and Kotlin `id` forms, the legacy `apply plugin:`,
// and a version-catalog alias (libs.plugins.android.application, or the
// camelCase alias name the Android Studio templates now generate). The library,
// dynamic-feature and test plugins deliberately match none of them.
var androidAppPluginRe = regexp.MustCompile(`com\.android\.application\b|plugins\.android[._]?[Aa]pplication\b`)

// appliesAndroidApplication reports whether the module at dir builds an
// Android app. Comments are stripped first: a module that used to be the app
// and says so in a comment is not the app.
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
