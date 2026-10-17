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

const cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

const jwtBearerGrantType = "urn:ietf:params:oauth:grant-type:jwt-bearer"

const assertionTTL = time.Hour

// maxTokenLifetime bounds how long a minted token is kept, whatever
// expires_in the endpoint answers: Google's are an hour.
const maxTokenLifetime = int64(time.Hour / time.Second)

const defaultTokenURL = "https://oauth2.googleapis.com/token"

type serviceAccount struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	ProjectID   string `json:"project_id"`
	TokenURI    string `json:"token_uri"`
}

type parsedServiceAccount struct {
	ClientEmail string
	ProjectID   string
	TokenURL    string
	PrivateKey  *rsa.PrivateKey
}

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

func parsePKCS8RSAPrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("gcloud: private_key is not valid PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
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
	if expiresIn <= 0 || expiresIn > maxTokenLifetime {
		expiresIn = maxTokenLifetime
	}
	return tr.AccessToken, time.Now().Add(time.Duration(expiresIn) * time.Second), nil
}
