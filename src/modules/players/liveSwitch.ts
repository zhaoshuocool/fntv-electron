import { BasePlayer, LiveSwitcher } from './types';

/** 只有实现了换列表能力的播放器才走直播切换，点播仍按原逻辑忽略重复请求。 */
export function hasLiveSwitch(player: BasePlayer | null): player is BasePlayer & LiveSwitcher {
    return player !== null && typeof (player as BasePlayer & LiveSwitcher).switchPlaylist === 'function';
}
