const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

function setup(body, response) {
    const listeners = {};
    const calls = [];
    const nav = [];
    const attrs = {'data-write-api': '/api/membership/tier/create', 'data-write-mode': 'create',
        'data-write-builder': 'membership-tier', 'data-write-number': 'sortOrder',
        'data-write-bool': 'isDefault', 'data-membership-create': 'true',
        'data-membership-shipping-kind': 'free_shipping', 'data-membership-discount-kind': 'discount',
        'data-write-nav': '/admin/membership?project=p-1', 'data-msg-failed': 'FAILED'};
    const fields = Object.entries(body).map(([name, value]) => ({
        tagName: 'INPUT', type: name === 'freeShipping' ? 'checkbox' : 'text', checked: value === '1',
        value, getAttribute(key) { return key === 'name' ? name : null; }
    }));
    const status = {children: [], removeAttribute() {}, setAttribute() {}, appendChild(el) { this.children.push(el); }};
    const button = {disabled: false, getAttribute() { return null; }, hasAttribute() { return false; }};
    const form = {
        getAttribute(key) { return attrs[key] ?? null; }, hasAttribute(key) { return Object.hasOwn(attrs, key); },
        setAttribute(key, value) { attrs[key] = value; }, removeAttribute(key) { delete attrs[key]; },
        querySelectorAll(selector) { return selector === '[name]' ? fields : []; },
        querySelector(selector) {
            if (selector === '[data-write-status]') { return status; }
            if (selector.startsWith('button')) { return button; }
            const m = selector.match(/^\[name="([^"]+)"\]$/);
            return m ? fields.find(el => el.getAttribute('name') === m[1]) : null;
        }
    };
    const document = {
        addEventListener(name, fn) { listeners[name] = fn; },
        querySelector() { return {getAttribute(name) { return name === 'content' ? 'CSRF' : null; }}; },
        createElement() { return {textContent: ''}; }
    };
    const window = {WBUI: {register() {}},
        location: {assign(url) { nav.push(url); }, reload() { nav.push('reload'); }},
        fetch(url, options) { calls.push({url, options}); return Promise.resolve({ok: response.code < 400, text: () => Promise.resolve(JSON.stringify(response))}); }};
    const context = vm.createContext({window, document});
    vm.runInContext(fs.readFileSync(__dirname + '/json_write.js', 'utf8'), context);
    vm.runInContext(fs.readFileSync(__dirname + '/membership_write.js', 'utf8'), context);
    function submit() { listeners.submit({target: form, submitter: button, preventDefault() {}}); }
    return {calls, nav, fields, status, button, submit};
}
const settle = () => new Promise(resolve => setImmediate(resolve));

test('real JSON write base sends converted membership DTO once with CSRF and navigates', async () => {
    const h = setup({projectId: 'p-1', name: 'Silver', thresholdYuan: '1,000.05', sortOrder: '-1', isDefault: '1', freeShipping: '1', discountPercent: '20'}, {code: 200, message: 'EN-CREATED', data: {id: 2}});
    h.submit(); h.submit();
    await settle();
    assert.equal(h.calls.length, 1);
    const call = h.calls[0];
    assert.equal(call.options.headers['X-CSRF-Token'], 'CSRF');
    assert.equal(call.options.credentials, 'same-origin');
    assert.deepEqual(JSON.parse(call.options.body), {projectId: 'p-1', name: 'Silver', sortOrder: -1, isDefault: true, thresholdAmount: 100005,
        entitlements: [{kind: 'free_shipping', valueInt: 1}, {kind: 'discount', valueInt: 20}]});
    assert.deepEqual(h.nav, ['/admin/membership?project=p-1']);
});

test('business failure keeps original amount and benefit input and releases submit lock', async () => {
    const h = setup({projectId: 'p-1', thresholdYuan: '1000.05', sortOrder: '0', discountPercent: '20'}, {code: 409, message: 'EN-NAME-TAKEN'});
    h.submit(); await settle();
    assert.equal(h.nav.length, 0);
    assert.equal(h.button.disabled, false);
    assert.equal(h.fields.find(el => el.getAttribute('name') === 'thresholdYuan').value, '1000.05');
    assert.equal(h.fields.find(el => el.getAttribute('name') === 'discountPercent').value, '20');
    assert.equal(h.status.children.at(-1).textContent, 'EN-NAME-TAKEN');
    h.submit(); await settle();
    assert.equal(h.calls.length, 2);
});
