package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/cuihairu/atlas/internal/config"
)

func TestBuildRedisClient_Topology(t *testing.T) {
	t.Run("single node by default", func(t *testing.T) {
		cfg := config.Config{RedisURL: "redis://:secret@redis-host:6380/2"}
		c := buildRedisClient(cfg)
		defer c.Close()
		client, ok := c.(*redis.Client)
		if !ok {
			t.Fatalf("expected *redis.Client, got %T", c)
		}
		if client.Options().Addr != "redis-host:6380" {
			t.Errorf("Addr = %q, want redis-host:6380", client.Options().Addr)
		}
		if client.Options().Password != "secret" {
			t.Errorf("Password not parsed from URL")
		}
		if client.Options().DB != 2 {
			t.Errorf("DB = %d, want 2", client.Options().DB)
		}
	})

	t.Run("sentinel failover", func(t *testing.T) {
		topo := resolveRedisTopology(config.Config{
			RedisURL:           "redis://:secret@redis-host:6379/1",
			RedisSentinelAddrs: "s1:26379, s2:26379,s3:26379",
			RedisMasterName:    "atlas",
		})
		if topo.mode != "sentinel" {
			t.Fatalf("mode = %q, want sentinel", topo.mode)
		}
		if len(topo.seeds) != 3 {
			t.Errorf("seeds = %v, want 3 entries", topo.seeds)
		}
		if topo.master != "atlas" {
			t.Errorf("master = %q, want atlas", topo.master)
		}
		if topo.password != "secret" || topo.db != 1 {
			t.Errorf("URL credentials not carried over: pass=%q db=%d", topo.password, topo.db)
		}
		// and it must build a live client
		c := buildRedisClient(config.Config{
			RedisURL:           "redis://:secret@redis-host:6379/1",
			RedisSentinelAddrs: "s1:26379,s2:26379,s3:26379",
			RedisMasterName:    "atlas",
		})
		defer c.Close()
		if c == nil {
			t.Fatal("nil client")
		}
	})

	t.Run("cluster takes precedence", func(t *testing.T) {
		topo := resolveRedisTopology(config.Config{
			RedisURL:           "redis://redis-host:6379/0",
			RedisSentinelAddrs: "s1:26379",
			RedisClusterAddrs:  "c1:6379,c2:6379,c3:6379",
		})
		if topo.mode != "cluster" {
			t.Fatalf("mode = %q, want cluster (cluster > sentinel > single)", topo.mode)
		}
		if len(topo.seeds) != 3 {
			t.Errorf("seeds = %v, want 3 entries", topo.seeds)
		}
		c := buildRedisClient(config.Config{RedisClusterAddrs: "c1:6379,c2:6379,c3:6379"})
		defer c.Close()
		if _, ok := c.(*redis.ClusterClient); !ok {
			t.Fatalf("expected *redis.ClusterClient, got %T", c)
		}
	})

	t.Run("pool size propagates", func(t *testing.T) {
		topo := resolveRedisTopology(config.Config{RedisURL: "redis://localhost:6379/0", RedisPoolSize: 42})
		if topo.poolSize != 42 {
			t.Errorf("poolSize = %d, want 42", topo.poolSize)
		}
		client := buildRedisClient(config.Config{RedisURL: "redis://localhost:6379/0", RedisPoolSize: 42}).(*redis.Client)
		defer client.Close()
		if client.Options().PoolSize != 42 {
			t.Errorf("Options().PoolSize = %d, want 42", client.Options().PoolSize)
		}
	})
}

func TestRedisURLParts(t *testing.T) {
	tt := []struct {
		name             string
		raw              string
		addr, user, pass string
		db               int
	}{
		{"default", "redis://localhost:6379/0", "localhost:6379", "", "", 0},
		{"custom host", "redis://r1.internal:7001", "r1.internal:7001", "", "", 0},
		{"password + db", "redis://:hunter2@r1:6379/3", "r1:6379", "", "hunter2", 3},
		{"user + password", "redis://atlas:pw@r1:6379/5", "r1:6379", "atlas", "pw", 5},
		{"no path", "redis://r1:6379", "r1:6379", "", "", 0},
		{"garbage", "::::", "localhost:6379", "", "", 0},
	}
	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			addr, user, pass, db := redisURLParts(tc.raw)
			if addr != tc.addr {
				t.Errorf("addr = %q, want %q", addr, tc.addr)
			}
			if user != tc.user {
				t.Errorf("user = %q, want %q", user, tc.user)
			}
			if pass != tc.pass {
				t.Errorf("pass = %q, want %q", pass, tc.pass)
			}
			if db != tc.db {
				t.Errorf("db = %d, want %d", db, tc.db)
			}
		})
	}
}

func TestSplitAddrs(t *testing.T) {
	got := splitAddrs("a:1, b:2 ,,c:3,")
	if len(got) != 3 || got[0] != "a:1" || got[1] != "b:2" || got[2] != "c:3" {
		t.Errorf("splitAddrs = %v, want [a:1 b:2 c:3]", got)
	}
	if splitAddrs("") != nil {
		t.Errorf("splitAddrs(\"\") should be nil")
	}
}

