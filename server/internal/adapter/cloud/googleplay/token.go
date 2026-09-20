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

const androidPublisherScope = "https://www.googleapis.com/auth/androidpublisher"


const playReportingScope = "https://www.googleapis.com/auth/playdeveloperreporting"

const jwtBearerGrantType = "urn:ietf:params:oauth:grant-type:jwt-bearer"

const assertionTTL = time.Hour

type serviceAccount struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
}

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

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

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
