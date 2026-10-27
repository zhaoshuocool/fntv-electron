import { session } from 'electron';

const HOST_PORT = /^[^\s]+:\d+$/;

/**
 * Electron 的 resolveProxy 返回 "PROXY host:port" / "SOCKS host:port" / "DIRECT"。
 * Go 的 http.Transport 只认 http/https 代理，socks 只能走 DialContext，所以这里给出协议前缀。
 */
export function parseResolveProxyResult(result: string): string | null {
    const trimmed = (result ?? '').trim();
    if (!trimmed || trimmed.toUpperCase() === 'DIRECT') return null;

    const [method, host] = trimmed.split(/\s+/);
    if (!method || !host || !HOST_PORT.test(host)) return null;

    const upper = method.toUpperCase();
    if (upper === 'PROXY') return `http://${host}`;
    if (upper === 'SOCKS') return `socks5://${host}`;
    return null;
}

export type ProxyResolver = (url: string) => Promise<string>;

/**
 * 开关打开时才生效：显式填的地址优先，没填才查系统代理。
 */
export async function resolveLiveProxyUrl(
    config: { enabled: boolean; proxyUrl: string; followSystem: boolean },
    targetUrl: string,
    resolve: ProxyResolver = (url) => session.defaultSession.resolveProxy(url),
): Promise<string | null> {
    if (!config.enabled) return null;
    if (config.proxyUrl) return config.proxyUrl;
    if (!config.followSystem) return null;

    try {
        return parseResolveProxyResult(await resolve(targetUrl));
    } catch {
        return null;
    }
}
