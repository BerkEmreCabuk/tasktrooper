package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const (
	FindingsDigestHeader = "[findings] What the previous attempt already did — continue from it, do not rediscover it."

	DefaultFindingsDigestChars = 2500

	maxDigestEvents = 600

	maxSubjectChars = 160
)

func IsFindingsDigest(content string) bool {
	return strings.HasPrefix(strings.TrimSpace(content), FindingsDigestHeader)
}

type toolEvent struct {
	Name    string
	Args    string
	Result  string
	IsError bool
}

func DigestFromSteps(steps []domain.SessionStep, summary string, maxChars int) string {
	events, traced := eventsFromSteps(steps)
	if strings.TrimSpace(summary) == "" {
		summary = traced
	}
	return buildDigest(events, summary, maxChars)
}

func DigestFromMessages(history []domain.Message, summary string, maxChars int) string {
	events, traced := eventsFromMessages(history)
	if strings.TrimSpace(summary) == "" {
		summary = traced
	}
	return buildDigest(events, summary, maxChars)
}

type stepToolPayload struct {
	Tool      string `json:"tool"`
	CallID    string `json:"call_id"`
	Arguments string `json:"arguments"`
	Content   string `json:"content"`
	IsError   bool   `json:"is_error"`
}

func eventsFromSteps(steps []domain.SessionStep) ([]toolEvent, string) {
	var events []toolEvent
	byCall := map[string]int{}
	summary := ""
	for _, step := range steps {
		switch step.StepType {
		case "tool_call_start":
			var p stepToolPayload
			if json.Unmarshal(step.Payload, &p) != nil || p.Tool == "" {
				continue
			}
			events = append(events, toolEvent{Name: p.Tool, Args: p.Arguments})
			if p.CallID != "" {
				byCall[p.CallID] = len(events) - 1
			}
		case "tool_call_result":
			var p stepToolPayload
			if json.Unmarshal(step.Payload, &p) != nil {
				continue
			}
			if idx, ok := byCall[p.CallID]; ok && p.CallID != "" {
				events[idx].Result = p.Content
				events[idx].IsError = p.IsError
				continue
			}
			if p.Tool != "" {
				events = append(events, toolEvent{Name: p.Tool, Result: p.Content, IsError: p.IsError})
			}
		case "assistant_message":
			var p struct {
				Content string `json:"content"`
			}
			if json.Unmarshal(step.Payload, &p) == nil && strings.TrimSpace(p.Content) != "" {
				summary = p.Content
			}
		}
	}
	return events, summary
}

func eventsFromMessages(history []domain.Message) ([]toolEvent, string) {
	var events []toolEvent
	byCall := map[string]int{}
	summary := ""
	for _, m := range history {
		switch m.Role {
		case domain.RoleAssistant:
			for _, tc := range m.ToolCalls {
				if tc.Function.Name == "" {
					continue
				}
				events = append(events, toolEvent{Name: tc.Function.Name, Args: tc.Function.Arguments})
				if tc.ID != "" {
					byCall[tc.ID] = len(events) - 1
				}
			}
			if len(m.ToolCalls) == 0 && strings.TrimSpace(m.Content) != "" {
				summary = m.Content
			}
		case domain.RoleTool:
			idx, ok := byCall[m.ToolCallID]
			if !ok || m.ToolCallID == "" {
				continue
			}
			events[idx].Result = m.Content
			events[idx].IsError = resultLooksFailed(m.Content)
		}
	}
	return events, summary
}

func resultLooksFailed(content string) bool {
	head := strings.ToLower(strings.TrimSpace(content))
	if idx := strings.IndexByte(head, '\n'); idx >= 0 {
		head = head[:idx]
	}
	return strings.HasPrefix(head, "error") || strings.HasPrefix(head, "exit error") ||
		strings.Contains(head, "failed:") || strings.Contains(head, "error:")
}

type section struct {
	label   string
	sep     string
	render  func(*item) string
	items   []*item
	index   map[string]*item
	dropped int
}

type item struct {
	key     string
	subject string
	count   int
	failed  int
	details []string
	last    string
}

const maxItemDetails = 3

func newSection(label, sep string, render func(*item) string) *section {
	return &section{label: label, sep: sep, render: render, index: map[string]*item{}}
}

