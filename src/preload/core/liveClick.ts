import { extractItemGuidFromUrl, isLiveRouteUrl } from './playTarget';

/**
 * 直播频道卡片本身不是 button，播放按钮只在 hover 时出现。
 * 只有当卡片链接是直播路由（/live/<guid>）时，整卡点击才算播放；
 * 点播卡片行为保持不变。
 */
export function shouldTakeLiveCardClick(href: string): boolean {
    return isLiveRouteUrl(href) && extractItemGuidFromUrl(href) !== null;
}
