package system

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"codex-orchestrator/internal/broker"
)

var taskURL = regexp.MustCompile(`https://[^\s<>"\)]+`)
var trelloDownload = regexp.MustCompile(`^/1/cards/([a-f0-9]{24})/attachments/([a-f0-9]{24})/(?:previews/[a-f0-9]{24}/)?download/(.+)$`)

func canonicalAssetURL(raw string) (string, error) {
	u, e := url.Parse(strings.TrimRight(raw, ".,;"))
	if e != nil {
		return "", fmt.Errorf("invalid linked file URL")
	}
	if u.Scheme != "https" || u.User != nil || u.Hostname() == "" || (u.Port() != "" && u.Port() != "443") {
		return "", fmt.Errorf("linked files require HTTPS without credentials")
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && !publicAssetIP(ip) {
		return "", fmt.Errorf("private network file URL blocked")
	}
	if strings.EqualFold(u.Hostname(), "localhost") || strings.HasSuffix(strings.ToLower(u.Hostname()), ".localhost") {
		return "", fmt.Errorf("private network file URL blocked")
	}
	if u.Hostname() == "trello.com" || u.Hostname() == "api.trello.com" {
		if m := trelloDownload.FindStringSubmatch(u.Path); m != nil {
			u.Host = "api.trello.com"
			u.Path = "/1/cards/" + m[1] + "/attachments/" + m[2] + "/download/" + m[3]
			u.RawPath = ""
			u.RawQuery = ""
		}
	}
	u.Fragment = ""
	return u.String(), nil
}

func publicAssetIP(ip net.IP) bool {
	_, shared, _ := net.ParseCIDR("100.64.0.0/10")
	if shared.Contains(ip) {
		return false
	}
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified() && !ip.IsMulticast()
}

func assetHTTPClient() *http.Client {
	dialer := net.Dialer{Timeout: 20 * time.Second}
	transport := &http.Transport{TLSHandshakeTimeout: 20 * time.Second, ResponseHeaderTimeout: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, e := net.SplitHostPort(address)
		if e != nil {
			return nil, e
		}
		ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
		if e != nil {
			return nil, fmt.Errorf("file host lookup failed")
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("file host has no addresses")
		}
		for _, ip := range ips {
			if !publicAssetIP(ip.IP) {
				return nil, fmt.Errorf("private network file host blocked")
			}
		}
		var conn net.Conn
		for _, ip := range ips {
			conn, e = dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if e == nil {
				return conn, nil
			}
		}
		return nil, fmt.Errorf("file host connection failed")
	}
	return &http.Client{Transport: transport, Timeout: 60 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many file redirects")
		}
		if _, e := canonicalAssetURL(req.URL.String()); e != nil {
			return e
		}
		req.Header.Del("Authorization")
		authorizeAsset(req)
		return nil
	}}
}

func authorizeAsset(req *http.Request) {
	if req.URL.Hostname() == "api.trello.com" && trelloDownload.MatchString(req.URL.Path) {
		req.Header.Set("Authorization", "OAuth oauth_consumer_key="+strconv.Quote(os.Getenv("TRELLO_KEY"))+", oauth_token="+strconv.Quote(os.Getenv("TRELLO_TOKEN")))
	}
}

func (a *API) TaskAssets(ctx context.Context, j *Job) ([]broker.Asset, error) {
	urls := taskURL.FindAllString(j.Description+"\n"+j.Answer, -1)
	if j.Card != "" {
		var attachments []struct {
			URL string `json:"url"`
		}
		if e := a.trello(ctx, "GET", "cards/"+j.Card+"/attachments", nil, &attachments); e != nil {
			return nil, e
		}
		for _, attachment := range attachments {
			urls = append(urls, attachment.URL)
		}
	}
	return downloadTaskAssets(ctx, urls, assetHTTPClient()), nil
}

func downloadTaskAssets(ctx context.Context, urls []string, client *http.Client) []broker.Asset {
	out := []broker.Asset{}
	seen := map[string]bool{}
	total := 0
	for _, raw := range urls {
		source, e := canonicalAssetURL(raw)
		if e != nil {
			out = append(out, broker.Asset{Name: fmt.Sprintf("%02d-unavailable.txt", len(out)+1), Source: raw, Error: e.Error()})
			continue
		}
		if seen[source] {
			continue
		}
		seen[source] = true
		if len(out) >= 32 {
			out = append(out, broker.Asset{Name: "attachment-limit.txt", Error: "More than 32 linked files; select the required references"})
			break
		}
		u, _ := url.Parse(source)
		name := regexp.MustCompile(`[^A-Za-z0-9_.-]`).ReplaceAllString(path.Base(u.Path), "_")
		if len(name) > 100 {
			name = name[len(name)-100:]
		}
		if name == "" || name == "." || name == "/" {
			name = "reference.bin"
		}
		asset := broker.Asset{Name: fmt.Sprintf("%02d-%s", len(out)+1, name), Source: source}
		req, _ := http.NewRequestWithContext(ctx, "GET", source, nil)
		authorizeAsset(req)
		resp, e := client.Do(req)
		if e != nil {
			asset.Error = "Linked file download failed (check access or connectivity)"
		} else {
			if resp.StatusCode/100 != 2 {
				asset.Error = fmt.Sprintf("Linked file HTTP %d", resp.StatusCode)
			} else {
				asset.Data, e = io.ReadAll(io.LimitReader(resp.Body, (16<<20)+1))
				if e != nil {
					asset.Error = "Linked file could not be read"
				} else if len(asset.Data) > 16<<20 || total+len(asset.Data) > 64<<20 {
					asset.Error = "Linked file exceeds task attachment size limits"
				}
			}
			resp.Body.Close()
		}
		if asset.Error != "" {
			asset.Data = nil
		}
		total += len(asset.Data)
		out = append(out, asset)
	}
	return out
}
