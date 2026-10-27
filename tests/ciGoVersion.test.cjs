const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');

function goVersion(value) {
    const parts = value.split('.').map(Number);
    return parts[0] * 100 + (parts[1] || 0);
}

// setup-go 把 GOTOOLCHAIN 钉成 local，所以工作流里的版本必须不低于 go.mod 的要求，
// 否则 CI 上 go build 直接报 "go.mod requires go >= x"。
test('the CI go toolchain can build the proxy module', () => {
    const workflow = fs.readFileSync(
        path.join(__dirname, '..', '.github', 'workflows', 'release.yml'),
        'utf8',
    );
    const module = fs.readFileSync(
        path.join(__dirname, '..', 'src', 'modules', 'proxy', 'go.mod'),
        'utf8',
    );

    const pinned = workflow.match(/go-version:\s*'(\d+\.\d+)/);
    const required = module.match(/^go (\d+\.\d+)/m);
    assert.ok(pinned, 'workflow must pin a go version');
    assert.ok(required, 'go.mod must state the required go version');

    const pinnedVersion = goVersion(pinned[1]);
    const requiredVersion = goVersion(required[1]);
    assert.ok(
        pinnedVersion >= requiredVersion,
        `workflow pins go ${pinned[1]} but the proxy module needs ${required[1]}`,
    );
});
