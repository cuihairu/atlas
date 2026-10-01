package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"net/http/httptest"
	"testing"
)

// fpOf mirrors extractCertFingerprint's derivation for a known Raw blob.
func fpOf(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:12]
}

func TestExtractCertFingerprint(t *testing.T) {
	// No TLS handshake at all (plain HTTP behind a terminator).
	if got := extractCertFingerprint(httptest.NewRequest("GET", "/", nil)); got != "" {
		t.Fatalf("plain request fingerprint = %q, want empty", got)
	}

	// TLS but no client certificate presented.
	r := httptest.NewRequest("GET", "/", nil)
	r.TLS = &tls.ConnectionState{}
	if got := extractCertFingerprint(r); got != "" {
		t.Fatalf("certless TLS fingerprint = %q, want empty", got)
	}

	// A client certificate: SHA-256 of Raw, first 12 hex chars.
	r2 := httptest.NewRequest("GET", "/", nil)
	raw := []byte("fake-der-certificate-bytes")
	r2.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{{Raw: raw}},
	}
	want := fpOf(raw)
	if len(want) != 12 {
		t.Fatalf("sanity: fingerprint length = %d, want 12", len(want))
	}
	if got := extractCertFingerprint(r2); got != want {
		t.Fatalf("fingerprint = %q, want %q", got, want)
	}
}

func TestActorStringWithCertFingerprint(t *testing.T) {
	cases := []struct {
		name  string
		actor Actor
		want  string
	}{
		{"anonymous", Actor{Role: RoleAdmin}, "admin:anonymous"},
		{"key only", Actor{Role: RoleAdmin, KeyFingerprint: "abcdef123456"}, "admin:abcdef123456"},
		{"cert only", Actor{Role: RoleAdmin, CertFingerprint: "0123456789ab"}, "admin::0123456789ab"},
		{"both", Actor{Role: RoleAdmin, KeyFingerprint: "abcdef123456", CertFingerprint: "0123456789ab"}, "admin:abcdef123456:0123456789ab"},
	}
	for _, tc := range cases {
		if got := tc.actor.String(); got != tc.want {
			t.Errorf("%s: String() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestCertFingerprintFrom(t *testing.T) {
	if got := CertFingerprintFrom(context.Background()); got != "" {
		t.Fatalf("empty context = %q, want empty", got)
	}
	ctx := context.WithValue(context.Background(), actorKey, Actor{Role: RoleAdmin, CertFingerprint: "0123456789ab"})
	if got := CertFingerprintFrom(ctx); got != "0123456789ab" {
		t.Fatalf("CertFingerprintFrom = %q, want 0123456789ab", got)
	}
}
