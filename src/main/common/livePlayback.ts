import { LiveChannel, PlayInfo } from '../../modules/fn_api/types';
import { PlayItem } from '../../modules/players/types';
import { isLivePlaylistItem } from '../../modules/players/liveItem';

/** 按 sort_num 升序返回线路副本，不改动 API 响应本身。 */
export function sortLiveChannels(channels: LiveChannel[]): LiveChannel[] {
    return [...channels].sort((a, b) => a.sort_num - b.sort_num);
}

/**
 * 一条线路一个播放项。索引按排序后的位置保留，这样跳过的线路不会把
 * 后面的线路挪到前面的索引上（Go proxy 的 sourceIndex 按同一顺序取值）。
 */
export function buildLivePlaylist(
    info: PlayInfo,
    makePlayLink: (sourceIndex: number) => string,
): PlayItem[] {
    return sortLiveChannels(info.live_channels ?? [])
        .map((channel, index) => ({ channel, index }))
        .filter(({ channel }) => channel.can_play === 1)
        .map(({ channel, index }) => ({
            itemGuid: info.guid,
            title: `${info.item.title} - ${channel.file_name}`,
            tvTitle: '',
            seasonNumber: 0,
            episodeNumber: 0,
            ts: 0,
            duration: 0,
            playLink: makePlayLink(index),
            live: true,
            sourceIndex: index,
        }));
}

/** 直播没有时长，写 play/record 会污染网页端的观看状态。 */
export function shouldRecordForGuid(playlist: PlayItem[], guid: string): boolean {
    const item = playlist.find((entry) => entry.itemGuid === guid);
    return item !== undefined && !isLivePlaylistItem(item);
}

/** sourceIndex 按线路在排序后列表中的位置取值，越界时回退到第一条可播放线路。 */
export function pickLiveEntry(playlist: PlayItem[], sourceIndex: number): PlayItem | null {
    if (playlist.length === 0) return null;
    const match = playlist.find((entry) => entry.sourceIndex === sourceIndex);
    return match ?? playlist[0];
}

/**
 * 系统代理查询要按直播上游线路的域名判断，不能拿 NAS 域名的结果代替：
 * 需要翻墙的是线路的服务器，不是飞牛本身。
 */
export function liveProxyTargetUrl(info: PlayInfo): string {
    const playable = sortLiveChannels(info.live_channels ?? []).find((channel) => channel.can_play === 1);
    return playable?.path ?? '';
}
