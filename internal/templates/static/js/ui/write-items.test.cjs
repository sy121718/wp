const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

// write-items.test.cjs —— 统一写基座的「错误明细（data.items）opt-in」行为测试。
//
// 与 write-base.test.cjs 的分工：那边测请求体整形（纯函数），write-latest.test.cjs
// 测迟到响应保护；这里测**失败路径的呈现**——哪些表单列出逐条明细、哪些不列。
//
// 为什么要真跑 onSubmit：opt-in 判据只有两处消费点（失败分支的 setStatus 明细、
// bulk 成功档的 reasons），把它们拆成纯函数测会漏掉「调用点压根没把 submitter 传进去」
// 这类缺陷。所以这里用最小 DOM 桩 + 真 fetch 桩，按 submit 事件走到最后。

const source = fs.readFileSync(__dirname + '/write-base.js', 'utf8');

// FormData 桩：只需知道它被构造出来、能被 append/set/delete 即可（请求体形状由
// write-base.test.cjs 覆盖，这里不重复断言）。
// 最小桩：只实现基座真正调到的那几个方法，不模拟真实 FormData。
function Body() { this.map = new Map(); }
Body.prototype.append = function (k, v) { const a = this.map.get(k) || []; a.push(String(v)); this.map.set(k, a); };
Body.prototype.set = function (k, v) { this.map.set(k, [String(v)]); };
Body.prototype.delete = function (k) { this.map.delete(k); };
Body.prototype.get = function (k) { const a = this.map.get(k); return a ? a[0] : null; };
Body.prototype.getAll = function (k) { return this.map.get(k) || []; };
Body.prototype.has = function (k) { return this.map.has(k); };
Body.prototype.entries = function () { return Array.from(this.map.entries()); };

// 状态区桩：setStatus 写 textContent / hidden / data-tone，明细是追加的 <ul data-write-details>。
function statusBox() {
    const box = {
        textContent: '',
        hidden: true,
        tones: [],
        children: [],
        _detail: null,
        setAttribute(n, v) { if (n === 'data-tone') box.tones.push(v); },
        querySelector(sel) { return sel === '[data-write-details]' ? box._detail : null; },
        appendChild(node) { box._detail = node; box.children.push(node); },
    };
    return box;
}

// 明细 ul 桩：记录每个 li 的 textContent。
function detailUl() {
    const ul = { tag: 'UL', items: [], removed: 0, setAttribute() {}, remove() { ul.removed++; }, appendChild(li) { ul.items.push(li); } };
    return ul;
}

// 环境：表单 + 一个字段 + 一个提交按钮 + 状态区，fetch 返回固定响应。
function env(attrs, response, opts) {
    opts = opts || {};
    const status = statusBox();
    const listeners = {};
    const nav = [];
    const calls = [];
    const button = { type: 'submit', disabled: !!(opts.buttonDisabled), tagName: 'BUTTON', getAttribute: () => null, hasAttribute: () => false, setAttribute() {}, removeAttribute() {} };
    const input = { name: 'target', type: 'text', value: opts.value === undefined ? '用户已填写的译文' : opts.value, tagName: 'INPUT', getAttribute: (n) => (n === 'name' ? 'target' : null), hasAttribute: () => false };
    const rows = opts.rows || [input];
    const form = {
        tagName: 'FORM',
        isConnected: true,
        getAttribute: (n) => (n in attrs ? attrs[n] : null),
        hasAttribute: (n) => n in attrs,
        setAttribute() {},
        closest: () => null,
        dispatchEvent() {},
        querySelector: (sel) => (sel === '[data-write-status]' ? status : null),
        querySelectorAll: (sel) => {
            if (sel === '[name]') return rows;
            if (sel === 'button[type="submit"], input[type="submit"]') return [button];
            return [];
        },
    };
    const document = {
        readyState: 'loading',
        addEventListener: (n, f) => { listeners[n] = f; },
        querySelector: () => null,
        createElement: (tag) => (tag === 'ul' ? detailUl() : { tag, textContent: '', setAttribute() {}, appendChild() {}, remove() {}, querySelector: () => null }),
    };
    const window = {
        document,
        location: { assign: (u) => nav.push(u), reload: () => nav.push('reload') },
        confirm: () => true,
        CSS: { escape: (s) => s },
        WBUI: {},
    };
    const context = {
        window,
        document,
        FormData: Body,
        // 最小桩：只带基座读取的 type / detail。
        CustomEvent: function (t, o) { this.type = t; this.detail = o && o.detail; },
        sessionStorage: { getItem: () => null, setItem() {}, removeItem() {} },
        console,
        fetch: async (url, o) => {
            calls.push({ url, opts: o });
            return {
                ok: response.ok === undefined ? true : response.ok,
                status: response.status || 200,
                headers: { get: (n) => (n === 'Content-Type' ? (response.type || 'application/json') : null) },
                json: async () => response.payload,
            };
        },
    };
    vm.createContext(context);
    vm.runInContext(source, context);
    const settle = async () => { await new Promise(setImmediate); await new Promise(setImmediate); };
    const submit = async (submitter) => {
        listeners.submit({ target: form, submitter: submitter || null, preventDefault() {} });
        await settle();
    };
    // 必须经 window 取：vm 出来的对象跨 realm 没有原型链，直挂在上面的键从外面读不到。
    return { window, form, status, input, button, nav, calls, submit, settle, base: window.WBUI.writeBase, context };
}

