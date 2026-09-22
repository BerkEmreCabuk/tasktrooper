package mobiledevice

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/makifbaysal/tasktrooper/server/internal/adapter/cloud/deviceagent"
	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type Store interface {
	List(ctx context.Context) ([]domain.MobileDevice, error)
	Get(ctx context.Context, id uuid.UUID) (domain.MobileDevice, bool, error)
	Create(ctx context.Context, in domain.MobileDevice, pin, token *string) (domain.MobileDevice, error)
	Update(ctx context.Context, in domain.MobileDevice, pin, token *string) (domain.MobileDevice, error)
	MarkConnected(ctx context.Context, id uuid.UUID) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type ReloadFunc func(ctx context.Context, devices []domain.MobileDevice) error

type HubProbe interface {
	Configured() bool
	ProbeDevice(ctx context.Context, udid string) bool
	Busy(udid string) bool
}

type LocalHost interface {
	SupportsIOSSimulators(ctx context.Context) bool
	SupportsAndroidEmulators(ctx context.Context) bool
	Catalog(ctx context.Context) (domain.LocalDeviceCatalog, error)

	Simulator(ctx context.Context, udid string) (domain.LocalSimulator, bool, error)
	SimulatorBooted(ctx context.Context, udid string) (bool, error)
	BootSimulator(ctx context.Context, udid string) error
	ShutdownSimulator(ctx context.Context, udid string) error

	AVDExists(ctx context.Context, name string) (bool, error)
	EmulatorSerial(ctx context.Context, avd string) (string, error)
	StartEmulator(ctx context.Context, avd string) (string, error)
	StopEmulator(ctx context.Context, serial string) error
}

type Service struct {
	store  Store
	bridge *deviceagent.Client
	reload ReloadFunc

	envDevice domain.MobileDevice

	probe HubProbe

	host LocalHost
}

func NewService(store Store, bridge *deviceagent.Client, envDevice domain.MobileDevice) *Service {
	if envDevice.Configured() {
		envDevice.Platform = domain.PlatformAndroid
		if envDevice.Name == "" {

			envDevice.Name = "Ortam cihazı"
		}
	}
	return &Service{store: store, bridge: bridge, envDevice: envDevice}
}

func (s *Service) SetReloader(fn ReloadFunc) { s.reload = fn }
func (s *Service) SetProbe(p HubProbe)       { s.probe = p }

func (s *Service) SetLocalHost(h LocalHost) { s.host = h }

func (s *Service) LocalCatalog(ctx context.Context) (domain.LocalDeviceCatalog, error) {
	if s == nil || s.host == nil {
		return domain.EmptyLocalDeviceCatalog(), nil
	}
	return s.host.Catalog(ctx)
}

func (s *Service) Effective(ctx context.Context) ([]domain.MobileDevice, string, error) {
	if s == nil || s.store == nil {
		return s.envFallback(), "env", nil
	}
	stored, err := s.store.List(ctx)
	if err != nil {
		return nil, "", err
	}
	var configured []domain.MobileDevice
	for _, d := range stored {
		if d.Configured() {
			configured = append(configured, d)
		}
	}
	if len(configured) > 0 {
		return configured, "settings", nil
	}
	return s.envFallback(), "env", nil
}

func (s *Service) envFallback() []domain.MobileDevice {
	if !s.envDevice.Configured() {
		return nil
	}
	return []domain.MobileDevice{s.envDevice}
}

func (s *Service) List(ctx context.Context) ([]domain.MobileDevice, error) {
	if s.store == nil {
		return s.envFallback(), nil
	}
	stored, err := s.store.List(ctx)
	if err != nil {
		return nil, err
	}
	if len(stored) == 0 {
		return s.envFallback(), nil
	}
	return stored, nil
}

func (s *Service) Statuses(ctx context.Context) ([]domain.MobileDeviceStatus, error) {
	devices, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domain.MobileDeviceStatus, 0, len(devices))
	for _, d := range devices {
		out = append(out, s.statusOf(ctx, d))
	}
	return out, nil
}

