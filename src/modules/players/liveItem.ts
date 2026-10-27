import { PlayItem } from './types';

/**
 * 直播播放项的统一判断：进度写回和字幕下载都以此为准，
 * 不再各自维护一份"是不是直播"的逻辑。
 */
export function isLivePlaylistItem(item: PlayItem | undefined): boolean {
    return item?.live === true;
}
