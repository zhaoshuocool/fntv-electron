const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const { hasLiveSwitch } = require('../dest/modules/players/liveSwitch.js');

test('only a player that can swap its playlist counts as a live switcher', () => {
    assert.equal(hasLiveSwitch({ isPlaying: () => true, switchPlaylist: async () => true }), true);
    assert.equal(hasLiveSwitch({ isPlaying: () => true }), false);
    assert.equal(hasLiveSwitch(null), false);
});

test('mpv switches the live playlist on the running instance and skips live subtitles', () => {
    const compiled = fs.readFileSync(
        path.join(__dirname, '..', 'dest', 'modules', 'players', 'impl', 'mpv.js'),
        'utf8',
    );
    assert.match(compiled, /switchPlaylist\(/);
    assert.match(compiled, /isLivePlaylistItem/);
});

test('the play handler switches a live channel instead of ignoring the request', () => {
    const compiled = fs.readFileSync(
        path.join(__dirname, '..', 'dest', 'main', 'handlers', 'plugins', 'media.js'),
        'utf8',
    );
    assert.match(compiled, /hasLiveSwitch/);
});
