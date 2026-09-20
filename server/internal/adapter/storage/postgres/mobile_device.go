package postgres

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/domain/secrets"
)

// MobileDeviceStore holds the registered test devices.
//
// The PIN and the hub token are encrypted at rest with the same cipher
// app_settings uses for github_token — see SetCipher for why it has to be
// handed in at boot rather than derived lazily.
type MobileDeviceStore struct {
	pool *DB

	cipherOnce sync.Once
	cipher     *secrets.Cipher
	cipherErr  error
}

func NewMobileDeviceStore(pool *DB) *MobileDeviceStore {
	return &MobileDeviceStore{pool: pool}
}

// SetCipher injects the cipher before runtime.scrubProcessSecrets wipes
// MCP_SECRETS_KEY from the environment. Without it every read of the PIN would
// fail after boot with a missing-key error, which surfaces to the operator as
// a device that cannot be unlocked rather than as a misconfiguration.
func (s *MobileDeviceStore) SetCipher(c *secrets.Cipher, err error) {
	s.cipherOnce.Do(func() { s.cipher, s.cipherErr = c, err })
}

func (s *MobileDeviceStore) getCipher() (*secrets.Cipher, error) {
	s.cipherOnce.Do(func() { s.cipher, s.cipherErr = secrets.NewCipherFromEnv() })
	return s.cipher, s.cipherErr
}

const mobileDeviceColumns = `id, name, platform, kind, hub_url, device_udid, platform_version,
	device_addr, pin_encrypted, token_encrypted, last_connected_at, updated_at`

// List returns every registration, oldest first, secrets decrypted.
//
// Registration order rather than name order: it is the order the local ports
// were allocated in, so the list on the screen matches what `adb devices`
// prints, and a phone does not move up the page because somebody renamed it.
func (s *MobileDeviceStore) List(ctx context.Context) ([]domain.MobileDevice, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+mobileDeviceColumns+`
		FROM mobile_devices ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list mobile devices: %w", err)
	}
	defer rows.Close()
	var out []domain.MobileDevice
	for rows.Next() {
		d, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Get returns one registration. A missing row is not an error: a device can be
// removed while a settings page still holds its id, and that is a stale tab
// rather than a fault.
func (s *MobileDeviceStore) Get(ctx context.Context, id uuid.UUID) (domain.MobileDevice, bool, error) {
	row := s.pool.QueryRow(ctx, `SELECT `+mobileDeviceColumns+`
		FROM mobile_devices WHERE id = $1`, id)
	d, err := s.scan(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.MobileDevice{}, false, nil
	}
	if err != nil {
		return domain.MobileDevice{}, false, err
	}
	return d, true, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func (s *MobileDeviceStore) scan(row scanner) (domain.MobileDevice, error) {
	var d domain.MobileDevice
	var pinCT, tokenCT []byte
	if err := row.Scan(&d.ID, &d.Name, &d.Platform, &d.Kind, &d.HubURL, &d.DeviceUDID,
		&d.PlatformVersion, &d.DeviceAddr, &pinCT, &tokenCT,
		&d.LastConnectedAt, &d.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.MobileDevice{}, err
		}
		return domain.MobileDevice{}, fmt.Errorf("read mobile device: %w", err)
	}
	if len(pinCT) == 0 && len(tokenCT) == 0 {
		return d, nil
	}
	cipher, cerr := s.getCipher()
	if cerr != nil {
		return domain.MobileDevice{}, cerr
	}
	var err error
	if len(pinCT) > 0 {
		if d.DevicePIN, err = cipher.Decrypt(pinCT); err != nil {
			return domain.MobileDevice{}, fmt.Errorf("decrypt device pin: %w", err)
		}
	}
	if len(tokenCT) > 0 {
		if d.HubToken, err = cipher.Decrypt(tokenCT); err != nil {
			return domain.MobileDevice{}, fmt.Errorf("decrypt hub token: %w", err)
		}
	}
	return d, nil
}

func (s *MobileDeviceStore) encrypt(pin, token *string) (pinCT, tokenCT []byte, err error) {
	if pin == nil && token == nil {
		return nil, nil, nil
	}
	cipher, err := s.getCipher()
	if err != nil {
		return nil, nil, err
	}
	if pin != nil && *pin != "" {
		if pinCT, err = cipher.Encrypt(*pin); err != nil {
			return nil, nil, fmt.Errorf("encrypt device pin: %w", err)
		}
	}
	if token != nil && *token != "" {
		if tokenCT, err = cipher.Encrypt(*token); err != nil {
			return nil, nil, fmt.Errorf("encrypt hub token: %w", err)
		}
	}
	return pinCT, tokenCT, nil
}

// Create registers a new device and returns it with its assigned id.
func (s *MobileDeviceStore) Create(ctx context.Context, in domain.MobileDevice, pin, token *string) (domain.MobileDevice, error) {
	pinCT, tokenCT, err := s.encrypt(pin, token)
	if err != nil {
		return domain.MobileDevice{}, err
	}
	var id uuid.UUID
	// in.DeviceKind() rather than in.Kind: a caller that left it blank means
	// the phone the table has always held, and writing '' would put a value in
	// the column that no switch in the code recognises.
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO mobile_devices (name, platform, kind, hub_url, device_udid, platform_version,
		                            device_addr, pin_encrypted, token_encrypted)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id
	`, in.Name, in.Platform, in.DeviceKind(), in.HubURL, in.DeviceUDID, in.PlatformVersion,
		in.DeviceAddr, pinCT, tokenCT).Scan(&id); err != nil {
		return domain.MobileDevice{}, fmt.Errorf("create mobile device: %w", err)
	}
	saved, _, err := s.Get(ctx, id)
	return saved, err
}

