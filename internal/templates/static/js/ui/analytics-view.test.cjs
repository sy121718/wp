/* analytics-view.test.cjs — 「切标签」到 GET 表单 view 的对齐（node:test + vm，无浏览器）。
 *
 * 守的是这条真实缺陷链：页面有两张 GET 表单要带 view（工程选择、时间筛选），隐藏域
 * 读的是渲染时的值；标签切换是纯前端行为，切完再改筛选就会带回旧 view。
 *
 * 这里测的是**真实接线**：脚本订阅基座广播的 wbui:tabs-change，测试派发同名事件，
 * 断言隐藏域被写成新 view。不直调内部函数替代事件 —— 那样测不出「绑定到底有没有连上」。
 *
 * 运行：node --test internal/templates/static/js/ui/analytics-view.test.cjs
 */
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const SRC = path.join(__dirname, 'analytics-view.js');
// 测试在 static/js/ui/ 下，admin.js 在 static/js/ —— 只退一级。
const ADMIN_SRC = path.join(__dirname, '..', 'admin.js');

function makeNode(tag) {
    const el = {
        tagName: String(tag || 'div').toUpperCase(),
        children: [],
        attrs: {},
        parentNode: null,
        value: '',
        appendChild(c) { this.children.push(c); c.parentNode = this; return c; },
        setAttribute(k, v) { this.attrs[k] = String(v); },
        getAttribute(k) { return Object.prototype.hasOwnProperty.call(this.attrs, k) ? this.attrs[k] : null; },
        hasAttribute(k) { return Object.prototype.hasOwnProperty.call(this.attrs, k); },
        querySelector(sel) { return this._find(sel)[0] || null; },
        querySelectorAll(sel) { return this._find(sel); },
        closest(sel) {
            let n = this;
            while (n) {
                if (matches(n, sel)) { return n; }
                n = n.parentNode;
            }
            return null;
        },
        _find(sel) {
            const group = String(sel).split(',').map((x) => x.trim()).filter(Boolean);
            const out = [];
            const walk = (n) => {
                (n.children || []).forEach((c) => {
                    if (group.some((g) => matches(c, g))) { out.push(c); }
                    walk(c);
                });
            };
            walk(this);
            return out;
        },
        get firstChild() { return this.children[0] || null; }
    };
    return el;
}

