package api

import (
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"

	"proxy/pkg/fnapi"
	"proxy/pkg/logger"
	"proxy/pkg/utils"

	"github.com/gin-gonic/gin"
)

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

// ServeLive 透传直播响应：播放列表改写相对 URI（改写回 proxy 入口，分片同样经过 proxy），其余内容原样转发。
func ServeLive(c *gin.Context, target *fnapi.LiveChannel, extraHeaders map[string]string, transport *http.Transport, proxyURL string) {
	requested := ResolveLiveURL(target, c.Param("rest"))
	if requested == nil {
		c.JSON(400, gin.H{"error": "Invalid live line URL"})
		return
	}

	response, err := utils.FetchLive(c.Request.Context(), requested.String(), extraHeaders, transport, proxyURL)
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

// ServeLiveHandler 处理 /api/v1/live/:itemGuid 及其下的相对路径（播放列表改写后仍回到这里）。
func ServeLiveHandler(c *gin.Context, sessions *PlaybackSessionStore) {
	params, err := parseQueryParam(c)
	if err != nil {
		c.JSON(400, gin.H{"error": "Invalid parameters"})
		return
	}

	session, err := sessions.Resolve(params.Session, params.ItemGuid)
	if err != nil {
		logger.Warnf("拒绝无效播放会话: itemGuid=%s", params.ItemGuid)
		c.JSON(401, gin.H{"error": "Invalid playback session"})
		return
	}

	fnApi := fnapi.NewApiService(session.Domain, session.Token, session.SkipVerify, session.AccessCookie)
	info, err := fnApi.GetPlayInfoCached(params.ItemGuid)
	if err != nil || !info.Success || info.Data.Type != "LiveChannel" {
		logger.Errorf("直播项解析失败: %v", err)
		c.JSON(500, gin.H{"error": "Failed to get play info"})
		return
	}

	target := SelectLiveChannel(info.Data, int(params.SourceIndex))
	if target == nil {
		c.JSON(404, gin.H{"error": "no playable live line"})
		return
	}

	ServeLive(c, target, utils.PassthroughHeaders(c.Request), utils.NewTransportWithProxy(session.ProxyURL, session.SkipVerify), session.ProxyURL)
}
