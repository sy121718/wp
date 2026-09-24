const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const source = fs.readFileSync(__dirname + '/drawer.js', 'utf8');

function setup() {
    const listeners = {}, events = [], calls = [], requests = [];
    let focused = null;
    const makeNode = (attrs = {}) => ({
        attrs, hidden: false, inert: false, isConnected: true, children: [],
        getAttribute(k) { return this.attrs[k] || null; },
        setAttribute(k, v) { this.attrs[k] = v; },
        removeAttribute(k) { delete this.attrs[k]; },
        matches() { return false; },
        closest(selector) { return selector === '[data-drawer-close]' && this.close ? this : null; },
        focus() { focused = this; calls.push('focus'); },
        querySelectorAll() { return []; },
        attributes: [],
        getClientRects() { return [1]; },
        contains(el) { return this.children.includes(el); },
    });
    const body = makeNode();
    Object.defineProperty(body, 'innerHTML', { get() { return this.html || ''; }, set(value) { this.html = value; this.children = []; } });
    body.innerHTML = '';
    body.querySelector = (sel) => sel === '[data-drawer-retry]' ? body.children[0]?.children[0] || null : null;
    body.appendChild = (node) => { body.children.push(node); body.html = 'content'; };
    const title = makeNode(), drawer = makeNode(), mask = makeNode();
    drawer.hidden = true;
    drawer.querySelector = (sel) => sel === '[data-drawer-body]' ? body : title;
    drawer.parentElement = { children: [drawer] };
    mask.hidden = true;
    const trigger = makeNode({ 'data-drawer-open': '#tpl-a', 'data-drawer-title': 'Edit' });
    trigger.closest = (sel) => sel.includes('[data-drawer-open]') ? trigger : null;
    const template = { content: { cloneNode() { return makeNode(); } } };
    const document = {
        activeElement: trigger,
        body: { classList: { add() {}, remove() {} } },
        querySelector(sel) { return ({ '[data-drawer]': drawer, '[data-drawer-mask]': mask, '#tpl-a': template })[sel] || null; },
        addEventListener(name, fn) { listeners[name] = fn; },
        dispatchEvent(event) { events.push(event); calls.push(event.type); },
        createElement(tag) {
            const el = makeNode();
            el.tagName = tag.toUpperCase();
            el.textContent = '';
            el.appendChild = (node) => el.children.push(node);
            return el;
        },
    };
    const WBUI = { controls: [], markOnce() { return true; }, register(fn) { this.init = fn; }, scan() { calls.push('scan'); } };
    const fetch = (url, options) => new Promise((resolve, reject) => requests.push({ url, options, resolve, reject }));
    class AbortController {
        constructor() { this.signal = { aborted: false }; }
        abort() { this.signal.aborted = true; }
    }
    class DOMParser {
        parseFromString(html) {
            const match = html.match(/^\s*<(div|form)([^>]*)>/i);
            const root = makeNode();
            root.tagName = match?.[1].toUpperCase();
            root.attrs['data-drawer-fragment'] = match?.[2].includes('data-drawer-fragment') ? '' : null;
            root.hasAttribute = (name) => name === 'data-drawer-fragment' && root.attrs[name] !== null;
            const script = makeNode();
            script.tagName = 'SCRIPT';
            root.attributes = [...html.matchAll(/\s(hx-post|action|onclick)="([^"]*)"/gi)]
                .map((match) => ({ name: match[1], value: match[2] }));
            root.matches = (sel) => sel === 'form' ? root.tagName === 'FORM' : root.hasAttribute('data-drawer-fragment');
            root.querySelector = (sel) => sel === 'form' && html.includes('<form') ? makeNode() : null;
            root.querySelectorAll = () => html.includes('<script') ? [script] : [];
            return { body: { children: match ? [root] : [], textContent: '' } };
        }
    }
    const window = { WBUI, htmx: { process() { calls.push('process'); } }, location: { href: 'https://example.test/admin' } };
    vm.runInNewContext(source, { window, document, fetch, AbortController, DOMParser, URL,
        CustomEvent: class { constructor(type, detail) { this.type = type; this.detail = detail?.detail; } } });
    WBUI.init(document);
    const click = (node) => listeners.click({ target: node, preventDefault() {} });
    const open = (url) => { trigger.attrs['data-drawer-url'] = url; click(trigger); };
    const close = () => click(Object.assign(makeNode(), { close: true }));
    return { body, drawer, trigger, calls, events, requests, open, close, click, window, get focused() { return focused; } };
}

