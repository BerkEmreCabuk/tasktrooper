package postgres

import (
	"context"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

type PushDeviceStore struct {
	pool *DB
}

func NewPushDeviceStore(pool *DB) *PushDeviceStore {
	return &PushDeviceStore{pool: pool}
}

func (s *PushDeviceStore) Upsert(ctx context.Context, deviceToken, platform string) (domain.PushDevice, error) {
	var d domain.PushDevice
	err := s.pool.QueryRow(ctx, `
		INSERT INTO push_devices (device_token, platform)
		VALUES ($1, $2)
		ON CONFLICT (tenant_id, device_token) DO UPDATE SET platform = EXCLUDED.platform, updated_at = now()
		RETURNING id, device_token, platform, created_at, updated_at
	`, deviceToken, platform).Scan(&d.ID, &d.DeviceToken, &d.Platform, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return domain.PushDevice{}, fmt.Errorf("upsert push device: %w", err)
	}
	return d, nil
}

func (s *PushDeviceStore) Delete(ctx context.Context, deviceToken string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM push_devices WHERE device_token = $1`, deviceToken)
	return err
}

func (s *PushDeviceStore) List(ctx context.Context) ([]domain.PushDevice, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, device_token, platform, created_at, updated_at FROM push_devices`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var devices []domain.PushDevice
	for rows.Next() {
		var d domain.PushDevice
		if err := rows.Scan(&d.ID, &d.DeviceToken, &d.Platform, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		devices = append(devices, d)
	}
	return devices, rows.Err()
}
