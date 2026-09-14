package domain

import (
	"errors"
	"fmt"
	"time"
)

// AgentCLIFlavor names one local agent CLI by the on-disk conventions its
// catalog is rendered in — the same vocabulary application/agentfs uses, and
// deliberately NOT the provider type.
//
// The two are one-to-one today and will not stay that way for free: a flavor is
// a file layout (.claude/skills/<name>/SKILL.md versus .cursor/rules/<name>.mdc)
// and a provider is an execution engine, and the moment a second binary reads
// the Claude layout the mapping stops being an identity. Keeping the URL, the
// stored row and the renderer on the same word means that day changes one
// table here instead of every caller.
type AgentCLIFlavor string

const (
	AgentCLIFlavorClaude      AgentCLIFlavor = "claude"
	AgentCLIFlavorCursor      AgentCLIFlavor = "cursor"
	AgentCLIFlavorAntigravity AgentCLIFlavor = "antigravity"
	AgentCLIFlavorOpencode    AgentCLIFlavor = "opencode"
)

// agentCLIFlavors is the complete, ordered set. Ordered because it is what the
// settings page renders, and a set whose order came out of a map would reshuffle
// the two cards between page loads.
var agentCLIFlavors = []AgentCLIFlavor{AgentCLIFlavorClaude, AgentCLIFlavorCursor, AgentCLIFlavorAntigravity, AgentCLIFlavorOpencode}

// AgentCLIFlavors returns every flavor the product declares, available or not.
//
// Unavailable ones are included for the same reason cursor_agent is a declared
// provider: the list says which CLIs this product runs, and one that silently
// omits the half-built one reads as complete.
func AgentCLIFlavors() []AgentCLIFlavor {
	out := make([]AgentCLIFlavor, len(agentCLIFlavors))
	copy(out, agentCLIFlavors)
	return out
}

// agentCLIProviders maps each flavor onto the provider an agent selects to run
// on it.
var agentCLIProviders = map[AgentCLIFlavor]LLMProviderType{
	AgentCLIFlavorClaude:      LLMProviderClaudeCode,
	AgentCLIFlavorCursor:      LLMProviderCursorAgent,
	AgentCLIFlavorAntigravity: LLMProviderAntigravity,
	AgentCLIFlavorOpencode:    LLMProviderOpencode,
}

// AgentCLIProviderFor resolves a flavor to its provider, and reports false for
// a flavor this server does not know.
func AgentCLIProviderFor(f AgentCLIFlavor) (LLMProviderType, bool) {
	p, ok := agentCLIProviders[f]
	return p, ok
}

// AgentCLIFlavorFor is the reverse: which CLI a provider's runs are executed by.
// False for every provider that is an HTTP endpoint.
func AgentCLIFlavorFor(t LLMProviderType) (AgentCLIFlavor, bool) {
	for flavor, provider := range agentCLIProviders {
		if provider == t {
			return flavor, true
		}
	}
	return "", false
}

// ValidAgentCLIFlavor reports whether f names a flavor at all. A flavor that is
// merely unknown is a 400 with a different sentence from one that is known and
// unavailable, and the caller needs to tell them apart.
func ValidAgentCLIFlavor(f AgentCLIFlavor) bool {
	_, ok := agentCLIProviders[f]
	return ok
}

