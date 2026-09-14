// Package googleplay is a thin client for the Google Play Developer API
// (https://androidpublisher.googleapis.com). Auth is the OAuth 2.0
// service-account JWT-bearer flow: a short-lived RS256 JWT signed with the
// service account's private key is exchanged at Google's token endpoint for
// an access token. This file hand-rolls both the JWT and the exchange
// against the standard library only (no oauth2 dependency, per the plan);
// client.go makes the actual Play Developer API calls.
package googleplay

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

// androidPublisherScope covers the Play Developer API.
const androidPublisherScope = "https://www.googleapis.com/auth/androidpublisher"

// playReportingScope covers the Play Developer Reporting API, a separate
// service that androidPublisherScope does not reach — and the only place the
// account's app list exists. It is minted as its own token, never folded into
// the publisher assertion: a scope Google refuses fails the whole exchange,
// and the app listing is optional while the publisher token carries every
// deploy.
const playReportingScope = "https://www.googleapis.com/auth/playdeveloperreporting"

// jwtBearerGrantType is the fixed grant_type for the OAuth JWT-bearer flow
// (RFC 7523) — the mechanism Google service accounts use to trade a
// self-signed JWT for an access token.
const jwtBearerGrantType = "urn:ietf:params:oauth:grant-type:jwt-bearer"

// assertionTTL is the lifetime of the JWT assertion signed with the service
// account's private key. Google rejects assertions with exp-iat over one
// hour; this uses the full hour since the assertion is single-use (traded
// for an access token immediately) and re-minted for every token exchange.
const assertionTTL = time.Hour

// serviceAccount is the subset of a GCP service-account JSON key file this
// client needs.
type serviceAccount struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
}

// parseServiceAccountJSON parses raw (the full contents of a service
// account's JSON key file) and returns its client email (the JWT issuer)
// alongside its RSA private key.
func parseServiceAccountJSON(raw string) (clientEmail string, priv *rsa.PrivateKey, err error) {
	var sa serviceAccount
	if err := json.Unmarshal([]byte(raw), &sa); err != nil {
		return "", nil, fmt.Errorf("googleplay: parsing service_account_json: %w", err)
	}
	if sa.ClientEmail == "" || sa.PrivateKey == "" {
		return "", nil, errors.New("googleplay: service_account_json missing client_email or private_key")
	}
	priv, err = parsePKCS8RSAPrivateKey([]byte(sa.PrivateKey))
	if err != nil {
		return "", nil, err
	}
	return sa.ClientEmail, priv, nil
}

// parsePKCS8RSAPrivateKey decodes a PEM-encoded PKCS8 RSA private key — the
// format Google issues inside a service account's JSON key file — into an
// *rsa.PrivateKey.
func parsePKCS8RSAPrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("googleplay: private_key is not valid PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("googleplay: parsing PKCS8 private key: %w", err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("googleplay: private_key is not an RSA private key")
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

// mintAssertion builds and RS256-signs a JWT-bearer assertion for the OAuth
// token exchange. Unlike Task 4's ES256 signature (raw r||s, split by
// hand), rsa.SignPKCS1v15's output is used directly as the signature — no
// further encoding is needed.
func mintAssertion(clientEmail, tokenURL, scope string, priv *rsa.PrivateKey) (string, error) {
	now := time.Now()
	exp := now.Add(assertionTTL)

	headerJSON, err := json.Marshal(jwtHeader{Alg: "RS256", Typ: "JWT"})
	if err != nil {
		return "", err
	}
	claimsJSON, err := json.Marshal(jwtClaims{
		Iss:   clientEmail,
		Scope: scope,
		Aud:   tokenURL,
		Iat:   now.Unix(),
		Exp:   exp.Unix(),
	})
	if err != nil {
		return "", err
	}

	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(claimsJSON)

	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("googleplay: signing jwt: %w", err)
	}

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// tokenResponse is Google's OAuth token endpoint success payload.
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

// fetchAccessToken mints a fresh JWT-bearer assertion for scope and exchanges
// it at tokenURL for an access token. Neither the assertion nor the returned
// access token is ever logged — callers only ever see the access token
// inside an Authorization header.
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
		snippet := domain.TruncateHead(string(data), 500)
		return "", time.Time{}, &apiError{Status: resp.StatusCode, Body: snippet}
	}

	var tr tokenResponse
	if err := json.Unmarshal(data, &tr); err != nil {
		return "", time.Time{}, fmt.Errorf("googleplay: decoding token response: %w", err)
	}
	if tr.AccessToken == "" {
		return "", time.Time{}, errors.New("googleplay: token response missing access_token")
	}
	expiresIn := tr.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	return tr.AccessToken, time.Now().Add(time.Duration(expiresIn) * time.Second), nil
}
