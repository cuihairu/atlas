package atlas

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// selfSignedCertPEM mints a throwaway certificate so the CA-loading path
// can be exercised without fixture files.
func selfSignedCertPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "atlas-test-ca"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// TestGRPCDialCreds pins the transport choice: plaintext unless GRPCTLS is
// asked for, and CA-bundle mistakes fail fast instead of dialing.
func TestGRPCDialCreds(t *testing.T) {
	creds, err := grpcDialCreds(Options{})
	if err != nil || creds == nil {
		t.Fatalf("default = %v, %v; want plaintext creds, nil error", creds, err)
	}

	creds, err = grpcDialCreds(Options{GRPCTLS: true})
	if err != nil || creds == nil {
		t.Fatalf("tls = %v, %v; want TLS creds, nil error", creds, err)
	}

	// Missing CA file fails at construction, not at first RPC.
	if _, err := grpcDialCreds(Options{GRPCTLS: true, GRPCTLSCACert: "/nonexistent/atlas-ca.pem"}); err == nil {
		t.Error("missing CA file accepted")
	}

	// A bundle without certificates is a config error too.
	badPath := filepath.Join(t.TempDir(), "bad.pem")
	if err := os.WriteFile(badPath, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := grpcDialCreds(Options{GRPCTLS: true, GRPCTLSCACert: badPath}); err == nil {
		t.Error("garbage CA bundle accepted")
	}

	// A real PEM bundle loads into the trust pool.
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, selfSignedCertPEM(t), 0o600); err != nil {
		t.Fatal(err)
	}
	creds, err = grpcDialCreds(Options{GRPCTLS: true, GRPCTLSCACert: caPath})
	if err != nil || creds == nil {
		t.Fatalf("valid CA = %v, %v; want creds, nil error", creds, err)
	}
}
