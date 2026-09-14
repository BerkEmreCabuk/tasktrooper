// Package prodops turns production signals — external alerts, our own health
// probes and failed deploys — into deduplicated incidents, derives a concrete
// remedy for each, and feeds it back to the agent that can fix it or to the
// human who must decide. It exists so nobody has to read logs to find out what
// broke and what to do about it.
package prodops

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// Defaults fill the fields a given alert shape does not carry.
type Defaults struct {
	Env    string
	Source string
}

// Normalize collapses the alert shapes we accept — Alertmanager, Sentry, GCP
// Cloud Monitoring and a plain generic payload — into one IncidentInput.
// Unknown shapes fall through to the generic reader rather than being
// rejected: a dropped alert is worse than a coarsely-titled one.
func Normalize(raw []byte, def Defaults) (domain.IncidentInput, error) {
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return domain.IncidentInput{}, fmt.Errorf("invalid JSON payload: %w", err)
	}
	if def.Env == "" {
		def.Env = domain.DeployEnvProd
	}
	if def.Source == "" {
		def.Source = domain.IncidentSourceWebhook
	}

	var in domain.IncidentInput
	switch {
	case hasKey(envelope, "alerts"):
		in = fromAlertmanager(envelope, def)
	case hasKey(envelope, "incident"):
		in = fromGoogleCloudMonitoring(envelope, def)
	case hasKey(envelope, "data") && strings.Contains(string(raw), "sentry"), hasKey(envelope, "culprit"):
		in = fromSentry(envelope, def)
	default:
		in = fromGeneric(envelope, def)
	}

	in.Payload = envelope
	in.Source = def.Source
	if in.Env == "" {
		in.Env = def.Env
	}
	if in.Severity == "" {
		in.Severity = domain.IncidentSeverityMedium
	}
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		in.Title = "Unlabelled production alert"
	}
	if len(in.Title) > 300 {
		in.Title = truncateUTF8(in.Title, 300)
	}
	if in.Fingerprint == "" {
		in.Fingerprint = domain.IncidentFingerprint(in.Title, in.Env)
	}
	return in, nil
}

// fromAlertmanager reads a Prometheus Alertmanager webhook. Only the first
// alert of the group is modelled: the group is one problem, and the rest of
// the batch stays available in the raw payload.
func fromAlertmanager(env map[string]any, def Defaults) domain.IncidentInput {
	in := domain.IncidentInput{Env: def.Env}
	alerts, _ := env["alerts"].([]any)
	if len(alerts) == 0 {
		in.Title = str(env, "commonAnnotations", "summary")
		in.Resolved = strings.EqualFold(str(env, "status"), "resolved")
		return in
	}
	first, _ := alerts[0].(map[string]any)
	labels, _ := first["labels"].(map[string]any)
	annotations, _ := first["annotations"].(map[string]any)

	alertname := strOf(labels, "alertname")
	in.Title = firstNonEmpty(strOf(annotations, "summary"), alertname, "Alertmanager alert")
	in.Detail = firstNonEmpty(strOf(annotations, "description"), strOf(annotations, "message"))
	in.Severity = domain.NormalizeSeverity(strOf(labels, "severity"))
	in.Env = firstNonEmpty(strOf(labels, "env"), strOf(labels, "environment"), def.Env)
	// A group is only resolved when every alert in it is: the first alert
	// resolving while others still fire must not close the incident.
	allResolved := true
	for _, a := range alerts {
		am, _ := a.(map[string]any)
		if !strings.EqualFold(strOf(am, "status"), "resolved") {
			allResolved = false
			break
		}
	}
	in.Resolved = allResolved || strings.EqualFold(str(env, "status"), "resolved")
	// Alertmanager's own fingerprint already dedupes per label set; reuse it so
	// our identity matches the sender's.
	in.Fingerprint = firstNonEmpty(strOf(first, "fingerprint"),
		domain.IncidentFingerprint(alertname, strOf(labels, "service"), strOf(labels, "job"), in.Env))
	if len(alerts) > 1 {
		in.Detail = strings.TrimSpace(fmt.Sprintf("%s\n\n(%d alerts in this group)", in.Detail, len(alerts)))
	}
	return in
}

