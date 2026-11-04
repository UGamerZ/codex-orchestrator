package system

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestMarkdownPageReferences(t *testing.T) {
	j := &Job{Description: "[https://a.intimcity.fit/](https://a.intimcity.fit/ \"‌\")"}
	calls := 0
	// Preserve the original encoding for parser fixtures, rather than silently
	// interpreting a Windows-1251 page as UTF-8.
	html := []byte{'<', 'h', 't', 'm', 'l', '>', 0xcf, 0xf0}
	client := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.String() != "https://a.intimcity.fit/" {
			t.Fatalf("Markdown markup leaked into URL: %s", r.URL)
		}
		if r.Header.Get("Authorization") != "" {
			t.Fatal("credentials sent to public site")
		}
		resp := response(string(html))
		resp.Header = http.Header{"Content-Type": {"text/html; charset=windows-1251"}}
		resp.Request = r
		return resp, nil
	})}
	assets := downloadTaskAssets(context.Background(), taskReferenceURLs(j), client)
	if calls != 1 || len(assets) != 1 {
		t.Fatalf("expected one canonical request: calls=%d assets=%+v", calls, assets)
	}
	a := assets[0]
	if a.Error != "" || !bytes.Equal(a.Data, html) || !strings.HasSuffix(a.Name, ".html") || a.ContentType != "text/html; charset=windows-1251" || a.FinalURL != "https://a.intimcity.fit/" {
		t.Fatalf("HTML snapshot lost bytes or metadata: %+v", a)
	}
	if _, err := time.Parse(time.RFC3339, a.FetchedAt); err != nil {
		t.Fatalf("missing fetch timestamp: %v", err)
	}
}

func TestReferencesRetainEarlierAnswers(t *testing.T) {
	j := &Job{Title: "Inspect https://example.org/title", Description: "![image](https://example.org/image.png)", Answer: "Continue", Clarifications: []Clarification{{Answer: "Use https://example.org/earlier%28v2%29"}}}
	urls := taskReferenceURLs(j)
	want := []string{"https://example.org/title", "https://example.org/image.png", "https://example.org/earlier%28v2%29"}
	if strings.Join(urls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("references: %v", urls)
	}
}

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
