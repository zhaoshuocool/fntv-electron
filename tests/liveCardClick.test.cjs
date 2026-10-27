const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const { shouldTakeLiveCardClick } = require('../dest/preload/core/liveClick.js');

const GUID = '0123456789abcdef0123456789abcdef';
const LIVE_GUID = '901c57665c39857859ebe8a499a36eadb4b1d6423056a11341cb1d6b85ceac9d';

test('live card clicks are taken only for live routes', () => {
    assert.equal(shouldTakeLiveCardClick(`https://nas.local/v/live/${LIVE_GUID}`), true);
    assert.equal(shouldTakeLiveCardClick(`https://nas.local/v/movie/${GUID}`), false);
    assert.equal(shouldTakeLiveCardClick('https://nas.local/v/live'), false);
});

test('the click handler keeps using the live predicate for card links', () => {
    const compiled = fs.readFileSync(
        path.join(__dirname, '..', 'dest', 'preload', 'plugins', 'playMaskButton.js'),
        'utf8',
    );
    assert.match(compiled, /shouldTakeLiveCardClick/);
});
