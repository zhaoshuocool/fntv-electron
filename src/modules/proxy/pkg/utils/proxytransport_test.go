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

	resp, viaProxy, err := FetchLive(context.Background(), server.URL, map[string]string{}, NewTransportWithProxy("http://127.0.0.1:9", false), "http://127.0.0.1:9", false)
	if err != nil {
		t.Fatalf("expected direct fallback, got error: %v", err)
	}
	defer resp.Body.Close()

	if viaProxy {
		t.Fatal("the fallback must be reported as a direct connection")
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "#EXTM3U") {
		t.Fatalf("fallback did not reach upstream: %s", body)
	}
}

func TestFetchLiveReportsWhichPathItUsed(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("#EXTM3U\n"))
	}))
	defer upstream.Close()

	proxyHits := 0
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHits++
		_, _ = w.Write([]byte("#EXTM3U\n"))
	}))
	defer proxy.Close()

	resp, viaProxy, err := FetchLive(context.Background(), upstream.URL, map[string]string{}, NewTransportWithProxy(proxy.URL, false), proxy.URL, false)
	if err != nil {
		t.Fatalf("proxied fetch failed: %v", err)
	}
	_ = resp.Body.Close()
	if !viaProxy {
		t.Fatal("a proxied request must be reported as proxied")
	}
	if proxyHits != 1 {
		t.Fatalf("the request did not go through the configured proxy (hits=%d)", proxyHits)
	}

	resp, viaProxy, err = FetchLive(context.Background(), upstream.URL, map[string]string{}, NewTransportWithProxy("", false), "", false)
	if err != nil {
		t.Fatalf("direct fetch failed: %v", err)
	}
	_ = resp.Body.Close()
	if viaProxy {
		t.Fatal("a direct request must not be reported as proxied")
	}
}

// 回退直连时不能把会话的 skipVerify 丢掉。
func TestFetchLiveFallbackKeepsSkipVerify(t *testing.T) {
	fallback := NewTransportWithProxy("", true)
	if !fallback.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("direct transport must keep the session skipVerify setting")
	}
}
