// Package oidc — minimal JWT bearer-token verifier (M14 W4).
//
// Scope: parse + verify RS256 JWTs against a pre-pinned PEM public
// key. Validate exp + (optional) aud + (optional) iss claims. Return
// the verified subject ("sub" claim) for context propagation.
//
// Out of scope (deferred):
//   - JWKS HTTP fetch from oidc-issuer (full OIDC discovery).
//   - HS256/ES256/EdDSA. RS256 is the most common production choice;
//     enough for M14's deployment story.
//   - Token revocation lists.
//   - Refresh-token rotation.
//
// See docs/M14_DISPATCH_PLAN.md §2.2.

package oidc

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Verifier validates RS256 JWTs against a pre-pinned RSA public key.
type Verifier struct {
	pubKey   *rsa.PublicKey
	audience string // empty → don't check aud
	issuer   string // empty → don't check iss
}

// NewFromPEM constructs a Verifier from a PEM-encoded RSA public key
// (the standard "BEGIN PUBLIC KEY" form, holding a SubjectPublicKeyInfo
// with an rsaEncryption AlgorithmIdentifier).
//
// audience / issuer may be empty — leaving the corresponding claim
// unchecked.
func NewFromPEM(pubKeyPEM []byte, audience, issuer string) (*Verifier, error) {
	block, _ := pem.Decode(pubKeyPEM)
	if block == nil {
		return nil, errors.New("oidc: PEM decode returned no block")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("oidc: parse public key: %w", err)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("oidc: only RSA public keys supported (got %T)", pub)
	}
	return &Verifier{pubKey: rsaPub, audience: audience, issuer: issuer}, nil
}

// Claims is the subset of standard JWT claims the verifier inspects.
type Claims struct {
	Issuer    string `json:"iss"`
	Subject   string `json:"sub"`
	Audience  any    `json:"aud"` // string or []string per RFC 7519
	ExpiresAt int64  `json:"exp"`
	NotBefore int64  `json:"nbf"`
	IssuedAt  int64  `json:"iat"`
}

// Verify parses + validates a JWT. Returns the verified claims, or
// an error explaining the failure.
func (v *Verifier) Verify(token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("oidc: malformed JWT (expected 3 segments)")
	}
	headerB64, payloadB64, sigB64 := parts[0], parts[1], parts[2]

	headerJSON, err := base64.RawURLEncoding.DecodeString(headerB64)
	if err != nil {
		return nil, fmt.Errorf("oidc: header decode: %w", err)
	}
	var header struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, fmt.Errorf("oidc: header parse: %w", err)
	}
	if header.Alg != "RS256" {
		return nil, fmt.Errorf("oidc: unsupported alg %q (only RS256)", header.Alg)
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, fmt.Errorf("oidc: payload decode: %w", err)
	}
	var claims Claims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return nil, fmt.Errorf("oidc: payload parse: %w", err)
	}

	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return nil, fmt.Errorf("oidc: signature decode: %w", err)
	}

	signedInput := headerB64 + "." + payloadB64
	hashed := sha256.Sum256([]byte(signedInput))
	if err := rsa.VerifyPKCS1v15(v.pubKey, crypto.SHA256, hashed[:], sig); err != nil {
		return nil, fmt.Errorf("oidc: signature verification: %w", err)
	}

	now := time.Now().Unix()
	if claims.ExpiresAt > 0 && now >= claims.ExpiresAt {
		return nil, fmt.Errorf("oidc: token expired (exp=%d, now=%d)", claims.ExpiresAt, now)
	}
	if claims.NotBefore > 0 && now < claims.NotBefore {
		return nil, fmt.Errorf("oidc: token not yet valid (nbf=%d, now=%d)", claims.NotBefore, now)
	}
	if v.issuer != "" && claims.Issuer != v.issuer {
		return nil, fmt.Errorf("oidc: issuer mismatch (got %q, want %q)", claims.Issuer, v.issuer)
	}
	if v.audience != "" {
		if !audienceMatches(claims.Audience, v.audience) {
			return nil, fmt.Errorf("oidc: audience mismatch (got %v, want %q)", claims.Audience, v.audience)
		}
	}
	return &claims, nil
}

// audienceMatches handles aud-as-string + aud-as-[]string per RFC 7519.
func audienceMatches(claimAud any, want string) bool {
	switch v := claimAud.(type) {
	case string:
		return v == want
	case []any:
		for _, a := range v {
			if s, ok := a.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}