// AgentCLIConnection is one local agent CLI the tenant has connected. A tenant
// may hold several at once, one per flavor (see AgentCLIState.Connections) —
// each AGENT already names its own provider, so "which CLI runs my board" was
// always really "which CLI runs THIS agent's tasks", answered per agent. A
// tenant with a Claude Code agent and a Cursor agent gains nothing from being
// forced to disconnect one to use the other.
//
// Multiple connected flavors do not collide on disk: application/agentfs
// materialises a TASK's catalog into that task's own workspace at dispatch,
// never into a directory two flavors would both write, and this row's own
// CatalogPath (see below) is namespaced per flavor for the same reason.
//
// The fields past the flavor are evidence rather than configuration: nothing
// reads BinaryPath to run anything (the executor resolves its own binary at
// boot), and nothing reads CatalogPath to build a run (see the note on
// CatalogPath). They are here so the settings page can show WHAT was verified,
// and so an operator debugging a bad run can see whether the catalog they are
// looking at came from this connect or from one three weeks ago.
type AgentCLIConnection struct {
	Flavor        AgentCLIFlavor  `json:"flavor"`
	ProviderType  LLMProviderType `json:"provider_type"`
	BinaryPath    string          `json:"binary_path"`
	BinaryVersion string          `json:"binary_version"`
	// CatalogPath is where connect wrote its snapshot of every enabled agent's
	// catalog. It is NOT what a board run reads: a run materialises its own
	// agent into its own task workspace, from the database, at dispatch. See
	// application/agentcli.Service.Connect.
	CatalogPath string    `json:"catalog_path"`
	AgentCount  int       `json:"agent_count"`
	SkillCount  int       `json:"skill_count"`
	ConnectedAt time.Time `json:"connected_at"`
}

// AgentCLIFlavorView is one card on the settings page.
type AgentCLIFlavorView struct {
	Flavor       AgentCLIFlavor  `json:"flavor"`
	ProviderType LLMProviderType `json:"provider_type"`
	Label        string          `json:"label"`
	Available    bool            `json:"available"`
	Connected    bool            `json:"connected"`
}

// AgentCLIState is what every endpoint in this surface answers with — connect,
// disconnect and read alike, so the client never has to merge a mutation's
// result into a list it fetched separately.
type AgentCLIState struct {
	// Connections is empty when no CLI is connected, which is a normal state
	// and not an error: a tenant running entirely on API providers never
	// connects one. It may hold more than one entry — connecting a flavor no
	// longer disconnects any other.
	Connections []AgentCLIConnection `json:"connections"`
	Flavors     []AgentCLIFlavorView `json:"flavors"`
}

// ErrAgentCLIBinaryMissing marks the refusal for a CLI that is not installed
// where the sessions would run.
//
// It is a separate sentinel from ErrAgentCLIUnauthenticated because the two
// have different fixes and the user cannot guess which one they are looking at
// from a shared message. "Install the CLI" and "sign the CLI in" send a person
// to different places, and a single "connect failed" sends them to neither.
//
// "where the sessions would run" rather than "on this host", and the wording is
// load-bearing: this text is the TAIL of the message a user reads, and on a
// cloud deployment the host is a Linux pod while the CLI belongs on their Mac.
// Saying "this host" there described a machine they cannot see and sent them to
// install something on it.
var ErrAgentCLIBinaryMissing = errors.New("agent cli binary is not installed where the sessions run")

// ErrAgentCLIUnauthenticated marks the refusal for a CLI that IS installed and
// has no usable subscription session. See ErrAgentCLIBinaryMissing for why this
// is its own sentinel.
var ErrAgentCLIUnauthenticated = errors.New("agent cli binary is installed but not signed in")

// ErrAgentCLINotConnected marks every refusal caused by dispatching work onto a
// host-executed provider whose CLI this tenant has not connected.
//
// Permanent, like ErrProviderUnavailable: nothing about it becomes true on a
// retry — somebody has to press the button — so a caller inside a retry loop
// must stop rather than back off.
var ErrAgentCLINotConnected = errors.New("agent cli flavor is not the connected one")

// ErrCLIFlavorNotConnected is what the board runner answers with when an
// agent's provider is a local CLI that is not itself connected.
//
// The sentence names the two facts a person needs and no others: which agent,
// and which CLI it wants. The alternative — dispatching anyway — hands the
// task to a binary nobody verified is installed or signed in, and the failure
// then arrives from inside a CLI session as whatever that binary says about
// its own missing credentials.
func ErrCLIFlavorNotConnected(agentName string, want LLMProviderType) error {
	label := string(want)
	if def, ok := LLMProviderDefinitionFor(want); ok && def.Label != "" {
		label = def.Label
	}
	return fmt.Errorf("agent %q runs on %s, which is not connected: connect it under LLM settings, "+
		"or move the agent to an API-backed provider: %w", agentName, label, ErrAgentCLINotConnected)
}
