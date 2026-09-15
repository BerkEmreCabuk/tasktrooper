package domain

import (
	"regexp"
	"sort"
	"strings"
)

// sessionEnvNames is every name TaskExecution.Env may set.
//
// This parameter exists for version pins and nothing else. The question that
// decides a name is not "is this useful" but "can a value for it change what
// the session RUNS" — and for anything that names a path to a program, a
// library, a config file or a proxy, the answer is yes. Version pins are
// resolved by a version manager the session already trusts; the knobs below
// change how a tool talks, never what it runs.
var sessionEnvNames = map[string]bool{
	"GOTOOLCHAIN":      true,
	"NODE_VERSION":     true,
	"PYTHON_VERSION":   true,
	"RUBY_VERSION":     true,
	"JAVA_VERSION":     true,
	"FLUTTER_VERSION":  true,
	"RUST_TOOLCHAIN":   true,
	"RUSTUP_TOOLCHAIN": true,

	"CI":                  true,
	"TERM":                true,
	"NO_COLOR":            true,
	"FORCE_COLOR":         true,
	"DEBIAN_FRONTEND":     true,
	"LANG":                true,
	"LC_ALL":              true,
	"TZ":                  true,
	"npm_config_loglevel": true,
}

// sessionEnvPrefix is TaskTrooper's own family, so a task can be handed its own
// values without this list being widened again: nothing outside this product
// reads TT_*.
const sessionEnvPrefix = "TT_"

const (
	maxSessionEnvEntries  = 64
	maxSessionEnvValueLen = 4096
)

var sessionEnvName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// SessionEnvAllowed reports whether name may be set on a session through
// TaskExecution.Env. Nothing is inferred from the shape of a name.
func SessionEnvAllowed(name string) bool {
	if sessionEnvNames[name] {
		return true
	}
	return len(name) > len(sessionEnvPrefix) && strings.HasPrefix(name, sessionEnvPrefix) &&
		sessionEnvName.MatchString(name)
}

// SessionEnv turns env into exec's "NAME=value" form, sorted so two identical
// requests produce an identical child. Names outside the allowlist, values too
// long to be a pin and values exec cannot carry (a NUL) are returned in refused
// instead, for the caller to log; they are never passed on.
func SessionEnv(env map[string]string) (allowed, refused []string) {
	names := make([]string, 0, len(env))
	for name := range env {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value := env[name]
		switch {
		case len(allowed) >= maxSessionEnvEntries,
			!SessionEnvAllowed(name),
			len(value) > maxSessionEnvValueLen,
			strings.ContainsRune(value, 0):
			refused = append(refused, name)
		default:
			allowed = append(allowed, name+"="+value)
		}
	}
	return allowed, refused
}
