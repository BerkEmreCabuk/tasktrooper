package repofacts

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// `uses:` prefixes that mean a step ships something. Matching the action is stronger evidence than matching a job name: a job called "release" may only tag, while amondnet/vercel-action always deploys.
var deployActionMarkers = []string{
	"amondnet/vercel-action", "vercel/action", "superfly/flyctl-actions",
	"google-github-actions/deploy-cloudrun", "google-github-actions/deploy-appengine",
	"google-github-actions/get-gke-credentials", "azure/webapps-deploy",
	"aws-actions/amazon-ecs-deploy-task-definition", "firebaseextended/action-hosting-deploy",
	"expo/expo-github-action", "apple-actions/upload-testflight-build",
	"cloudflare/wrangler-action", "cloudflare/pages-action", "netlify/actions",
	"appleboy/ssh-action", "docker/build-push-action",
}

// Shell fragments inside `run:` steps that ship; kubectl apply and helm upgrade are the two that matter for this repo's own shape.
var deployRunMarkers = []string{
	"kubectl apply", "kubectl set image", "kubectl rollout", "helm upgrade",
	"terraform apply", "vercel deploy", "vercel --prod", "flyctl deploy",
	"fly deploy", "netlify deploy", "firebase deploy", "gcloud run deploy",
	"gcloud app deploy", "eas submit", "fastlane", "serverless deploy",
	"wrangler deploy", "wrangler publish",
}

// The subset of a GitHub Actions workflow this package reads; `on` is a raw node because it is legally a string, a list or a map, and pinning it to one shape is how trigger parsing silently loses the branch filter that makes "push" mean "push to main".
type workflowFile struct {
	Name string    `yaml:"name"`
	On   yaml.Node `yaml:"on"`
	Jobs map[string]struct {
		Name  string `yaml:"name"`
		Steps []struct {
			Name string `yaml:"name"`
			Uses string `yaml:"uses"`
			Run  string `yaml:"run"`
		} `yaml:"steps"`
		Environment yaml.Node `yaml:"environment"`
	} `yaml:"jobs"`
}