const tick = () => new Promise((resolve) => setImmediate(resolve));
const response = (html, status = 200, type = 'text/html') => ({
    status, ok: status >= 200 && status < 300,
    headers: { get() { return type; } }, text: async () => html,
});

test('legacy template clone preserves processing, scan, event, focus order', () => {
    const s = setup();
    s.click(s.trigger);
    assert.equal(s.body.innerHTML, 'content');
    assert.deepEqual(s.calls, ['process', 'scan', 'wbui:drawer-open', 'focus']);
    assert.equal(s.events[0].detail.template, '#tpl-a');
});

test('async open displays busy state then processes valid form in order', async () => {
    const s = setup();
    s.open('/admin/form');
    assert.equal(s.drawer.hidden, false);
    assert.equal(s.body.getAttribute('aria-busy'), 'true');
    assert.equal(s.body.children[0].getAttribute('role'), 'status');
    assert.equal(s.requests[0].options.credentials, 'same-origin');
    assert.equal(s.requests[0].options.cache, 'no-store');
    s.requests[0].resolve(response('<div data-drawer-fragment><form method="post"></form></div>'));
    await tick();
    assert.equal(s.body.getAttribute('aria-busy'), null);
    assert.deepEqual(s.calls.slice(-4), ['process', 'scan', 'wbui:drawer-open', 'focus']);
});

test('invalid response shows retry and never processes markup', async () => {
    const s = setup();
    s.open('/admin/form');
    s.requests[0].resolve(response('<script>alert(1)</script><form></form>'));
    await tick();
    assert.equal(s.body.children[0].getAttribute('role'), 'alert');
    assert.equal(s.calls.includes('process'), false);
    const retry = s.body.children[0].children[0];
    retry.closest = (sel) => sel === '[data-drawer-retry]' ? retry : null;
    s.click(retry);
    assert.equal(s.requests.length, 2);
    assert.equal(s.requests[1].options.signal.aborted, false);
});

test('rejects active attributes and cross-origin htmx targets', async () => {
    const s = setup();
    for (const html of [
        '<form data-drawer-fragment onclick="alert(1)"></form>',
        '<form data-drawer-fragment hx-post="https://attacker.test/submit"></form>',
        '<form data-drawer-fragment><script>alert(1)</script></form>',
    ]) {
        s.open('/form');
        s.requests.at(-1).resolve(response(html));
        await tick();
        assert.equal(s.body.children[0].getAttribute('role'), 'alert');
        assert.equal(s.calls.includes('process'), false);
    }
});

test('switch, close and retry reject obsolete responses even when abort is ignored', async () => {
    const s = setup();
    s.open('/a');
    s.open('/b');
    assert.equal(s.requests[0].options.signal.aborted, true);
    s.requests[1].resolve(response('<form data-drawer-fragment></form>'));
    await tick();
    const eventCount = s.events.length;
    s.requests[0].resolve(response('<form data-drawer-fragment></form>'));
    await tick();
    assert.equal(s.events.length, eventCount);
    s.open('/c');
    s.close();
    assert.equal(s.requests[2].options.signal.aborted, true);
    s.requests[2].resolve(response('<form data-drawer-fragment></form>'));
    await tick();
    assert.equal(s.drawer.hidden, true);
    assert.equal(s.events.filter(e => e.type === 'wbui:drawer-open').length, 1);
});

test('rejects cross-origin URL, non-200, non-HTML, and missing form', async () => {
    const s = setup();
    s.open('https://attacker.test/form');
    assert.equal(s.requests.length, 0);
    for (const result of [response('<form data-drawer-fragment></form>', 204), response('<form data-drawer-fragment></form>', 200, 'application/json'), response('<p>empty</p>'), response('<html><body><form data-drawer-fragment></form></body></html>'), response('<form></form>')]) {
        s.open('/next');
        s.requests.at(-1).resolve(result);
        await tick();
        assert.equal(s.body.children[0].getAttribute('role'), 'alert');
    }
});
