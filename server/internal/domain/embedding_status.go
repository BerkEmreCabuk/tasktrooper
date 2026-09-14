package domain

// EmbeddingHostState is how far along "this tenant can actually produce an
// embedding right now" is, for the one provider whose engine is a laptop:
// LLMProviderLocalRunner, LM Studio on the acting member's own Mac.
//
// It is a STATE rather than an error because none of the non-ready values is a
// failure of the request that asked. Each one is a thing a person can go and
// do — open the laptop, switch LM Studio's server on, download the model — and
// each has a different fix, which is the whole reason they are separate values
// instead of one "embeddings unavailable". The desktop app already draws the
// same distinction on the Mac side (web/desktop/src/main/services/detect.ts,
// probeLmStudio: "one combined item would send half the people who see it to
// the wrong place"); this type carries that distinction across the tunnel
// rather than collapsing it on arrival.
type EmbeddingHostState string

const (
	// EmbeddingHostReady — a Mac is attached, LM Studio answers, and the pinned
	// model is loaded. Embeddings work.
	EmbeddingHostReady EmbeddingHostState = "ready"
	// EmbeddingHostNoMac — this member has no Mac attached at all. Fix: open
	// the TaskTrooper desktop app on it.
	EmbeddingHostNoMac EmbeddingHostState = "no_mac"
	// EmbeddingHostMacNotReady — a Mac is attached but has not pushed an
	// environment report yet (the desktop app is still starting). Fix: wait.
	// It resolves by itself in seconds, so it must not read as a broken setup.
	EmbeddingHostMacNotReady EmbeddingHostState = "mac_not_ready"
	// EmbeddingHostLMStudioDown — the Mac is there, LM Studio is not answering
	// on it. Fix: install LM Studio, or switch its local server on from the
	// Developer tab.
	EmbeddingHostLMStudioDown EmbeddingHostState = "lm_studio_down"
	// EmbeddingHostModelMissing — LM Studio answers but does not have
	// PinnedLocalEmbeddingModel. Fix: download that model. It is a DOWNLOAD,
	// not a restart, which is exactly why this is not folded into the state
	// above.
	EmbeddingHostModelMissing EmbeddingHostState = "model_missing"
	// EmbeddingHostUnknown — the question could not be asked or its answer
	// could not be read: no probe wired, a tunnel that failed mid-call, a
	// desktop app too old to report these checks. Deliberately not "ready" and
	// deliberately not a failure either — the caller renders it as "we could
	// not check", which is true, instead of guessing in either direction.
	EmbeddingHostUnknown EmbeddingHostState = "unknown"
)

// EmbeddingHostStatus is one reading of the above, with the far side's own
// wording kept intact.
type EmbeddingHostStatus struct {
	State EmbeddingHostState `json:"state"`
	// Detail is the MAC'S OWN sentence for a non-ready state — the detail and
	// remediation its preflight wrote. Nothing on this side rewrites it: it was
	// composed by the program standing on that machine, and the cloud cannot
	// say anything truer about a host it has never seen. Empty when the state
	// carries no extra explanation.
	Detail string `json:"detail,omitempty"`
}

// EmbeddingStatus answers "what is producing this tenant's embeddings, and can
// it do it right now" in one object.
//
// It exists because the settings page previously computed the answer from the
// provider catalog, and LLMProviderLocalRunner is deliberately absent from that
// catalog (see LLMProviderLocalRunner's doc) — so a tenant whose embeddings
// were working on their own Mac was told no provider could produce embeddings
// at all. The catalog cannot answer this question; this can.
type EmbeddingStatus struct {
	// Provider is the RESOLVED provider, never the raw stored value: "" (auto)
	// is turned into what auto actually means on this deployment.
	Provider LLMProviderType `json:"provider"`
	// Model is the resolved model, likewise. Empty when a provider is chosen
	// but no model is pinned or stored for it.
	Model string `json:"model,omitempty"`
	// Dimensions is the vector length when it is known from the model alone
	// (the pin), 0 otherwise — same convention as Service.ResolvedEmbedding.
	Dimensions int `json:"dimensions,omitempty"`
	// OnMemberMac reports that Provider is the member's own machine, which is
	// what makes Host below meaningful. False for every HTTP provider, where
	// there is no laptop in the path and Host is always EmbeddingHostUnknown.
	OnMemberMac bool `json:"on_member_mac"`
	// Host is the readiness of that machine. Only consulted when OnMemberMac.
	Host EmbeddingHostStatus `json:"host"`
}
