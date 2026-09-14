package shell

import (
	"strings"
	"testing"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

func TestBlockingCommandReasonRefusesServers(t *testing.T) {
	// Every one of these runs until interrupted, so in the foreground the only
	// possible outcome is the tool timeout.
	cases := []string{
		"npm run dev",
		"pnpm dev",
		"yarn start",
		"bun run dev",
		"npx next dev",
		"vite",
		"nodemon server.js",
		"webpack serve",
		"python3 -m http.server 8080",
		"uvicorn app:app --reload",
		"docker compose up",
		"docker-compose up --build",
		"tail -f /var/log/app.log",
		"kubectl port-forward svc/api 8080:80",
		"journalctl -fu bridge",
		// The shape that actually burned the budget: the server hidden behind a cd.
		"cd /data/workspaces/task-43f50579 && npm run dev 2>&1 | head -30",
		// Environment prefixes and sudo do not change what the command does.
		"PORT=3000 npm run dev",
		"sudo docker compose up",
	}
	for _, cmd := range cases {
		if reason := blockingCommandReason(cmd); reason == "" {
			t.Errorf("blockingCommandReason(%q) = \"\", want a refusal", cmd)
		}
	}
}

func TestBlockingCommandReasonAllowsTerminatingCommands(t *testing.T) {
	// These finish on their own. Refusing any of them would block real work,
	// which is why the patterns are kept narrow.
	cases := []string{
		"npm run build",
		"npm test",
		"npm run typecheck",
		"npm ci",
		"go build ./...",
		"go test ./...",
		"git status",
		"ls -la",
		"cat package.json",
		"grep -rn dev src",
		"docker compose build",
		"docker compose down",
		"tail -n 50 /var/log/app.log",
		"kubectl get pods",
		// "dev" appearing as an argument rather than the script being run.
		"cat /dev/null",
		"echo dev start serve",
	}
	for _, cmd := range cases {
		if reason := blockingCommandReason(cmd); reason != "" {
			t.Errorf("blockingCommandReason(%q) = %q, want \"\"", cmd, reason)
		}
	}
}

// Backgrounding is the supported way to get a server up, so a detached command
// passes through untouched — that is the escape hatch the refusal points at.
func TestBlockingCommandReasonAllowsBackgroundedServer(t *testing.T) {
	if reason := blockingCommandReason("npm run dev > /tmp/dev.log 2>&1 &"); reason != "" {
		t.Fatalf("backgrounded server refused: %q", reason)
	}
}

func TestBlockingCommandReasonNamesTheAlternative(t *testing.T) {
	reason := blockingCommandReason("npm run dev")
	for _, want := range []string{"build", "/tmp/dev.log"} {
		if !strings.Contains(reason, want) {
			t.Errorf("refusal %q does not mention %q; the agent needs to be told what to do instead", reason, want)
		}
	}
}

// The shipped 60s cap was below the cost of the loop every developer agent
// runs — install, build, test — so the agent could never verify its own work.
// A slow command must be able to ask for the time it needs, up to the ceiling.
func TestResolveTimeoutHonoursTheRequestedBudget(t *testing.T) {
	s := &shellTool{timeout: 3 * time.Minute, maxTimeout: 15 * time.Minute}

	if got := s.resolveTimeout(0); got != 3*time.Minute {
		t.Errorf("unspecified budget = %s, want the configured default", got)
	}
	if got := s.resolveTimeout(600); got != 10*time.Minute {
		t.Errorf("requested 600s = %s, want 10m", got)
	}
}

// The ceiling is the operator's, not the agent's: a request beyond it is
// clamped rather than honoured, so one command cannot hold a run indefinitely.
func TestResolveTimeoutClampsToTheCeiling(t *testing.T) {
	s := &shellTool{timeout: 3 * time.Minute, maxTimeout: 15 * time.Minute}

	if got := s.resolveTimeout(86400); got != 15*time.Minute {
		t.Errorf("requested a day = %s, want the ceiling", got)
	}
}

// A ceiling below the default would silently shorten every command, so New
// raises it to the default instead.
func TestNewNeverLetsTheCeilingUndercutTheDefault(t *testing.T) {
	s := New("/tmp", 5*time.Minute, time.Minute, domain.TerminalSandboxConfig{}).(*shellTool)

	if got := s.resolveTimeout(0); got != 5*time.Minute {
		t.Fatalf("default budget = %s, want 5m; a low ceiling must not shorten it", got)
	}
}
