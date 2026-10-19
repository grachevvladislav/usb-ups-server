const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const test = require('node:test');
const ui = path.join(__dirname, '../packaging/ui');
function application(fetch) {
    const scope = { SYNO: { namespace(name) { assert.equal(name, 'org.usbupsserver'); } },
        org: { usbupsserver: {} }, Vue: { extend(options) { return options; } }, fetch,
        setInterval() { throw Error('unexpected timer during render'); }, clearInterval() {}, URL, Blob };
    vm.runInNewContext(fs.readFileSync(path.join(ui, 'USBUPSServer.js'), 'utf8'), scope);
    const app = scope.org.usbupsserver.App;
    const instance = Object.assign(app.data(), { $set(target, key, value) { target[key] = value; } });
    for (const [name, method] of Object.entries(app.methods)) instance[name] = method.bind(instance);
    function render() {
        return app.render.call(instance, (tag, attributes, children) => {
            if (Array.isArray(attributes)) return { tag, attributes: {}, children: attributes };
            return { tag, attributes: attributes || {}, children: children || [] };
        });
    }
    return { instance, render };
}
function nodes(tree) {
    if (!tree || typeof tree !== 'object') return [];
    return [tree].concat((tree.children || []).flatMap(nodes));
}

test('native DSM views use Synology controls without frames or login fields', () => {
    const config = JSON.parse(fs.readFileSync(path.join(ui, 'config'), 'utf8'));
    assert.equal(config['.url'], undefined);
    const entry = config['USBUPSServer.js']['org.usbupsserver.App'];
    assert.equal(entry.type, 'app'); assert.equal(entry.appWindow, 'org.usbupsserver.App');
    assert.equal(entry.allowMultiInstance, false);
    const { instance, render } = application();
    instance.loaded = true; instance.session = { username: 'vlad' };
    instance.cfg = { enabled: true, driver: 'nutdrv_qx', port: 3493, poll_interval: 5 };
    instance.live = { ok: true, health: { enabled: true }, variables: { 'ups.status': 'OL', 'input.voltage': '228' } };
    const all = [];
    for (const tab of ['status', 'settings', 'logs']) {
        instance.tab = tab;
        const tree = render();
        assert.equal(tree.tag, 'v-app-instance');
        assert.equal(tree.attributes.props.className, entry.appWindow);
        assert.equal(tree.children[0].tag, 'v-app-window');
        assert.equal(tree.children[0].attributes.props.resizable, true);
        const view = nodes(tree);
        assert.ok(!view.some(n => ['iframe', 'frame', 'webview'].includes(n.tag)));
        assert.ok(!view.some(n => n.attributes.props && n.attributes.props.type === 'password'));
        all.push(...view);
    }
    for (const component of ['v-form', 'v-form-item', 'v-input', 'v-single-select', 'v-checkbox', 'v-button']) {
        assert.ok(all.some(n => n.tag === component), `native control ${component} is absent`);
    }
});

test('API uses only the DSM session and same-origin route', async () => {
    const calls = [];
    const { instance } = application(async (url, request) => {
        calls.push({ url, request });
        return { ok: true, json: async () => ({ username: 'vlad', dsm: true }) };
    });
    instance.synoToken = 'test-dsm-token';
    await instance.api('session');
    assert.equal(calls[0].url, '/webman/3rdparty/usb-ups-server/app/api/session');
    assert.equal(calls[0].request.credentials, 'same-origin');
    assert.equal(calls[0].request.headers['X-SYNO-TOKEN'], 'test-dsm-token');
    assert.equal(calls[0].request.headers.Authorization, undefined);
    instance.cfg = { enabled: true, driver: 'nutdrv_qx', subdriver: 'armac', vendor_id: '0925', product_id: '1234', serial: '', listen: '192.0.2.20', port: '3493', poll_interval: '5' };
    await instance.api('settings', instance.payload());
    const body = JSON.parse(calls[1].request.body);
    assert.equal(body.port, 3493); assert.equal(body.poll_interval, 5); assert.equal(body.enabled, true);
    assert.equal(calls[1].request.headers['X-UPS-Request'], '1');
});