func (s *section) add(key, subject, detail string, isError bool) {
	it, ok := s.index[key]
	if !ok {
		it = &item{key: key, subject: subject}
		s.index[key] = it
		s.items = append(s.items, it)
	} else {
		s.moveToBack(it)
	}
	it.count++
	if isError {
		it.failed++
	}
	if detail != "" {
		it.last = detail
		if !slices.Contains(it.details, detail) && len(it.details) < maxItemDetails {
			it.details = append(it.details, detail)
		}
	}
}

func (s *section) moveToBack(target *item) {
	for i, it := range s.items {
		if it != target {
			continue
		}
		s.items = append(s.items[:i], s.items[i+1:]...)
		s.items = append(s.items, target)
		return
	}
}

func (s *section) line() string {
	if len(s.items) == 0 {
		return ""
	}
	parts := make([]string, 0, len(s.items))
	for _, it := range s.items {
		parts = append(parts, s.render(it))
	}
	prefix := s.label + ": "
	if s.dropped > 0 {
		prefix += fmt.Sprintf("(+%d earlier) ", s.dropped)
	}
	return prefix + strings.Join(parts, s.sep)
}

func (s *section) dropOldest() bool {
	if len(s.items) == 0 {
		return false
	}
	delete(s.index, s.items[0].key)
	s.items = s.items[1:]
	s.dropped++
	return true
}

func buildDigest(events []toolEvent, summary string, maxChars int) string {
	if maxChars <= 0 {
		maxChars = DefaultFindingsDigestChars
	}
	if len(events) > maxDigestEvents {
		events = events[len(events)-maxDigestEvents:]
	}

	changed := newSection("Files changed", ", ", func(it *item) string {
		return annotate(it.subject, tally(it, strings.Join(it.details, ", ")))
	})
	read := newSection("Files read", ", ", func(it *item) string { return annotate(it.subject, tally(it, "")) })
	searched := newSection("Searched", ", ", func(it *item) string {
		return annotate(it.subject, tally(it, strings.Join(it.details, ", ")))
	})

	commands := newSection("Commands", "; ", func(it *item) string {
		return annotate("`"+it.subject+"` → "+it.last, tally(it, ""))
	})
	other := newSection("Other tools", ", ", func(it *item) string { return annotate(it.subject, tally(it, "")) })

	for _, ev := range events {
		args := parseArgs(ev.Args)
		switch classify(ev.Name) {
		case kindWrite:
			subject := fileSubject(ev.Name, args, ev.Args)
			changed.add(subject, subject, ev.Name, ev.IsError)
		case kindRead:
			subject := fileSubject(ev.Name, args, ev.Args)
			read.add(subject, subject, "", ev.IsError)
		case kindSearch:
			subject := searchSubject(ev.Name, args, ev.Args)
			searched.add(subject, subject, ev.Name, ev.IsError)
		case kindCommand:
			subject := commandSubject(ev.Name, args, ev.Args)
			commands.add(subject, subject, commandOutcome(ev), ev.IsError)
		default:
			other.add(ev.Name, ev.Name, "", ev.IsError)
		}
	}

	sections := []*section{changed, commands, read, searched, other}
	sacrificial := []*section{other, searched, read, commands, changed}
	outcome := ""
	if trimmed := strings.TrimSpace(summary); trimmed != "" {
		outcome = "Outcome: " + collapse(domain.TruncateHead(trimmed, maxChars/3))
	}

	if outcome == "" && !anyItems(sections) {
		return ""
	}

	for {
		out := renderDigest(sections, outcome)
		if len(out) <= maxChars {
			return out
		}
		if !dropOne(sacrificial) {
			return domain.TruncateHead(out, maxChars)
		}
	}
}

func anyItems(sections []*section) bool {
	for _, s := range sections {
		if len(s.items) > 0 {
			return true
		}
	}
	return false
}

func renderDigest(sections []*section, outcome string) string {
	lines := []string{FindingsDigestHeader}
	for _, s := range sections {
		if line := s.line(); line != "" {
			lines = append(lines, line)
		}
	}
	if outcome != "" {
		lines = append(lines, outcome)
	}
	return strings.Join(lines, "\n")
}

