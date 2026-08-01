// Package testtls — in-memory CA + cert generation for M14 spec tests.
//
// Production deployments use real CAs (Let's Encrypt, internal PKI,
// etc.). The spec tests need fresh self-signed material to exercise
// mTLS round-trips deterministically; this package builds it in-memory
// so tests never touch disk.

package testtls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"time"
)

// Bundle holds an ephemeral CA, a server cert, and a client cert.
// All PEM-encoded; CA cert + each leaf cert + leaf key separately.
type Bundle struct {
	CACertPEM     []byte
	ServerCertPEM []byte
	ServerKeyPEM  []byte
	ClientCertPEM []byte
	ClientKeyPEM  []byte
	ClientCN      string // CommonName on the client cert
}

// NewBundle generates a fresh CA + (server, client) leaf cert pair.
// The client cert's CommonName is "test-client" (use ClientCN field
// for the value when wiring CN allow-lists).
func NewBundle() (*Bundle, error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("ca key: %w", err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "gorti-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, fmt.Errorf("ca cert: %w", err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, fmt.Errorf("ca parse: %w", err)
	}

	serverCertPEM, serverKeyPEM, err := makeLeaf("gorti-test-server", true, caCert, caKey)
	if err != nil {
		return nil, fmt.Errorf("server leaf: %w", err)
	}
	clientCertPEM, clientKeyPEM, err := makeLeaf("test-client", false, caCert, caKey)
	if err != nil {
		return nil, fmt.Errorf("client leaf: %w", err)
	}

	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})

	return &Bundle{
		CACertPEM:     caPEM,
		ServerCertPEM: serverCertPEM,
		ServerKeyPEM:  serverKeyPEM,
		ClientCertPEM: clientCertPEM,
		ClientKeyPEM:  clientKeyPEM,
		ClientCN:      "test-client",
	}, nil
}

func makeLeaf(cn string, isServer bool, ca *x509.Certificate, caKey *ecdsa.PrivateKey) (certPEM, keyPEM []byte, err error) {
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	if isServer {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		tmpl.DNSNames = []string{"localhost"}
		tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	} else {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	keyDER, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		return nil, nil, err
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}