const URL = 'data-write-url';
const FAIL_ITEMS = {
    ok: false, status: 400,
    payload: {
        code: 400, message: '整批被拒', data: {
            items: [
                { id: 'nav-1', name: '首页', reasonKey: 'admin.x.err.y', message: '菜单文字已变化，请刷新后重试' },
                { id: 'nav-2', name: '关于', reasonKey: 'admin.x.err.z', message: '语境非法' },
            ],
        },
    },
};

// ── bulk：存量行为不变 ──────────────────────────────────────────────────────

test('bulk 档失败：仍逐条列出 data.items（存量行为，未声明 opt-in 也列）', async () => {
    const e = env({ 'data-write': 'bulk', [URL]: '/api/navigation/delete', 'data-write-extra': 'action' }, FAIL_ITEMS, {
        rows: [{ name: 'ids', type: 'checkbox', checked: true, value: 'nav-1', tagName: 'INPUT', getAttribute: (n) => (n === 'name' ? 'ids' : null), hasAttribute: () => false }],
    });
    await e.submit();
    assert.equal(e.status.textContent, '整批被拒');
    assert.deepEqual(e.status.tones, ['error']);
    const ul = e.status._detail;
    assert.ok(ul, 'bulk 必须保留逐条明细');
    assert.deepEqual(ul.items.map((li) => li.textContent), [
        'nav-1: 菜单文字已变化，请刷新后重试',
        'nav-2: 语境非法',
    ]);
});

test('bulk 档成功且有 skipped：逐条原因仍按 items 列出（存量行为）', async () => {
    const e = env({ 'data-write': 'bulk', [URL]: '/api/navigation/delete' }, {
        payload: { code: 200, message: '部分完成', data: { skipped: 1, items: [{ id: 'nav-3', message: '被引用，无法删除' }] } },
    }, {
        rows: [{ name: 'ids', type: 'checkbox', checked: true, value: 'nav-3', tagName: 'INPUT', getAttribute: (n) => (n === 'name' ? 'ids' : null), hasAttribute: () => false }],
    });
    await e.submit();
    assert.equal(e.status.textContent, '部分完成');
    assert.deepEqual(e.status.tones, ['error']);
    assert.deepEqual(e.status._detail.items.map((li) => li.textContent), ['nav-3: 被引用，无法删除']);
});

// ── bulk：旧判据逐字保留（message 或 text 任一即可，不认 reasonKey / name）──

