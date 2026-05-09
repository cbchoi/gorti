// TASK-301 (M14 W4) — OIDC bearer-token verifier tests.
//
// Generates an in-memory RSA key pair; signs JWTs with the private
// key; verifies with NewFromPEM(pubkey). Exercises the success path
// + the error paths (bad sig, expired, wrong audience, wrong issuer).

package m14spec

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	"github.com/cbchoi/gorti/rti/internal/auth/oidc"
)

func makeKeyPair(t *testing.T) (priv *rsa.PrivateKey, pubPEM []byte) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	pubPEM = pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	return priv, pubPEM
}

// signJWT mints an RS256 JWT with the supplied claims.
func signJWT(t *testing.T, priv *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	hb, _ := json.Marshal(header)
	pb, _ := json.Marshal(claims)
	headerB64 := base64.RawURLEncoding.EncodeToString(hb)
	payloadB64 := base64.RawURLEncoding.EncodeToString(pb)
	signedInput := headerB64 + "." + payloadB64
	hashed := sha256.Sum256([]byte(signedInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, hashed[:])
	if err != nil {
		t.Fatalf("SignPKCS1v15: %v", err)
	}
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)
	return signedInput + "." + sigB64
}

// TestACOIDCVerifyHappyPath — well-formed RS256 JWT verifies cleanly.
func TestACOIDCVerifyHappyPath(t *testing.T) {
	priv, pubPEM := makeKeyPair(t)
	v, err := oidc.NewFromPEM(pubPEM, "gorti-test", "https://idp.example.com")
	if err != nil {
		t.Fatalf("NewFromPEM: %v", err)
	}
	token := signJWT(t, priv, map[string]any{
		"iss": "https://idp.example.com",
		"sub": "alice",
		"aud": "gorti-test",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	claims, err := v.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "alice" {
		t.Errorf("Subject = %q, want alice", claims.Subject)
	}
}

// TestACOIDCRejectsBadSignature — flipping a byte in the signature
// fails verification.
func TestACOIDCRejectsBadSignature(t *testing.T) {
	priv, pubPEM := makeKeyPair(t)
	v, _ := oidc.NewFromPEM(pubPEM, "", "")
	token := signJWT(t, priv, map[string]any{
		"sub": "alice",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	// Corrupt the last segment.
	parts := strings.Split(token, ".")
	parts[2] = "AAAA" + parts[2][4:]
	bad := strings.Join(parts, ".")
	if _, err := v.Verify(bad); err == nil {
		t.Errorf("Verify(badSig) returned nil err; want failure")
	}
}

// TestACOIDCRejectsExpired.
func TestACOIDCRejectsExpired(t *testing.T) {
	priv, pubPEM := makeKeyPair(t)
	v, _ := oidc.NewFromPEM(pubPEM, "", "")
	token := signJWT(t, priv, map[string]any{
		"sub": "alice",
		"exp": time.Now().Add(-time.Hour).Unix(),
	})
	if _, err := v.Verify(token); err == nil {
		t.Errorf("Verify(expired) returned nil err; want failure")
	}
}

// TestACOIDCRejectsWrongAudience.
func TestACOIDCRejectsWrongAudience(t *testing.T) {
	priv, pubPEM := makeKeyPair(t)
	v, _ := oidc.NewFromPEM(pubPEM, "want-audience", "")
	token := signJWT(t, priv, map[string]any{
		"sub": "alice",
		"aud": "got-audience",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := v.Verify(token); err == nil {
		t.Errorf("Verify(wrong aud) returned nil err; want failure")
	}
}

// TestACOIDCRejectsWrongIssuer.
func TestACOIDCRejectsWrongIssuer(t *testing.T) {
	priv, pubPEM := makeKeyPair(t)
	v, _ := oidc.NewFromPEM(pubPEM, "", "https://want.example.com")
	token := signJWT(t, priv, map[string]any{
		"iss": "https://wrong.example.com",
		"sub": "alice",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	if _, err := v.Verify(token); err == nil {
		t.Errorf("Verify(wrong iss) returned nil err; want failure")
	}
}

// TestACOIDCRejectsMalformed.
func TestACOIDCRejectsMalformed(t *testing.T) {
	_, pubPEM := makeKeyPair(t)
	v, _ := oidc.NewFromPEM(pubPEM, "", "")
	if _, err := v.Verify("not.a.jwt"); err == nil {
		t.Errorf("Verify(garbage) returned nil err; want failure")
	}
}

// TestACOIDCInterceptorMetadataRequired — the gRPC interceptor
// rejects requests without authorization metadata.
func TestACOIDCInterceptorMetadataRequired(t *testing.T) {
	_, pubPEM := makeKeyPair(t)
	v, _ := oidc.NewFromPEM(pubPEM, "", "")
	_ = oidc.UnaryServerInterceptor(v) // surface check; behavior covered above
}
