package apikey

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
	"github.com/makifbaysal/tasktrooper/server/internal/port"
)

type Service struct {
	store port.APIKeyStore
}

func NewService(store port.APIKeyStore) *Service {
	return &Service{store: store}
}

func (s *Service) Create(ctx context.Context, req domain.CreateAPIKeyRequest) (domain.CreateAPIKeyResponse, error) {
	if req.Name == "" {
		return domain.CreateAPIKeyResponse{}, fmt.Errorf("name is required")
	}
	key, err := generateKey()
	if err != nil {
		return domain.CreateAPIKeyResponse{}, err
	}
	hash := hashKey(key)
	prefix := key
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	rec, err := s.store.Create(ctx, req.Name, hash, prefix, req.ToolPolicy)
	if err != nil {
		return domain.CreateAPIKeyResponse{}, err
	}
	return domain.CreateAPIKeyResponse{APIKeyRecord: rec, Key: key}, nil
}

func (s *Service) List(ctx context.Context) ([]domain.APIKeyRecord, error) {
	return s.store.List(ctx)
}

func (s *Service) Delete(ctx context.Context, name string) error {
	return s.store.Delete(ctx, name)
}

func (s *Service) Authenticate(ctx context.Context, token string) (*domain.APIKeyRecord, error) {
	return s.store.FindByHash(ctx, hashKey(token))
}

func hashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func generateKey() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "sk-bridge-" + hex.EncodeToString(b), nil
}
