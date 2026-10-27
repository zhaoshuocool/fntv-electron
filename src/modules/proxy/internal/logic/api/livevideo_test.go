package api

import (
	"net/url"
	"strings"
	"testing"

	"proxy/pkg/fnapi"
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

func TestRewriteHlsPlaylistLeavesAbsoluteUrlsAndCommentsAlone(t *testing.T) {
	base, err := url.Parse("https://iptv.example/u/token/relay/x.m3u8")
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}

	body := "#EXTM3U\n#EXTINF:3,\nseg.ts\n#EXT-X-ENDLIST\n"
	out := RewriteHlsPlaylist(body, base)

	if !strings.Contains(out, "https://iptv.example/u/token/relay/seg.ts") {
		t.Fatalf("relative URI not rewritten: %s", out)
	}
	if !strings.Contains(out, "#EXTINF:3,") || !strings.Contains(out, "#EXT-X-ENDLIST") {
		t.Fatalf("comment line changed: %s", out)
	}

	absolute := "#EXTINF:3,\nhttps://cdn.example/seg.ts\n"
	if out = RewriteHlsPlaylist(absolute, base); strings.Contains(out, base.String()) {
		t.Fatalf("absolute URI rewritten: %s", out)
	}
}