// Update rewrites a registration.
//
// A nil PIN or token means "leave what is stored alone", an empty string means
// "clear it". Without that distinction the settings form would have to re-send
// the PIN on every edit — and to re-send it, it would first have to display it,
// which is the one thing a stored credential must not do.
func (s *MobileDeviceStore) Update(ctx context.Context, in domain.MobileDevice, pin, token *string) (domain.MobileDevice, error) {
	pinCT, tokenCT, err := s.encrypt(pin, token)
	if err != nil {
		return domain.MobileDevice{}, err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE mobile_devices SET
			name             = $2,
			platform         = $3,
			kind             = $4,
			hub_url          = $5,
			device_udid      = $6,
			platform_version = $7,
			device_addr      = $8,
			-- $11/$12 say whether this write is touching the credential at all.
			pin_encrypted    = CASE WHEN $11 THEN $9 ELSE pin_encrypted END,
			token_encrypted  = CASE WHEN $12 THEN $10 ELSE token_encrypted END,
			updated_at       = now()
		WHERE id = $1
	`, in.ID, in.Name, in.Platform, in.DeviceKind(), in.HubURL, in.DeviceUDID, in.PlatformVersion,
		in.DeviceAddr, pinCT, tokenCT, pin != nil, token != nil)
	if err != nil {
		return domain.MobileDevice{}, fmt.Errorf("update mobile device: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domain.MobileDevice{}, fmt.Errorf("no such registered device")
	}
	saved, _, err := s.Get(ctx, in.ID)
	return saved, err
}

// MarkConnected stamps a successful pair/connect, so the UI can distinguish
// "these values were typed in" from "the phone answered".
func (s *MobileDeviceStore) MarkConnected(ctx context.Context, id uuid.UUID) error {
	if _, err := s.pool.Exec(ctx,
		`UPDATE mobile_devices SET last_connected_at = now(), updated_at = now() WHERE id = $1`, id); err != nil {
		return fmt.Errorf("mark mobile device connected: %w", err)
	}
	return nil
}

// Delete unregisters one device, credentials included.
func (s *MobileDeviceStore) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := s.pool.Exec(ctx, `DELETE FROM mobile_devices WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete mobile device: %w", err)
	}
	return nil
}
