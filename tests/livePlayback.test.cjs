const test = require('node:test');
const assert = require('node:assert/strict');

const { sortLiveChannels } = require('../dest/main/common/livePlayback.js');
const { isLivePlaylistItem } = require('../dest/modules/players/liveItem.js');

const itemBase = {
    itemGuid: 'x',
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
