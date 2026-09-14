// OAuth App (Authorization Code) akışı — PAT'e alternatif olarak "GitHub ile
// bağlan" butonu için. Client secret sadece tenant-manager'de tutulur.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	authorizeURL = "https://github.com/login/oauth/authorize"
	tokenURL     = "https://github.com/login/oauth/access_token"
)

// AuthorizeURL, kullanıcıyı GitHub'ın izin ekranına yönlendirecek URL'i üretir.
// scope "repo" ister: private repo oluşturma, içerik ve PR yazma.
// "admin:repo_hook" push webhook'larını kurup güncelleyebilmek için: klasik
// OAuth scope'larında hook yönetimi "repo"nun içinde DEĞİL, ayrı scope.
// "read:org" org listesi için: bu scope olmadan /user/orgs sessizce boş döner
// ve kullanıcı üyesi olduğu org'u import ekranında hiç göremez.
//
// prompt=consent bilerek var. GitHub, uygulama daha önce yetkilendirilmişse
// izin ekranını atlayıp doğrudan callback'e döner; bağlantıyı koparıp yeniden
// kuran kullanıcıya hiçbir seçim sunulmamasının sebebi bu. Org erişimi tam da o
// ekrandan veriliyor, dolayısıyla ekranı atlamak org'u erişilemez bırakıyordu.
func AuthorizeURL(clientID, redirectURI, state string) string {
	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("scope", "repo admin:repo_hook read:org")
	q.Set("state", state)
	q.Set("prompt", "consent")
	return authorizeURL + "?" + q.Encode()
}

// ExchangeCode, callback'te alınan code'u access token'a çevirir.
func ExchangeCode(ctx context.Context, clientID, clientSecret, code, redirectURI string) (string, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var out struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("github oauth: unexpected response: %s", strings.TrimSpace(string(data)))
	}
	if out.Error != "" {
		return "", fmt.Errorf("github oauth: %s: %s", out.Error, out.ErrorDesc)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("github oauth: empty access_token in response")
	}
	return out.AccessToken, nil
}