test('bulk 档失败：text-only 的旧条目仍列出（旧判据是 message || text）', async () => {
    const e = env({ 'data-write': 'bulk', [URL]: '/api/navigation/delete' }, {
        ok: false, status: 400,
        payload: {
            code: 400, message: '整批被拒',
            data: { items: [{ id: 'nav-1', text: '旧的 text 原因' }, { id: 'nav-2', message: '成品句' }, { id: 'nav-3', reasonKey: 'admin.x.err.k' }] },
        },
    }, {
        rows: [{ name: 'ids', type: 'checkbox', checked: true, value: 'nav-1', tagName: 'INPUT', getAttribute: (n) => (n === 'name' ? 'ids' : null), hasAttribute: () => false }],
    });
    await e.submit();
    assert.deepEqual(e.status._detail.items.map((li) => li.textContent), [
        'nav-1: 旧的 text 原因',
        'nav-2: 成品句',
    ], 'text-only 必须照旧列出；只有 reasonKey 的项旧版本来就不列，不得新增');
});

// ── create：默认不展示 ──────────────────────────────────────────────────────

test('create 档失败：未声明 data-write-items 时**不**列出 items，只显示一句结论', async () => {
    const e = env({ 'data-write': 'create', [URL]: '/api/navigation/update' }, FAIL_ITEMS);
    await e.submit();
    assert.equal(e.status.textContent, '整批被拒');
    assert.equal(e.status._detail, null, '未 opt-in 的 create 不得摊出 data.items');
});

test('update 档失败：同样默认不列 items（opt-in 不是「给所有写表单开」）', async () => {
    const e = env({ 'data-write': 'update', [URL]: '/api/product/update' }, FAIL_ITEMS);
    await e.submit();
    assert.equal(e.status._detail, null);
});

// ── create + opt-in：展示 ───────────────────────────────────────────────────

test('create 档声明 data-write-items 后：失败逐条列出 id 与原因', async () => {
    const e = env({ 'data-write': 'create', [URL]: '/api/navigation/update', 'data-write-items': '' }, FAIL_ITEMS);
    await e.submit();
    assert.equal(e.status.textContent, '整批被拒');
    const ul = e.status._detail;
    assert.ok(ul, 'opt-in 后必须有明细');
    assert.deepEqual(ul.items.map((li) => li.textContent), [
        'nav-1: 菜单文字已变化，请刷新后重试',
        'nav-2: 语境非法',
    ]);
});

test('opt-in 写在提交按钮上也生效（与其它声明同一条「按钮优先」规则）', async () => {
    const e = env({ 'data-write': 'create', [URL]: '/api/navigation/update' }, FAIL_ITEMS);
    const submitter = { hasAttribute: (n) => n === 'data-write-items', getAttribute: () => '', setAttribute() {}, removeAttribute() {}, disabled: false };
    await e.submit(submitter);
    assert.ok(e.status._detail, '按钮上的 opt-in 必须被认');
    assert.equal(e.status._detail.items.length, 2);
});

// ── XSS：明细只走 textContent ───────────────────────────────────────────────

test('明细内容一律 textContent，HTML 标记原样当文本（不解析、不进 innerHTML）', async () => {
    const payload = {
        code: 400, message: '整批被拒',
        data: { items: [{ id: '<img src=x onerror=bad()>', message: '<script>alert(1)</script>' }] },
    };
    const e = env({ 'data-write': 'create', [URL]: '/api/navigation/update', 'data-write-items': '' }, { ok: false, status: 400, payload });
    await e.submit();
    assert.equal(e.status._detail.items[0].textContent, '<img src=x onerror=bad()>: <script>alert(1)</script>');
    assert.equal(e.status._detail.items[0].innerHTML, undefined, 'li 上不得出现 innerHTML 赋值');
});

// ── 畸形 items：不崩、不 stringify、不显示 raw 对象 ─────────────────────────

