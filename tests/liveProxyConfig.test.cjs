const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const Module = require('node:module');

const { parseResolveProxyResult, resolveLiveProxyUrl } = require('../dest/main/common/systemProxy.js');

test('proxy resolve results are parsed or rejected without crashing', () => {
    assert.equal(parseResolveProxyResult('PROXY 127.0.0.1:7890'), 'http://127.0.0.1:7890');
    assert.equal(parseResolveProxyResult('SOCKS 127.0.0.1:1080'), 'socks5://127.0.0.1:1080');
    assert.equal(parseResolveProxyResult('DIRECT'), null);
    assert.equal(parseResolveProxyResult('PROXY not-a-host'), null);
    assert.equal(parseResolveProxyResult(''), null);
});

test('live proxy defaults are off and an explicit url wins over the system lookup', async () => {
    const testRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'fntv-liveproxy-'));
    const paths = { home: testRoot, userData: path.join(testRoot, '.fntv') };
    const electronMock = {
        app: { getPath: (name) => paths[name], setPath: (name, value) => { paths[name] = value; } },
        safeStorage: {
            isEncryptionAvailable: () => true,
            encryptString: (value) => Buffer.from(`protected:${value}`, 'utf8'),
            decryptString: (value) => value.toString('utf8').replace(/^protected:/, ''),
        },
    };

    const originalLoad = Module._load;
    Module._load = (request, parent, isMain) =>
        request === 'electron' ? electronMock : originalLoad.call(this, request, parent, isMain);

    try {
        const fnConfig = require('../dest/modules/fn_config/config.js');
        assert.deepEqual(fnConfig.getLiveProxyConfig(), { enabled: false, proxyUrl: '', followSystem: false });

        fnConfig.setLiveProxyConfig({ enabled: true, proxyUrl: 'http://127.0.0.1:7890', followSystem: true });
        assert.deepEqual(fnConfig.getLiveProxyConfig(), { enabled: true, proxyUrl: 'http://127.0.0.1:7890', followSystem: true });

        const resolved = await resolveLiveProxyUrl(
            fnConfig.getLiveProxyConfig(),
            'https://iptv.example/u/token/x.m3u8',
            async () => 'PROXY 127.0.0.1:7777',
        );
        assert.equal(resolved, 'http://127.0.0.1:7890');

        const systemOnly = await resolveLiveProxyUrl(
            { enabled: true, proxyUrl: '', followSystem: true },
            'https://iptv.example/u/token/x.m3u8',
            async () => 'SOCKS 127.0.0.1:1080',
        );
        assert.equal(systemOnly, 'socks5://127.0.0.1:1080');

        const off = await resolveLiveProxyUrl(
            { enabled: false, proxyUrl: 'http://127.0.0.1:7890', followSystem: true },
            'https://iptv.example/',
            async () => 'PROXY 127.0.0.1:7777',
        );
        assert.equal(off, null);
    } finally {
        Module._load = originalLoad;
        fs.rmSync(testRoot, { recursive: true, force: true });
    }
});

const { liveProxyTargetUrl } = require('../dest/main/common/livePlayback.js');

test('the system proxy lookup uses the live upstream line, not the NAS domain', () => {
    const info = {
        guid: 'g',
        item: { title: 'CCTV6电影' },
        live_channels: [
            { guid: 'g1', path: 'https://nas.example/blocked.m3u8', file_name: '线路1', type: 3, sort_num: 0, can_play: 0, play_error: '' },
            { guid: 'g2', path: 'https://iptv.example/live.m3u8', file_name: '线路2', type: 3, sort_num: 1, can_play: 1, play_error: '' },
        ],
    };
    assert.equal(liveProxyTargetUrl(info), 'https://iptv.example/live.m3u8');
    assert.equal(liveProxyTargetUrl({ guid: 'g', item: { title: 't' }, live_channels: null }), '');
});

test('live sessions hand the resolved proxy url to the proxy session input', () => {
    const proxySession = fs.readFileSync(
        path.join(__dirname, '..', 'src', 'main', 'common', 'proxySession.ts'),
        'utf8',
    );
    assert.match(proxySession, /proxyUrl\?: string/);

    const mediaSource = fs.readFileSync(
        path.join(__dirname, '..', 'src', 'main', 'handlers', 'plugins', 'media.ts'),
        'utf8',
    );
    // 只有直播分支才解析代理，点播请求拿到的是空串（直连）
    assert.match(mediaSource, /isLive \? await resolveLiveProxyUrl\(/);

    const media = fs.readFileSync(
        path.join(__dirname, '..', 'dest', 'main', 'handlers', 'plugins', 'media.js'),
        'utf8',
    );
    assert.match(media, /resolveLiveProxyUrl/);
    assert.match(media, /proxyUrl: liveProxyUrl \?\? ''/);
    assert.match(media, /liveProxyTargetUrl\)\(response\.data\)/);
});

test('the live proxy settings are reachable over IPC with defaults kept off', () => {
    const handler = fs.readFileSync(
        path.join(__dirname, '..', 'dest', 'main', 'handlers', 'plugins', 'config.js'),
        'utf8',
    );
    assert.match(handler, /registerHandler\)\('get-live-proxy-config', handleGetLiveProxyConfig\)/);
    assert.match(handler, /registerHandler\)\('set-live-proxy-config', handleSetLiveProxyConfig\)/);
    assert.match(handler, /event\.reply\('live-proxy-info', fnConfig\.getLiveProxyConfig\(\)\)/);
    assert.match(handler, /enabled === true/, 'omitting enabled must not turn the switch on');
    assert.match(handler, /proxyUrl \|\| ''/, 'an empty url must stay empty, not inherit the download proxy default');

    const preload = fs.readFileSync(
        path.join(__dirname, '..', 'dest', 'preload', 'index.js'),
        'utf8',
    );
    for (const channel of ['get-live-proxy-config', 'set-live-proxy-config', 'live-proxy-info', 'live-proxy-set']) {
        assert.match(preload, new RegExp(`'${channel}'`), `preload allowlist missing ${channel}`);
    }
});

test('the settings panel exposes the live proxy switch without inheriting the download proxy defaults', () => {
    const loginPage = fs.readFileSync(
        path.join(__dirname, '..', 'resource', 'login', 'index.html'),
        'utf8',
    );

    assert.match(loginPage, /id="liveProxyEnabledSwitch"/);
    assert.match(loginPage, /id="liveProxyUrlInput"/);
    assert.match(loginPage, /id="liveProxyFollowSystemSwitch"/);

    assert.match(loginPage, /ipcRenderer\.send\('get-live-proxy-config'\)/);
    assert.match(loginPage, /ipcRenderer\.send\('set-live-proxy-config', \{\s*enabled: liveProxyEnabledSwitch\.checked,\s*proxyUrl: liveProxyUrlInput\.value\.trim\(\),\s*followSystem: liveProxyFollowSystemSwitch\.checked\s*\}\)/);

    // 面板显示的必须是保存过的值，默认关；空地址不能套用下载代理的 ghfast 默认
    assert.match(loginPage, /liveProxyEnabledSwitch\.checked = data\.enabled === true/);
    assert.match(loginPage, /liveProxyUrlInput\.value = data\.proxyUrl \|\| ''/);
    assert.match(loginPage, /liveProxyFollowSystemSwitch\.checked = data\.followSystem === true/);
    assert.doesNotMatch(loginPage, /set-live-proxy-config[\s\S]{0,200}ghfast/);
});
