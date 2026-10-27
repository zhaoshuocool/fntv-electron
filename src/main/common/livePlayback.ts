import { LiveChannel } from '../../modules/fn_api/types';

/** 按 sort_num 升序返回线路副本，不改动 API 响应本身。 */
export function sortLiveChannels(channels: LiveChannel[]): LiveChannel[] {
    return [...channels].sort((a, b) => a.sort_num - b.sort_num);
}
