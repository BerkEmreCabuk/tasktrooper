package catalogrepo

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type agentManifest struct {
	Name          string        `yaml:"name"`
	Description   string        `yaml:"description"`
	SubagentType  string        `yaml:"subagent_type"`
	ProviderType  string        `yaml:"provider_type"`
	Model         string        `yaml:"model"`
	ModelHeavy    string        `yaml:"model_heavy"`
	Effort        string        `yaml:"effort"`
	MaxTurns      int           `yaml:"max_turns"`
	SelfEvolution *bool         `yaml:"self_evolution"`
	Enabled       *bool         `yaml:"enabled"`
	ToolPolicy    toolPolicyYAM `yaml:"tool_policy"`
	Roles         []roleYAM     `yaml:"roles"`
	Subscriptions []string      `yaml:"subscriptions"`
}

type toolPolicyYAM struct {
	AllowTools      []string `yaml:"allow_tools"`
	AllowMCPServers []string `yaml:"allow_mcp_servers"`
}

type roleYAM struct {
	Key   string   `yaml:"key"`
	Areas []string `yaml:"areas"`
}

// readAgentDir parses agents/<slug>/ into a domain definition and returns the
// content hash (Etag) the sync compares against a live agent's applied one.
func readAgentDir(dir, slug string) (domain.UpstreamAgent, string, error) {
	manifestRaw, err := os.ReadFile(filepath.Join(dir, manifestFile))
	if err != nil {
		return domain.UpstreamAgent{}, "", fmt.Errorf("catalog agent %s: %w", slug, err)
	}
	var m agentManifest
	if err := yaml.Unmarshal(manifestRaw, &m); err != nil {
		return domain.UpstreamAgent{}, "", fmt.Errorf("catalog agent %s: parse %s: %w", slug, manifestFile, err)
	}
	if m.Name == "" {
		return domain.UpstreamAgent{}, "", fmt.Errorf("catalog agent %s: %s requires a name", slug, manifestFile)
	}
	promptRaw, err := os.ReadFile(filepath.Join(dir, promptFile))
	if err != nil {
		return domain.UpstreamAgent{}, "", fmt.Errorf("catalog agent %s: %w", slug, err)
	}
	agent := domain.UpstreamAgent{
		Slug:          slug,
		Name:          m.Name,
		Description:   m.Description,
		SubagentType:  m.SubagentType,
		SystemPrompt:  strings.TrimSpace(string(promptRaw)),
		ProviderType:  domain.LLMProviderType(m.ProviderType),
		Model:         m.Model,
		ModelHeavy:    m.ModelHeavy,
		Effort:        m.Effort,
		MaxTurns:      m.MaxTurns,
		SelfEvolution: m.SelfEvolution == nil || *m.SelfEvolution,
		Enabled:       m.Enabled == nil || *m.Enabled,
		ToolPolicy: domain.ToolPolicy{
			AllowTools:      m.ToolPolicy.AllowTools,
			AllowMCPServers: m.ToolPolicy.AllowMCPServers,
		},
	}
	for _, r := range m.Roles {
		agent.Roles = append(agent.Roles, domain.TemplateRoleSuggestion{Key: r.Key, Areas: r.Areas})
	}
	for _, sub := range m.Subscriptions {
		agent.Subscriptions = append(agent.Subscriptions, domain.TaskColumn(sub))
	}

	files, err := collectFiles(dir)
	if err != nil {
		return domain.UpstreamAgent{}, "", err
	}

	if skills, err := readSkillsDir(filepath.Join(dir, skillsDir)); err != nil {
		return domain.UpstreamAgent{}, "", err
	} else {
		agent.Skills = skills
	}
	if rules, err := readRulesDir(filepath.Join(dir, rulesDir)); err != nil {
		return domain.UpstreamAgent{}, "", err
	} else {
		agent.Rules = rules
	}

	etag, err := hashFiles(dir, files)
	if err != nil {
		return domain.UpstreamAgent{}, "", fmt.Errorf("catalog agent %s: %w", slug, err)
	}
	return agent, etag, nil
}

func readSkillsDir(dir string) ([]domain.UpstreamSkill, error) {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, nil
	}
	skillDirs, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []domain.UpstreamSkill
	for _, d := range skillDirs {
		if !d.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, d.Name(), "SKILL.md"))
		if err != nil {
			return nil, fmt.Errorf("catalog skill %s: %w", d.Name(), err)
		}
		meta, body, err := parseDoc(string(raw))
		if err != nil {
			return nil, fmt.Errorf("catalog skill %s: %w", d.Name(), err)
		}
		name := meta["name"]
		if name == "" {
			name = d.Name()
		}
		out = append(out, domain.UpstreamSkill{
			Name:        name,
			Description: meta["description"],
			Category:    meta["category"],
			TechStack:   strings.TrimSpace(meta["tech_stack"]),
			Content:     body,
			Sha:         hashContent(body),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func readRulesDir(dir string) ([]domain.UpstreamRule, error) {
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []domain.UpstreamRule
	for _, d := range entries {
		if d.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, d.Name()))
		if err != nil {
			return nil, err
		}
		meta, body, err := parseDoc(string(raw))
		if err != nil {
			return nil, fmt.Errorf("catalog rule %s: %w", d.Name(), err)
		}
		enabled := true
		if v, ok := meta["enabled"]; ok && strings.TrimSpace(v) == "false" {
			enabled = false
		}
		priority := 0
		if n, err := strconv.Atoi(meta["priority"]); err == nil {
			priority = n
		}
		out = append(out, domain.UpstreamRule{
			Name:     meta["name"],
			Content:  body,
			Priority: priority,
			Enabled:  enabled,
		})
	}
	return out, nil
}

// collectFiles gathers every file under dir, sorted for a stable hash. Only
// the checkout's own VCS metadata is skipped; skills and rules live in
// subdirectories that must count toward the agent Etag, so they are walked
// fully rather than whitelisted at the top level.
func collectFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == dir {
				return nil
			}
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	})
	sort.Strings(files)
	return files, err
}

func hashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// hashFiles hashes every file under dir, path-qualified so moving content
// between files still changes the Etag. Sorted order is guaranteed by
// collectFiles.
func hashFiles(dir string, files []string) (string, error) {
	h := sha256.New()
	for _, rel := range files {
		raw, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			return "", err
		}
		h.Write([]byte(rel))
		h.Write([]byte{0})
		h.Write(raw)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
