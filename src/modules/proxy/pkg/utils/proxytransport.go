package utils

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/url"
	"strings"
	"time"

	"proxy/pkg/logger"
)

// liveTransportTimeout 直播请求的响应头超时（播放列表轮询频繁，取值与点播一致）。
const liveTransportTimeout = 120 * time.Second

// NewTransportWithProxy 构造直播用的 transport。
// http/https 代理走 Transport.Proxy；socks5 只能走 DialContext；空值保持直连，不读环境变量。
func NewTransportWithProxy(proxyURL string, skipVerify bool) *http.Transport {
	transport := &http.Transport{
		ResponseHeaderTimeout: liveTransportTimeout,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: skipVerify},
	}

	if proxyURL == "" {
		return transport
	}

	parsed, err := url.Parse(proxyURL)
	if err != nil || parsed.Host == "" {
		return transport
	}

	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		transport.Proxy = func(*http.Request) (*url.URL, error) { return parsed, nil }
	case "socks5":
		transport.DialContext = socksDialer(parsed)
	default:
		logger.Warnf("无法识别的直播代理协议: %s", parsed.Scheme)
	}

	return transport
}

// FetchLive 请求直播资源；配了代理但代理不可达时回退直连，并记录实际走的是哪条路。
// 返回第二个值表示这次实际走的是代理还是直连。
// 这里不设 http.Client.Timeout：直播分片要持续读流，整请求超时会把分片掐断；
// 超时由 transport 的 ResponseHeaderTimeout 和请求 context 控制。
func FetchLive(ctx context.Context, target string, headers map[string]string, transport *http.Transport, proxyURL string, skipVerify bool) (*http.Response, bool, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, false, err
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	client := &http.Client{Transport: transport}
	response, err := client.Do(request)
	if err == nil {
		if proxyURL != "" {
			logger.Infof("直播请求走代理: %s", proxyURL)
			return response, true, nil
		}
		logger.Infof("直播请求直连: %s", request.URL.Host)
		return response, false, nil
	}

	if proxyURL == "" {
		return nil, false, err
	}

	logger.Warnf("直播代理不可达，回退直连: %v", err)
	response, err = (&http.Client{Transport: NewTransportWithProxy("", skipVerify)}).Do(request)
	if err != nil {
		return nil, false, err
	}
	return response, false, nil
}