test('initialization obtains DSM CSRF token before accessing the package', async () => {
    const calls = [];
    const { instance } = application(async (url, request) => {
        calls.push({ url, request });
        let data = {};
        if (url === '/webman/login.cgi') data = { SynoToken: 'current-session-token' };
        else {
            assert.equal(request.headers['X-SYNO-TOKEN'], 'current-session-token');
            if (url.endsWith('/session')) data = { username: 'vlad', dsm: true };
            if (url.endsWith('/settings')) data = { config: {}, drivers: [], subdrivers: [] };
            if (url.endsWith('/usb')) data = [];
            if (url.endsWith('/logs')) data = { text: '' };
        }
        return { ok: true, json: async () => data };
    });
    await instance.initialize();
    assert.equal(calls[0].url, '/webman/login.cgi');
    assert.equal(calls[0].request.credentials, 'same-origin');
    assert.equal(instance.loaded, true);
    assert.equal(instance.session.username, 'vlad');
});

test('expired DSM login does not access the package API', async () => {
    const calls = [];
    const { instance } = application(async (url) => {
        calls.push(url);
        return { ok: true, json: async () => ({ success: false }) };
    });
    await instance.initialize();
    assert.equal(instance.loaded, false);
    assert.match(instance.error, /Сессия DSM истекла/);
    assert.deepEqual(calls, ['/webman/login.cgi']);
});

test('USB selection is restored from saved IDs without rewriting the configuration', async () => {
    const { instance } = application();
    instance.cfg = { vendor_id: '0925', product_id: '1234', serial: '', driver: 'custom' };
    instance.devices = [{ vendor_id: '0925', product_id: '1234', serial: 'unreliable', path: '/dev/bus/usb/005/002' }];
    instance.syncUSBSelection();
    assert.equal(instance.selectedUSB, '0');
    assert.equal(instance.cfg.driver, 'custom');
    assert.equal(instance.cfg.serial, '');
    instance.devices.unshift({ vendor_id: 'abcd', product_id: '4321' });
    instance.syncUSBSelection();
    assert.equal(instance.selectedUSB, '1');
    instance.devices.push({ vendor_id: '0925', product_id: '1234', serial: 'other' });
    instance.syncUSBSelection();
    assert.equal(instance.selectedUSB, 'configured');
    instance.cfg.serial = 'other'; instance.syncUSBSelection();
    assert.equal(instance.selectedUSB, '2');
    instance.devices = []; instance.syncUSBSelection();
    assert.equal(instance.selectedUSB, 'configured');
});

test('window constrains size and scrolls only its content vertically without a username footer', () => {
    const { instance, render } = application();
    instance.loaded = true; instance.session = { username: 'vlad' };
    for (const tab of ['status', 'settings', 'logs']) {
        instance.tab = tab;
        const tree = render(), all = nodes(tree);
        assert.equal(tree.children[0].attributes.props.minWidth, 900);
        assert.equal(tree.children[0].attributes.props.minHeight, 580);
        const content = all.find(n => n.tag === 'section');
        assert.equal(content.attributes.style.overflowX, 'hidden');
        assert.equal(content.attributes.style.overflowY, 'auto');
        assert.equal(content.attributes.key, tab);
        const footers = all.filter(n => n.attributes.class === 'ups-footbar');
        assert.equal(footers.length, tab === 'settings' ? 1 : 0);
        assert.ok(!JSON.stringify(tree).includes('DSM: vlad'));
    }
});

test('missing USB permissions opens a specific command dialog and blocks starting monitoring', async () => {
    const { instance, render } = application(() => { throw Error('must not start NUT'); });
    instance.loaded = true; instance.tab = 'settings';
    instance.cfg = { vendor_id: '0925', product_id: '1234', enabled: true };
    instance.devices = [{ vendor_id: '0925', product_id: '1234', access: 'denied', name: 'RICHCOMM', path: '/dev/bus/usb/005/002' }];
    instance.syncUSBSelection();
    assert.match(instance.permissionHelp.command, /install-rule 0925 1234$/);
    assert.ok(nodes(render()).some(n => n.attributes.attrs && n.attributes.attrs.role === 'alertdialog'));
    await instance.apply();
    assert.equal(instance.busy, false);
    instance.devices[0].access = 'granted'; instance.syncUSBSelection();
    assert.equal(instance.permissionHelp, null);
});
