package tlsutil

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeCert generates a self-signed certificate (or CA when isCA) and writes
// cert.pem / key.pem into dir under the given name prefix.
func writeCert(t *testing.T, dir, name string, isCA bool, parent *tls.Certificate) *tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  isCA,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	if isCA {
		tmpl.KeyUsage |= x509.KeyUsageCertSign
	}

	var parentCert *x509.Certificate
	var parentKey *ecdsa.PrivateKey
	if parent != nil {
		parentCert, err = x509.ParseCertificate(parent.Leaf.Raw)
		if err != nil {
			t.Fatalf("parse parent: %v", err)
		}
		parentKey = parent.PrivateKey.(*ecdsa.PrivateKey)
	} else {
		parentCert, parentKey = tmpl, key
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, parentCert, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}
	cert := &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}

	keyPEM, _ := x509.MarshalECPrivateKey(key)
	writePEM(t, filepath.Join(dir, name+".crt.pem"), "CERTIFICATE", der)
	writePEM(t, filepath.Join(dir, name+".key.pem"), "EC PRIVATE KEY", keyPEM)
	if isCA {
		writePEM(t, filepath.Join(dir, name+".ca-bundle.pem"), "CERTIFICATE", der)
	}
	return cert
}

func writePEM(t *testing.T, path, blockType string, der []byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	if err := pem.Encode(f, &pem.Block{Type: blockType, Bytes: der}); err != nil {
		t.Fatalf("encode %s: %v", path, err)
	}
}

func TestServerConfig_PlainTLS(t *testing.T) {
	dir := t.TempDir()
	writeCert(t, dir, "server", false, nil)

	cfg, err := ServerConfig(Options{
		CertFile: filepath.Join(dir, "server.crt.pem"),
		KeyFile:  filepath.Join(dir, "server.key.pem"),
	})
	if err != nil {
		t.Fatalf("ServerConfig: %v", err)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("expected min TLS 1.2, got %x", cfg.MinVersion)
	}
	if cfg.ClientAuth != tls.NoClientCert {
		t.Errorf("expected no client cert required, got %v", cfg.ClientAuth)
	}
}

func TestServerConfig_MutualTLS(t *testing.T) {
	dir := t.TempDir()
	ca := writeCert(t, dir, "ca", true, nil)
	writeCert(t, dir, "server", false, ca)

	cfg, err := ServerConfig(Options{
		CertFile:     filepath.Join(dir, "server.crt.pem"),
		KeyFile:      filepath.Join(dir, "server.key.pem"),
		ClientCAFile: filepath.Join(dir, "ca.ca-bundle.pem"),
	})
	if err != nil {
		t.Fatalf("ServerConfig: %v", err)
	}
	if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Errorf("expected RequireAndVerifyClientCert, got %v", cfg.ClientAuth)
	}
	if cfg.ClientCAs == nil {
		t.Error("expected client CA pool to be set")
	}
}

func TestServerConfig_RejectsBadInput(t *testing.T) {
	if _, err := ServerConfig(Options{}); err == nil {
		t.Error("expected error when nothing configured")
	}
	if _, err := ServerConfig(Options{CertFile: "only-cert"}); err == nil {
		t.Error("expected error when key missing")
	}
	dir := t.TempDir()
	if _, err := ServerConfig(Options{CertFile: filepath.Join(dir, "missing.crt"), KeyFile: filepath.Join(dir, "missing.key")}); err == nil {
		t.Error("expected error for unreadable key pair")
	}
	writeCert(t, dir, "server", false, nil)
	if _, err := ServerConfig(Options{
		CertFile:     filepath.Join(dir, "server.crt.pem"),
		KeyFile:      filepath.Join(dir, "server.key.pem"),
		ClientCAFile: filepath.Join(dir, "garbage.pem"),
	}); err == nil {
		t.Error("expected error for invalid CA bundle")
	}
}

// End-to-end: an mTLS server rejects certificate-less clients and accepts
// ones presenting a CA-signed certificate.
func TestMutualTLS_Handshake(t *testing.T) {
	dir := t.TempDir()
	ca := writeCert(t, dir, "ca", true, nil)
	writeCert(t, dir, "server", false, ca)
	writeCert(t, dir, "client", false, ca)

	srvCfg, err := ServerConfig(Options{
		CertFile:     filepath.Join(dir, "server.crt.pem"),
		KeyFile:      filepath.Join(dir, "server.key.pem"),
		ClientCAFile: filepath.Join(dir, "ca.ca-bundle.pem"),
	})
	if err != nil {
		t.Fatalf("server config: %v", err)
	}

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	srv.TLS = srvCfg
	srv.StartTLS()
	defer srv.Close()

	clientCert, err := tls.LoadX509KeyPair(filepath.Join(dir, "client.crt.pem"), filepath.Join(dir, "client.key.pem"))
	if err != nil {
		t.Fatalf("load client cert: %v", err)
	}
	caPEM, err := os.ReadFile(filepath.Join(dir, "ca.ca-bundle.pem"))
	if err != nil {
		t.Fatalf("read CA: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)

	// With client certificate → 200.
	ok := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, Certificates: []tls.Certificate{clientCert}}},
	}
	resp, err := ok.Get(srv.URL)
	if err != nil {
		t.Fatalf("mTLS request: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 with client cert, got %d", resp.StatusCode)
	}

	// Without client certificate → handshake rejected.
	anon := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}},
	}
	if resp, err := anon.Get(srv.URL); err == nil {
		resp.Body.Close()
		t.Error("expected handshake failure without client certificate")
	}
}