func collectWorkflows(root string, t *treeScan, f *Facts) {
	var paths []string
	for _, p := range t.files {
		if !strings.HasPrefix(p, ".github/workflows/") {
			continue
		}
		if ext := strings.ToLower(filepath.Ext(p)); ext == ".yml" || ext == ".yaml" {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)

	for _, rel := range paths {
		raw, err := readCapped(filepath.Join(root, rel))
		if err != nil {
			f.Warnings = append(f.Warnings, "could not read "+rel)
			continue
		}
		var wf workflowFile
		if err := yaml.Unmarshal(raw, &wf); err != nil {
			f.Warnings = append(f.Warnings, rel+" is not parseable YAML")
			continue
		}
		w := Workflow{File: rel, Name: wf.Name}
		w.Triggers = parseTriggers(&wf.On)
		for _, trig := range w.Triggers {
			if trig == "workflow_dispatch" {
				w.Dispatchable = true
			}
		}
		for jobKey, job := range wf.Jobs {
			label := jobKey
			if job.Name != "" {
				label = job.Name
			}
			w.Jobs = append(w.Jobs, label)
			if jobDeploys(jobKey, job.Name) {
				w.Deploys = true
			}
			for _, st := range job.Steps {
				if stepDeploys(st.Uses, st.Run) {
					w.Deploys = true
				}
			}
		}
		sort.Strings(w.Jobs)
		f.Workflows = append(f.Workflows, w)
	}
}

// Renders the `on:` node into event strings, keeping the branch filter attached ("push:main") because "runs on push" and "runs on push to main" are different facts for anyone deciding whether a commit ships.
func parseTriggers(node *yaml.Node) []string {
	if node == nil || node.Kind == 0 {
		return nil
	}
	switch node.Kind {
	case yaml.ScalarNode:
		return []string{node.Value}
	case yaml.SequenceNode:
		var out []string
		for _, child := range node.Content {
			out = append(out, child.Value)
		}
		return out
	case yaml.MappingNode:
		var out []string
		for i := 0; i+1 < len(node.Content); i += 2 {
			event := node.Content[i].Value
			out = append(out, renderEvent(event, node.Content[i+1]))
		}
		sort.Strings(out)
		return out
	}
	return nil
}

func renderEvent(event string, spec *yaml.Node) string {
	if spec == nil || spec.Kind != yaml.MappingNode {
		return event
	}
	for i := 0; i+1 < len(spec.Content); i += 2 {
		key := spec.Content[i].Value
		val := spec.Content[i+1]
		switch {
		case key == "branches" && val.Kind == yaml.SequenceNode:
			var branches []string
			for _, b := range val.Content {
				branches = append(branches, b.Value)
			}
			if len(branches) > 0 {
				return event + ":" + strings.Join(branches, ",")
			}
		case key == "tags" && val.Kind == yaml.SequenceNode && len(val.Content) > 0:
			return event + ":tags " + val.Content[0].Value
		case event == "schedule" && val.Kind == yaml.ScalarNode:
			return "schedule:" + val.Value
		}
	}
	if event == "schedule" && spec.Kind == yaml.SequenceNode && len(spec.Content) > 0 {
		return "schedule"
	}
	return event
}

func jobDeploys(key, name string) bool {
	hay := strings.ToLower(key + " " + name)
	for _, kw := range []string{"deploy", "release", "publish", "ship", "rollout", "testflight"} {
		if strings.Contains(hay, kw) {
			return true
		}
	}
	return false
}

func stepDeploys(uses, run string) bool {
	u := strings.ToLower(uses)
	for _, marker := range deployActionMarkers {
		if strings.HasPrefix(u, marker) {
			return true
		}
	}
	r := strings.ToLower(run)
	for _, marker := range deployRunMarkers {
		if strings.Contains(r, marker) {
			return true
		}
	}
	return false
}

// Turns workflows and platform markers into the concrete answer to "what makes this repository ship". Workflow-driven deploys rank above provider git-integrations: when both exist, the workflow is what runs, and a profile naming the integration instead would send agents to the wrong dashboard.
func deriveDeploys(f *Facts) {
	workflowDeploy := false
	for _, w := range f.Workflows {
		if !w.Deploys {
			continue
		}
		workflowDeploy = true
		for _, trigger := range deployTriggersOf(w) {
			f.Deploys = append(f.Deploys, DeployTarget{
				Provider:    "GitHub Actions",
				Environment: environmentOf(w, trigger),
				Trigger:     trigger,
				Evidence:    w.File,
				Automatic:   strings.HasPrefix(trigger, "push:") || strings.HasPrefix(trigger, "push"),
			})
		}
	}

	// A hosting integration with no deploy workflow means the provider's own git hook ships the repo: landing a commit on the default branch IS the deploy — the single most consequential fact a profile can carry, and the one the free-text profile never stated.
	if !workflowDeploy {
		for _, in := range f.Integrations {
			if in.Category != "hosting" {
				continue
			}
			branch := f.Git.DefaultBranch
			if branch == "" {
				branch = "the default branch"
			}
			f.Deploys = append(f.Deploys,
				DeployTarget{
					Provider:    in.Name,
					Environment: "production",
					Trigger:     "push:" + branch + " (provider git integration — no deploy workflow in .github/workflows)",
					Evidence:    in.Evidence,
					Automatic:   true,
				},
				DeployTarget{
					Provider:    in.Name,
					Environment: "preview",
					Trigger:     "pull_request (provider git integration)",
					Evidence:    in.Evidence,
					Automatic:   true,
				})
		}
	}

	sort.SliceStable(f.Deploys, func(i, j int) bool {
		if f.Deploys[i].Provider != f.Deploys[j].Provider {
			return f.Deploys[i].Provider < f.Deploys[j].Provider
		}
		return f.Deploys[i].Environment < f.Deploys[j].Environment
	})
}

func deployTriggersOf(w Workflow) []string {
	if len(w.Triggers) == 0 {
		return []string{"unknown trigger"}
	}
	return w.Triggers
}

// Guesses prod vs preview from the workflow's own naming and its trigger; stays "" rather than guessing when neither says anything — a wrong environment label is worse than no label.
func environmentOf(w Workflow, trigger string) string {
	hay := strings.ToLower(w.Name + " " + w.File + " " + strings.Join(w.Jobs, " "))
	switch {
	case strings.Contains(hay, "prod"):
		return "production"
	case strings.Contains(hay, "staging"), strings.Contains(hay, "stage"):
		return "staging"
	case strings.Contains(hay, "preview"), strings.HasPrefix(trigger, "pull_request"):
		return "preview"
	}
	return ""
}

func summarizeWorkflows(f Facts) []string {
	out := make([]string, 0, len(f.Workflows))
	for _, w := range f.Workflows {
		label := w.Name
		if label == "" {
			label = filepath.Base(w.File)
		}
		line := fmt.Sprintf("`%s` (%s)", label, w.File)
		if len(w.Triggers) > 0 {
			line += " on " + strings.Join(w.Triggers, ", ")
		}
		if len(w.Jobs) > 0 {
			line += " — jobs: " + strings.Join(w.Jobs, ", ")
		}
		if w.Deploys {
			line += " — DEPLOYS"
		}
		out = append(out, line)
	}
	return out
}