func (s *Service) statusOf(ctx context.Context, device domain.MobileDevice) domain.MobileDeviceStatus {
	source := "settings"
	if device.Managed() {
		source = "env"
	}
	status := domain.MobileDeviceStatus{
		ID:              idString(device.ID),
		Name:            device.Name,
		Platform:        platformOr(device.Platform),
		Kind:            device.DeviceKind(),
		Managed:         device.Managed(),
		Configured:      device.Configured(),
		HubURL:          device.HubURL,
		DeviceUDID:      device.DeviceUDID,
		PlatformVersion: device.PlatformVersion,
		DeviceAddr:      device.DeviceAddr,
		HasPIN:          device.DevicePIN != "",
		HasToken:        device.HubToken != "",
		LastConnectedAt: device.LastConnectedAt,
		Source:          source,
		HubManaged:      s.envDevice.HubURL != "",
	}
	if status.HubURL == "" {

		status.HubURL = s.envDevice.HubURL
	}
	if s.probe != nil && s.probe.Configured() && device.DeviceUDID != "" {

		switch {
		case s.probe.ProbeDevice(ctx, device.DeviceUDID):
			status.HubReachable = true
		case s.probe.Busy(device.DeviceUDID):
			status.HubReachable, status.DeviceBusy = true, true
		default:

			status.HubReachable, status.DeviceBusy = s.hubHeld(ctx, device)
		}
	}

	if device.Local() {
		status.DeviceOnline, status.Detail = s.localOnline(ctx, device)
		return status
	}
	if s.bridge.Configured() && device.DeviceAddr != "" {
		bridgeStatus, berr := s.bridge.Status(ctx, device.DeviceAddr)
		if berr != nil {
			status.Detail = berr.Error()
		} else {
			status.DeviceOnline = bridgeStatus.Online
			if !bridgeStatus.Online && bridgeStatus.Detail != "" {
				status.Detail = bridgeStatus.Detail
			}
		}
	}
	return status
}

func (s *Service) localOnline(ctx context.Context, device domain.MobileDevice) (online bool, detail string) {
	if s.host == nil {
		return false, "this host has no local simulators or emulators"
	}
	switch device.DeviceKind() {
	case domain.DeviceKindIOSSimulator:
		booted, err := s.host.SimulatorBooted(ctx, localTargetOf(device))
		if err != nil {
			return false, err.Error()
		}
		return booted, ""
	case domain.DeviceKindAndroidEmulator:
		serial, err := s.host.EmulatorSerial(ctx, localTargetOf(device))
		if err != nil {
			return false, err.Error()
		}
		return serial != "", ""
	}
	return false, ""
}

func localTargetOf(device domain.MobileDevice) string {
	if addr := strings.TrimSpace(device.DeviceAddr); addr != "" {
		return addr
	}
	return strings.TrimSpace(device.DeviceUDID)
}

func (s *Service) hubHeld(ctx context.Context, device domain.MobileDevice) (reachable, busy bool) {
	if device.Local() {

		online, _ := s.localOnline(ctx, device)
		return online, online
	}
	if !s.bridge.Configured() || device.DeviceAddr == "" {
		return false, false
	}
	st, err := s.bridge.Status(ctx, device.DeviceAddr)
	if err != nil || !st.Online {
		return false, false
	}
	return true, true
}

func idString(id uuid.UUID) string {
	if id == uuid.Nil {
		return ""
	}
	return id.String()
}

func platformOr(p string) string {
	if p == "" {
		return domain.PlatformAndroid
	}
	return p
}

func (s *Service) Add(ctx context.Context, req domain.SaveMobileDeviceRequest) ([]domain.MobileDeviceStatus, error) {
	if s.store == nil {
		return nil, fmt.Errorf("mobile device registration is not available in this build")
	}
	kind, ok := domain.ValidDeviceKind(req.Kind)
	if !ok {
		return nil, fmt.Errorf("unknown device kind %q — expected remote_adb, ios_simulator or android_emulator", req.Kind)
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("give the device a name")
	}
	device, err := s.resolveTarget(ctx, kind, req)
	if err != nil {
		return nil, err
	}
	hub, token, err := s.hubFor(req)
	if err != nil {
		return nil, err
	}
	device.Name = name
	device.HubURL = hub
	if v := strings.TrimSpace(req.PlatformVersion); v != "" {

		device.PlatformVersion = v
	}
	created, err := s.store.Create(ctx, device, req.DevicePIN, token)
	if err != nil {
		return nil, err
	}
	if _, cerr := s.connect(ctx, created); cerr != nil {
		log.Info().Err(cerr).Str("device", created.Name).Msg("mobile device: registered but not reachable yet")
	}
	return s.reloadAndReport(ctx)
}

