const test = require('node:test');
const assert = require('node:assert/strict');
const { extractItemGuidFromUrl, isItemGuid, isLiveRouteUrl } = require('../dest/preload/core/playTarget.js');

const GUID = '0123456789abcdef0123456789abcdef';

test('extracts item guid from legacy routes', () => {
    assert.equal(extractItemGuidFromUrl(`https://nas.local/v/movie/${GUID}`), GUID);
    assert.equal(extractItemGuidFromUrl(`https://nas.local/v/tv/episode/${GUID}?tab=info`), GUID);
    assert.equal(extractItemGuidFromUrl(`https://nas.local/v/other/${GUID}/`), GUID);
});

test('extracts item guid from current generic and hash routes', () => {
    assert.equal(extractItemGuidFromUrl(`https://nas.local/v/detail/${GUID}`), GUID);
    assert.equal(extractItemGuidFromUrl(`https://nas.local/#/v/video/${GUID}`), GUID);
    assert.equal(extractItemGuidFromUrl(`https://nas.local/player?item_guid=${GUID}`), GUID);
});

test('supports prefixed and UUID identifiers without accepting unrelated pages', () => {
    const prefixed = `fv_${GUID}`;
    const uuid = '123e4567-e89b-42d3-a456-426614174000';
    assert.equal(extractItemGuidFromUrl(`https://nas.local/v/item/${prefixed}`), prefixed);
    assert.equal(extractItemGuidFromUrl(`https://nas.local/v/play/${uuid}`), uuid);
    assert.equal(extractItemGuidFromUrl('https://nas.local/v/library'), null);
    assert.equal(isItemGuid(prefixed), true);
    assert.equal(isItemGuid('library'), false);
});

test('accepts live route guids of the current length without binding to it', () => {
    const liveGuid = '901c57665c39857859ebe8a499a36eadb4b1d6423056a11341cb1d6b85ceac9d';
    assert.equal(extractItemGuidFromUrl(`https://nas.local/v/live/${liveGuid}`), liveGuid);
    const sha256 = 'a'.repeat(64);
    assert.equal(extractItemGuidFromUrl(`https://nas.local/v/live/${sha256}`), sha256);
    assert.equal(extractItemGuidFromUrl('https://nas.local/v/live/zzz'), null);
    assert.equal(isLiveRouteUrl('https://nas.local/v/live/' + liveGuid), true);
    assert.equal(isLiveRouteUrl('https://nas.local/v/movie/' + GUID), false);
});
