package system

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
)

func TestTaskAssetsNormalizeAndAuthenticate(t *testing.T) {
	t.Setenv("TRELLO_KEY", "test-key")
	t.Setenv("TRELLO_TOKEN", "test-token")
	raw := "https://trello.com/1/cards/0123456789abcdef01234567/attachments/1123456789abcdef01234567/previews/2123456789abcdef01234567/download/screen.webp"
	calls := 0
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "api.trello.com" || strings.Contains(r.URL.Path, "previews") {
			t.Fatal("preview URL not normalized")
		}
		if !strings.Contains(r.Header.Get("Authorization"), `oauth_token="test-token"`) {
			t.Fatal("missing scoped authentication")
		}
		return response("image-bytes"), nil
	})}
	assets := downloadTaskAssets(context.Background(), []string{raw, raw}, client)
	if calls != 1 || len(assets) != 1 || string(assets[0].Data) != "image-bytes" || assets[0].Error != "" {
		t.Fatalf("assets %+v", assets)
	}
	req, _ := http.NewRequest("GET", "https://example.org/image.png", nil)
	authorizeAsset(req)
	if req.Header.Get("Authorization") != "" {
		t.Fatal("Trello credential sent outside Trello")
	}
	redirect, _ := http.NewRequest("GET", "https://example.org/image.png", nil)
	redirect.Header.Set("Authorization", "secret")
	if e := assetHTTPClient().CheckRedirect(redirect, []*http.Request{req}); e != nil {
		t.Fatal(e)
	}
	if redirect.Header.Get("Authorization") != "" {
		t.Fatal("credential leaked through redirect")
	}
}

func TestTaskAssetsRejectUnsafeDestinations(t *testing.T) {
	for _, raw := range []string{"http://example.org/file", "https://user:pass@example.org/file", "https://127.0.0.1/file", "https://169.254.169.254/file", "https://[::1]/file", "https://localhost/file", "https://example.org:444/file"} {
		if _, e := canonicalAssetURL(raw); e == nil {
			t.Fatalf("unsafe destination accepted: %s", raw)
		}
	}
	for _, raw := range []string{"10.1.2.3", "172.16.1.2", "192.168.1.1", "127.0.0.1", "169.254.169.254", "::1", "fd00::1"} {
		if publicAssetIP(net.ParseIP(raw)) {
			t.Fatalf("private IP accepted: %s", raw)
		}
	}
	redirect, _ := http.NewRequest("GET", "https://127.0.0.1/secret", nil)
	if assetHTTPClient().CheckRedirect(redirect, nil) == nil {
		t.Fatal("private redirect accepted")
	}
	assets := downloadTaskAssets(context.Background(), []string{"https://example.org/file"}, &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		r := response("denied")
		r.StatusCode = 403
		return r, nil
	})})
	if len(assets) != 1 || assets[0].Error != "Linked file HTTP 403" || len(assets[0].Data) != 0 {
		t.Fatal("download failure not exposed")
	}
}
