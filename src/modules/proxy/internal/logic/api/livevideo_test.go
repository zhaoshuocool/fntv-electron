package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"proxy/pkg/fnapi"

	"github.com/gin-gonic/gin"
)

func liveInfo(channels []fnapi.LiveChannel) fnapi.PlayInfo {
	return fnapi.PlayInfo{Type: "LiveChannel", LiveChannels: channels}
}

func TestSelectLiveChannelFallsBackToFirstPlayableLine(t *testing.T) {
	info := liveInfo([]fnapi.LiveChannel{
		{FileName: "线路1", Path: "https://iptv.example/u/token/a", SortNum: 0, CanPlay: 1},
		{FileName: "线路2", Path: "https://iptv.example/u/token/b", SortNum: 1, CanPlay: 1},
	})

	picked := SelectLiveChannel(info, 9)
	if picked == nil || picked.FileName != "线路1" {
		t.Fatalf("expected fallback to 线路1, got %+v", picked)
	}

	picked = SelectLiveChannel(info, 1)
	if picked == nil || picked.FileName != "线路2" {
		t.Fatalf("expected 线路2, got %+v", picked)
	}
}

func TestSelectLiveChannelSkipsUnplayableLines(t *testing.T) {
	info := liveInfo([]fnapi.LiveChannel{
		{FileName: "线路1", SortNum: 0, CanPlay: 0},
		{FileName: "线路2", SortNum: 1, CanPlay: 1},
		{FileName: "线路3", SortNum: 2, CanPlay: 0},
	})

	if picked := SelectLiveChannel(info, 0); picked == nil || picked.FileName != "线路2" {
		t.Fatalf("expected 线路2 as first playable line, got %+v", picked)
	}

	if SelectLiveChannel(liveInfo(nil), 0) != nil {
		t.Fatal("no live lines must return nil")
	}

	if SelectLiveChannel(liveInfo([]fnapi.LiveChannel{{FileName: "线路1", CanPlay: 0}}), 0) != nil {
		t.Fatal("no playable line must return nil")
	}
}

// 实测分片是绝对地址（spec 2.4），而代理开关只对经过 proxy 的流量生效（spec 4.5），
// 所以绝对地址也必须改写回 proxy 入口，不能原样放行。
func TestRewriteHlsPlaylistRoutesEverySegmentBackThroughProxy(t *testing.T) {
	entry, err := url.Parse("http://127.0.0.1:22345/api/v1/live/g?session=abc")
	if err != nil {
		t.Fatalf("parse entry: %v", err)
	}
	playlist, err := url.Parse("https://iptv.example/u/token/relay/ysp.m3u8?tok=x")
	if err != nil {
		t.Fatalf("parse playlist: %v", err)
	}

	body := "#EXTM3U\n#EXTINF:3,\nseg.ts\nhttps://cdn.example/other.ts?tok=x\n#EXT-X-ENDLIST\n"
	out := RewriteHlsPlaylist(body, entry, playlist)

	seen := 0
	for _, line := range strings.Split(out, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		seen++
		rewritten, err := url.Parse(line)
		if err != nil {
			t.Fatalf("rewritten line is not a URL: %s", line)
		}
		if rewritten.Host != "127.0.0.1:22345" || rewritten.Path != "/api/v1/live/g" {
			t.Fatalf("segment does not come back through the proxy entry: %s", line)
		}
		if rewritten.Query().Get("session") != "abc" {
			t.Fatalf("session lost on segment URL: %s", line)
		}

		upstream := rewritten.Query().Get("u")
		if upstream == "" {
			t.Fatalf("upstream address lost on segment URL: %s", line)
		}
		resolved, err := ResolveLiveTarget(playlist, upstream, "")
		if err != nil {
			t.Fatalf("round trip failed for %s: %v", upstream, err)
		}
		if resolved.String() != upstream {
			t.Fatalf("round trip changed the target: %s -> %s", upstream, resolved)
		}
	}

	if seen != 2 {
		t.Fatalf("expected 2 rewritten URI lines, got %d: %s", seen, out)
	}
	if !strings.Contains(out, "#EXTINF:3,") || !strings.Contains(out, "#EXT-X-ENDLIST") {
		t.Fatalf("comment line changed: %s", out)
	}

	lines := strings.Split(out, "\n")
	relativeLine, err := url.Parse(lines[2])
	if err != nil || relativeLine.Query().Get("u") != "https://iptv.example/u/token/relay/seg.ts?tok=x" {
		t.Fatalf("relative URI not resolved against the playlist directory: %s", out)
	}
	absoluteLine, err := url.Parse(lines[3])
	if err != nil || absoluteLine.Query().Get("u") != "https://cdn.example/other.ts?tok=x" {
		t.Fatalf("absolute URI not carried back: %s", out)
	}
}

