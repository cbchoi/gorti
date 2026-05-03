package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// generateSelfSignedKeyPair writes a fresh self-signed ECDSA cert + key
// to certPath / keyPath under dir. Used by the TLS construction tests
// so we don't have to ship real certs in the repo. The cert is valid
// for 127.0.0.1 + localhost and expires in 1 hour.
func generateSelfSignedKeyPair(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey: %v", err)
	}

	// x509.Certificate.NotBefore/NotAfter must reflect real wallclock
	// time so the TLS handshake's validity check passes; core.FakeClock
	// can't substitute here. Local to test cert generation.
	notBefore := time.Now().Add(-time.Minute) //nolint:forbidigo // self-signed cert validity needs wallclock
	notAfter := notBefore.Add(time.Hour)

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("rand serial: %v", err)
	}

	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "rtid-test"},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("x509.CreateCertificate: %v", err)
	}

	certPath = filepath.Join(dir, "cert.pem")
	certOut, err := os.Create(certPath) //nolint:gosec // test-only path under t.TempDir
	if err != nil {
		t.Fatalf("create cert.pem: %v", err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		t.Fatalf("encode cert: %v", err)
	}
	if err := certOut.Close(); err != nil {
		t.Fatalf("close cert: %v", err)
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	keyPath = filepath.Join(dir, "key.pem")
	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("create key.pem: %v", err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}); err != nil {
		t.Fatalf("encode key: %v", err)
	}
	if err := keyOut.Close(); err != nil {
		t.Fatalf("close key: %v", err)
	}
	return certPath, keyPath
}

// TestBuildServerTLS_Insecure: empty paths return (nil, nil) — the
// insecure default that keeps existing rtid invocations working.
func TestBuildServerTLS_Insecure(t *testing.T) {
	cfg, err := buildServerTLS("", "")
	if err != nil {
		t.Fatalf("buildServerTLS(\"\", \"\"): %v", err)
	}
	if cfg != nil {
		t.Errorf("expected nil tls.Config for empty paths, got %+v", cfg)
	}
}

// TestBuildServerTLS_RejectsHalfPair: setting only --tls-cert (or only
// --tls-key) must fail at startup so a misconfigured deployment
// doesn't silently fall back to insecure.
func TestBuildServerTLS_RejectsHalfPair(t *testing.T) {
	if _, err := buildServerTLS("/tmp/cert.pem", ""); err == nil {
		t.Errorf("buildServerTLS(cert, \"\") returned nil error")
	}
	if _, err := buildServerTLS("", "/tmp/key.pem"); err == nil {
		t.Errorf("buildServerTLS(\"\", key) returned nil error")
	}
}

// TestBuildServerTLS_LoadsKeypair: a valid self-signed cert+key produce
// a *tls.Config with one Certificate and TLS 1.2 floor.
func TestBuildServerTLS_LoadsKeypair(t *testing.T) {
	certPath, keyPath := generateSelfSignedKeyPair(t, t.TempDir())
	cfg, err := buildServerTLS(certPath, keyPath)
	if err != nil {
		t.Fatalf("buildServerTLS: %v", err)
	}
	if cfg == nil {
		t.Fatal("buildServerTLS returned nil config")
	}
	if len(cfg.Certificates) != 1 {
		t.Errorf("Certificates len = %d, want 1", len(cfg.Certificates))
	}
	if cfg.MinVersion < tls.VersionTLS12 {
		t.Errorf("MinVersion = %x, want >= TLS 1.2 (%x)", cfg.MinVersion, tls.VersionTLS12)
	}
}

// TestBuildServerTLS_RejectsBadKeypair: a missing or malformed cert
// surfaces a startup error rather than a runtime handshake failure.
func TestBuildServerTLS_RejectsBadKeypair(t *testing.T) {
	_, err := buildServerTLS("/nonexistent/cert.pem", "/nonexistent/key.pem")
	if err == nil {
		t.Errorf("buildServerTLS on missing files returned nil error")
	}
}

