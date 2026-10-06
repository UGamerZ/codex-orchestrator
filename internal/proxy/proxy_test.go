package proxy

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAllowlist(t *testing.T) {
	for _, target := range []string{"chatgpt.com:443", "api.openai.com:443", "auth.openai.com:443"} {
		if !Allowed(target) {
			t.Errorf("expected allow %s", target)
		}
	}
	for _, target := range []string{"github.com:443", "api.telegram.org:443", "api.trello.com:443", "chatgpt.com:80", "chatgpt.com.evil.test:443", "127.0.0.1:443", "169.254.169.254:443", "[::1]:443", "chatgpt.com:22"} {
		if Allowed(target) {
			t.Errorf("unexpected allow %s", target)
		}
	}
}
func TestRejectPrivateDNS(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "10.1.2.3", "172.16.1.1", "192.168.1.1", "169.254.169.254", "::1", "fc00::1", "fe80::1", "0.0.0.0"} {
		if Public(net.ParseIP(address)) {
			t.Errorf("private address allowed %s", address)
		}
	}
	if !Public(net.ParseIP("8.8.8.8")) {
		t.Fatal("public address rejected")
	}
}
func TestForbiddenTunnel(t *testing.T) {
	for _, method := range []string{"GET", "CONNECT"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "https://github.com/", nil)
		r.Host = "github.com:443"
		Handler(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("unexpected HTTP %d", w.Code)
		}
	}
}
