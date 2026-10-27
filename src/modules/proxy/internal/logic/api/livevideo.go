package api

import (
	"crypto/tls"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"proxy/pkg/fnapi"
	"proxy/pkg/logger"

	"github.com/gin-gonic/gin"
)

const liveFetchTimeout = 120 * time.Second

// SelectLiveChannel 按 sort_num 取线路；sourceIndex 越界或不可播时回退到第一条可播线路。
func SelectLiveChannel(info fnapi.PlayInfo, sourceIndex int) *fnapi.LiveChannel {
	if len(info.LiveChannels) == 0 {
		return nil
	}

	sorted := append([]fnapi.LiveChannel(nil), info.LiveChannels...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].SortNum < sorted[j].SortNum })

	if sourceIndex >= 0 && sourceIndex < len(sorted) && sorted[sourceIndex].CanPlay == 1 {
		return &sorted[sourceIndex]
	}

	for i := range sorted {
		if sorted[i].CanPlay == 1 {
			return &sorted[i]
		}
	}

	return nil
}

// RewriteHlsPlaylist 只改写 URI 行：注释行和已经是绝对地址的行保持原样。
// baseURL 是请求入口地址（proxy 自己的入口），这样分片也会经过 proxy；
// 入口地址带 session 查询时，改写后的地址必须保住这个查询。
func RewriteHlsPlaylist(body string, baseURL *url.URL) string {
	lines := strings.Split(body, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
			continue
		}

		resolved := baseURL.ResolveReference(&url.URL{Path: trimmed})
		if resolved.Scheme == "" || resolved.Host == "" {
			continue
		}
		if baseURL.RawQuery != "" {
			resolved.RawQuery = baseURL.RawQuery
		}
		lines[i] = resolved.String()
	}

	return strings.Join(lines, "\n")
}

// ResolveLiveURL 把播放列表里的相对路径解析回上游线路地址。
func ResolveLiveURL(target *fnapi.LiveChannel, subPath string) *url.URL {
	upstream, err := url.Parse(target.Path)
	if err != nil {
		return nil
	}
	if subPath == "" {
		return upstream
	}

	base := *upstream
	base.Path = path.Dir(upstream.Path)
	resolved := base.ResolveReference(&url.URL{Path: subPath})
	resolved.RawQuery = upstream.RawQuery
	return resolved
}

// newLiveTransport 构造直播请求用的 transport（Task 7 换成带代理支持的版本）。
func newLiveTransport(skipVerify bool) *http.Transport {
	return &http.Transport{
		ResponseHeaderTimeout: liveFetchTimeout,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: skipVerify},
	}
}

// ServeLive 透传直播响应：播放列表改写相对 URI，其余内容原样转发。
func ServeLive(c *gin.Context, target *fnapi.LiveChannel, extraHeaders map[string]string, transport *http.Transport) {
	requested := ResolveLiveURL(target, c.Param("rest"))
	if requested == nil {
		c.JSON(400, gin.H{"error": "Invalid live line URL"})
		return
	}

	request, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, requested.String(), nil)
	if err != nil {
		logger.Errorf("直播请求构造失败: %v", err)
		c.JSON(400, gin.H{"error": "Invalid live request"})
		return
	}
	for key, value := range extraHeaders {
		request.Header.Set(key, value)
	}

	client := &http.Client{Transport: transport, Timeout: liveFetchTimeout}
	response, err := client.Do(request)
	if err != nil {
		logger.Errorf("直播线路请求失败: %v", err)
		c.JSON(502, gin.H{"error": "Live line unreachable"})
		return
	}
	defer response.Body.Close()

	for key, values := range response.Header {
		for _, value := range values {
			c.Header(key, value)
		}
	}
	c.Writer.WriteHeader(response.StatusCode)

	if !strings.Contains(response.Header.Get("Content-Type"), "mpegurl") {
		_, _ = io.Copy(c.Writer, response.Body)
		return
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		logger.Errorf("读取直播播放列表失败: %v", err)
		return
	}

	entry := *c.Request.URL
	entry.Path = strings.TrimSuffix(entry.Path, "/") + "/"
	_, _ = io.WriteString(c.Writer, RewriteHlsPlaylist(string(body), &entry))
}
