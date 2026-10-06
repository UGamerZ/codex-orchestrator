package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Exact hosts only. No generic forwarding, private targets, wildcards or non-TLS ports.
var allowed = map[string]bool{"chatgpt.com": true, "api.openai.com": true, "auth.openai.com": true}

func Allowed(target string) bool {
	host, port, e := net.SplitHostPort(target)
	return e == nil && port == "443" && allowed[strings.ToLower(host)]
}
func Public(ip net.IP) bool {
	return ip != nil && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast() && !ip.IsUnspecified() && ip.IsGlobalUnicast()
}
func Handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "CONNECT" || !Allowed(r.Host) {
		http.Error(w, "destination forbidden", 403)
		return
	}
	host, _, _ := net.SplitHostPort(r.Host)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	ips, e := net.DefaultResolver.LookupIPAddr(ctx, host)
	if e != nil {
		http.Error(w, "DNS failed", 502)
		return
	}
	var target net.Conn
	for _, ip := range ips {
		if !Public(ip.IP) {
			continue
		}
		target, e = (&net.Dialer{Timeout: 8 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(ip.IP.String(), "443"))
		if e == nil {
			break
		}
	}
	if target == nil {
		http.Error(w, "connection failed", 502)
		return
	}
	h, ok := w.(http.Hijacker)
	if !ok {
		target.Close()
		http.Error(w, "tunnel unavailable", 500)
		return
	}
	client, buf, e := h.Hijack()
	if e != nil {
		target.Close()
		return
	}
	fmt.Fprint(buf, "HTTP/1.1 200 Connection Established\r\n\r\n")
	if e = buf.Flush(); e != nil {
		client.Close()
		target.Close()
		return
	}
	target.SetDeadline(time.Now().Add(time.Hour))
	client.SetDeadline(time.Now().Add(time.Hour))
	go func() { defer target.Close(); defer client.Close(); io.Copy(target, buf) }()
	go func() { defer target.Close(); defer client.Close(); io.Copy(client, target) }()
}
func Run() error {
	return (&http.Server{Addr: ":8080", Handler: http.HandlerFunc(Handler), ReadHeaderTimeout: 10 * time.Second}).ListenAndServe()
}
