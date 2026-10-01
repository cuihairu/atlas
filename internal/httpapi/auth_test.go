package httpapi

import (
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAdminAuth_IPWhitelist(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	cfg := AuthConfig{
		IPWhitelist: []*net.IPNet{cidr},
		Logger:      slog.Default(),
	}

	handler := AdminAuth(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		wantCode   int
	}{
		{"allowed by remote", "10.0.1.5:1234", "", 200},
		{"allowed by xff", "192.168.1.1:80", "10.0.2.10, 172.16.0.1", 200},
		{"blocked", "172.16.0.1:80", "", 403},
		{"blocked no xff", "192.168.1.1:80", "", 403},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/v1/admin/stats", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.wantCode {
				t.Errorf("got %d, want %d", rec.Code, tt.wantCode)
			}
		})
	}
}

func TestAdminAuth_APIKey(t *testing.T) {
	cfg := AuthConfig{
		APIKeys: map[string]struct{}{
			"secret-key-1": {},
			"secret-key-2": {},
		},
		Logger: slog.Default(),
	}

	handler := AdminAuth(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name     string
		auth     string
		wantCode int
	}{
		{"valid key", "Bearer secret-key-1", 200},
		{"valid key 2", "Bearer secret-key-2", 200},
		{"missing header", "", 401},
		{"wrong key", "Bearer wrong-key", 401},
		{"bad format", "Basic secret-key-1", 401},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/v1/admin/stats", nil)
			if tt.auth != "" {
				req.Header.Set("Authorization", tt.auth)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.wantCode {
				t.Errorf("got %d, want %d", rec.Code, tt.wantCode)
			}
		})
	}
}

func TestAdminAuth_Combined(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	cfg := AuthConfig{
		APIKeys:     map[string]struct{}{"key1": {}},
		IPWhitelist: []*net.IPNet{cidr},
		Logger:      slog.Default(),
	}

	handler := AdminAuth(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	tests := []struct {
		name     string
		ip       string
		key      string
		wantCode int
	}{
		{"both ok", "10.0.1.1:80", "Bearer key1", 200},
		{"ip blocked key ok", "172.16.0.1:80", "Bearer key1", 403},
		{"ip ok key missing", "10.0.1.1:80", "", 401},
		{"both fail ip first", "172.16.0.1:80", "", 403},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/v1/admin/stats", nil)
			req.RemoteAddr = tt.ip
			if tt.key != "" {
				req.Header.Set("Authorization", tt.key)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tt.wantCode {
				t.Errorf("got %d, want %d", rec.Code, tt.wantCode)
			}
		})
	}
}

func TestParseIPWhitelist(t *testing.T) {
	nets, err := ParseIPWhitelist("10.0.0.0/8, 192.168.1.5, 172.16.0.0/12")
	if err != nil {
		t.Fatal(err)
	}
	if len(nets) != 3 {
		t.Fatalf("got %d nets, want 3", len(nets))
	}
	// Single IP becomes /32
	if !nets[1].Contains(net.ParseIP("192.168.1.5")) {
		t.Error("single IP not contained")
	}
	if nets[1].Contains(net.ParseIP("192.168.1.6")) {
		t.Error("single IP /32 should not contain neighbor")
	}
}

func TestParseAPIKeys(t *testing.T) {
	keys := ParseAPIKeys("key1, key2 , key3")
	if len(keys) != 3 {
		t.Fatalf("got %d keys, want 3", len(keys))
	}
	if _, ok := keys["key1"]; !ok {
		t.Error("key1 not found")
	}
	if _, ok := keys["key2"]; !ok {
		t.Error("key2 not found")
	}
}