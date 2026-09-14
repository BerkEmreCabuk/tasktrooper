// Package localdevice detects and drives the simulators and emulators that
// exist on THIS host: `xcrun simctl` iOS simulators and Android SDK AVDs.
//
// It is the local twin of adapter/deviceagent. The bridge answers "connect the
// phone at 100.x.y.z:5555" with an HTTP call to a sidecar next to Appium in a
// cluster; this answers "boot the simulator with this UDID" by running a binary
// on the machine the process is already on. Same three questions in both cases —
// what is there, attach it, is it up — which is what lets the mobiledevice
// service switch on the device's kind and otherwise stay one piece of code.
//
// # Nothing is interpolated into a shell
//
// Every command here is exec.Command with a validated argument vector, and
// there is no shell anywhere in the package. That is not defence in depth, it
// is the only defence: the arguments are a simulator UDID and an AVD name that
// arrive over the settings API, so a `sh -c "xcrun simctl boot "+udid` would be
// remote command execution on the operator's own laptop, which is a strictly
// worse place to have it than a disposable pod. Validation is allow-list
// (ValidateSimulatorUDID, ValidateAVDName, ValidateEmulatorSerial) and runs
// before the exec, not after it.
//
// # Degrading on Linux
//
// Every capability question answers false when the binary is not there, and the
// catalog answers two empty arrays. So a Linux cluster node behaves exactly as
// it did before this package existed: no simulators to offer, no AVDs, and the
// mobiledevice service refusing iOS for the same reason it always did.
package localdevice

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/platform/childenv"
)

const (
	// listTimeout bounds an inventory command. simctl and adb answer these from
	// local state, so a slow one means the toolchain is wedged rather than busy,
	// and a settings page must not hang on it.
	listTimeout = 30 * time.Second

	// actionTimeout bounds a command that changes something (boot, shutdown,
	// emu kill). Longer than a list because simctl's boot itself can sit behind
	// CoreSimulator starting up.
	actionTimeout = 90 * time.Second

	// defaultBootTimeout is the ceiling on waiting for a device to become
	// usable. A cold Android emulator on a laptop genuinely takes minutes; past
	// this it is not slow, it is stuck, and the operator needs to be told
	// rather than left with a spinner.
	defaultBootTimeout = 4 * time.Minute

	// defaultPollInterval is how often a boot wait re-asks. Frequent enough
	// that a fast simulator does not sit idle, cheap enough that four minutes
	// of polling is not a load.
	defaultPollInterval = 2 * time.Second
)

// simulatorUDIDPattern is simctl's identity: an uppercase UUID. Anchored and
// exact — a permissive "no spaces" rule would still accept `-rf` or a path.
var simulatorUDIDPattern = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)