// fromGoogleCloudMonitoring reads a Cloud Monitoring notification. A "closed"
// state is a recovery, which resolves the matching incident.
func fromGoogleCloudMonitoring(env map[string]any, def Defaults) domain.IncidentInput {
	incident, _ := env["incident"].(map[string]any)
	in := domain.IncidentInput{Env: def.Env}
	in.Title = firstNonEmpty(strOf(incident, "summary"), strOf(incident, "policy_name"), "Cloud Monitoring alert")
	in.Detail = strOf(incident, "documentation")
	in.Severity = domain.NormalizeSeverity(firstNonEmpty(strOf(incident, "severity"), "high"))
	in.Resolved = strings.EqualFold(strOf(incident, "state"), "closed")
	resource, _ := incident["resource"].(map[string]any)
	labels, _ := resource["labels"].(map[string]any)
	if v := strOf(labels, "environment"); v != "" {
		in.Env = v
	}
	in.Fingerprint = domain.IncidentFingerprint(
		strOf(incident, "policy_name"), strOf(incident, "condition_name"),
		strOf(labels, "service_name"), strOf(labels, "cluster_name"), in.Env)
	return in
}

// fromSentry reads a Sentry issue alert (both the modern `data.issue` shape and
// the legacy flat event).
func fromSentry(env map[string]any, def Defaults) domain.IncidentInput {
	in := domain.IncidentInput{Env: def.Env}
	issue := map[string]any{}
	if data, ok := env["data"].(map[string]any); ok {
		if v, ok := data["issue"].(map[string]any); ok {
			issue = v
		} else if v, ok := data["event"].(map[string]any); ok {
			issue = v
		}
	}
	if len(issue) == 0 {
		issue = env
	}
	in.Title = firstNonEmpty(strOf(issue, "title"), strOf(issue, "message"), strOf(env, "message"), "Sentry issue")
	in.Detail = firstNonEmpty(strOf(issue, "culprit"), strOf(env, "culprit"))
	in.Severity = domain.NormalizeSeverity(firstNonEmpty(strOf(issue, "level"), strOf(env, "level"), "high"))
	if v := firstNonEmpty(strOf(issue, "environment"), strOf(env, "environment")); v != "" {
		in.Env = v
	}
	in.Fingerprint = domain.IncidentFingerprint(
		firstNonEmpty(strOf(issue, "id"), strOf(env, "id"), in.Title),
		firstNonEmpty(strOf(issue, "project"), strOf(env, "project")), in.Env)
	return in
}

// fromGeneric reads the documented plain shape: {title, detail, severity, env,
// fingerprint, resolved}. It is also the fallback for unrecognised payloads.
func fromGeneric(env map[string]any, def Defaults) domain.IncidentInput {
	in := domain.IncidentInput{
		Title:       firstNonEmpty(strOf(env, "title"), strOf(env, "message"), strOf(env, "summary"), strOf(env, "error")),
		Detail:      firstNonEmpty(strOf(env, "detail"), strOf(env, "description"), strOf(env, "body")),
		Severity:    domain.NormalizeSeverity(firstNonEmpty(strOf(env, "severity"), strOf(env, "level"))),
		Env:         firstNonEmpty(strOf(env, "env"), strOf(env, "environment"), def.Env),
		Fingerprint: strOf(env, "fingerprint"),
	}
	if v, ok := env["resolved"].(bool); ok {
		in.Resolved = v
	}
	if strings.EqualFold(strOf(env, "status"), "resolved") {
		in.Resolved = true
	}
	return in
}

func hasKey(m map[string]any, key string) bool {
	_, ok := m[key]
	return ok
}

func strOf(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	switch v := m[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strings.TrimSuffix(fmt.Sprintf("%.0f", v), ".0")
	case bool:
		return fmt.Sprintf("%t", v)
	}
	return ""
}

// str walks a nested map path and reads the leaf as a string.
func str(m map[string]any, path ...string) string {
	cur := m
	for i, key := range path {
		if i == len(path)-1 {
			return strOf(cur, key)
		}
		next, ok := cur[key].(map[string]any)
		if !ok {
			return ""
		}
		cur = next
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// truncateUTF8 cuts at a byte budget without splitting a multi-byte rune —
// Postgres rejects invalid UTF-8 outright, which would drop the whole alert.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}