func (s *Service) resolveTarget(ctx context.Context, kind string, req domain.SaveMobileDeviceRequest) (domain.MobileDevice, error) {
	device := domain.MobileDevice{Kind: kind, Platform: domain.PlatformForKind(kind)}
	switch kind {
	case domain.DeviceKindIOSSimulator:
		if s.host == nil || !s.host.SupportsIOSSimulators(ctx) {

			return device, fmt.Errorf("this host has no iOS simulators: Appium's XCUITest driver needs a macOS host with Xcode, and this one is not")
		}
		udid := firstNonEmpty(req.DeviceUDID, req.DeviceAddr)
		if udid == "" {
			return device, fmt.Errorf("choose a simulator — its UDID comes from the local device catalog")
		}
		sim, found, err := s.host.Simulator(ctx, udid)
		if err != nil {
			return device, err
		}
		if !found {
			return device, fmt.Errorf("no simulator with UDID %s on this host", udid)
		}

		device.DeviceUDID, device.DeviceAddr = sim.UDID, sim.UDID
		device.PlatformVersion = runtimeVersion(sim.Runtime)
		return device, nil

	case domain.DeviceKindAndroidEmulator:
		if s.host == nil || !s.host.SupportsAndroidEmulators(ctx) {
			return device, fmt.Errorf("this host has no Android emulators — install the Android SDK's platform-tools and emulator, or attach a physical device instead")
		}
		avd := firstNonEmpty(req.DeviceAddr, req.DeviceUDID)
		if avd == "" {
			return device, fmt.Errorf("choose an AVD — its name comes from the local device catalog")
		}
		exists, err := s.host.AVDExists(ctx, avd)
		if err != nil {
			return device, err
		}
		if !exists {
			return device, fmt.Errorf("no AVD named %q on this host", avd)
		}

		device.DeviceAddr = avd
		return device, nil

	default:
		platform, ok := domain.ValidPlatform(req.Platform)
		if !ok {
			return device, fmt.Errorf("only Android devices can be driven from this installation: Appium's iOS driver needs a macOS host, and the cluster has none")
		}
		addr := strings.TrimSpace(req.DeviceAddr)
		if addr == "" {
			return device, fmt.Errorf("the device address is required — it is the phone's Tailscale address plus the port on its wireless debugging screen")
		}
		device.Platform, device.DeviceAddr = platform, addr
		return device, nil
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if t := strings.TrimSpace(v); t != "" {
			return t
		}
	}
	return ""
}

