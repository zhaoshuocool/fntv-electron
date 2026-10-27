package api

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
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

// resolveLiveURI 把播放列表里的一条 URI 解析成上游真实地址：
// 绝对地址保持原样，相对路径解析回播放列表所在目录（目录要带斜杠，否则最后一段会被当成文件名丢掉）。
func resolveLiveURI(playlist *url.URL, uri string) (*url.URL, error) {
	target, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}

	if target.Scheme != "" {
		if target.Host == "" {
			return nil, errors.New("直播分片地址无法解析")
		}
		return target, nil
	}

	base := *playlist
	base.Path = path.Dir(playlist.Path) + "/"
	resolved := base.ResolveReference(&url.URL{Path: uri})
	resolved.RawQuery = playlist.RawQuery
	if resolved.Scheme == "" || resolved.Host == "" {
		return nil, errors.New("直播分片路径无法解析")
	}
	return resolved, nil
}

// ResolveLiveTarget 解析一个直播请求要回源到哪个地址：
// 改写时带回来的绝对地址优先（实测分片是绝对地址），否则把相对路径解析回播放列表所在目录。
// u 只接受带协议的绝对地址，不能被 protocol-relative 值带跑。
func ResolveLiveTarget(playlist *url.URL, absolute string, subPath string) (*url.URL, error) {
	if absolute != "" {
		target, err := url.Parse(absolute)
		if err != nil {
			return nil, err
		}
		if (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
			return nil, errors.New("直播分片地址无法解析")
		}
		return target, nil
	}

	if subPath == "" {
		return playlist, nil
	}
	return resolveLiveURI(playlist, subPath)
}

// RewriteHlsPlaylist 把每条 URI 行改写回 proxy 入口（spec 4.5：分片也必须经过 proxy），
// 上游真实地址用 u 参数带回来；注释行保持原样。
func RewriteHlsPlaylist(body string, entry *url.URL, playlist *url.URL) string {
	lines := strings.Split(body, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		target, err := resolveLiveURI(playlist, trimmed)
		if err != nil {
			continue
		}

		query := url.Values{}
		for key, values := range entry.Query() {
			query.Set(key, values[0])
		}
		query.Set("u", target.String())

		rewritten := *entry
		rewritten.RawQuery = query.Encode()
		lines[i] = rewritten.String()
	}

	return strings.Join(lines, "\n")
}

// ProxyLiveEntry 构造改写回 proxy 入口的地址，保住 session 查询。
func ProxyLiveEntry(c *gin.Context, itemGuid string) *url.URL {
	entry := &url.URL{
		Scheme: "http",
		Host:   c.Request.Host,
		Path:   "/api/v1/live/" + url.PathEscape(itemGuid),
	}
	entry.RawQuery = c.Request.URL.RawQuery
	return entry
}

// copyLiveHeaders 转发上游响应头；改写过的 body 不能沿用上游的 Content-Length。
func copyLiveHeaders(c *gin.Context, response *http.Response, rewritten bool) {
	for key, values := range response.Header {
		if key == "Connection" || key == "Transfer-Encoding" {
			continue
		}
		if rewritten && (key == "Content-Length" || key == "Content-Encoding") {
			continue
		}
		for _, value := range values {
			c.Header(key, value)
		}
	}
}

// ServeLive 透传直播响应：播放列表改写回 proxy 入口（分片同样经过 proxy），其余内容原样转发。
func ServeLive(c *gin.Context, target *fnapi.LiveChannel, entry *url.URL, extraHeaders map[string]string, transport *http.Transport, proxyURL string, skipVerify bool) {
	playlist, err := url.Parse(target.Path)
	if err != nil || playlist.Host == "" {
		c.JSON(400, gin.H{"error": "Invalid live line URL"})
		return
	}

	requested, err := ResolveLiveTarget(playlist, c.Query("u"), c.Param("rest"))
	if err != nil {
		c.JSON(400, gin.H{"error": "Invalid live segment path"})
		return
	}

	response, viaProxy, err := utils.FetchLive(c.Request.Context(), requested.String(), extraHeaders, transport, proxyURL, skipVerify)
	if err != nil {
		logger.Errorf("直播线路请求失败: %v", err)
		c.JSON(502, gin.H{"error": "Live line unreachable"})
		return
	}
	defer response.Body.Close()

	if !strings.Contains(response.Header.Get("Content-Type"), "mpegurl") {
		copyLiveHeaders(c, response, false)
		c.Writer.WriteHeader(response.StatusCode)
		_, _ = io.Copy(c.Writer, response.Body)
		return
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		logger.Errorf("读取直播播放列表失败: %v", err)
		return
	}

	rewritten := RewriteHlsPlaylist(string(body), entry, playlist)
	copyLiveHeaders(c, response, true)
	c.Header("Content-Length", strconv.Itoa(len(rewritten)))
	c.Writer.WriteHeader(response.StatusCode)
	_, _ = io.WriteString(c.Writer, rewritten)

	if viaProxy {
		logger.Infof("直播播放列表走代理改写: %s", proxyURL)
	}
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

	ServeLive(c, target, ProxyLiveEntry(c, params.ItemGuid), utils.PassthroughHeaders(c.Request), utils.NewTransportWithProxy(session.ProxyURL, session.SkipVerify), session.ProxyURL, session.SkipVerify)
}
