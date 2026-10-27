const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const {
    sortLiveChannels,
    buildLivePlaylist,
    shouldRecordForGuid,
    pickLiveEntry,
} = require('../dest/main/common/livePlayback.js');
const { isLivePlaylistItem } = require('../dest/modules/players/liveItem.js');

const CCTV6_GUID = '901c57665c39857859ebe8a499a36eadb4b1d6423056a11341cb1d6b85ceac9d';

const itemBase = {
    itemGuid: CCTV6_GUID,
    title: 't',
    tvTitle: '',
    seasonNumber: 0,
    episodeNumber: 0,
    ts: 0,
    duration: 0,
    playLink: '',
    live: false,
};

const line = (fileName, sortNum, canPlay = 1) => ({
    guid: 'g',
    path: `https://iptv.example/u/token/${fileName}`,
    file_name: fileName,
    type: 3,
    sort_num: sortNum,
    can_play: canPlay,
    play_error: '',
});

const info = { guid: CCTV6_GUID, item: { title: 'CCTV6电影' }, live_channels: [line('线路2', 1), line('线路1', 0)] };
const infoWithAllUnplayable = { guid: CCTV6_GUID, item: { title: 'CCTV6电影' }, live_channels: [line('线路1', 0, 0), line('线路2', 1, 0)] };
const infoWithoutLiveChannels = { guid: CCTV6_GUID, item: { title: 'CCTV6电影' }, live_channels: null };

test('sorts live lines by sort_num and keeps unplayable lines visible to the caller', () => {
    const input = [line('线路2', 1), line('线路1', 0), line('线路3', 2, 0)];
    const sorted = sortLiveChannels(input);
    assert.deepEqual(sorted.map((c) => c.file_name), ['线路1', '线路2', '线路3']);
    assert.deepEqual(input.map((c) => c.file_name), ['线路2', '线路1', '线路3'], 'must not mutate the response');
});

test('the live predicate is the only source of truth for skipping record and subtitles', () => {
    assert.equal(isLivePlaylistItem({ ...itemBase, live: true }), true);
    assert.equal(isLivePlaylistItem({ ...itemBase, live: false }), false);
    assert.equal(isLivePlaylistItem(undefined), false);
});

test('live playlist keeps line order and marks entries live', () => {
    const items = buildLivePlaylist(info, (index) => `p://x/${index}`);
    assert.deepEqual(items.map((i) => i.title), ['CCTV6电影 - 线路1', 'CCTV6电影 - 线路2']);
    assert.equal(items[0].playLink, 'p://x/0');
    assert.equal(items[0].itemGuid, CCTV6_GUID);
    assert.equal(items[0].live, true);
    assert.equal(items[0].duration, 0);
    assert.equal(items[0].sourceIndex, 0);
});

test('a channel with no playable line produces no playlist entries', () => {
    assert.deepEqual(buildLivePlaylist(infoWithAllUnplayable, (index) => `p://x/${index}`), []);
    assert.deepEqual(buildLivePlaylist(infoWithoutLiveChannels, (index) => `p://x/${index}`), []);
});

test('skipped lines keep their sorted position so the proxy source index still matches', () => {
    const mixed = {
        guid: CCTV6_GUID,
        item: { title: 'CCTV6电影' },
        live_channels: [line('线路1', 0, 0), line('线路2', 1), line('线路3', 2)],
    };
    const items = buildLivePlaylist(mixed, (index) => `p://x/${index}`);
    assert.deepEqual(items.map((i) => i.playLink), ['p://x/1', 'p://x/2']);
});

test('live entries never write playback records', () => {
    const playlist = buildLivePlaylist(info, (index) => `p://x/${index}`);
    assert.equal(shouldRecordForGuid(playlist, CCTV6_GUID), false);
    assert.equal(shouldRecordForGuid([{ ...itemBase, live: false }], CCTV6_GUID), true);
    assert.equal(shouldRecordForGuid(playlist, 'unknown'), false);
});

test('a live source index falls back to the first playable entry', () => {
    const playlist = buildLivePlaylist(info, (index) => `p://x/${index}`);
    assert.equal(pickLiveEntry(playlist, 1), playlist[1]);
    assert.equal(pickLiveEntry(playlist, 9), playlist[0]);
    assert.equal(pickLiveEntry([], 0), null);

    const mixed = {
        guid: CCTV6_GUID,
        item: { title: 'CCTV6电影' },
        live_channels: [line('线路1', 0, 0), line('线路2', 1), line('线路3', 2)],
    };
    const mixedPlaylist = buildLivePlaylist(mixed, (index) => `p://x/${index}`);
    assert.equal(pickLiveEntry(mixedPlaylist, 2), mixedPlaylist[1], 'skipped line keeps its sorted index');
});

test('the play handler builds a live playlist and gates records through the guard', () => {
    const compiled = fs.readFileSync(
        path.join(__dirname, '..', 'dest', 'main', 'handlers', 'plugins', 'media.js'),
        'utf8',
    );
    assert.match(compiled, /LiveChannel/);
    assert.match(compiled, /buildLivePlaylist/);
    assert.match(compiled, /shouldRecordForGuid/);
});