func runtimeVersion(runtime string) string {
	fields := strings.Fields(runtime)
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

func (s *Service) Update(ctx context.Context, id uuid.UUID, req domain.SaveMobileDeviceRequest) ([]domain.MobileDeviceStatus, error) {
	device, err := s.registered(ctx, id)
	if err != nil {
		return nil, err
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		device.Name = name
	}
	if addr := firstNonEmpty(req.DeviceAddr, req.DeviceUDID); addr != "" {
		if device.Local() {

			retargeted, rerr := s.resolveTarget(ctx, device.DeviceKind(), req)
			if rerr != nil {
				return nil, rerr
			}
			device.DeviceAddr, device.DeviceUDID = retargeted.DeviceAddr, retargeted.DeviceUDID
			if retargeted.PlatformVersion != "" {
				device.PlatformVersion = retargeted.PlatformVersion
			}
		} else {
			device.DeviceAddr = addr
		}
	}
	if v := strings.TrimSpace(req.PlatformVersion); v != "" {
		device.PlatformVersion = v
	}
	if hub := strings.TrimSpace(req.HubURL); hub != "" {
		device.HubURL = hub
	}
	if _, err := s.store.Update(ctx, device, req.DevicePIN, req.HubToken); err != nil {
		return nil, err
	}

	if _, cerr := s.connect(ctx, device); cerr != nil {
		log.Info().Err(cerr).Str("device", device.Name).Msg("mobile device: saved but not reachable yet")
	}
	return s.reloadAndReport(ctx)
}

func (s *Service) Remove(ctx context.Context, id uuid.UUID) ([]domain.MobileDeviceStatus, error) {
	device, err := s.registered(ctx, id)
	if err != nil {
		return nil, err
	}
	if derr := s.disconnect(ctx, device); derr != nil {
		log.Warn().Err(derr).Msg("mobile device: disconnect on unregister failed")
	}
	if err := s.store.Delete(ctx, id); err != nil {
		return nil, err
	}
	return s.reloadAndReport(ctx)
}

func (s *Service) Pair(ctx context.Context, id uuid.UUID, req domain.PairMobileDeviceRequest) ([]domain.MobileDeviceStatus, error) {
	if !s.bridge.Configured() {
		return nil, fmt.Errorf("no device bridge is configured — pairing must be done on the Appium host")
	}
	device, err := s.registered(ctx, id)
	if err != nil {
		return nil, err
	}
	if device.Local() {

		return nil, fmt.Errorf("pairing applies to a physical phone — this device runs on the host and only needs connecting")
	}
	res, err := s.bridge.Pair(ctx, strings.TrimSpace(req.PairAddr), strings.TrimSpace(req.Code))
	if err != nil {
		return nil, err
	}
	if !res.OK {
		return nil, fmt.Errorf("pairing failed: %s", res.Detail)
	}
	if _, cerr := s.connect(ctx, device); cerr != nil {
		log.Warn().Err(cerr).Msg("mobile device: connect after pairing failed")
	}
	return s.reloadAndReport(ctx)
}

func (s *Service) Connect(ctx context.Context, id uuid.UUID) ([]domain.MobileDeviceStatus, error) {
	device, err := s.registered(ctx, id)
	if err != nil {
		return nil, err
	}
	if _, cerr := s.connect(ctx, device); cerr != nil {
		return nil, cerr
	}
	return s.reloadAndReport(ctx)
}

func (s *Service) connect(ctx context.Context, device domain.MobileDevice) (domain.MobileDevice, error) {
	switch device.DeviceKind() {
	case domain.DeviceKindIOSSimulator:
		return s.connectSimulator(ctx, device)
	case domain.DeviceKindAndroidEmulator:
		return s.connectEmulator(ctx, device)
	}
	if device.DeviceAddr == "" {
		return device, fmt.Errorf("no device address is registered")
	}
	if !s.bridge.Configured() {
		return device, fmt.Errorf("no device bridge is configured")
	}
	res, err := s.bridge.Connect(ctx, device.DeviceAddr)
	if err != nil {
		return device, err
	}
	if !res.OK {
		return device, fmt.Errorf("could not connect: %s", res.Detail)
	}
	if s.store == nil || device.Managed() {
		return device, nil
	}
	if local := strings.TrimSpace(res.LocalAddr); local != "" && local != device.DeviceUDID {
		device.DeviceUDID = local
		if _, err := s.store.Update(ctx, device, nil, nil); err != nil {
			return device, err
		}
	}
	if err := s.store.MarkConnected(ctx, device.ID); err != nil {
		log.Warn().Err(err).Msg("mobile device: stamping the connection failed")
	}
	return device, nil
}

func (s *Service) connectSimulator(ctx context.Context, device domain.MobileDevice) (domain.MobileDevice, error) {
	if s.host == nil {
		return device, fmt.Errorf("this host has no iOS simulators")
	}
	udid := localTargetOf(device)
	if udid == "" {
		return device, fmt.Errorf("no simulator is registered")
	}
	if err := s.host.BootSimulator(ctx, udid); err != nil {
		return device, err
	}

	changed := device.DeviceUDID != udid
	device.DeviceUDID = udid
	if sim, found, err := s.host.Simulator(ctx, udid); err == nil && found {
		if v := runtimeVersion(sim.Runtime); v != "" && v != device.PlatformVersion {
			device.PlatformVersion, changed = v, true
		}
	}
	return s.stampConnected(ctx, device, changed)
}

func (s *Service) connectEmulator(ctx context.Context, device domain.MobileDevice) (domain.MobileDevice, error) {
	if s.host == nil {
		return device, fmt.Errorf("this host has no Android emulators")
	}
	avd := localTargetOf(device)
	if avd == "" {
		return device, fmt.Errorf("no AVD is registered")
	}
	serial, err := s.host.StartEmulator(ctx, avd)
	if err != nil {
		return device, err
	}
	if serial == "" {
		return device, fmt.Errorf("emulator %q reported no adb serial", avd)
	}
	changed := serial != device.DeviceUDID
	device.DeviceUDID = serial
	return s.stampConnected(ctx, device, changed)
}

func (s *Service) stampConnected(ctx context.Context, device domain.MobileDevice, changed bool) (domain.MobileDevice, error) {
	if s.store == nil || device.Managed() {
		return device, nil
	}
	if changed {
		if _, err := s.store.Update(ctx, device, nil, nil); err != nil {
			return device, err
		}
	}
	if err := s.store.MarkConnected(ctx, device.ID); err != nil {
		log.Warn().Err(err).Msg("mobile device: stamping the connection failed")
	}
	return device, nil
}

func (s *Service) disconnect(ctx context.Context, device domain.MobileDevice) error {
	switch device.DeviceKind() {
	case domain.DeviceKindIOSSimulator:
		if s.host == nil {
			return nil
		}
		return s.host.ShutdownSimulator(ctx, localTargetOf(device))
	case domain.DeviceKindAndroidEmulator:
		if s.host == nil {
			return nil
		}
		serial := strings.TrimSpace(device.DeviceUDID)
		if serial == "" {

			found, err := s.host.EmulatorSerial(ctx, localTargetOf(device))
			if err != nil {
				return err
			}
			serial = found
		}
		if serial == "" {
			return nil
		}
		return s.host.StopEmulator(ctx, serial)
	}
	if !s.bridge.Configured() || device.DeviceAddr == "" {
		return nil
	}
	_, err := s.bridge.Disconnect(ctx, device.DeviceAddr)
	return err
}

func (s *Service) registered(ctx context.Context, id uuid.UUID) (domain.MobileDevice, error) {
	if s.store == nil {
		return domain.MobileDevice{}, fmt.Errorf("mobile device registration is not available in this build")
	}
	if id == uuid.Nil {
		return domain.MobileDevice{}, fmt.Errorf("this device comes from the cluster's own configuration and cannot be changed here")
	}
	device, ok, err := s.store.Get(ctx, id)
	if err != nil {
		return domain.MobileDevice{}, err
	}
	if !ok {
		return domain.MobileDevice{}, fmt.Errorf("no such registered device")
	}
	return device, nil
}

func (s *Service) hubFor(req domain.SaveMobileDeviceRequest) (string, *string, error) {
	hub := strings.TrimSpace(req.HubURL)
	if hub == "" {
		hub = strings.TrimSpace(s.envDevice.HubURL)
	}
	if hub == "" {
		return "", nil, fmt.Errorf("the Appium hub address is not configured in this installation — set MOBILE_APPIUM_HUB_URL on the tenant")
	}
	token := req.HubToken
	if token == nil && s.envDevice.HubToken != "" {

		env := s.envDevice.HubToken
		token = &env
	}
	return hub, token, nil
}

func (s *Service) reloadAndReport(ctx context.Context) ([]domain.MobileDeviceStatus, error) {
	devices, _, err := s.Effective(ctx)
	if err == nil {
		s.applyReload(ctx, devices)
	}
	return s.Statuses(ctx)
}

func (s *Service) applyReload(ctx context.Context, devices []domain.MobileDevice) {
	if s.reload == nil {
		return
	}
	if err := s.reload(ctx, devices); err != nil {
		log.Warn().Err(err).Msg("mobile device: applying the new registrations failed")
	}
}
