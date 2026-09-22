package prodops

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type Defaults struct {
	Env    string
	Source string
}

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

	allResolved := true
	for _, a := range alerts {
		am, _ := a.(map[string]any)
		if !strings.EqualFold(strOf(am, "status"), "resolved") {
			allResolved = false
			break
		}
	}
	in.Resolved = allResolved || strings.EqualFold(str(env, "status"), "resolved")

	in.Fingerprint = firstNonEmpty(strOf(first, "fingerprint"),
		domain.IncidentFingerprint(alertname, strOf(labels, "service"), strOf(labels, "job"), in.Env))
	if len(alerts) > 1 {
		in.Detail = strings.TrimSpace(fmt.Sprintf("%s\n\n(%d alerts in this group)", in.Detail, len(alerts)))
	}
	return in
}

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