func TestResolveLiveTargetPrefersTheCarriedAbsoluteAddress(t *testing.T) {
	playlist, err := url.Parse("https://iptv.example/u/token/relay/ysp.m3u8?tok=x")
	if err != nil {
		t.Fatalf("parse playlist: %v", err)
	}

	target, err := ResolveLiveTarget(playlist, "https://cdn.example/other.ts?tok=x", "")
	if err != nil {
		t.Fatalf("absolute target rejected: %v", err)
	}
	if target.String() != "https://cdn.example/other.ts?tok=x" {
		t.Fatalf("unexpected target: %s", target)
	}

	target, err = ResolveLiveTarget(playlist, "", "seg.ts")
	if err != nil {
		t.Fatalf("relative target rejected: %v", err)
	}
	if target.String() != "https://iptv.example/u/token/relay/seg.ts?tok=x" {
		t.Fatalf("relative resolution wrong: %s", target)
	}

	if _, err := ResolveLiveTarget(playlist, "//evil.example/a.ts", ""); err == nil {
		t.Fatal("scheme-less value must be rejected")
	}
}

// 改写后 body 长度变了，回包不能沿用上游的 Content-Length；分片请求要真的回上游。
func TestServeLiveReportsTheRewrittenBodyLength(t *testing.T) {
	gin.SetMode(gin.TestMode)

	playlistHits := 0
	var playlistURL string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/play/info"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"success":true,"data":{"guid":"item-guid","type":"LiveChannel","item":{"title":"CCTV6电影"},"live_channels":[{"guid":"channel-guid","path":"%s","file_name":"线路1","type":3,"sort_num":0,"can_play":1,"play_error":""}]}}`, playlistURL)
		case strings.HasSuffix(r.URL.Path, ".ts"):
			w.Header().Set("Content-Type", "video/mp2t")
			_, _ = w.Write([]byte("segment-bytes"))
		default:
			playlistHits++
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.Header().Set("Content-Encoding", "identity")
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:3,\nseg.ts\n"))
		}
	}))
	defer upstream.Close()

	playlistURL = upstream.URL + "/relay/ysp.m3u8?tok=x"

	store := NewPlaybackSessionStore()
	sessionID, err := store.Create(PlaybackSessionRequest{
		Token:     "nas-token",
		Account:   "user",
		Domain:    upstream.URL,
		ItemGuids: []string{"item-guid"},
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	router := gin.New()
	router.GET("/api/v1/live/:itemGuid", func(c *gin.Context) { ServeLiveHandler(c, store) })

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/live/item-guid?session="+sessionID, nil))

	body := recorder.Body.String()
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", recorder.Code, body)
	}
	if got := recorder.Header().Get("Content-Length"); got != strconv.Itoa(len(body)) {
		t.Fatalf("Content-Length %s does not match rewritten body (%d): %s", got, len(body), body)
	}
	if recorder.Header().Get("Content-Encoding") != "" {
		t.Fatalf("stale Content-Encoding copied through: %s", recorder.Header().Get("Content-Encoding"))
	}
	if !strings.Contains(body, "/api/v1/live/item-guid?") {
		t.Fatalf("rewrite did not point back at the proxy entry: %s", body)
	}
	if playlistHits == 0 {
		t.Fatal("playlist was not fetched from the upstream line")
	}

	segment := url.QueryEscape(upstream.URL + "/segment.ts")
	segmentRecorder := httptest.NewRecorder()
	router.ServeHTTP(segmentRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/live/item-guid?session="+sessionID+"&u="+segment, nil))
	if segmentRecorder.Code != http.StatusOK || !strings.Contains(segmentRecorder.Body.String(), "segment-bytes") {
		t.Fatalf("segment request did not reach upstream: %d %s", segmentRecorder.Code, segmentRecorder.Body.String())
	}
}

// spec 4.5：直播路径要在点播选流之前，play/list 失败的直播项也要按直播处理；
// 但点播有视频流时不能多一次 play/info 调用。
func TestPlayVideoHandlerTriesLiveBeforeSelectingVideoStreams(t *testing.T) {
	source, err := os.ReadFile("playvideo.go")
	if err != nil {
		t.Fatalf("read playvideo.go: %v", err)
	}
	text := string(source)

	if !strings.Contains(text, "func liveInfoFor") {
		t.Fatal("playvideo.go must keep the live lookup in one helper")
	}

	liveAt := strings.Index(text, "liveInfoFor(fnApi, params.ItemGuid)")
	streamAt := strings.Index(text, "videoStreams := resp.Data.VideoStreams")
	if liveAt < 0 || streamAt < 0 {
		t.Fatalf("live path or video stream selection missing: %s", text)
	}
	if liveAt > streamAt {
		t.Fatal("live path runs after video stream selection")
	}

	if strings.Count(text, "liveInfoFor(fnApi, params.ItemGuid)") != 2 {
		t.Fatal("both the failed play/list path and the empty stream path must try the live branch")
	}
	if strings.Count(text, "GetPlayInfoCached") != 1 {
		t.Fatal("the play/info call must live only in the helper so vod pays no extra call")
	}
}
