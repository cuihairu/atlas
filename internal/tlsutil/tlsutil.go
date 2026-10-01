// Package tlsutil builds TLS configurations for Atlas listeners (TODO
// v0.1.17): TLS for the Registry API, upgraded to mutual TLS when a client
// CA bundle is provided.
package tlsutil

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// Options describes the server-side TLS material for one listener.
type Options struct {
	// CertFile and KeyFile are the server certificate and private key
	// (PEM). Both must be set for TLS to be enabled at all.
	CertFile string
	KeyFile  string

	// ClientCAFile is a PEM bundle of CAs allowed to sign client
	// certificates. When set, the listener requires and verifies a client
	// certificate (mutual TLS); when empty, plain server-side TLS is used.
	ClientCAFile string
}

// Enabled reports whether the options request TLS at all.
func (o Options) Enabled() bool {
	return o.CertFile != "" || o.KeyFile != ""
}

// ServerConfig builds a *tls.Config from the options. Client auth is
// required and verified whenever ClientCAFile is set — a CA file without a
// server certificate is a configuration error.
func ServerConfig(o Options) (*tls.Config, error) {
	if !o.Enabled() {
		return nil, fmt.Errorf("tls: cert and key are required")
	}
	if o.CertFile == "" || o.KeyFile == "" {
		return nil, fmt.Errorf("tls: cert and key must both be provided")
	}

	cert, err := tls.LoadX509KeyPair(o.CertFile, o.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("tls: load server certificate: %w", err)
	}

	cfg := &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}

	if o.ClientCAFile != "" {
		pem, err := os.ReadFile(o.ClientCAFile)
		if err != nil {
			return nil, fmt.Errorf("tls: read client CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("tls: no valid certificates in client CA bundle %s", o.ClientCAFile)
		}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return cfg, nil
}
