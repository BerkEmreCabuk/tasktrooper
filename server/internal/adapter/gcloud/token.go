// Package gcloud is a read-only client for the two Google Cloud APIs this
// product binds a repository to: Cloud Run (run.googleapis.com) and GKE
// (container.googleapis.com). Auth is the OAuth 2.0 service-account JWT-bearer
// flow — a short-lived RS256 JWT signed with the service account's private key
// is exchanged at Google's token endpoint for an access token.
//
// The flow is the same one internal/adapter/googleplay implements against a
// different service, and the two are deliberately NOT shared: they ask for
// different scopes, they fail differently (a Play credential that cannot list
// is normal, a Cloud credential that cannot list means an ungranted IAM role),
// and folding them together would put every mobile release behind a change
// made for a Cloud Run listing.
package gcloud

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

// cloudPlatformScope is the scope both APIs document.
//
// The read-only variant (cloud-platform.read-only) was the obvious choice and
// is not used: container.googleapis.com's discovery document lists only
// cloud-platform, so asking for the narrower one would make GKE listing fail
// on some projects and work on others. It costs nothing real — for a SERVICE
// account the OAuth scope is not the privilege boundary, IAM is: this
// integration is safe because the service account is granted roles/run.viewer
// and roles/container.viewer, and a wider scope cannot exceed those. Narrowing
// the scope would only have made the failure mode confusing.
const cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

// jwtBearerGrantType is the fixed grant_type for the OAuth JWT-bearer flow
// (RFC 7523).
const jwtBearerGrantType = "urn:ietf:params:oauth:grant-type:jwt-bearer"

// assertionTTL is the lifetime of the JWT assertion. Google rejects
// assertions with exp-iat over one hour; the full hour is safe because the
// assertion is single-use — traded for an access token immediately and
// re-minted for every exchange.
const assertionTTL = time.Hour

// defaultTokenURL is Google's OAuth 2.0 token endpoint, used when the key file
// names none.
const defaultTokenURL = "https://oauth2.googleapis.com/token"

// serviceAccount is the subset of a GCP service-account JSON key file this
// client needs. TokenURI is read rather than hardcoded because it is the field
// Google itself uses to move the endpoint, and a key file that names a
// different one is not malformed.
type serviceAccount struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	ProjectID   string `json:"project_id"`
	TokenURI    string `json:"token_uri"`
}

// parsedServiceAccount is a key file after parsing: the signing key plus the
// identifiers the rest of the integration displays and addresses by.
type parsedServiceAccount struct {
	ClientEmail string
	ProjectID   string
	TokenURL    string
	PrivateKey  *rsa.PrivateKey
}

// parseServiceAccountJSON parses raw — the full contents of a service
// account's JSON key file.
//
// Every error it returns names only the FIELD at fault, never its value: raw
// carries a private key, and an error message is the one place a secret
// reliably escapes into a log.
func parseServiceAccountJSON(raw string) (parsedServiceAccount, error) {
	var sa serviceAccount
	if err := json.Unmarshal([]byte(raw), &sa); err != nil {
		return parsedServiceAccount{}, errors.New("gcloud: service_account_json is not valid JSON")
	}
	if sa.ClientEmail == "" || sa.PrivateKey == "" {
		return parsedServiceAccount{}, errors.New("gcloud: service_account_json missing client_email or private_key")
	}
	priv, err := parsePKCS8RSAPrivateKey([]byte(sa.PrivateKey))
	if err != nil {
		return parsedServiceAccount{}, err
	}
	tokenURL := sa.TokenURI
	if tokenURL == "" {
		tokenURL = defaultTokenURL
	}
	return parsedServiceAccount{
		ClientEmail: sa.ClientEmail,
		ProjectID:   sa.ProjectID,
		TokenURL:    tokenURL,
		PrivateKey:  priv,
	}, nil
}

// parsePKCS8RSAPrivateKey decodes the PEM-encoded PKCS8 RSA private key Google
// issues inside a service account's JSON key file.
func parsePKCS8RSAPrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("gcloud: private_key is not valid PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		// x509's own message describes the DER structure, not the key, so it
		// is safe to keep — but it is dropped anyway: nothing downstream can
		// act on it, and a parse error over key material is the wrong place to
		// start trusting a third party's formatting.
		return nil, errors.New("gcloud: private_key is not a valid PKCS8 key")
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("gcloud: private_key is not an RSA private key")
	}
	return rsaKey, nil
}

type jwtHeader struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

type jwtClaims struct {
	Iss   string `json:"iss"`
	Scope string `json:"scope"`
	Aud   string `json:"aud"`
	Iat   int64  `json:"iat"`
	Exp   int64  `json:"exp"`
}

// mintAssertion builds and RS256-signs a JWT-bearer assertion for the token
// exchange.
func mintAssertion(clientEmail, tokenURL, scope string, priv *rsa.PrivateKey) (string, error) {
	now := time.Now()

	headerJSON, err := json.Marshal(jwtHeader{Alg: "RS256", Typ: "JWT"})
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(jwtClaims{
		Iss:   clientEmail,
		Scope: scope,
		Aud:   tokenURL,
		Iat:   now.Unix(),
		Exp:   now.Add(assertionTTL).Unix(),
	})
	if err != nil {
		return "", err
	}

	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)

	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, digest[:])
	if err != nil {
		return "", errors.New("gcloud: signing the token assertion failed")
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// tokenResponse is Google's OAuth token endpoint success payload.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

// fetchAccessToken mints a fresh assertion for scope and exchanges it at
// tokenURL. Neither the assertion nor the returned token is ever logged —
// callers only ever see the token inside an Authorization header.
func fetchAccessToken(ctx context.Context, httpClient *http.Client, tokenURL, clientEmail, scope string, priv *rsa.PrivateKey) (token string, expiresAt time.Time, err error) {
	assertion, err := mintAssertion(clientEmail, tokenURL, scope, priv)
	if err != nil {
		return "", time.Time{}, err
	}

	form := url.Values{}
	form.Set("grant_type", jwtBearerGrantType)
	form.Set("assertion", assertion)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The token endpoint's error body describes the assertion
		// ({"error":"invalid_grant"}), never echoes it, so a snippet is safe
		// and is the only way to tell a clock skew apart from a revoked key.
		return "", time.Time{}, &apiError{Status: resp.StatusCode, Body: domain.TruncateHead(string(data), 500)}
	}

	var tr tokenResponse
	if err := json.Unmarshal(data, &tr); err != nil {
		return "", time.Time{}, fmt.Errorf("gcloud: decoding token response: %w", err)
	}
	if tr.AccessToken == "" {
		return "", time.Time{}, errors.New("gcloud: token response missing access_token")
	}
	expiresIn := tr.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	return tr.AccessToken, time.Now().Add(time.Duration(expiresIn) * time.Second), nil
}