func dropOne(sections []*section) bool {
	for _, s := range sections {
		if s.dropOldest() {
			return true
		}
	}
	return false
}

func tally(it *item, detail string) string {
	out := detail
	if it.count > 1 {
		out = strings.TrimSpace(out + fmt.Sprintf(" ×%d", it.count))
	}
	if it.failed > 0 {
		if out == "" {
			return fmt.Sprintf("%d failed", it.failed)
		}
		out += fmt.Sprintf(", %d failed", it.failed)
	}
	return out
}

func annotate(subject, tally string) string {
	if tally == "" {
		return subject
	}
	return subject + " (" + tally + ")"
}

type toolKind int

const (
	kindOther toolKind = iota
	kindRead
	kindWrite
	kindSearch
	kindCommand
)

var (
	writeToolNames = map[string]bool{
		"write_file": true, "edit_file": true, "edit_lines": true,
		"delete_file": true, "move_file": true, "apply_patch": true, "create_file": true,
	}
	readToolNames = map[string]bool{
		"read_file": true, "get_repo_tree": true, "get_symbol_skeleton": true,
		"expand_symbol_context": true, "list_files": true, "list_directory": true,
	}
	searchToolNames = map[string]bool{
		"grep_code": true, "codebase_search": true, "search_files": true,
	}
	commandToolNames = map[string]bool{
		"run_terminal": true, "bash": true, "shell": true, "run_command": true,
	}
)

func classify(name string) toolKind {
	switch {
	case writeToolNames[name]:
		return kindWrite
	case readToolNames[name]:
		return kindRead
	case searchToolNames[name]:
		return kindSearch
	case commandToolNames[name]:
		return kindCommand
	}
	switch {
	case containsAny(name, "write", "edit", "create", "delete", "patch", "rename"):
		return kindWrite
	case containsAny(name, "search", "grep", "find", "query"):
		return kindSearch
	case containsAny(name, "terminal", "bash", "shell", "exec", "command"):
		return kindCommand
	case containsAny(name, "read", "list", "tree", "cat"):
		return kindRead
	}
	return kindOther
}

func containsAny(name string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(name, n) {
			return true
		}
	}
	return false
}

func parseArgs(raw string) map[string]any {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var args map[string]any
	if json.Unmarshal([]byte(raw), &args) != nil {
		return nil
	}
	return args
}

func argString(args map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := args[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func fileSubject(tool string, args map[string]any, raw string) string {
	path := argString(args, "path", "file_path", "file", "filename", "dir", "directory")
	if path == "" {
		return fallbackSubject(tool, raw)
	}
	if dest := argString(args, "destination", "to", "new_path", "target"); dest != "" {
		return clip(path + " → " + dest)
	}
	return clip(path)
}

func searchSubject(tool string, args map[string]any, raw string) string {
	needle := argString(args, "pattern", "query", "q", "search", "text")
	if needle == "" {
		return fallbackSubject(tool, raw)
	}
	subject := `"` + collapse(needle) + `"`
	if scope := argString(args, "path", "dir", "directory"); scope != "" {
		subject += " in " + scope
	}
	return clip(subject)
}

func commandSubject(tool string, args map[string]any, raw string) string {
	cmd := argString(args, "command", "cmd", "script")
	if cmd == "" {
		return fallbackSubject(tool, raw)
	}
	return clip(collapse(cmd))
}

func fallbackSubject(tool, raw string) string {
	preview := collapse(raw)
	if preview == "" {
		return tool
	}
	return clip(tool + " " + preview)
}

var exitStatusRe = regexp.MustCompile(`exit status (\d+)`)

func commandOutcome(ev toolEvent) string {
	if m := exitStatusRe.FindStringSubmatch(firstLine(ev.Result)); m != nil {
		return "exit status " + m[1]
	}
	if ev.IsError {
		if line := firstLine(ev.Result); line != "" {
			return "failed: " + clip(line)
		}
		return "failed"
	}
	return "ok"
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = s[:idx]
	}
	return collapse(s)
}

func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func clip(s string) string {
	if len(s) <= maxSubjectChars {
		return s
	}
	return domain.TruncateHead(s, maxSubjectChars) + "…"
}