test('畸形 items：非数组 data 不展示、null 项跳过、缺 message 时退回 reasonKey / name', async () => {
    const e = env({ 'data-write': 'create', [URL]: '/api/navigation/update', 'data-write-items': '' }, {
        ok: false, status: 400,
        payload: {
            code: 400, message: '参数不合法',
            data: { items: [null, 'raw', 42, {}, { reasonKey: 'admin.x.err.k' }, { name: '只有名字' }, { message: '正常一条' }] },
        },
    });
    await e.submit();
    assert.equal(e.status.textContent, '参数不合法');
    assert.deepEqual(e.status._detail.items.map((li) => li.textContent), [
        'admin.x.err.k',
        '只有名字',
        '正常一条',
    ], 'raw 值、空对象、null 一律不产出额外的行；不 JSON.stringify');
});

test('opt-in 档：对象/数组/数字字段一律不转字符串，id-only 不产出 [object Object]', async () => {
    const e = env({ 'data-write': 'create', [URL]: '/api/navigation/update', 'data-write-items': '' }, {
        ok: false, status: 400,
        payload: {
            code: 400, message: '参数不合法',
            data: {
                items: [
                    { id: { deep: 1 }, message: { deep: 2 } },
                    { id: ['a'], text: ['b'] },
                    { id: 7 },
                    { id: 'nav-9', name: { x: 1 }, message: () => 'x', reasonKey: 42 },
                    { id: 'nav-8', message: '   ', reasonKey: 'admin.x.err.blank' },
                ],
            },
        },
    });
    await e.submit();
    assert.deepEqual(e.status._detail.items.map((li) => li.textContent), [
        'nav-8: admin.x.err.blank',
    ], '只有声明的字符串字段能进文本；message 为空白时回落到 reasonKey');
});

test('opt-in 档：合法 message + 对象/数组/数字 id 不转换，非字符串 id 当空，只显示原因', async () => {
    const e = env({ 'data-write': 'create', [URL]: '/api/navigation/update', 'data-write-items': '' }, {
        ok: false, status: 400,
        payload: {
            code: 400, message: '参数不合法',
            data: {
                items: [
                    { id: { deep: 1 }, message: '对象 id 的原因' },
                    { id: ['a'], message: '数组 id 的原因' },
                    { id: 7, message: '数字 id 的原因' },
                    { id: 0, message: '数字零 id 的原因' },
                ],
            },
        },
    });
    await e.submit();
    assert.deepEqual(e.status._detail.items.map((li) => li.textContent), [
        '对象 id 的原因',
        '数组 id 的原因',
        '数字 id 的原因',
        '数字零 id 的原因',
    ], '非字符串 id 一律当空：不得出现 "[object Object]: 原因"、"7: 原因"、"0: 原因"');
});

test('opt-in 档：合法字符串 id 照旧拼成 "id: 原因"', async () => {
    const e = env({ 'data-write': 'create', [URL]: '/api/navigation/update', 'data-write-items': '' }, {
        ok: false, status: 400,
        payload: {
            code: 400, message: '参数不合法',
            data: {
                items: [
                    { id: 'nav-1', message: '合法 id 的原因' },
                    { id: 'nav-2', name: '合法 id 的 name 回落' },
                ],
            },
        },
    });
    await e.submit();
    assert.deepEqual(e.status._detail.items.map((li) => li.textContent), [
        'nav-1: 合法 id 的原因',
        'nav-2: 合法 id 的 name 回落',
    ]);
});

test('opt-in 档：空字符串 message 不算原因，回落到 reasonKey / name', async () => {
    const e = env({ 'data-write': 'create', [URL]: '/api/navigation/update', 'data-write-items': '' }, {
        ok: false, status: 400,
        payload: { code: 400, message: '失败', data: { items: [{ id: 'nav-1', message: '', name: '首页' }, { id: 'nav-2', message: '', name: '' }] } },
    });
    await e.submit();
    assert.deepEqual(e.status._detail.items.map((li) => li.textContent), ['nav-1: 首页']);
});

