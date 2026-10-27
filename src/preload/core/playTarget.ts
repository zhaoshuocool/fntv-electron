const GUID_PATTERN = /^(?:[a-z][a-z0-9]*_)?[a-f0-9]{32,64}$/i;
const UUID_PATTERN = /^[a-f0-9]{8}-[a-f0-9]{4}-[1-5][a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$/i;
const ROUTE_MARKERS = new Set(['movie', 'episode', 'other', 'video', 'detail', 'item', 'play', 'live']);
const LIVE_ROUTE_MARKER = 'live';
const QUERY_KEYS = ['item_guid', 'itemGuid', 'guid', 'id'];

function safeDecode(value: string): string {
    try {
        return decodeURIComponent(value);
    } catch {
        return value;
    }
}

function normalizeCandidate(value: string | null | undefined): string | null {
    if (!value) return null;

    const candidate = safeDecode(value).trim();
    if (!candidate || candidate.length > 128 || /[/?#&=]/.test(candidate)) return null;
    return candidate;
}

function looksLikeIdentifier(value: string): boolean {
    return GUID_PATTERN.test(value) || UUID_PATTERN.test(value);
}

function extractFromPath(path: string): string | null {
    const segments = path
        .split('/')
        .map(normalizeCandidate)
        .filter((segment): segment is string => Boolean(segment));

    for (let index = segments.length - 2; index >= 0; index -= 1) {
        if (!ROUTE_MARKERS.has(segments[index].toLowerCase())) continue;
        const candidate = segments[index + 1];
        if (candidate && looksLikeIdentifier(candidate)) return candidate;
    }

    for (let index = segments.length - 1; index >= 0; index -= 1) {
        const candidate = segments[index];
        if (looksLikeIdentifier(candidate)) return candidate;
    }

    return null;
}

/**
 * 从飞牛影视的新旧详情页 URL 中提取播放项标识。
 * 路由结构决定哪一段是标识（所以 guid 长度变化不影响），
 * 但候选段本身必须像标识：32–64 位 hex 或 UUID。
 */
export function extractItemGuidFromUrl(value: string): string | null {
    try {
        const url = new URL(value, 'http://fntv.local');

        for (const key of QUERY_KEYS) {
            const candidate = normalizeCandidate(url.searchParams.get(key));
            if (candidate) return candidate;
        }

        const pathCandidate = extractFromPath(url.pathname);
        if (pathCandidate) return pathCandidate;

        const hash = url.hash.replace(/^#/, '');
        if (hash) {
            const hashUrl = new URL(hash.startsWith('/') ? hash : `/${hash}`, 'http://fntv.local');
            for (const key of QUERY_KEYS) {
                const candidate = normalizeCandidate(hashUrl.searchParams.get(key));
                if (candidate) return candidate;
            }
            return extractFromPath(hashUrl.pathname);
        }
    } catch {
        return extractFromPath(value);
    }

    return null;
}

function hasLiveSegment(path: string): boolean {
    return path
        .split('/')
        .map(normalizeCandidate)
        .some((segment) => segment !== null && segment.toLowerCase() === LIVE_ROUTE_MARKER);
}

/**
 * 直播详情页的路径标记（/live/<guid>）。只看路由，不依赖 guid 长度。
 */
export function isLiveRouteUrl(value: string): boolean {
    try {
        const url = new URL(value, 'http://fntv.local');
        if (hasLiveSegment(url.pathname)) return true;

        const hash = url.hash.replace(/^#/, '');
        if (!hash) return false;
        const hashUrl = new URL(hash.startsWith('/') ? hash : `/${hash}`, 'http://fntv.local');
        return hasLiveSegment(hashUrl.pathname);
    } catch {
        return hasLiveSegment(value);
    }
}

export function isItemGuid(value: string | null | undefined): value is string {
    const candidate = normalizeCandidate(value);
    return candidate !== null && (GUID_PATTERN.test(candidate) || UUID_PATTERN.test(candidate));
}