// avdNamePattern is what `avdmanager create avd` permits. Notably no spaces:
// the SDK itself replaces them with underscores, so a name with one in it never
// came from the SDK.
//
// The first character may not be a dash or a dot, which is a security rule
// rather than a naming one: `emulator -avd --help` and `-avd -ports:5554` are
// argument injection even with no shell involved, because the emulator binary
// parses its own argv. A leading dot would likewise let a name reach for a path.
var avdNamePattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,63}$`)

// emulatorSerialPattern is how adb names a running emulator. The console port
// is what makes it unique on a host, and it is always numeric.
var emulatorSerialPattern = regexp.MustCompile(`^emulator-[0-9]{1,6}$`)

// ValidateSimulatorUDID refuses anything that is not a simctl UDID.
func ValidateSimulatorUDID(udid string) error {
	if !simulatorUDIDPattern.MatchString(udid) {
		return fmt.Errorf("%q is not a simulator UDID — copy it from the local device catalog", udid)
	}
	return nil
}

// ValidateAVDName refuses anything that is not an AVD name.
func ValidateAVDName(name string) error {
	if !avdNamePattern.MatchString(name) {
		return fmt.Errorf("%q is not an AVD name — letters, digits, dot, dash and underscore only", name)
	}
	return nil
}

// ValidateEmulatorSerial refuses anything that is not an emulator's adb serial.
// Physical-device serials are refused on purpose: this package must never be
// able to `emu kill` a phone somebody has plugged in.
func ValidateEmulatorSerial(serial string) error {
	if !emulatorSerialPattern.MatchString(serial) {
		return fmt.Errorf("%q is not an emulator serial", serial)
	}
	return nil
}

// Config overrides what would otherwise be discovered. Every field is empty in
// production; the tests set them to stand-in scripts, the same way the Claude
// Code executor's tests point Binary at testdata/fake-claude.sh.
type Config struct {
	// GOOS overrides runtime.GOOS so the Linux degradation can be exercised
	// from a developer's Mac — the case with no CI machine to prove it on.
	GOOS string
	// Xcrun, ADB and Emulator are absolute paths or names on PATH.
	Xcrun    string
	ADB      string
	Emulator string
	// BootTimeout and PollInterval bound the wait for a device to come up.
	// Zero means the defaults above, which is what production uses. The tests
	// shorten them so a wait that will never finish fails in a second rather
	// than holding the suite for four minutes — the exact failure that made
	// them configurable.
	BootTimeout  time.Duration
	PollInterval time.Duration
}

// Host is the machine this process runs on, as far as local devices go.
type Host struct {
	goos     string
	xcrun    string
	adb      string
	emulator string

	bootTimeout  time.Duration
	pollInterval time.Duration
}

// New resolves the toolchain once, at boot.
//
// Resolution failures are not errors: a host with no Xcode and no Android SDK
// is the normal case for a cluster node, and it must boot exactly as it did
// before. What it gets is a Host that reports no capabilities, which every
// caller already has to handle — a nil Host does the same.
func New(cfg Config) *Host {
	goos := cfg.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	h := &Host{goos: goos, bootTimeout: cfg.BootTimeout, pollInterval: cfg.PollInterval}
	if h.bootTimeout <= 0 {
		h.bootTimeout = defaultBootTimeout
	}
	if h.pollInterval <= 0 {
		h.pollInterval = defaultPollInterval
	}

	// simctl ships inside Xcode's command-line tools and exists nowhere else.
	// Gating on GOOS as well as on the binary means a `xcrun` shim on a Linux
	// box cannot make this host claim simulators it has no way to run.
	if goos == "darwin" {
		h.xcrun = resolve(cfg.Xcrun, "xcrun")
	}
	h.adb = resolve(cfg.ADB, "adb", androidSDKPaths(goos, "platform-tools", "adb")...)
	h.emulator = resolve(cfg.Emulator, "emulator", androidSDKPaths(goos, "emulator", "emulator")...)

	log.Debug().
		Str("goos", goos).
		Bool("xcrun", h.xcrun != "").
		Bool("adb", h.adb != "").
		Bool("emulator", h.emulator != "").
		Msg("localdevice: host toolchain resolved")
	return h
}

// resolve takes the explicit value if there is one, then PATH, then the
// well-known install locations. PATH before the fallbacks so an operator with
// two SDKs gets the one their shell would have used.
func resolve(explicit, name string, fallbacks ...string) string {
	if explicit != "" {
		if p, err := exec.LookPath(explicit); err == nil {
			return p
		}
		// An explicit path that does not resolve is a misconfiguration, not a
		// reason to silently use a different binary than the one named.
		return ""
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	for _, candidate := range fallbacks {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate
		}
	}
	return ""
}

// androidSDKPaths are the places the SDK puts a tool when it is not on PATH.
// The Android tools are routinely installed by Android Studio, which does not
// touch the shell profile, so "not on PATH" is the common case rather than the
// broken one.
func androidSDKPaths(goos, dir, name string) []string {
	var roots []string
	for _, env := range []string{"ANDROID_HOME", "ANDROID_SDK_ROOT"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			roots = append(roots, v)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		switch goos {
		case "darwin":
			roots = append(roots, filepath.Join(home, "Library", "Android", "sdk"))
		default:
			roots = append(roots, filepath.Join(home, "Android", "Sdk"))
		}
	}
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		out = append(out, filepath.Join(root, dir, name))
	}
	return out
}

// SupportsIOSSimulators reports whether this host can run one. Both halves are
// required: macOS without Xcode's command-line tools has no simctl, and simctl
// exists on no other OS.
func (h *Host) SupportsIOSSimulators(context.Context) bool {
	return h != nil && h.goos == "darwin" && h.xcrun != ""
}

// SupportsAndroidEmulators reports whether this host can drive one.
//
// adb is mandatory — it is how the emulator is talked to once it is up — but the
// `emulator` binary is not: a host where somebody already started an AVD from
// Android Studio can drive it perfectly well without ever launching one itself,
// and refusing that host would be refusing a working device because of a
// missing convenience.
func (h *Host) SupportsAndroidEmulators(ctx context.Context) bool {
	if h == nil || h.adb == "" {
		return false
	}
	if h.emulator != "" {
		return true
	}
	serials, err := h.RunningEmulators(ctx)
	return err == nil && len(serials) > 0
}

// Catalog is everything this host could drive if somebody registered it.
//
// A failure on one side does not fail the other: a broken Android SDK must not
// make the simulators disappear from the settings page. Failures are logged and
// reported as an empty list, because "the host has none" and "asking failed" are
// the same thing to a form whose only job is to offer a choice.
func (h *Host) Catalog(ctx context.Context) (domain.LocalDeviceCatalog, error) {
	out := domain.EmptyLocalDeviceCatalog()
	if h == nil {
		return out, nil
	}
	if h.SupportsIOSSimulators(ctx) {
		sims, err := h.Simulators(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("localdevice: listing simulators failed")
		} else {
			out.IOS = sims
		}
	}
	if h.adb != "" || h.emulator != "" {
		avds, err := h.AVDs(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("localdevice: listing AVDs failed")
		} else {
			out.Android = avds
		}
	}
	return out, nil
}

// simctlList is the shape of `simctl list devices --json`. The map key is the
// runtime identifier ("com.apple.CoreSimulator.SimRuntime.iOS-17-4"), which is
// the only place the version appears.
type simctlList struct {
	Devices map[string][]struct {
		UDID        string `json:"udid"`
		Name        string `json:"name"`
		State       string `json:"state"`
		IsAvailable bool   `json:"isAvailable"`
	} `json:"devices"`
}

// Simulators lists every usable simulator, booted or shut down.
//
// Both states, because a shut-down simulator is the normal thing to register:
// the whole point of the connect step is that this host boots it. Unavailable
// ones are dropped — they are devices whose runtime was uninstalled, and simctl
// keeps listing them so the UI can show a repair prompt, which this is not.
func (h *Host) Simulators(ctx context.Context) ([]domain.LocalSimulator, error) {
	if h == nil || h.xcrun == "" {
		return []domain.LocalSimulator{}, nil
	}
	raw, err := h.output(ctx, listTimeout, h.xcrun, "simctl", "list", "devices", "--json")
	if err != nil {
		return nil, err
	}
	var parsed simctlList
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("decode simctl device list: %w", err)
	}
	out := []domain.LocalSimulator{}
	for runtimeID, devices := range parsed.Devices {
		name := runtimeName(runtimeID)
		for _, d := range devices {
			if !d.IsAvailable || d.UDID == "" {
				continue
			}
			out = append(out, domain.LocalSimulator{
				UDID:    d.UDID,
				Name:    d.Name,
				Runtime: name,
				State:   d.State,
			})
		}
	}
	// Map iteration order is random, and a list of simulators that reshuffles
	// on every page load is one nobody can point at. Runtime then name, so the
	// newest iOS does not sort between two older ones by device name.
	sortSimulators(out)
	return out, nil
}

// runtimeName turns simctl's runtime identifier into what a person calls it:
// "com.apple.CoreSimulator.SimRuntime.iOS-17-4" → "iOS 17.4". An identifier
// that does not follow the shape is returned unchanged rather than mangled —
// it is still more useful to the operator than an empty column.
func runtimeName(id string) string {
	const prefix = "com.apple.CoreSimulator.SimRuntime."
	trimmed := strings.TrimPrefix(id, prefix)
	if trimmed == id {
		return id
	}
	parts := strings.SplitN(trimmed, "-", 2)
	if len(parts) != 2 {
		return trimmed
	}
	return parts[0] + " " + strings.ReplaceAll(parts[1], "-", ".")
}

// RuntimeVersion is the numeric half of a runtime name — "iOS 17.4" → "17.4" —
// which is what Appium's platformVersion capability wants.
func RuntimeVersion(runtime string) string {
	fields := strings.Fields(runtime)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

func sortSimulators(sims []domain.LocalSimulator) {
	// Insertion sort: the list is a few dozen entries at most, and pulling in
	// sort.Slice for it would be the only reason this file needed the import.
	for i := 1; i < len(sims); i++ {
		for j := i; j > 0 && lessSimulator(sims[j], sims[j-1]); j-- {
			sims[j], sims[j-1] = sims[j-1], sims[j]
		}
	}
}

func lessSimulator(a, b domain.LocalSimulator) bool {
	if a.Runtime != b.Runtime {
		return a.Runtime < b.Runtime
	}
	return a.Name < b.Name
}

// Simulator finds one by UDID.
func (h *Host) Simulator(ctx context.Context, udid string) (domain.LocalSimulator, bool, error) {
	if err := ValidateSimulatorUDID(udid); err != nil {
		return domain.LocalSimulator{}, false, err
	}
	sims, err := h.Simulators(ctx)
	if err != nil {
		return domain.LocalSimulator{}, false, err
	}
	for _, s := range sims {
		if strings.EqualFold(s.UDID, udid) {
			return s, true, nil
		}
	}
	return domain.LocalSimulator{}, false, nil
}

// SimulatorBooted reports whether the simulator is up.
func (h *Host) SimulatorBooted(ctx context.Context, udid string) (bool, error) {
	sim, ok, err := h.Simulator(ctx, udid)
	if err != nil || !ok {
		return false, err
	}
	return strings.EqualFold(sim.State, "Booted"), nil
}

// BootSimulator brings one up and waits until it is actually usable.
//
// The two steps are not redundant. `simctl boot` returns as soon as the boot has
// been *started*, and a session created against a half-booted simulator fails in
// ways that read to an agent as a broken app — springboard not up, no window
// server yet. `bootstatus -b` is the wait, and it is also idempotent, which is
// why an already-booted device is not an error worth failing on.
func (h *Host) BootSimulator(ctx context.Context, udid string) error {
	if h == nil || h.xcrun == "" {
		return fmt.Errorf("this host has no iOS simulators")
	}
	if err := ValidateSimulatorUDID(udid); err != nil {
		return err
	}
	booted, err := h.SimulatorBooted(ctx, udid)
	if err != nil {
		return err
	}
	if !booted {
		if _, err := h.output(ctx, actionTimeout, h.xcrun, "simctl", "boot", udid); err != nil {
			// Booting a device that came up between the check and the call is a
			// race with the operator's own Simulator.app, not a failure.
			if !strings.Contains(strings.ToLower(err.Error()), "current state: booted") {
				return fmt.Errorf("boot simulator: %w", err)
			}
		}
	}
	waitCtx, cancel := context.WithTimeout(ctx, h.bootTimeout)
	defer cancel()
	if _, err := h.output(waitCtx, h.bootTimeout, h.xcrun, "simctl", "bootstatus", udid, "-b"); err != nil {
		return fmt.Errorf("wait for simulator boot: %w", err)
	}
	return nil
}

// ShutdownSimulator puts one back down. An already-shut-down device is a
// success: the caller's intent is a state, not a transition.
func (h *Host) ShutdownSimulator(ctx context.Context, udid string) error {
	if h == nil || h.xcrun == "" {
		return fmt.Errorf("this host has no iOS simulators")
	}
	if err := ValidateSimulatorUDID(udid); err != nil {
		return err
	}
	if _, err := h.output(ctx, actionTimeout, h.xcrun, "simctl", "shutdown", udid); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "current state: shutdown") {
			return nil
		}
		return fmt.Errorf("shutdown simulator: %w", err)
	}
	return nil
}

// AVDs lists the Android virtual devices this host has defined.
func (h *Host) AVDs(ctx context.Context) ([]string, error) {
	if h == nil || h.emulator == "" {
		return []string{}, nil
	}
	raw, err := h.output(ctx, listTimeout, h.emulator, "-list-avds")
	if err != nil {
		return nil, err
	}
	out := []string{}
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// The emulator binary prints diagnostics ("INFO | storing crashdata…")
		// onto the same stream as the names on some SDK versions. Filtering by
		// the name grammar rather than by known prefixes means a new diagnostic
		// does not become a device nobody can boot.
		if line == "" || avdNamePattern.FindString(line) != line {
			continue
		}
		out = append(out, line)
	}
	return out, scanner.Err()
}

// AVDExists reports whether a name is one of this host's AVDs.
func (h *Host) AVDExists(ctx context.Context, name string) (bool, error) {
	if err := ValidateAVDName(name); err != nil {
		return false, err
	}
	avds, err := h.AVDs(ctx)
	if err != nil {
		return false, err
	}
	for _, a := range avds {
		if a == name {
			return true, nil
		}
	}
	// A host with no `emulator` binary can still be driving an AVD somebody
	// started from Android Studio, and refusing to register that one because
	// the inventory command is missing would refuse a device that works.
	if h.emulator == "" {
		serial, serr := h.EmulatorSerial(ctx, name)
		return serial != "", serr
	}
	return false, nil
}

// RunningEmulators are the adb serials of every emulator currently up.
func (h *Host) RunningEmulators(ctx context.Context) ([]string, error) {
	if h == nil || h.adb == "" {
		return nil, nil
	}
	raw, err := h.output(ctx, listTimeout, h.adb, "devices")
	if err != nil {
		return nil, err
	}
	var out []string
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		// Only the fully-attached ones: a serial in "offline" or
		// "unauthorized" is a device Appium cannot create a session against,
		// and reporting it as running would hand a run a dead phone.
		if len(fields) < 2 || fields[1] != "device" {
			continue
		}
		if emulatorSerialPattern.MatchString(fields[0]) {
			out = append(out, fields[0])
		}
	}
	return out, scanner.Err()
}

// EmulatorSerial finds which serial, if any, the named AVD is running on.
//
// Asked of each serial rather than derived from the console port, because the
// port is allocated in start order: an AVD that was restarted is on a different
// one, and a registration that remembered 5554 would drive whichever emulator
// happens to be there now.
func (h *Host) EmulatorSerial(ctx context.Context, avd string) (string, error) {
	if err := ValidateAVDName(avd); err != nil {
		return "", err
	}
	serials, err := h.RunningEmulators(ctx)
	if err != nil {
		return "", err
	}
	for _, serial := range serials {
		name, err := h.emulatorAVDName(ctx, serial)
		if err != nil {
			log.Debug().Err(err).Str("serial", serial).Msg("localdevice: asking an emulator its AVD name failed")
			continue
		}
		if name == avd {
			return serial, nil
		}
	}
	return "", nil
}

// emulatorAVDName asks one emulator what it is. `adb emu avd name` answers with
// the name and then a line saying OK, which is the console protocol's
// acknowledgement rather than part of the answer.
func (h *Host) emulatorAVDName(ctx context.Context, serial string) (string, error) {
	if err := ValidateEmulatorSerial(serial); err != nil {
		return "", err
	}
	raw, err := h.output(ctx, listTimeout, h.adb, "-s", serial, "emu", "avd", "name")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "OK" {
			continue
		}
		return line, nil
	}
	return "", nil
}

// StartEmulator brings an AVD up if it is not already, and returns the adb
// serial it is on — which is what Appium is handed as the udid.
//
// Headless (-no-window -no-audio) because the device is a test fixture driven by
// an agent: a window would steal focus on the operator's desktop every time a
// QA task started, and the audio device is one more thing to fail on a machine
// that may have none.
func (h *Host) StartEmulator(ctx context.Context, avd string) (string, error) {
	if h == nil || h.adb == "" {
		return "", fmt.Errorf("this host has no Android SDK platform-tools (adb)")
	}
	if err := ValidateAVDName(avd); err != nil {
		return "", err
	}
	serial, err := h.EmulatorSerial(ctx, avd)
	if err != nil {
		return "", err
	}
	if serial == "" {
		if h.emulator == "" {
			return "", fmt.Errorf("%q is not running and this host has no emulator binary to start it with", avd)
		}
		if err := h.spawnEmulator(avd); err != nil {
			return "", err
		}
		if serial, err = h.waitForSerial(ctx, avd); err != nil {
			return "", err
		}
	}
	if err := h.waitForBoot(ctx, serial); err != nil {
		return "", err
	}
	return serial, nil
}

// spawnEmulator starts the process and deliberately does NOT tie it to the
// caller's context.
//
// An emulator outlives the HTTP request that asked for it — that is the whole
// point of booting one — so an exec.CommandContext would kill it the moment the
// settings page got its response. The Wait is there only to reap the child when
// it eventually exits; without it the process table fills with zombies on a
// host that starts and stops emulators all day.
func (h *Host) spawnEmulator(avd string) error {
	cmd := exec.Command(h.emulator, "-avd", avd, "-no-window", "-no-audio")
	// Scrubbed, like every other child this process spawns (internal/platform/childenv).
	// The emulator is long-lived and reachable over adb, so of all the children
	// here it is the LEAST safe one to hand this process's environment to: it
	// outlives the request that started it and an app running inside it can read
	// its own process environment. ANDROID_HOME/ANDROID_SDK_ROOT/JAVA_HOME and
	// PATH/HOME are all on the allowlist, which is everything an emulator needs
	// to find its SDK.
	cmd.Env = childenv.For(os.Environ(), nil)
	// The child's stdio is discarded rather than inherited: the emulator is
	// chatty, and interleaving its log into this process's structured output
	// makes both unreadable.
	cmd.Stdout, cmd.Stderr = nil, nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start emulator %q: %w", avd, err)
	}
	go func() { _ = cmd.Wait() }()
	log.Info().Str("avd", avd).Int("pid", cmd.Process.Pid).Msg("localdevice: emulator starting")
	return nil
}

// waitForSerial polls until adb sees the AVD. Polling rather than
// `adb wait-for-device`, because that waits for ANY device: on a host with a
// phone plugged in or a second emulator already up it returns immediately and
// the caller carries on with the wrong serial.
func (h *Host) waitForSerial(ctx context.Context, avd string) (string, error) {
	deadline := time.Now().Add(h.bootTimeout)
	for {
		serial, err := h.EmulatorSerial(ctx, avd)
		if err != nil {
			return "", err
		}
		if serial != "" {
			return serial, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("emulator %q did not appear in adb within %s", avd, h.bootTimeout)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(h.pollInterval):
		}
	}
}

// waitForBoot waits for the system to finish coming up, not merely for adb to
// answer. adb answers while Android is still on the boot animation, and a
// session created then fails against an app that is not installed yet.
func (h *Host) waitForBoot(ctx context.Context, serial string) error {
	if err := ValidateEmulatorSerial(serial); err != nil {
		return err
	}
	waitCtx, cancel := context.WithTimeout(ctx, h.bootTimeout)
	defer cancel()
	if _, err := h.output(waitCtx, h.bootTimeout, h.adb, "-s", serial, "wait-for-device"); err != nil {
		return fmt.Errorf("wait for %s: %w", serial, err)
	}
	deadline := time.Now().Add(h.bootTimeout)
	for {
		raw, err := h.output(waitCtx, listTimeout, h.adb, "-s", serial, "shell", "getprop", "sys.boot_completed")
		if err == nil && strings.TrimSpace(string(raw)) == "1" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("emulator %s did not finish booting within %s", serial, h.bootTimeout)
		}
		select {
		case <-waitCtx.Done():
			return waitCtx.Err()
		case <-time.After(h.pollInterval):
		}
	}
}

// StopEmulator shuts one down through the emulator console. `emu kill` rather
// than killing the process: this code did not necessarily start it, and the
// console is the only handle that works for an AVD somebody launched from
// Android Studio.
func (h *Host) StopEmulator(ctx context.Context, serial string) error {
	if h == nil || h.adb == "" {
		return fmt.Errorf("this host has no Android SDK platform-tools (adb)")
	}
	if err := ValidateEmulatorSerial(serial); err != nil {
		return err
	}
	if _, err := h.output(ctx, actionTimeout, h.adb, "-s", serial, "emu", "kill"); err != nil {
		return fmt.Errorf("stop emulator %s: %w", serial, err)
	}
	return nil
}

// output runs one command and returns its stdout.
//
// The argument vector is passed to exec.Command as a vector — no shell, no
// interpolation, no expansion. Everything variable in it has been through one
// of the Validate* functions above before reaching here.
func (h *Host) output(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	if name == "" {
		return nil, fmt.Errorf("no binary for this operation on this host")
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	// Same scrub as spawnEmulator: adb and simctl need PATH, HOME and the SDK
	// locations, none of which are credentials, and nothing here needs this
	// process's environment. `adb shell` in particular runs an arbitrary command
	// on a device, so its environment is the one place a secret must not be.
	cmd.Env = childenv.For(os.Environ(), nil)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		return nil, fmt.Errorf("%s %s: %s", filepath.Base(name), strings.Join(args, " "), truncate(detail, 400))
	}
	return out, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
