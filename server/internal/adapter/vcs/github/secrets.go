package github

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"

	"golang.org/x/crypto/nacl/box"
)

type repoPublicKey struct {
	KeyID string `json:"key_id"`
	Key   string `json:"key"`
}

func PutRepoSecret(ctx context.Context, token, owner, repo, name, value string) error {
	var pk repoPublicKey
	if err := doJSON(ctx, token, http.MethodGet, "/repos/"+owner+"/"+repo+"/actions/secrets/public-key", nil, &pk); err != nil {
		return err
	}

	keyBytes, err := base64.StdEncoding.DecodeString(pk.Key)
	if err != nil {
		return fmt.Errorf("github: decoding repo public key: %w", err)
	}
	if len(keyBytes) != 32 {
		return fmt.Errorf("github: repo public key has unexpected length %d, want 32", len(keyBytes))
	}
	var pubKey [32]byte
	copy(pubKey[:], keyBytes)

	sealed, err := box.SealAnonymous(nil, []byte(value), &pubKey, rand.Reader)
	if err != nil {
		return fmt.Errorf("github: sealing secret value: %w", err)
	}

	body := map[string]any{
		"encrypted_value": base64.StdEncoding.EncodeToString(sealed),
		"key_id":          pk.KeyID,
	}

	return doJSON(ctx, token, http.MethodPut, "/repos/"+owner+"/"+repo+"/actions/secrets/"+url.PathEscape(name), body, nil)
}
