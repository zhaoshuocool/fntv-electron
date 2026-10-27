package api

import (
	"context"
	"errors"
	"proxy/pkg/fnapi"
	"proxy/pkg/logger"
	"proxy/pkg/utils"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

var (
	// 115盘请求速率限制器，1s/次
	globalLimiter = rate.NewLimiter(rate.Limit(1), 1)
	globalCtx     = context.Background()
)

func waitLimiter() error {
	return globalLimiter.Wait(globalCtx)
}

// parseQueryParam 解析查询参数
func parseQueryParam(c *gin.Context) (*PlayVideoParams, error) {
	var params PlayVideoParams

	// 先绑定 URL Path 里的参数
	if err := c.ShouldBindUri(&params); err != nil {
		return nil, err
	}

	// 再绑定 query 参数
	if err := c.ShouldBindQuery(&params); err != nil {
		return nil, err
	}

	if params.Session == "" || params.ItemGuid == "" {
		return nil, errors.New("missing required parameters")
	}

	return &params, nil
}

// ParseCloudInfo 解析云存储信息
func ParseCloudInfo(info fnapi.StreamResponse) *CloudStorageInfo {
	if info.CloudStorageInfo == nil {
		return nil
	}

	// 没有直链
	if len(info.DirectLinkQualities) <= 0 {
		return nil
	}

	result := &CloudStorageInfo{}
	result.DownloadURL = info.DirectLinkQualities[0].URL

	if len(info.Header.Cookie) > 0 {
		result.Cookie = strings.Join(info.Header.Cookie, "; ")
	}

	result.CloudType = CloudType(info.CloudStorageInfo.CloudStorageType)

	return result
}

func getStreamUserAgent(info fnapi.StreamResponse) string {
	for _, userAgent := range info.Header.UserAgent {
		userAgent = strings.TrimSpace(userAgent)
		if userAgent != "" {
			return userAgent
		}
	}

	return ""
}

func applyPlaybackCookie(headers map[string]string, useCloudDirect bool, cloudCookie, accessCookie string) {
	delete(headers, "Cookie")
	if useCloudDirect {
		if cloudCookie != "" {
			headers["Cookie"] = cloudCookie
		}
		return
	}
	headers["Cookie"] = fnapi.ComposeCookieHeader(accessCookie)
}

// liveInfoFor 只在直播路径上调用：play/list 失败或没有视频流时才查一次 play/info，
// 点播有视频流时不会多这一次调用。
func liveInfoFor(fnApi *fnapi.ApiService, itemGuid string) *fnapi.PlayInfo {
	info, err := fnApi.GetPlayInfoCached(itemGuid)
	if err != nil || !info.Success || info.Data.Type != "LiveChannel" {
		return nil
	}
	return &info.Data
}

func serveLiveChannel(c *gin.Context, info *fnapi.PlayInfo, session *PlaybackSession, params *PlayVideoParams) {
	target := SelectLiveChannel(*info, int(params.SourceIndex))
	if target == nil {
		logger.Warnf("直播频道没有可播放线路: itemGuid=%s", params.ItemGuid)
		c.JSON(404, gin.H{"error": "no playable live line"})
		return
	}

	logger.Infof("直播线路透传 | itemGuid: %s | 线路: %s", params.ItemGuid, target.FileName)
	ServeLive(c, target, ProxyLiveEntry(c, params.ItemGuid), utils.PassthroughHeaders(c.Request), utils.NewTransportWithProxy(session.ProxyURL, session.SkipVerify), session.ProxyURL, session.SkipVerify)
}

func PlayVideoHandler(c *gin.Context, sessions *PlaybackSessionStore) {
	params, err := parseQueryParam(c)
	if err != nil {
		logger.Errorf("解析参数失败: %v", err)
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
	resp, err := fnApi.GetStreamListCached(params.ItemGuid)
	if err != nil || !resp.Success {
		// 直播项没有媒体文件，play/list 可能直接失败；这里也要按直播处理
		if live := liveInfoFor(fnApi, params.ItemGuid); live != nil {
			serveLiveChannel(c, live, session, params)
			return
		}
		logger.Errorf("获取播放信息失败或为空: %v", err)
		c.JSON(500, gin.H{"error": "Failed to get play info"})
		return
	}

	if len(resp.Data.VideoStreams) == 0 {
		if live := liveInfoFor(fnApi, params.ItemGuid); live != nil {
			serveLiveChannel(c, live, session, params)
			return
		}
		logger.Warnf("播放信息里没有视频流: itemGuid=%s", params.ItemGuid)
		c.JSON(404, gin.H{"error": "no playable stream"})
		return
	}

	// 选择视频流
	videoStreams := resp.Data.VideoStreams
	targetMediaGuid := videoStreams[0].MediaGUID
	if params.SourceIndex > 0 && int(params.SourceIndex) < len(videoStreams) {
		targetMediaGuid = videoStreams[params.SourceIndex].MediaGUID
	}

	// 获取流地址信息
	streamResp, err := fnApi.GetStreamCached(targetMediaGuid, session.Account)
	if err != nil || !streamResp.Success {
		logger.Errorf("获取视频流失败: %v", err)
		c.JSON(500, gin.H{"error": "Failed to get stream"})
		return
	}

	var (
		targetUrl    = fnApi.GetVideoURL(targetMediaGuid)
		proxyType    = TransparentProxy
		skipVerify   = session.SkipVerify
		extraHeaders = utils.PassthroughHeaders(c.Request)
	)

	cloudInfo := ParseCloudInfo(streamResp.Data)
	streamUserAgent := getStreamUserAgent(streamResp.Data)
	useCloudDirect := cloudInfo != nil && !session.UseNasLocal
	cloudCookie := ""
	if cloudInfo != nil {
		cloudCookie = cloudInfo.Cookie
	}
	applyPlaybackCookie(extraHeaders, useCloudDirect, cloudCookie, session.AccessCookie)

	// 云盘直链模式
	if useCloudDirect {
		logger.Infof("启用云存储直连: type=%d", cloudInfo.CloudType)

		targetUrl = cloudInfo.DownloadURL
		// 禁止跳过证书验证，云厂商的证书通常是合法的，不需要跳过验证
		skipVerify = false

		if streamUserAgent != "" {
			extraHeaders["User-Agent"] = streamUserAgent
		}

		// 选择播放策略
		switch cloudInfo.CloudType {
		case QuarkPan:
			proxyType = ChunkedProxy // 夸克需要切片
		case Cloud115Pan:
			// 115 特殊处理：UA 和限流
			_ = waitLimiter()
			// 其他网盘使用默认的 TransparentProxy
		}
	} else {
		// 本地 NAS 转发模式 ---
		// 只有请求 NAS 时才需要 Authorization Token
		extraHeaders["Authorization"] = session.Token
	}

	// 执行代理
	logger.Infof("开始代理 | 模式: %v | itemGuid: %s", proxyType, params.ItemGuid)

	switch proxyType {
	case ChunkedProxy:
		// 边下边播处理 (如夸克)
		handler := utils.NewCloudStorageHandler(targetUrl, extraHeaders, skipVerify)
		handler.HandleRequest(c)
	default:
		// 透明代理 (本地 NAS 或 115/阿里等直链)
		utils.DynamicProxy(c, targetUrl, extraHeaders, skipVerify)
	}
}
