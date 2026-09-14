// Package appstore is a thin client for the App Store Connect API
// (https://api.appstoreconnect.apple.com). Auth is a short-lived ES256 JWT
// signed with the team's .p8 private key — this file mints that token,
// hand-rolled against the standard library only (no JWT dependency, per the
// plan). client.go makes the actual HTTP calls.
package appstore

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"time"
)

// tokenAudience is the fixed "aud" claim App Store Connect requires.
const tokenAudience = "appstoreconnect-v1"

// tokenTTL is the lifetime we mint tokens for. Apple accepts up to 20
// minutes; 15 matches the plan and leaves margin.
const tokenTTL = 15 * time.Minute

// parseP8PrivateKey decodes a PEM-encoded PKCS8 EC private key — the format
// Apple hands out as a .p8 download — into an *ecdsa.PrivateKey.
func parseP8PrivateKey(pemBytes []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("appstore: p8 key is not valid PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("appstore: parsing PKCS8 private key: %w", err)
	}
	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, errors.New("appstore: p8 key is not an ECDSA private key")
	}
	return ecKey, nil
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ"`
}

type jwtClaims struct {
	Iss string `json:"iss"`
	Iat int64  `json:"iat"`
	Exp int64  `json:"exp"`
	Aud string `json:"aud"`
}

// mintToken builds and ES256-signs a short-lived JWT for App Store Connect
// API auth. The signature is encoded as raw r||s — 64 bytes, each half
// left-zero-padded to 32 — per Apple's requirement; this is deliberately
// NOT the ASN.1 DER encoding ecdsa.Sign's r/s would produce if marshaled
// with encoding/asn1 or x509's signature helpers.
func mintToken(keyID, issuerID string, priv *ecdsa.PrivateKey) (token string, expiresAt time.Time, err error) {
	now := time.Now()
	exp := now.Add(tokenTTL)

	headerJSON, err := json.Marshal(jwtHeader{Alg: "ES256", Kid: keyID, Typ: "JWT"})
	if err != nil {
		return "", time.Time{}, err
	}
	claimsJSON, err := json.Marshal(jwtClaims{Iss: issuerID, Iat: now.Unix(), Exp: exp.Unix(), Aud: tokenAudience})
	if err != nil {
		return "", time.Time{}, err
	}

	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)

	digest := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, priv, digest[:])
	if err != nil {
		return "", time.Time{}, fmt.Errorf("appstore: signing jwt: %w", err)
	}

	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), exp, nil
}