test('data 不是对象（null / 数组 / 字符串）时 items 判定为缺失：只显示结论', async () => {
    for (const data of [null, ['a'], 'raw', 42]) {
        const e = env({ 'data-write': 'create', [URL]: '/api/navigation/update', 'data-write-items': '' }, {
            ok: false, status: 400, payload: { code: 400, message: '失败', data },
        });
        await e.submit();
        assert.equal(e.status._detail, null, 'data=' + JSON.stringify(data) + ' 不得产出明细');
    }
});

test('data.items 不是数组时同一条判据：只显示结论', async () => {
    const e = env({ 'data-write': 'create', [URL]: '/api/navigation/update', 'data-write-items': '' }, {
        ok: false, status: 400, payload: { code: 400, message: '失败', data: { items: { id: 'x' } } },
    });
    await e.submit();
    assert.equal(e.status._detail, null);
});

test('items 全为空时不留空 <ul>（结论区里不该多出一片空白）', async () => {
    const e = env({ 'data-write': 'create', [URL]: '/api/navigation/update', 'data-write-items': '' }, {
        ok: false, status: 400, payload: { code: 400, message: '失败', data: { items: [null, {}] } },
    });
    await e.submit();
    assert.equal(e.status._detail, null);
});

// ── 失败不导航、保留输入 ───────────────────────────────────────────────────

test('opt-in 表单失败：不导航、不刷新、输入原样留在 DOM、按钮解锁', async () => {
    const e = env({ 'data-write': 'create', [URL]: '/api/navigation/update', 'data-write-items': '', 'data-write-nav': '/admin/navigations/translations?lang=en' }, FAIL_ITEMS);
    await e.submit();
    assert.deepEqual(e.nav, [], '失败不得导航');
    assert.equal(e.input.value, '用户已填写的译文', '已填内容必须保留');
    assert.equal(e.button.disabled, false, '失败后照常解锁');
    assert.ok(e.status._detail, '明细照常列出');
});

test('opt-in 表单网络层失败：只显示结论、不列明细、不导航', async () => {
    const e = env({ 'data-write': 'create', [URL]: '/api/navigation/update', 'data-write-items': '' }, { ok: false, status: 400, type: 'text/html' });
    await e.submit();
    assert.deepEqual(e.nav, []);
    assert.equal(e.status._detail, null);
    assert.match(e.status.textContent, /content-type/);
    assert.equal(e.input.value, '用户已填写的译文');
});

// ── 成功路径：显式导航不退化 ───────────────────────────────────────────────

test('opt-in 表单成功：仍按 data-write-nav 显式声明导航（{id} 取响应 data.id）', async () => {
    const e = env({ 'data-write': 'create', [URL]: '/api/navigation/update', 'data-write-items': '', 'data-write-nav': '/admin/navigations/translations?id={id}' }, {
        payload: { code: 200, message: '已保存 2 条译文', data: { id: 'p 1', written: 2 } },
    });
    await e.submit();
    assert.deepEqual(e.nav, ['/admin/navigations/translations?id=p%201']);
});

test('opt-in 表单成功但响应里带 redirect：仍不导航（导航只能来自显式声明）', async () => {
    const e = env({ 'data-write': 'create', [URL]: '/api/navigation/update', 'data-write-items': '' }, {
        payload: { code: 200, message: '已保存', data: { id: 'a1', redirect: '/server-chosen' } },
    });
    await e.submit();
    assert.deepEqual(e.nav, []);
});

test('itemText 纯函数：就是 failureLine 的行格式（id: 原因），绝不 stringify 整个对象', () => {
    const e = env({ 'data-write': 'create', [URL]: '/api/navigation/update' }, { payload: {} });
    assert.equal(e.base.itemText({ id: 'a', name: 'n', reasonKey: 'k', message: 'm' }), 'a: m');
    assert.equal(e.base.itemText({ id: 'a', text: 't' }), 'a: t');
    assert.equal(e.base.itemText({ message: 'm' }), 'm');
    assert.equal(e.base.itemText({}), '');
    assert.equal(e.base.itemText(null), '');
    assert.equal(e.base.itemText('raw'), '');
    assert.equal(e.base.itemText({ foo: { bar: 1 } }), '', '未声明的键不得出现在文本里');
});
