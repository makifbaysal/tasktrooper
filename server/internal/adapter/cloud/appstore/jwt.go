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

	"github.com/makifbaysal/tasktrooper/server/internal/domain"
)

const tokenAudience = "appstoreconnect-v1"

const tokenTTL = 15 * time.Minute

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

// MintToken signs an App Store Connect API token from a store credential
// (key_id, issuer_id, p8), for an adapter outside this package that calls
// the API with the workspace's key (the App Store metrics data source). The
// token is valid until expiresAt, tokenTTL from now.
func MintToken(cred domain.StoreCredential) (token string, expiresAt time.Time, err error) {
	keyID, issuerID, p8 := cred.Data["key_id"], cred.Data["issuer_id"], cred.Data["p8"]
	if keyID == "" || issuerID == "" || p8 == "" {
		return "", time.Time{}, errors.New("appstore: credential missing key_id, issuer_id, or p8")
	}
	priv, err := parseP8PrivateKey([]byte(p8))
	if err != nil {
		return "", time.Time{}, err
	}
	return mintToken(keyID, issuerID, priv)
}
