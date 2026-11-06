const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
// 公共控件属主在 pkg/view/ui；本测试读取真实源，不复制到旧位置。
const UI_SRC = path.join(__dirname, '../../../../..', 'pkg/view/ui/assets/js/ui');

const source = fs.readFileSync(path.join(UI_SRC, 'drawer.js'), 'utf8');

function setup() {
    const listeners = {}, events = [], calls = [];
    let focused = null, body;
    const makeNode = (attrs = {}) => ({
        attrs, hidden: false, inert: false, isConnected: true, children: [],
        getAttribute(k) { return this.attrs[k] ?? null; },
        hasAttribute(k) { return Object.hasOwn(this.attrs, k); },
        setAttribute(k, v) { this.attrs[k] = v; },
        removeAttribute(k) { delete this.attrs[k]; },
        matches() { return false; },
        closest(selector) { return selector === '[data-drawer-close]' && this.close ? this : null; },
        focus() { focused = this; calls.push('focus'); },
        querySelectorAll() { return []; },
        contains(el) { return this.children.includes(el); },
    });
    function makeBody(attrs = { id: 'drawer-body' }) {
        const node = makeNode(attrs);
        node.cloneNode = () => makeBody({ ...node.attrs });
        node.replaceWith = next => { node.isConnected = false; body = next; };
        node.appendChild = child => node.children.push(child);
        return node;
    }
    body = makeBody();
    const title = makeNode(), drawer = makeNode(), mask = makeNode(), status = makeNode();
    drawer.hidden = true;
    drawer.querySelector = sel => ({ '[data-drawer-body]': body, '[data-drawer-title]': title, '[data-write-status]': status })[sel] || null;
    status.replaceChildren = () => { status.children = []; };
    mask.hidden = true;
    const trigger = makeNode({ 'data-drawer-open': '#tpl-a', 'data-drawer-title': '编辑' });
    trigger.closest = sel => sel === '[data-drawer-open]' ? trigger : null;
    const background = makeNode(), alreadyInert = makeNode();
    alreadyInert.inert = true;
    drawer.parentElement = { children: [trigger, background, alreadyInert, drawer, mask] };
    const template = { content: { cloneNode: () => makeNode() } };
    const document = {
        activeElement: trigger,
        body: { classList: { add() {}, remove() {} } },
        querySelector(sel) { return ({ '[data-drawer]': drawer, '[data-drawer-mask]': mask, '#tpl-a': template })[sel] || null; },
        addEventListener(name, fn) { (listeners[name] ||= []).push(fn); },
        dispatchEvent(event) {
            events.push(event);
            calls.push(event.type === 'wb:rescan' ? 'process' : event.type);
            for (const fn of listeners[event.type] || []) fn(event);
        },
    };
    const marked = new WeakSet();
    const WBUI = { markOnce(el) { if (marked.has(el)) return false; marked.add(el); return true; },
        register(fn) { this.init = fn; }, scan() { calls.push('scan'); } };
    vm.runInNewContext(source, { window: { WBUI }, document,
        fetch() { throw new Error('抽屉控件不得发送网络请求'); },
        CustomEvent: class { constructor(type, options) { this.type = type; this.detail = options?.detail; } } });
    WBUI.init(document);
    const click = node => { for (const fn of listeners.click) fn({ target: node, preventDefault() {} }); };
    const close = () => click(Object.assign(makeNode(), { close: true }));
    return { get body() { return body; }, drawer, trigger, title, background, alreadyInert, status, calls, events,
        click, close, document, WBUI, get focused() { return focused; } };
}

test('本地模板先交扫描桥、增强控件、广播打开，最后聚焦', () => {
    const s = setup(), oldBody = s.body;
    s.click(s.trigger);
    assert.equal(oldBody.isConnected, false);
    assert.equal(s.body.children.length, 1);
    assert.equal(s.body.getAttribute('id'), 'drawer-body');
    assert.equal(s.title.textContent, '编辑');
    assert.deepEqual(s.calls, ['process', 'scan', 'wbui:drawer-open', 'focus']);
    assert.equal(s.events.find(e => e.type === 'wbui:drawer-open').detail.template, '#tpl-a');
});

test('关闭移除本轮目标并恢复焦点和自身设置的 inert，重开采用新实例', () => {
    const s = setup();
    s.click(s.trigger);
    const oldBody = s.body;
    assert.equal(s.background.inert, true);
    s.close();
    assert.equal(oldBody.isConnected, false);
    assert.equal(s.drawer.hidden, true);
    assert.equal(s.body.children.length, 0);
    assert.equal(s.background.inert, false);
    assert.equal(s.alreadyInert.inert, true);
    assert.equal(s.focused, s.trigger);
    s.click(s.trigger);
    assert.equal(s.drawer.hidden, false);
    assert.notEqual(s.body, oldBody);
    assert.equal(s.body.children.length, 1);
});

test('远程打开只准备新目标，内容就绪由适配层通知，旧目标通知不激活', () => {
    const s = setup();
    s.trigger.attrs['hx-get'] = '/form';
    s.click(s.trigger);
    assert.equal(s.drawer.hidden, false);
    assert.equal(s.body.children.length, 0);
    assert.deepEqual(s.calls, ['focus']);
    const oldBody = s.body;
    s.close();
    s.click(s.trigger);
    s.document.dispatchEvent({ type: 'wb:content-ready', detail: { el: oldBody } });
    assert.equal(s.events.filter(e => e.type === 'wbui:drawer-open').length, 0);
    s.document.dispatchEvent({ type: 'wb:content-ready', detail: { el: s.body } });
    assert.equal(s.events.filter(e => e.type === 'wbui:drawer-open').length, 1);
    assert.equal(s.events.at(-1).detail.template, null);
});

test('重复扫描不重复注册页面级监听，空错误区在打开时清理', () => {
    const s = setup();
    s.WBUI.init(s.document);
    s.status.children = [{}];
    s.click(s.trigger);
    assert.equal(s.events.filter(e => e.type === 'wbui:drawer-open').length, 1);
    assert.equal(s.status.children.length, 0);
    assert.equal(s.status.hidden, true);
});