func TestApplyPGPoolOptions(t *testing.T) {
	t.Run("zero config keeps DSN defaults", func(t *testing.T) {
		pc, err := pgxpool.ParseConfig("postgres://atlas:atlas@localhost:5432/atlas")
		if err != nil {
			t.Fatal(err)
		}
		applyPGPoolOptions(pc, config.Config{})
		if pc.MaxConns < 1 {
			t.Errorf("MaxConns = %d, expected DSN/library default", pc.MaxConns)
		}
	})

	t.Run("overrides applied and clamped", func(t *testing.T) {
		pc, err := pgxpool.ParseConfig("postgres://atlas:atlas@localhost:5432/atlas")
		if err != nil {
			t.Fatal(err)
		}
		applyPGPoolOptions(pc, config.Config{
			PGPoolMaxConns:          64,
			PGPoolMinConns:          128, // > max → clamped down
			PGPoolMaxConnLifetime:   30 * time.Minute,
			PGPoolMaxConnIdleTime:   5 * time.Minute,
			PGPoolHealthCheckPeriod: 15 * time.Second,
		})
		if pc.MaxConns != 64 {
			t.Errorf("MaxConns = %d, want 64", pc.MaxConns)
		}
		if pc.MinConns != 64 {
			t.Errorf("MinConns = %d, want clamped to 64", pc.MinConns)
		}
		if pc.MaxConnLifetime != 30*time.Minute {
			t.Errorf("MaxConnLifetime = %v", pc.MaxConnLifetime)
		}
		if pc.MaxConnIdleTime != 5*time.Minute {
			t.Errorf("MaxConnIdleTime = %v", pc.MaxConnIdleTime)
		}
		if pc.HealthCheckPeriod != 15*time.Second {
			t.Errorf("HealthCheckPeriod = %v", pc.HealthCheckPeriod)
		}
	})
}

// genCert signs a self-signed certificate (or CA when isCA) and writes
// cert/key PEM files into dir under the given prefix, returning the file
// paths. Mirrors the generator used by internal/tlsutil tests.
func genCert(t *testing.T, dir, prefix string, isCA bool, parent *tls.Certificate) *tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: prefix},
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
	writePEMFile(t, filepath.Join(dir, prefix+".crt.pem"), "CERTIFICATE", der)
	writePEMFile(t, filepath.Join(dir, prefix+".key.pem"), "EC PRIVATE KEY", keyPEM)
	if isCA {
		writePEMFile(t, filepath.Join(dir, prefix+".ca-bundle.pem"), "CERTIFICATE", der)
	}
	return cert
}

func writePEMFile(t *testing.T, path, blockType string, der []byte) {
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

func TestNewListener_PlaintextWhenUnconfigured(t *testing.T) {
	ln, err := newListener("127.0.0.1:0", nil)
	if err != nil {
		t.Fatalf("newListener: %v", err)
	}
	defer ln.Close()

	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		accepted <- c
	}()

	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	peer := <-accepted
	defer peer.Close()
	if _, ok := peer.(*tls.Conn); ok {
		t.Errorf("unconfigured port must accept plaintext TCP, not TLS")
	}
}

func TestNewListener_TLSHandshake(t *testing.T) {
	dir := t.TempDir()
	cert := genCert(t, dir, "server", false, nil)
	ln, err := newListener("127.0.0.1:0", &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{*cert},
	})
	if err != nil {
		t.Fatalf("newListener: %v", err)
	}
	defer ln.Close()

	serverErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()
		tlsConn, ok := conn.(*tls.Conn)
		if !ok {
			serverErr <- fmt.Errorf("accepted %T, want *tls.Conn", conn)
			return
		}
		serverErr <- tlsConn.Handshake()
	}()

	rootPool := x509.NewCertPool()
	rootPool.AddCert(cert.Leaf)
	client, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    rootPool,
		ServerName: "localhost",
	})
	if err != nil {
		t.Fatalf("tls dial: %v", err)
	}
	defer client.Close()
	if err := client.Handshake(); err != nil {
		t.Fatalf("client handshake: %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("server handshake: %v", err)
	}
}

func TestNewListener_MTLSRejectsClientWithoutCert(t *testing.T) {
	dir := t.TempDir()
	ca := genCert(t, dir, "ca", true, nil)
	caPEM, err := os.ReadFile(filepath.Join(dir, "ca.ca-bundle.pem"))
	if err != nil {
		t.Fatalf("read ca bundle: %v", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caPEM) {
		t.Fatalf("no valid certs in ca bundle")
	}
	serverCert := genCert(t, dir, "server", false, ca)
	clientCert := genCert(t, dir, "client", false, ca)

	ln, err := newListener("127.0.0.1:0", &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{*serverCert},
		ClientCAs:    caPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	})
	if err != nil {
		t.Fatalf("newListener: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				conn.Read(make([]byte, 1)) // drives the handshake
				conn.Close()
			}()
		}
	}()

	// Without a client certificate the handshake must fail. Over TLS 1.3
	// the client Handshake can return before the server's alert arrives,
	// so the rejection is observed on the first read/write.
	if conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{
		RootCAs:    caPool,
		ServerName: "localhost",
	}); err == nil {
		conn.Write([]byte("x"))
		if _, rerr := conn.Read(make([]byte, 1)); rerr == nil {
			conn.Close()
			t.Fatalf("expected rejection without client certificate")
		}
		conn.Close()
	}

	// With a certificate signed by the CA the handshake must succeed.
	conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{
		RootCAs:      caPool,
		ServerName:   "localhost",
		Certificates: []tls.Certificate{*clientCert},
	})
	if err != nil {
		t.Fatalf("tls dial with client cert: %v", err)
	}
	conn.Close()
}
