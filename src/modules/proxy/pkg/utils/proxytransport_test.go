package utils

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestNewTransportWithProxyKeepsDirectPathWhenUnset(t *testing.T) {
	transport := NewTransportWithProxy("", false)
	if transport.Proxy != nil {
		t.Fatal("unset proxy must not read the environment")
	}
	if transport.DialContext != nil {
		t.Fatal("unset proxy must not install a dialer")
	}
}

func TestNewTransportWithProxyAppliesHttpProxy(t *testing.T) {
	transport := NewTransportWithProxy("http://127.0.0.1:7890", false)
	target, err := transport.Proxy(&http.Request{URL: &url.URL{Scheme: "https", Host: "iptv.example"}})
	if err != nil {
		t.Fatalf("proxy lookup failed: %v", err)
	}
	if target == nil || target.String() != "http://127.0.0.1:7890" {
		t.Fatalf("proxy not applied: %v", target)
	}
}

func TestNewTransportWithProxyUsesDialContextForSocks(t *testing.T) {
	transport := NewTransportWithProxy("socks5://127.0.0.1:1080", false)
	if transport.Proxy != nil {
		t.Fatal("socks proxy must not be set as an http proxy")
	}
	if transport.DialContext == nil {
		t.Fatal("socks proxy needs DialContext")
	}
}

func TestNewTransportWithProxyIgnoresMalformedProxy(t *testing.T) {
	transport := NewTransportWithProxy("not a url", false)
	if transport.Proxy != nil || transport.DialContext != nil {
		t.Fatal("malformed proxy must fall back to a direct transport")
	}
}

func TestFetchLiveFallsBackToDirectWhenProxyUnreachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("#EXTM3U\n"))
	}))
	defer server.Close()

	resp, err := FetchLive(context.Background(), server.URL, map[string]string{}, NewTransportWithProxy("http://127.0.0.1:9", false), "http://127.0.0.1:9")
	if err != nil {
		t.Fatalf("expected direct fallback, got error: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "#EXTM3U") {
		t.Fatalf("fallback did not reach upstream: %s", body)
	}
}