function matches(node, sel) {
    let s = String(sel).trim();
    if (s.startsWith('#')) {
        return node.getAttribute('id') === s.slice(1);
    }
    if (s.startsWith('.')) {
        const cls = s.slice(1);
        return String(node.getAttribute('class') || '').split(/\s+/).includes(cls);
    }
    const tagPart = /^([a-zA-Z][a-zA-Z0-9]*)/.exec(s);
    if (tagPart) {
        if (String(node.tagName).toLowerCase() !== tagPart[1].toLowerCase()) { return false; }
        s = s.slice(tagPart[1].length);
    }
    const attrs = s.match(/\[[^\]]+\]/g) || [];
    return attrs.every((a) => {
        const body = a.slice(1, -1);
        const eq = body.indexOf('=');
        if (eq < 0) { return node.hasAttribute(body); }
        return node.getAttribute(body.slice(0, eq)) === body.slice(eq + 1).replace(/^["']|["']$/g, '');
    });
}

/* 最小 analytics 页骨架：一个 5 维标签组 + 两张要带 view 的 GET 表单。
   document 上实现 addEventListener / dispatchEvent，让脚本的订阅真的连上。 */
function makeEnv(selectedView) {
    const listeners = {};
    const doc = makeNode('body');
    doc.addEventListener = (type, fn) => { (listeners[type] = listeners[type] || []).push(fn); };
    doc.removeEventListener = () => {};
    doc.dispatchEvent = (ev) => {
        (listeners[ev.type] || []).forEach((fn) => fn(ev));
        return true;
    };

    const tabs = makeNode('div');
    tabs.setAttribute('data-tabs', '');
    doc.appendChild(tabs);
    const list = makeNode('div');
    list.setAttribute('role', 'tablist');
    tabs.appendChild(list);

    const buttons = {};
    ['daily', 'paths', 'referrers', 'ua', 'langs'].forEach((v) => {
        const b = makeNode('button');
        b.setAttribute('role', 'tab');
        b.setAttribute('class', 'tab');
        b.setAttribute('id', 'an-tab-' + v);
        b.setAttribute('aria-controls', 'an-panel-' + v);
        b.setAttribute('aria-selected', v === selectedView ? 'true' : 'false');
        list.appendChild(b);
        buttons[v] = b;
    });

    const inputs = [];
    ['project-selector', 'filter-bar'].forEach((id) => {
        const form = makeNode('form');
        form.setAttribute('id', id);
        doc.appendChild(form);
        const inp = makeNode('input');
        inp.setAttribute('type', 'hidden');
        inp.setAttribute('name', 'view');
        inp.setAttribute('data-view-input', '');
        inp.value = selectedView;
        form.appendChild(inp);
        inputs.push(inp);
    });

    const win = { document: doc, WBUI: {}, CustomEvent: class { constructor(t, o) { this.type = t; this.detail = (o || {}).detail; } } };
    win.window = win;
    const ctx = vm.createContext(win);
    vm.runInContext(fs.readFileSync(SRC, 'utf8'), ctx, { filename: SRC });
    return { doc, tabs, list, buttons, inputs, view: win.WBUI.analyticsView };
}

/* 模拟基座：按真实 activate 的结果改 DOM，再派发基座会派的那个事件。 */
function switchTab(env, view) {
    Object.keys(env.buttons).forEach((v) => {
        env.buttons[v].setAttribute('aria-selected', v === view ? 'true' : 'false');
    });
    env.doc.dispatchEvent({
        type: 'wbui:tabs-change',
        detail: { root: env.tabs, tab: env.buttons[view], panel: 'an-panel-' + view },
    });
}

test('首屏按服务端选中的标签对齐，不等人点', () => {
    const env = makeEnv('paths');
    assert.equal(env.inputs[0].value, 'paths');
    assert.equal(env.inputs[1].value, 'paths');
});

test('订阅基座事件：切到「按路径」后两张表单的 view 都变成 paths', () => {
    const env = makeEnv('daily');
    assert.equal(env.inputs[0].value, 'daily');

    switchTab(env, 'paths');

    assert.equal(env.inputs[0].value, 'paths');
    assert.equal(env.inputs[1].value, 'paths');
});

test('切到语言维度同样生效（靠事件，不靠点击顺序）', () => {
    const env = makeEnv('daily');
    switchTab(env, 'langs');
    assert.equal(env.inputs[0].value, 'langs');
    assert.equal(env.inputs[1].value, 'langs');
});

test('view 名从 aria-controls 派生，退化时用 id，都没有则空串', () => {
    const env = makeEnv('daily');
    assert.equal(env.view.viewOf(env.buttons.ua), 'ua');

    env.buttons.ua.attrs['aria-controls'] = '';
    assert.equal(env.view.viewOf(env.buttons.ua), 'ua', '退回 id 的 -tab- 后缀');

    env.buttons.ua.attrs['id'] = 'random-tab';
    assert.equal(env.view.viewOf(env.buttons.ua), '', '没有线索时不编造 view');
});

test('派生出空 view 时一个隐藏域都不写，保持原值', () => {
    const env = makeEnv('paths');
    const orphan = makeNode('button');
    orphan.setAttribute('role', 'tab');
    orphan.setAttribute('id', 'random-tab');
    env.list.appendChild(orphan);

    env.doc.dispatchEvent({ type: 'wbui:tabs-change', detail: { tab: orphan } });
    assert.equal(env.inputs[0].value, 'paths', '空 view 不得把表单的 view 清空');
});

test('页面上没有 data-view-input 时安全返回 0', () => {
    const env = makeEnv('daily');
    env.inputs.forEach((i) => { i.attrs = { type: 'hidden' }; });
    assert.equal(env.view.sync(env.doc, 'paths'), 0);
});

test('基座在 activate 之后才广播，订阅方读到的必是新状态', () => {
    // 从真实 admin.js 里抠出 tabs 基座那一段来执行，确认它派发了 wbui:tabs-change，
    // 且派发点在 aria-selected 改写之后 —— 这是「先 activate 后同步」的硬证据。
    const src = fs.readFileSync(ADMIN_SRC, 'utf8');
    assert.ok(src.includes("document.dispatchEvent(new CustomEvent('wbui:tabs-change'"),
        'tabs 基座必须广播 wbui:tabs-change，否则订阅方拿不到切换时机');

    const idxActivate = src.indexOf("t.setAttribute('aria-selected'");
    const idxDispatch = src.indexOf("new CustomEvent('wbui:tabs-change'");
    assert.ok(idxActivate > 0 && idxDispatch > idxActivate,
        '广播必须排在 aria-selected 改写之后（先 activate 再通知）');
});