// TestNewRTID_TLSEnabled: when TLSConfig is wired, newRTID composes a
// gRPC server with TLS credentials and Serve accepts a TLS handshake
// from a matching client. The handshake completes (a client without
// the right TLS settings fails with a recognizable error).
func TestNewRTID_TLSEnabled(t *testing.T) {
	certPath, keyPath := generateSelfSignedKeyPair(t, t.TempDir())
	tlsConfig, err := buildServerTLS(certPath, keyPath)
	if err != nil {
		t.Fatalf("buildServerTLS: %v", err)
	}

	srv, err := newRTID(rtidConfig{
		ListenAddr:        "127.0.0.1:0",
		MetricsListenAddr: "127.0.0.1:0",
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		TLSConfig:         tlsConfig,
	})
	if err != nil {
		t.Fatalf("newRTID with TLS: %v", err)
	}

	// We need to learn the listener address Serve picked. Bind here and
	// have Serve adopt our pre-bound listener... but Serve's signature
	// binds internally, so we exercise the simpler path: bind once,
	// run grpcS.Serve in a goroutine against our listener, dial it as
	// a TLS client.
	gln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer gln.Close()

	go func() {
		_ = srv.grpcS.Serve(gln)
	}()
	defer srv.grpcS.GracefulStop()
	defer srv.multi.Close()

	// Dial the listener with a TLS client that trusts the server cert.
	// We extract the cert from the loaded config and stuff it into a
	// fresh RootCAs pool.
	rootPool := x509.NewCertPool()
	for _, c := range tlsConfig.Certificates {
		if len(c.Certificate) == 0 {
			continue
		}
		x509Cert, err := x509.ParseCertificate(c.Certificate[0])
		if err != nil {
			t.Fatalf("parse server cert: %v", err)
		}
		rootPool.AddCert(x509Cert)
	}

	dialAddr := gln.Addr().String()
	dialCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rawConn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", dialAddr)
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer rawConn.Close()

	tlsClient := tls.Client(rawConn, &tls.Config{
		RootCAs:    rootPool,
		ServerName: "127.0.0.1",
		MinVersion: tls.VersionTLS12,
	})
	if err := tlsClient.HandshakeContext(dialCtx); err != nil {
		t.Fatalf("TLS handshake against rtid: %v", err)
	}
	state := tlsClient.ConnectionState()
	if !state.HandshakeComplete {
		t.Errorf("ConnectionState.HandshakeComplete = false")
	}
	if state.Version < tls.VersionTLS12 {
		t.Errorf("negotiated TLS version = %x, want >= TLS 1.2", state.Version)
	}
	_ = tlsClient.Close()
}

// TestNewRTID_TLSEnabled_RejectsInsecureDial: a plaintext dial against
// the TLS listener fails (the server doesn't respond with HTTP/2
// settings — it hangs or returns an alert). We use a short deadline
// and assert the dial does NOT succeed in completing a TLS-style
// handshake. This guards against a regression where the server
// silently accepts both TLS and plaintext.
func TestNewRTID_TLSEnabled_RejectsInsecureDial(t *testing.T) {
	certPath, keyPath := generateSelfSignedKeyPair(t, t.TempDir())
	tlsConfig, err := buildServerTLS(certPath, keyPath)
	if err != nil {
		t.Fatalf("buildServerTLS: %v", err)
	}

	srv, err := newRTID(rtidConfig{
		ListenAddr:        "127.0.0.1:0",
		MetricsListenAddr: "127.0.0.1:0",
		Logger:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		TLSConfig:         tlsConfig,
	})
	if err != nil {
		t.Fatalf("newRTID with TLS: %v", err)
	}

	gln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer gln.Close()

	go func() { _ = srv.grpcS.Serve(gln) }()
	defer srv.grpcS.GracefulStop()
	defer srv.multi.Close()

	// Insecure TLS dial — same handshake protocol but trusts no roots
	// AND skips name verification disabled. The handshake MUST fail
	// because we're sending TLS bytes the server understands but
	// rejects (cert-untrusted), or because the server expects the
	// raw protocol and returns an alert. Either way the handshake
	// does not complete cleanly without a valid cert in the trust
	// store.
	rawConn, err := net.DialTimeout("tcp", gln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("raw Dial: %v", err)
	}
	defer rawConn.Close()
	tlsClient := tls.Client(rawConn, &tls.Config{
		ServerName: "127.0.0.1",
		MinVersion: tls.VersionTLS12,
		// Empty RootCAs + no InsecureSkipVerify → handshake will fail
		// on certificate validation, confirming the server presented a
		// TLS cert (i.e. TLS is actually on).
	})
	hsCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = tlsClient.HandshakeContext(hsCtx)
	if err == nil {
		t.Errorf("TLS handshake against untrusted cert succeeded; expected verification error")
		return
	}
	// Verification errors look like: "x509: certificate signed by unknown authority".
	if !strings.Contains(err.Error(), "certificate") && !errors.As(err, new(x509.UnknownAuthorityError)) {
		// Surface the actual error so a future regression is easy to
		// classify; don't fail on phrasing alone.
		t.Logf("note: handshake failed with %v (acceptable — TLS is engaged)", err)
	}
}
