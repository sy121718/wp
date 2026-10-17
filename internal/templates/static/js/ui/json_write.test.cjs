/* json_write.test.cjs — 控件基座 ui/json_write.js 的行为契约（node:test + vm，无浏览器、无依赖）。
 *
 * 照 drawer.test.cjs 的手法：一小段 DOM 桩 + 可控的 fetch 桩，把「写表单」的三条分支
 * （create / single / bulk）与它们的失败面钉住。它守的是四类**不会有人发现**的缺陷：
 *
 *   1. CSRF 头没带上 —— /api 组会 403，而页面上只看到一句通用失败；
 *   2. 失败时把表单清了 —— 用户已填的内容是最贵的，这条最容易被容忍也最伤；
 *   3. 批量把上限/去重漏了 —— 一次点击发 500 条，服务端才拦（本该前端先给快速反馈）；
 *   4. data-confirm 的确认框还没弹就发了请求（与 ui/confirm.js 的 bypass 协议对不上）。
 *
 * 运行：node --test internal/templates/static/js/ui/json_write.test.cjs
 */
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

// 先加载真基座 _util.js（WBUI.register/scan/each/markOnce 都在它里面），再加载被测控件：
// 不桩掉基座，是为了让「注册进来」这条链路本身也被测到（漏注册正是本次踩过的坑）。
const utilSource = fs.readFileSync(__dirname + '/_util.js', 'utf8');
const source = fs.readFileSync(__dirname + '/json_write.js', 'utf8');
// confirm.js 也载入真文件：提交确认是**两个控件之间的协议**（属性摘除—恢复），
// 用桩假装它「会打标记」就把协议本身漏掉了 —— 而协议错的表现正是「确认框不弹了」。
const confirmSource = fs.readFileSync(__dirname + '/confirm.js', 'utf8');


test('receipt partial failure shows each failed item and keeps noop out of reasons', async () => {
    const env = setup({
        attrs: { 'data-write-api': '/api/customer/bulk-unlock', 'data-write-mode': 'bulk', 'data-write-outcome': 'receipt', 'data-msg-refresh': '刷新' },
        bulkRows: [{ id: '42', checked: true }],
        respond: { code: 200, message: '部分完成', data: { skipped: 1, items: [{ id: 42, noop: true, message: '无需解锁' }, { id: 7, message: '客户不存在' }] } },
    });
    env.fire(); await tick();
    assert.equal(env.status.getAttribute('data-write-state'), 'error');
    assert.equal(env.status.querySelector('[data-write-reasons]').textContent, '7: 客户不存在');
    assert.ok(env.rows[0].parentNode);
    assert.equal(env.form.getAttribute('aria-busy'), null);
});


test('bulk update and receipt identify customers with identical failure reasons', async () => {
    for (const outcome of ['update', 'receipt']) {
        const env = setup({
            attrs: { 'data-write-api': '/api/customer/bulk-status', 'data-write-mode': 'bulk', 'data-write-outcome': outcome },
            bulkRows: [{ id: '7', checked: true }, { id: '8', checked: true }],
            respond: { code: 200, message: 'Partial result', data: { updated: 0, skipped: 2, items: [{ id: 7, message: 'Missing customer' }, { id: 8, message: 'Missing customer' }] } },
        });
        env.fire(); await tick();
        const reasons = env.status.querySelector('[data-write-reasons]');
        assert.deepEqual(reasons.children.map(item => item.textContent), ['7: Missing customer', '8: Missing customer']);
    }
});

test('customer template submits numeric status and unlock payloads', async () => {
    const html = fs.readFileSync(__dirname + '/../../../admin/user/customers.html', 'utf8');
    for (const action of ['status', 'unlock']) {
        const form = formBlockOf(html.replaceAll('{{r.ID}}', '42').replaceAll('{{r.NextStatus}}', '0'), '<form id="customer-' + action + '-');
        const env = setup({ attrs: form.attrs, createFields: form.controls, respond: { code: 200, data: {} } });
        env.fire(); await tick();
        assert.equal(env.requests[0].url, '/api/customer/' + action);
        assert.deepEqual(env.requests[0].body, action === 'status' ? { customerId: 42, status: 0 } : { customerId: 42 });
        assert.equal(env.calls.reload, 1);
    }
});


test('single uses the clicked row identifier in a shared form', async () => {
    const env = setup({
        attrs: { 'data-write-api': '/api/contenttemplate/activate', 'data-write-mode': 'single' },
        buttons: [{ attrs: { name: 'id', value: 'first-template' } }, { attrs: { name: 'id', value: 'second-template' } }],
        respond: { code: 200, data: {} },
    });
    env.fireWith(env.buttons[1]); await tick();
    assert.equal(env.requests[0].body.id, 'second-template');
});


test('receipt outcome displays server summary and preserves noop rows', async () => {
    const env = setup({
        attrs: { 'data-write-api': '/api/customer/bulk-unlock', 'data-write-mode': 'bulk', 'data-write-outcome': 'receipt', 'data-msg-refresh': '刷新列表' },
        bulkRows: [{ id: '42', checked: true }, { id: '43', checked: true }],
        respond: { code: 200, message: '已解锁 1 个，1 个原本未锁定。', data: { updated: 1, noop: 1, skipped: 0, items: [{ id: 42, updated: true }, { id: 43, noop: true }] } },
    });
    env.fire(); await tick();
    assert.equal(env.status.children[0].textContent, '已解锁 1 个，1 个原本未锁定。');
    assert.equal(env.status.getAttribute('data-write-state'), 'ok');
    assert.equal(env.status.querySelector('[data-write-reasons]'), null);
    assert.ok(env.rows.every(row => row.parentNode && row.children[0].checked));
    assert.equal(env.calls.reload, 0);
    assert.ok(env.status.querySelector('[data-write-refresh]'));
});

// —— 极简 DOM 桩（只实现被测脚本真正用到的那几个入口） ——

function classesOf(el) {
    return String(el.attrs.class || el.className || '').split(/\s+/).filter(Boolean);
}

function matches(el, selector) {
    // 引号里的点不是分隔符：[name$=".key"] 的 ".key" 必须整段留在属性选择器里，
    // 否则会被下面的 split 拆成两段，永远不命中。
    const safe = String(selector).replace(/"[^"]*"/g, (s) => s.replace(/\./g, '@@DOT@@'));
    return safe.trim().split(/(?=\[|\.)/).every((rawPart) => {
        const part = rawPart.split('@@DOT@@').join('.');
        if (!part) { return true; }
        if (part.startsWith('.')) { return classesOf(el).indexOf(part.slice(1)) >= 0; }
        if (part.startsWith('[')) {
            // 属性选择器的操作符（$= / ^= / *=）与引号内的点：真实浏览器都认，
            // 桩不认就等于让用到它们的契约永远测不到（假绿）。
            // 属性名不能把操作符吃进去：[^\]=]+ 贪婪匹配会把 name$ 整个当成名字，
            // 于是 [name$=".id"] 退化成「属性 name$ 等于 .id」—— 永远不命中。
            const m = part.match(/^\[([^\]=$^*]+)([$^*]?)(?:="([^"]*)")?\]$/);
            if (!m) { return false; }
            const [, name, op, want] = m;
            const has = el.hasAttribute(name);
            if (!has) { return false; }
            if (want === undefined) { return true; }
            const actual = String(el.getAttribute(name));
            if (op === '$=') { return actual.endsWith(want); }
            if (op === '^=') { return actual.startsWith(want); }
            if (op === '*=') { return actual.indexOf(want) >= 0; }
            return actual === want;
        }
        return el.tagName === part.toUpperCase();
    });
}

function descendants(el) {
    const out = [];
    (el.children || []).forEach((child) => { out.push(child, ...descendants(child)); });
    return out;
}

function makeEl(tag, attrs, props) {
    const el = {
        tagName: String(tag).toUpperCase(),
        attrs: Object.assign({}, attrs || {}),
        dataset: {},
        children: [],
        parentNode: null,
        listeners: {},
        textContent: '',
        className: '',
        hidden: false,
        value: '',
        checked: false,
        disabled: false,
        type: (props && props.type) || '',
        multiple: false,
        selectedOptions: [],
        getAttribute(name) { return Object.prototype.hasOwnProperty.call(this.attrs, name) ? this.attrs[name] : null; },
        setAttribute(name, value) { this.attrs[name] = String(value); if (name === 'value') { this.value = String(value); } },
        removeAttribute(name) { delete this.attrs[name]; },
        hasAttribute(name) { return Object.prototype.hasOwnProperty.call(this.attrs, name); },
        addEventListener(type, fn) { (this.listeners[type] = this.listeners[type] || []).push(fn); },
        appendChild(child) { this.children.push(child); child.parentNode = this; return child; },
        removeChild(child) { this.children = this.children.filter((c) => c !== child); child.parentNode = null; return child; },
        querySelectorAll(sel) { return descendants(this).filter((x) => matches(x, sel)); },
        querySelector(sel) { return this.querySelectorAll(sel)[0] || null; },
        closest(sel) { let node = this; while (node) { if (matches(node, sel)) { return node; } node = node.parentNode; } return null; },
        focus() {},
        click() { (this.listeners.click || []).forEach((fn) => fn({ preventDefault() {} })); },
        // <dialog> 的最小行为：showModal/close + returnValue + close 事件。
        // confirm.js 靠「close 事件里读 returnValue」判定确认与否，所以这两个钩子缺一不可。
        showModal() { this.open = true; },
        close() { this.open = false; (this.listeners.close || []).forEach((fn) => fn({})); },
        focus() {},
    };
    // classList：confirm.js 靠它切换 is-alert / btn-danger（都用 toggle(name, force) 形态）。
    Object.defineProperty(el, 'classList', {
        get() {
            const self = el;
            const write = (list) => { self.attrs.class = list.join(' '); self.className = self.attrs.class; };
            return {
                add(...names) { const l = classesOf(self); names.forEach((n) => { if (l.indexOf(n) < 0) { l.push(n); } }); write(l); },
                remove(...names) { write(classesOf(self).filter((n) => names.indexOf(n) < 0)); },
                contains(name) { return classesOf(self).indexOf(name) >= 0; },
                toggle(name, force) {
                    const has = classesOf(self).indexOf(name) >= 0;
                    const want = force === undefined ? !has : !!force;
                    if (want && !has) { this.add(name); } else if (!want && has) { this.remove(name); }
                    return want;
                },
            };
        },
        configurable: true,
    });

    // innerHTML：只解析「标签 + 属性」的骨架（confirm.js 往 <dialog> 里写的就是这个），
    // 文本节点用不到 —— 那几处标题/文案都是后面用 textContent 填的。
    Object.defineProperty(el, 'innerHTML', {
        set(html) {
            const root = { children: [] };
            const stack = [root];
            const tagRe = /<(\/?)(\w+)([^>]*)>/g;
            let m;
            while ((m = tagRe.exec(String(html)))) {
                if (m[1]) { if (stack.length > 1) { stack.pop(); } continue; }
                const attrs = {};
                const attrRe = /([\w-]+)(?:="([^"]*)")?/g;
                let a;
                while ((a = attrRe.exec(m[3]))) { attrs[a[1]] = a[2] === undefined ? '' : a[2]; }
                const child = makeEl(m[2], attrs);
                child.parentNode = stack[stack.length - 1];
                stack[stack.length - 1].children.push(child);
                stack.push(child);
            }
            this.children = root.children;
        },
        get() { return ''; },
        configurable: true,
        enumerable: true,
    });
    // textContent 按真实语义实现：写它 = 清空子节点再放文本；读它 = 拼接子树文本。
    // 少了这一条，被测代码「先清空结果区再逐行 append」的写法在断言里永远是空串。
    Object.defineProperty(el, 'textContent', {
        get() {
            if (this.children.length > 0) { return this.children.map((c) => c.textContent).join(''); }
            return this._text === undefined ? '' : this._text;
        },
        set(v) { this.children = []; this._text = String(v); },
        configurable: true,
        enumerable: true,
    });
    // 真实 DOM 里 input.value 是「值反射」：写属性同时写 content attribute，
    // 所以 [value="…"] 选择器能命中（被测代码靠它把结果映射回表格行）。
    Object.defineProperty(el, 'value', {
        get() { return el.attrs.value === undefined ? '' : el.attrs.value; },
        set(v) { el.attrs.value = String(v); },
        configurable: true,
        enumerable: true,
    });
    return el;
}

// setup 造一个页面：meta(CSRF) + 一个表单（按 mode 组装）+ fetch/location 桩。
//
// fetch 的行为由 `respond` 决定：它收到 {url, options}，返回响应对象（或 reject）。
function setup(config) {
    const cfg = config || {};
    let dialog = null;
    const meta = makeEl('meta', { name: 'csrf-token', content: 'TOK-123' });
    const body = makeEl('body');
    const document = {
        listeners: {},
        readyState: 'complete',
        body,
        dataset: {},
        querySelector(sel) { return matches(meta, sel) ? meta : body.querySelector(sel); },
        querySelectorAll(sel) { return body.querySelectorAll(sel); },
        addEventListener(type, fn, capture) { (this.listeners[type] = this.listeners[type] || []).push({ fn, capture: !!capture }); },
        // dispatch 按真实事件的阶段顺序跑：capture 监听先于 bubble 监听，
        // 无论它们谁先注册（confirm.js 在 json_write.js 之后加载，却必须先拦住提交）。
        dispatch(type, event) {
            event.preventDefault = event.preventDefault || function () { this.prevented = true; };
            const list = (this.listeners[type] || []).slice()
                .sort((a, b) => (a.capture === b.capture ? 0 : (a.capture ? -1 : 1)));
            list.forEach((entry) => entry.fn(event));
            return event;
        },
        dispatchEvent() {},
        createElement(tag) { const el = makeEl(tag); if (tag === 'dialog') { dialog = el; } return el; },
    };

    const form = makeEl('form', cfg.attrs || {});
    Object.assign(form.dataset, cfg.dataset || {});
    if (cfg.confirmBypass) { form.dataset.wbConfirmBypass = '1'; }

    const status = makeEl('p', Object.assign({ 'data-write-status': '' }, cfg.statusAttrs || {}));
    form.appendChild(status);

    if (cfg.createFields) {
        cfg.createFields.forEach((f) => {
            const el = makeEl(f.tag || 'input', { name: f.name }, { type: f.type || 'text' });
            el.value = f.value;
            el.checked = !!f.checked;
            el.tagName = (f.tag || 'input').toUpperCase();
            form.appendChild(el);
        });
    }
    if (cfg.id) {
        const hid = makeEl('input', { name: 'id' });
        hid.value = cfg.id;
        form.appendChild(hid);
    }
    const rows = [];
    if (cfg.bulkRows) {
        const tbody = makeEl('tbody');
        form.appendChild(tbody);
        cfg.bulkRows.forEach((row) => {
            const tr = makeEl('tr', Object.assign({}, row.attrs || {}));
            const box = makeEl('input', { name: 'ids' }, { type: 'checkbox' });
            box.value = row.id;
            box.checked = !!row.checked;
            tr.appendChild(box);
            tbody.appendChild(tr);
            rows.push(tr);
        });
    }
    // scopeRows：ids 装在**本表单之外**的容器里（data-write-ids-scope 的用例）。
    // 现实形态是商品列表的「按规则改价」抽屉：抽屉内容克隆到 document 级，
    // 勾选框留在页面那个批量表单里。
    if (cfg.scopeRows) {
        const scope = makeEl('div', { class: 'ids-scope' });
        cfg.scopeRows.forEach((row) => {
            const box = makeEl('input', { name: 'ids' }, { type: 'checkbox' });
            box.value = row.id;
            box.checked = !!row.checked;
            scope.appendChild(box);
        });
        body.appendChild(scope);
    }
    // rowBoxes：data-write-rows 的容器（一行一个 div，里面是该行的具名字段）。
    // 用真实的 class 选择器（.nav-tr-row）而不是给桩开后门：选择器解析本身就是被测契约的一部分。
    if (cfg.rowBoxes) {
        cfg.rowBoxes.forEach((box) => {
            const div = makeEl('div', { class: box.cls || 'wb-row' });
            Object.keys(box.fields || {}).forEach((name) => {
                const el = makeEl('input', { name }, { type: 'text' });
                el.value = box.fields[name];
                div.appendChild(el);
            });
            form.appendChild(div);
        });
    }
    // objBox：data-write-object 的容器（一个 div，里面是驱动 select 与若干输入框）。
    // options 上的 data-rule-keys 是**键清单的真源**：同一容器里的输入框服务不同子类型，
    // 哪些键进对象由当前选中项决定。
    if (cfg.objBox) {
        const div = makeEl('div', { class: cfg.objBox.cls || 'rule-fields' });
        const sel = makeEl('select', { name: cfg.objBox.driver });
        const options = (cfg.objBox.options || []).map((opt) => {
            const o = makeEl('option', opt.keys ? { 'data-rule-keys': opt.keys } : {});
            o.value = opt.value;
            o.selected = !!opt.selected;
            return o;
        });
        sel.options = options;
        sel.selectedOptions = options.filter((o) => o.selected);
        sel.value = (sel.selectedOptions[0] || options[0] || { value: '' }).value;
        div.appendChild(sel);
        Object.keys(cfg.objBox.fields || {}).forEach((name) => {
            const el = makeEl('input', { name }, { type: 'text' });
            el.value = cfg.objBox.fields[name];
            div.appendChild(el);
        });
        form.appendChild(div);
    }
    const submit = makeEl('button', {}, { type: 'submit' });
    form.appendChild(submit);
    // buttons：一个表单里并排的多个动作（批量删除 / 改状态 / 打标签）。
    // outside=true 表示按钮在表单之外、靠 form 属性关联（行内删除就是这种形态）。
    const buttons = (cfg.buttons || []).map((spec) => {
        const btn = makeEl('button', Object.assign({ type: 'submit' }, spec.attrs || {}));
        btn.form = form;
        if (spec.outside) { btn.setAttribute('form', spec.form || 'f1'); } else { form.appendChild(btn); }
        return btn;
    });
    body.appendChild(form);

    const requests = [];
    const calls = { assign: [], reload: 0 };
    const deferred = [];
    const sandbox = {
        console,
        document,
        setTimeout,
        fetch(url, options) {
            requests.push({ url, options, body: JSON.parse(options.body) });
            if (cfg.fetchRejects) { return Promise.reject(new Error('network down')); }
            // deferFetch：先挂起，等测试调用 env.resolveFetch(...) —— 用来观察「请求在飞」的中间态
            // （没有这个钩子，「重复提交只发一次」就只能在请求已完成后测，等于没测）。
            if (cfg.deferFetch) {
                return new Promise((resolve) => {
                    deferred.push((body) => resolve({
                        ok: !(body && body.code >= 400),
                        status: (body && body.code) || 200,
                        text: () => Promise.resolve(JSON.stringify(body || {})),
                    }));
                });
            }
            const body = typeof cfg.respond === 'function' ? cfg.respond(requests[requests.length - 1]) : cfg.respond;
            return Promise.resolve({
                ok: !(body && body.code >= 400),
                status: (body && body.code) || 200,
                text: () => Promise.resolve(cfg.responseText === undefined ? JSON.stringify(body) : cfg.responseText),
            });
        },
        sessionStorage: { getItem: () => null },
    };
    sandbox.window = sandbox;
    sandbox.location = {
        reload() { calls.reload += 1; },
        assign(url) { calls.assign.push(url); },
    };
    vm.runInNewContext(utilSource, sandbox);
    if (cfg.withConfirm !== false) { vm.runInNewContext(confirmSource, sandbox); }
    vm.runInNewContext(source, sandbox);

    // 真实浏览器里 requestSubmit 会**同步派发** submit 事件（confirm.js 正是靠这一点把
    // 「摘属性 → 重派发 → 恢复属性」做成一次性令牌），所以桩必须同步派发而不是排队。
    form.requestSubmit = function (submitterEl) {
        return document.dispatch('submit', { target: form, submitter: submitterEl || null, preventDefault() { this.prevented = true; } });
    };
    form.submit = function () { form.nativeSubmitted = true; };

    const WBUI = sandbox.WBUI;
    return {
        WBUI, form, status, submit, buttons, rows, requests, calls, sandbox,
        get dialog() { return dialog; },
        resolveFetch(body) {
            if (!deferred.length) { throw new Error('没有在飞的请求'); }
            deferred.shift()(body || {});
        },
        dispatch(type, event) { return document.dispatch(type, event); },
        fire() { return document.dispatch('submit', { target: form, submitter: submit, preventDefault() { this.prevented = true; } }); },
        // fireWith 用指定的按钮提交（多动作表单：谁被点，就以谁为 submitter）。
        fireWith(btn) { return document.dispatch('submit', { target: form, submitter: btn, preventDefault() { this.prevented = true; } }); },
        // fireEnter 回车提交：没有 submitter，只有表单级声明可用。
        fireEnter() { return document.dispatch('submit', { target: form, submitter: null, preventDefault() { this.prevented = true; } }); },
        // click 点一个元素（按钮级 data-confirm 走的是 click 这条路，与表单级不同）。
        click(el) { return document.dispatch('click', { target: el, preventDefault() { this.prevented = true; } }); },
        // acceptConfirm 模拟「用户点了确认按钮」：<dialog> 以 returnValue='ok' 关闭。
        acceptConfirm() {
            if (!dialog) { throw new Error('确认框还没建出来（submit 没被 confirm.js 拦下？）'); }
            dialog.returnValue = 'ok';
            dialog.close();
        },
    };
}

function tick(times) {
    let chain = Promise.resolve();
    for (let i = 0; i < (times || 4); i += 1) { chain = chain.then(() => new Promise((r) => setImmediate(r))); }
    return chain;
}

// —— 分支 1：create ——

test('create 成功按响应 id 跳工作台，且 payload 取具名字段（未勾选的复选框不进 body）', async () => {
    const env = setup({
        attrs: { 'data-write-api': '/api/block/create', 'data-write-mode': 'create', 'data-write-nav': '/workbench?block={id}' },
        createFields: [
            { name: 'projectId', value: 'p-1' },
            { name: 'name', value: '站点页眉' },
            { name: 'freeShipping', type: 'checkbox', value: '1', checked: false },
        ],
        respond: { code: 200, data: { id: 'blk-9' } },
    });
    env.fire();
    await tick();
    assert.equal(env.requests.length, 1, '应恰好发一次请求');
    assert.equal(env.requests[0].url, '/api/block/create');
    assert.deepEqual(env.requests[0].body, { projectId: 'p-1', name: '站点页眉' });
    assert.equal(env.requests[0].options.headers['X-CSRF-Token'], 'TOK-123', 'CSRF 头必须带上（否则 /api 组一律 403）');
    assert.deepEqual(env.calls.assign, ['/workbench?block=blk-9']);
});

test('create 失败：就地显示服务端文案，已填内容一个字段都不动', async () => {
    const env = setup({
        attrs: { 'data-write-api': '/api/block/create', 'data-write-mode': 'create', 'data-msg-failed': '操作失败，请稍后重试' },
        createFields: [{ name: 'name', value: '重复的名字' }],
        respond: { code: 409, message: '同名块已存在' },
    });
    env.fire();
    await tick();
    assert.equal(env.status.textContent, '同名块已存在');
    assert.equal(env.status.getAttribute('data-write-state'), 'error');
    assert.equal(env.form.querySelector('[name="name"]').value, '重复的名字', '失败后表单被清空 = 用户的输入被丢掉');
    assert.deepEqual(env.calls.assign, [], '失败不能跳走');
    assert.equal(env.submit.disabled, false, '失败后提交按钮要恢复可用');
});

test('网络层失败（拿不到 JSON）也要给一句受控文案，不把异常抛到页面上', async () => {
    const env = setup({
        attrs: { 'data-write-api': '/api/block/create', 'data-write-mode': 'create', 'data-msg-failed': '操作失败，请稍后重试' },
        createFields: [{ name: 'name', value: 'x' }],
        fetchRejects: true,
    });
    env.fire();
    await tick();
    assert.equal(env.status.textContent, '操作失败，请稍后重试');
});

// —— 分支 2：single（单条删除） ——

test('single 成功整页刷新（列表派生列由服务端重算），payload 只有 id', async () => {
    const env = setup({
        attrs: { 'data-write-api': '/api/block/delete', 'data-write-mode': 'single' },
        id: 'blk-1',
        respond: { code: 200, data: null },
    });
    env.fire();
    await tick();
    assert.deepEqual(env.requests[0].body, { id: 'blk-1' });
    assert.equal(env.calls.reload, 1);
});

test('single 失败给受控文案且不刷新', async () => {
    const env = setup({
        attrs: { 'data-write-api': '/api/block/delete', 'data-write-mode': 'single', 'data-msg-failed': '操作失败' },
        id: 'blk-1',
        respond: { code: 409, message: '该区块仍被引用：页面 #42' },
    });
    env.fire();
    await tick();
    assert.equal(env.status.textContent, '该区块仍被引用：页面 #42');
    assert.equal(env.calls.reload, 0);
});

test('single 可带点名额外字段：id 最后写、点名的才发、csrf_token 永不发', async () => {
    // 多工程部署下的商品删除必须带 projectId：服务端 projectId 为空时只肯走「唯一工程」
    // 兜底，多工程直接拒（product/service 的 resolveProjectID）。声明式点名是唯一能带上它
    // 又不会把「表单里有什么就发什么」变成盲发的方式。
    const env = setup({
        attrs: {
            'data-write-api': '/api/product/delete', 'data-write-mode': 'single',
            'data-write-extra': 'projectId',
        },
        id: 'p-1',
        createFields: [
            { name: 'projectId', value: 'prj-9' },
            { name: 'csrf_token', value: 'TOK' },
            { name: 'unrelated', value: 'x' },
        ],
        respond: { code: 200, data: null },
    });
    env.fire();
    await tick();
    assert.deepEqual(env.requests[0].body, { projectId: 'prj-9', id: 'p-1' });
    assert.equal(env.calls.reload, 1);
});

// —— 分支 3：bulk（批量删除） ——

test('bulk：data-write-ids-scope 让 ids 从本表单之外收（抽屉里的批量动作）', async () => {
    // 商品列表「按规则改价」：表单在 <template> 里、打开时克隆到 document 级，
    // 勾选框留在页面那个批量表单里。不收作用域的话 ids 恒为空 ——
    // 症状是「点了应用调价什么也没发生」，而基座只会说「没勾选任何项」。
    const env = setup({
        attrs: {
            'data-write-api': '/api/product/bulk-pricing', 'data-write-mode': 'bulk',
            'data-write-ids-scope': '.ids-scope', 'data-write-extra': 'projectId',
        },
        scopeRows: [{ id: 'p1', checked: true }, { id: 'p2', checked: false }, { id: 'p3', checked: true }],
        createFields: [{ name: 'projectId', value: 'proj-1' }],
        respond: { code: 200, data: { deleted: 2, skipped: 0, items: [] } },
    });
    env.fire(); await tick();
    assert.equal(env.requests.length, 1);
    assert.deepEqual(env.requests[0].body, { projectId: 'proj-1', ids: ['p1', 'p3'] });
});

test('bulk：ids-scope 指向不存在的选择器时退回本表单，不发空 ids', async () => {
    // 选择器打错 / 目标还没渲染：退回本表单内收，而不是发一个空 ids ——
    // 那样服务端会回「没勾选任何项」，把人指向错误的原因。
    const env = setup({
        attrs: {
            'data-write-api': '/api/product/bulk-pricing', 'data-write-mode': 'bulk',
            'data-write-ids-scope': '.not-rendered-yet',
        },
        bulkRows: [{ id: 'p9', checked: true }],
        respond: { code: 200, data: { deleted: 1, skipped: 0, items: [] } },
    });
    env.fire(); await tick();
    assert.deepEqual(env.requests[0].body.ids, ['p9']);
});

test('bulk 去重保序后提交，成功按服务端结果移除行并给出计数、原因与刷新入口', async () => {
    const env = setup({
        attrs: {
            'data-write-api': '/api/block/bulk-delete', 'data-write-mode': 'bulk',
            'data-msg-all-deleted': '已删除 {count} 个块。',
            'data-msg-partial': '已删除 {deleted} 个，{skipped} 个未能删除（被引用的全局块需先解除引用）。',
            'data-msg-refresh': '刷新列表',
        },
        bulkRows: [
            { id: 'b1', checked: true, attrs: { 'data-write-row-name': '页眉' } },
            { id: 'b1', checked: true, attrs: { 'data-write-row-name': '页眉' } },   // 重复勾选（同 id 两行）
            { id: 'b2', checked: true, attrs: { 'data-write-row-name': '页脚' } },
            { id: 'b3', checked: false, attrs: { 'data-write-row-name': '未勾选' } },
        ],
        respond: {
            code: 200,
            data: {
                deleted: 1, skipped: 1,
                items: [
                    { id: 'b1', deleted: true },
                    { id: 'b2', deleted: false, message: '该区块仍被引用：页面 #42' },
                ],
            },
        },
    });
    env.fire();
    await tick();
    assert.deepEqual(env.requests[0].body, { ids: ['b1', 'b2'] }, '去重保序：重复 id 只提交一次');
    assert.match(env.status.textContent, /已删除 1 个，1 个未能删除/);
    const rendered = env.status.children.map((c) => c.textContent).join(' | ');
    assert.match(rendered, /该区块仍被引用：页面 #42/, '逐条原因要显示出来（否则运营不知道剩下去哪解引用）');
    assert.equal(env.rows[0].parentNode, null, '成功的行应就地移除');
    assert.ok(env.rows[2].parentNode, '未勾选 / 未删除的行不能被移除');
    assert.ok(env.status.querySelector('[data-write-refresh]'), '批量结束要留一个刷新入口，结果不能一闪而过');
});

test('bulk 超过 200 条在客户端就被拦下（不发请求），两种占位符形态都要填对', async () => {
    // 生产形态（%s，与 response.translate 同序）与命名形态（{max}/{count}）各跑一遍：
    // 前者是模板真正传进来的（词条服务端页面共用一句），后者是留给其它页面的写法。
    // 只测一种，另一种就会在上线后把裸 "%s" 显示给用户 —— 没有异常、没有日志。
    const forms = [
        ['位置填参（生产形态）', '一次最多操作 %s 项，当前 %s 项，请分批进行'],
        ['命名占位符', '一次最多操作 {max} 项，当前 {count} 项，请分批进行'],
    ];
    for (const [name, template] of forms) {
        const rows = [];
        for (let i = 0; i < 201; i += 1) { rows.push({ id: 'b' + i, checked: true }); }
        const env = setup({
            attrs: {
                'data-write-api': '/api/block/bulk-delete', 'data-write-mode': 'bulk',
                'data-msg-too-many': template,
            },
            bulkRows: rows,
            respond: { code: 200, data: { deleted: 0, skipped: 0, items: [] } },
        });
        env.fire();
        await tick(2);
        assert.equal(env.requests.length, 0, `${name}：客户端上限不是安全边界，但也不该把 500 条发出去`);
        assert.equal(env.status.textContent, '一次最多操作 200 项，当前 201 项，请分批进行', name);
    }
});

test('bulk 一条都没勾选时给「未选中」文案且不发请求', async () => {
    const env = setup({
        attrs: { 'data-write-api': '/api/block/bulk-delete', 'data-write-mode': 'bulk', 'data-msg-none-selected': '没有选中任何块，列表未改动。' },
        bulkRows: [{ id: 'b1', checked: false }],
        respond: { code: 200, data: {} },
    });
    env.fire();
    await tick(2);
    assert.equal(env.requests.length, 0);
    assert.equal(env.status.textContent, '没有选中任何块，列表未改动。');
});

// —— 与 ui/confirm.js 的协议（集成：载入真 confirm.js，不用「会打标记」的假桩） ——

test('破坏性提交：确认前不发请求；确认后恰好发一次并整页刷新；放行令牌必须是「一次性」的', async () => {
    const env = setup({
        attrs: { 'data-write-api': '/api/block/delete', 'data-write-mode': 'single', 'data-confirm': '确定删除？' },
        id: 'blk-1',
        respond: { code: 200 },
    });

    // 第一次 submit：confirm.js 在 capture 阶段拦下并弹框，控件不该看到这次提交。
    const event = env.fire();
    await tick(2);
    assert.equal(env.requests.length, 0, '用户还没确认，不许发请求');
    assert.ok(event.prevented, 'confirm.js 必须拦住这次提交（它自己 preventDefault 后再弹框）');
    assert.ok(env.dialog && env.dialog.open, '确认框要真的弹出来 —— 这是唯一的确认入口');

    // 用户点「确定」：confirm.js 摘掉 data-confirm → 同步重派发 → 恢复属性。
    env.acceptConfirm();
    await tick();
    assert.equal(env.requests.length, 1, '确认之后恰好发一次请求');
    assert.equal(env.requests[0].url, '/api/block/delete');
    assert.equal(env.calls.reload, 1, 'single 成功分支走整页刷新（列表要重新从服务端取）');

    // 关键回归：放行令牌必须一次性 —— 属性回到原位、不留任何旁路标记。
    assert.equal(env.form.getAttribute('data-confirm'), '确定删除？', 'data-confirm 必须恢复');
    assert.equal(env.form.dataset.wbConfirmBypass, undefined, '不该再有旁路标记');
    assert.equal(env.form.hasAttribute('wbConfirmBypass'), false, '标记不能残留在元素上');

    // 第二次点同一条删除：还得重新确认（旧写法在这里会直接执行，确认框再也不弹）。
    env.fire();
    await tick(2);
    assert.equal(env.requests.length, 1, '未经确认的第二次提交同样不能发请求');
    assert.ok(env.dialog.open, '第二次也必须弹确认框');
});

// —— 重复提交（in-flight）：连点两次只发一个请求 ——

test('请求在飞时的重复提交被忽略；结束后锁、aria-busy、按钮状态全部释放', async () => {
    const env = setup({
        attrs: {
            'data-write-api': '/api/block/bulk-delete', 'data-write-mode': 'bulk',
            'data-msg-partial': '已删除 {deleted} 个，{skipped} 个未能删除。',
        },
        // 两行都勾着：第一笔结束后要留一行没被删掉的，才能验证「锁释放后还能再提交」。
        bulkRows: [{ id: 'b1', checked: true }, { id: 'b2', checked: true }],
        deferFetch: true,
    });

    env.fire();
    await tick(2);
    assert.equal(env.requests.length, 1, '第一次提交发出请求');
    assert.equal(env.submit.disabled, true, '在飞期间提交按钮禁用（视觉反馈）');
    assert.equal(env.form.getAttribute('aria-busy'), 'true', '在飞期间表单要标 aria-busy（读屏用户也要知道）');

    // 用户等不及又点了一次（或按了回车）—— 这一次必须被丢掉，而不是变成第二个请求。
    env.fire();
    await tick(2);
    assert.equal(env.requests.length, 1, '请求在飞时重复提交只发一次（按钮禁用拦不住回车提交）');

    env.resolveFetch({
        code: 200,
        data: {
            deleted: 1, skipped: 1,
            items: [{ id: 'b1', deleted: false, message: '仍被其它页面引用' }, { id: 'b2', deleted: true }],
        },
    });
    await tick();
    assert.equal(env.submit.disabled, false, '结束后按钮必须恢复可点');
    assert.equal(env.form.getAttribute('aria-busy'), null, '结束后 aria-busy 必须清掉');
    assert.match(env.status.textContent, /^已删除 1 个，1 个未能删除。/);
    assert.match(env.status.textContent, /仍被其它页面引用/, '逐条原因与计数在同一个结果区里');
    assert.equal(env.rows[1].parentNode, null, '只有服务端确认删掉的行才移除');

    // 锁真的释放了：下一笔能正常发出去（这一条才是「释放」的验收，只看按钮状态会被骗过）。
    env.fire();
    await tick(2);
    assert.equal(env.requests.length, 2, '上一笔结束后必须能再次提交');
    assert.deepEqual(env.requests[1].body, { ids: ['b1'] }, '未删掉的那一行还在，重提只带它');
});

test('批量逐条失败原因全部列出（截断到 3 条正好会丢掉最需要看的那几条）', async () => {
    const items = [];
    for (let i = 1; i <= 5; i += 1) { items.push({ id: 'b' + i, deleted: false, message: '第 ' + i + ' 个块仍被引用：页面 #' + i }); }
    const env = setup({
        attrs: {
            'data-write-api': '/api/block/bulk-delete', 'data-write-mode': 'bulk',
            'data-msg-all-skipped': '没有块被删除（{skipped} 个未能删除）。',
        },
        bulkRows: items.map((it) => ({ id: it.id, checked: true })),
        respond: { code: 200, data: { deleted: 0, skipped: 5, items: items } },
    });
    env.fire();
    await tick();
    const reasons = env.status.querySelector('[data-write-reasons]');
    assert.ok(reasons, '逐条原因要有自己的容器（不能和计数挤成一行）');
    assert.equal(reasons.children.length, 5, '5 条失败原因必须全部列出');
    assert.ok(!env.status.textContent.includes('…'), '不许用省略号吞掉原因');
    assert.match(env.status.textContent, /页面 #5/, '最后一条也要在（截断时丢掉的恰好是它）');
});

test('本地拦下（一条没勾选）之后表单不能卡在「提交中」', async () => {
    const env = setup({
        attrs: { 'data-write-api': '/api/block/bulk-delete', 'data-write-mode': 'bulk', 'data-msg-none-selected': '没有选中任何块，列表未改动。' },
        bulkRows: [{ id: 'b1', checked: false }],
        respond: { code: 200, data: { deleted: 1, skipped: 0, items: [{ id: 'b1', deleted: true }] } },
    });
    env.fire();
    await tick(2);
    assert.equal(env.requests.length, 0, '本地就能判定：不发请求');
    assert.equal(env.status.textContent, '没有选中任何块，列表未改动。');
    assert.equal(env.submit.disabled, false, '本地拦下也必须解锁（否则此后再也提交不了）');
    assert.equal(env.form.getAttribute('aria-busy'), null);

    // 勾上之后必须能正常提交 —— 这条断言才是「解锁」的验收，否则只看按钮状态也能通过。
    env.rows[0].children[0].checked = true;
    env.fire();
    await tick();
    assert.equal(env.requests.length, 1, '解锁后应能正常提交');
});

// —— 多动作表单：按钮级声明优先、没有声明的按钮与回车保持原生提交 ——

test('混合表单：没有声明端点的按钮与回车提交都保持原生提交（表单级也不声明）', async () => {
    const env = setup({
        // 表单级只有文案、没有 data-write-api：批量栏三个动作里「改状态」没有 JSON 端点，
        // 若表单级声明了端点，它会**被继承**成一个 fetch —— 恰恰是这条回归要挡的。
        attrs: { 'data-msg-none-selected': '没有选中任何联系人。' },
        buttons: [{ attrs: { formaction: '/admin/mail/contacts/bulk-status' } }],
        bulkRows: [{ id: 'c1', checked: true }],
        respond: { code: 200, data: {} },
    });

    const clicked = env.fireWith(env.buttons[0]);
    await tick(2);
    assert.equal(clicked.prevented, undefined, '没有声明端点的按钮必须走原生提交（页面 POST），不能被邻居拖进 fetch');
    assert.equal(env.requests.length, 0);

    const entered = env.fireEnter();
    await tick(2);
    assert.equal(entered.prevented, undefined, '回车提交没有 submitter，表单级也没声明 → 原生提交');
    assert.equal(env.requests.length, 0);

    const defaultBtn = env.fire();
    await tick(2);
    assert.equal(defaultBtn.prevented, undefined, '默认提交按钮同样没有声明 → 原生提交');
    assert.equal(env.requests.length, 0);
});

test('多动作表单：按钮级声明覆盖表单级，各按钮发自己的端点与结果口径', async () => {
    const env = setup({
        attrs: {
            'data-write-api': '/api/mail/contact/delete', 'data-write-mode': 'bulk',
            'data-msg-all-deleted': '已删除 {count} 位联系人。',
            'data-msg-refresh': '刷新列表',
        },
        createFields: [{ name: 'add', value: 'vip' }],
        bulkRows: [{ id: 'c1', checked: true }, { id: 'c2', checked: true }],
        buttons: [
            { attrs: { formaction: '/admin/mail/contacts/bulk-status' } },
            {
                attrs: {
                    'data-write-api': '/api/mail/contact/tag', 'data-write-outcome': 'update',
                    'data-write-extra': 'add', 'data-msg-all-updated': '已为 {count} 位联系人更新标签。',
                },
            },
        ],
        respond: (req) => (req.url.indexOf('/tag') >= 0
            ? { code: 200, data: { updated: 2, skipped: 0, items: [] } }
            : { code: 200, data: { deleted: 2, skipped: 0, items: [{ id: 'c1', deleted: true }, { id: 'c2', deleted: true }] } }),
    });

    // 打标签：用按钮自己的端点、自己的 outcome 与自己的文案，额外参数按名带上。
    env.fireWith(env.buttons[1]);
    await tick();
    assert.equal(env.requests.length, 1);
    assert.equal(env.requests[0].url, '/api/mail/contact/tag', '按钮级 data-write-api 覆盖表单级');
    assert.deepEqual(env.requests[0].body, { add: 'vip', ids: ['c1', 'c2'] });
    assert.equal(env.status.textContent.indexOf('已为 2 位联系人更新标签。'), 0, '摘要句用按钮级文案');
    assert.ok(env.rows[0].parentNode && env.rows[1].parentNode, 'update 不是破坏性动作：一行都不许移除');

    // 默认提交按钮没有声明 → 回落表单级（删除），这是表单级作为「默认值」的验收。
    env.fire();
    await tick();
    assert.equal(env.requests.length, 2);
    assert.equal(env.requests[1].url, '/api/mail/contact/delete');
    assert.equal(env.status.textContent.indexOf('已删除 2 位联系人。'), 0, '回落表单级文案');
    assert.equal(env.rows[0].parentNode, null, '删除口径下成功条目才移除行');
});

test('outcome=update：计数读 data.updated、逐条原因按 updated 判、缺 all-updated 时不给句子（不回落 all-deleted）', async () => {
    const partial = setup({
        attrs: {
            'data-write-api': '/api/mail/contact/tag', 'data-write-mode': 'bulk', 'data-write-outcome': 'update',
            'data-write-extra': 'add',
            'data-msg-partial': '已为 {updated} 位更新标签，{skipped} 位未能更新：',
            'data-msg-all-skipped': '{count} 位都没有更新。',
        },
        createFields: [{ name: 'add', value: 'vip' }],
        bulkRows: [{ id: 'c1', checked: true }, { id: 'c2', checked: true }, { id: 'c3', checked: true }],
        respond: {
            code: 200,
            data: {
                updated: 2, skipped: 1,
                items: [
                    { id: 'c1', updated: true },
                    { id: 'c2', updated: true },
                    { id: 'c3', updated: false, message: '联系人不存在' },
                ],
            },
        },
    });
    partial.fire();
    await tick();
    assert.equal(partial.status.textContent.indexOf('已为 2 位更新标签，1 位未能更新：'), 0, '计数必须读 data.updated（读 deleted 会显示 0）');
    assert.match(partial.status.textContent, /联系人不存在/, '逐条原因按 items[].updated 判');
    assert.ok(partial.rows.every((r) => r.parentNode), 'update 成功后任何一行都不移除');

    // 缺 data-msg-all-updated：**不给句子**，也不许回落到 all-deleted。
    // 打标签成功显示「已删除 N 位」会让操作者以为人没了，刷新才发现还在 —— 这是本属性
    //（data-write-outcome）存在的全部理由，所以回落在这里等于把属性本身废掉。
    const noSentence = setup({
        attrs: {
            'data-write-api': '/api/mail/contact/tag', 'data-write-mode': 'bulk', 'data-write-outcome': 'update',
            'data-msg-all-deleted': '已删除 {count} 位（回落句）。',
            'data-msg-refresh': '刷新列表',
        },
        bulkRows: [{ id: 'c1', checked: true }],
        respond: { code: 200, data: { updated: 1, skipped: 0, items: [{ id: 'c1', updated: true }] } },
    });
    noSentence.fire();
    await tick();
    assert.equal(noSentence.status.textContent.indexOf('已删除'), -1, '缺 all-updated 时不许回落到 all-deleted');
    assert.equal(noSentence.status.textContent.indexOf('回落句'), -1, '没有成品句就不给句子');
    assert.ok(noSentence.status.querySelector('[data-write-refresh]'), '没有句子时刷新入口仍要在（否则结果区空白）');
    assert.ok(noSentence.rows[0].parentNode, 'update 不移除行');
});

test('data-write-extra 只发点名字段：ids 与 csrf_token 不可能被覆盖，缺失/未勾选的也不发', async () => {
    const env = setup({
        attrs: {
            'data-write-api': '/api/mail/contact/tag', 'data-write-mode': 'bulk', 'data-write-outcome': 'update',
            'data-write-extra': 'ids,csrf_token,add,ghost,optin',
        },
        createFields: [
            { name: 'ids', value: 'HACK' },              // 想覆盖批量主体
            { name: 'csrf_token', value: 'TOKEN' },      // 走请求头，不进身体
            { name: 'add', value: 'vip' },
            { name: 'optin', type: 'checkbox', value: '1', checked: false },   // 未勾选
            // ghost 根本没有这个字段
        ],
        bulkRows: [{ id: 'c1', checked: true }],
        respond: { code: 200, data: { updated: 1, skipped: 0, items: [] } },
    });
    env.fire();
    await tick();
    assert.deepEqual(env.requests[0].body, { add: 'vip', ids: ['c1'] }, 'ids 必须由批量主体决定（最后写），额外字段不得参与');
});

// —— 无 JS 状态的另一半：控件由基座放出来 ——

test('data-requires-js 的控件在初始化时解除 hidden（模板里默认 hidden，无 JS 时不出现）', () => {
    const env = setup({ attrs: {} });
    const control = makeEl('div', { 'data-requires-js': '' });
    control.hidden = true;
    const scope = { querySelectorAll: (sel) => (matches(control, sel) ? [control] : []) };
    env.WBUI.scan(scope);
    assert.equal(control.hidden, false, '有 JS 时控件必须放出来');
});

// —— 数字字段：显式点名才转，且只在安全整数范围内转 ——

test('data-write-number：点名的 id 按数字发（single/create），没点名的原样发字符串（UUID 消费者不受影响）', async () => {
    // 严格 API 会拒绝字符串数字（mail 联系人端点就是），但 DOM 的值永远是字符串 ——
    // 所以模板必须显式点名；反过来，没点名的消费者（block 的 UUID id）一个字都不许被改。
    const declared = setup({
        attrs: {
            'data-write-api': '/api/mail/contact/delete', 'data-write-mode': 'single',
            'data-write-number': 'id',
        },
        id: '7',
        respond: { code: 200, data: {} },
    });
    declared.fire();
    await tick();
    assert.deepEqual(declared.requests[0].body, { id: 7 }, '点名的 id 必须是 JSON 数字，不是 "7"');
    assert.equal(typeof declared.requests[0].body.id, 'number');

    const undeclared = setup({
        attrs: { 'data-write-api': '/api/block/delete', 'data-write-mode': 'single' },
        id: '9f1c-uuid-4a2b',
        respond: { code: 200, data: {} },
    });
    undeclared.fire();
    await tick();
    assert.deepEqual(undeclared.requests[0].body, { id: '9f1c-uuid-4a2b' }, '没点名就不许改类型');
});

test('data-write-number：create 档同样生效（id=0 走新建、id>0 走编辑），其它字段仍是字符串', async () => {
    const edit = setup({
        attrs: { 'data-write-api': '/api/mail/contact/save', 'data-write-mode': 'create', 'data-write-number': 'id' },
        createFields: [{ name: 'id', value: '31' }, { name: 'email', value: 'a@b.c' }, { name: 'status', value: 'pending' }],
        respond: { code: 200, data: {} },
    });
    edit.fire();
    await tick();
    assert.equal(edit.requests[0].body.id, 31);
    assert.equal(typeof edit.requests[0].body.id, 'number');
    assert.equal(edit.requests[0].body.email, 'a@b.c', '字符串字段不许被顺手转掉');

    const created = setup({
        attrs: { 'data-write-api': '/api/mail/contact/save', 'data-write-mode': 'create', 'data-write-number': 'id' },
        createFields: [{ name: 'id', value: '0' }, { name: 'email', value: 'a@b.c' }],
        respond: { code: 200, data: {} },
    });
    created.fire();
    await tick();
    assert.equal(created.requests[0].body.id, 0, 'id=0 是「新建」，必须是数字 0 而不是 "0"');
});

test('data-write-number 的边界：空值/负数/科学计数/十六进制/带空格/超安全整数一律原样发字符串', async () => {
    // 页面侧不替调用方猜类型：这些值转过去都会**静默变义**（"" → 0、"-1" → -1、"1e3" → 1000），
    // 交给严格 API 回 400 才是诚实的。超安全整数更危险 —— 精度丢失后可能命中另一条真实记录。
    const cases = [
        ['', ''],
        ['-1', '-1'],
        ['1e3', '1e3'],
        ['0x10', '0x10'],
        ['5.0', '5.0'],
        [String(Number.MAX_SAFE_INTEGER) + '0', String(Number.MAX_SAFE_INTEGER) + '0'],
    ];
    for (const [raw, want] of cases) {
        const env = setup({
            attrs: { 'data-write-api': '/api/mail/contact/delete', 'data-write-mode': 'single', 'data-write-number': 'id' },
            id: raw,
            respond: { code: 200, data: {} },
        });
        env.fire();
        await tick();
        assert.deepEqual(env.requests[0].body, { id: want }, `id=${JSON.stringify(raw)} 应原样发出`);
    }

    const maxSafe = setup({
        attrs: { 'data-write-api': '/api/mail/contact/delete', 'data-write-mode': 'single', 'data-write-number': 'id' },
        id: String(Number.MAX_SAFE_INTEGER),
        respond: { code: 200, data: {} },
    });
    maxSafe.fire();
    await tick();
    assert.equal(maxSafe.requests[0].body.id, Number.MAX_SAFE_INTEGER, '安全整数上限之内仍要转成数字');

    // single 档本来就会 trim 隐藏域的值（既有行为），" 5" 因此是合法的十进制整数；
    // create 档不 trim，同样的值会原样发出字符串 —— 两档的差异属于既有语义，这里钉住不漂移。
    const padded = setup({
        attrs: { 'data-write-api': '/api/mail/contact/delete', 'data-write-mode': 'single', 'data-write-number': 'id' },
        id: ' 5',
        respond: { code: 200, data: {} },
    });
    padded.fire();
    await tick();
    assert.deepEqual(padded.requests[0].body, { id: 5 }, 'single 档先 trim 再转（" 5" → 5）');
    const paddedCreate = setup({
        attrs: { 'data-write-api': '/api/mail/contact/save', 'data-write-mode': 'create', 'data-write-number': 'id' },
        createFields: [{ name: 'id', value: ' 5' }],
        respond: { code: 200, data: {} },
    });
    paddedCreate.fire();
    await tick();
    assert.deepEqual(paddedCreate.requests[0].body, { id: ' 5' }, 'create 档不 trim：带空格原样发字符串，由端点判非法');
});

// —— 真模板集成：读 admin/mail/mail_contacts.html 的实际控件，不看手写的假控件 ——
//
// 为什么必须读真模板：这次的 P1 恰恰是「通用 Node 用例全绿、真模板发出来的却是字符串 id」——
// 假控件的属性是人手写的，人写对了、模板写错了，测试就永远发现不了。所以这里把模板里
// **真实的 form 属性与真实 input** 抽出来，喂给同一个 json_write，再断言产出的身体。

test('bulk 显式 ids 数字数组转换且非法超界保串，未声明保持 string', async () => {
    const ids = ['7', '9007199254740991', '9007199254740992', '-1', '1e3', '0x10', '5.0', 'uuid-9'];
    for (const declared of [true, false]) {
        const env = setup({
            attrs: Object.assign({ 'data-write-api': '/api/review', 'data-write-mode': 'bulk' }, declared ? { 'data-write-number': 'ids' } : {}),
            bulkRows: ids.map((id) => ({ id, checked: true })), respond: { code: 200, data: {} },
        });
        env.fire(); await tick();
        assert.deepEqual(env.requests[0].body.ids, declared ? [7, Number.MAX_SAFE_INTEGER, ...ids.slice(2)] : ids);
    }
});

test('bulk 两个同名状态按钮优先取本次 submitter，回车走表单字段', async () => {
    const env = setup({
        attrs: { 'data-write-api': '/api/comment/review', 'data-write-mode': 'bulk', 'data-write-outcome': 'review', 'data-write-extra': 'projectId,status', 'data-write-number': 'ids' },
        createFields: [{ name: 'projectId', value: 'p1' }, { name: 'status', value: 'pending' }],
        bulkRows: [{ id: '7', checked: true }],
        buttons: ['approved', 'rejected'].map((value) => ({ attrs: { name: 'status', value } })),
        respond: { code: 200, message: '审核完成', data: { Affected: 1, Status: 'approved' } },
    });
    for (const btn of env.buttons) { env.fireWith(btn); await tick(); }
    env.fireEnter(); await tick();
    assert.deepEqual(env.requests.map((req) => req.body), [
        { projectId: 'p1', status: 'approved', ids: [7] },
        { projectId: 'p1', status: 'rejected', ids: [7] },
        { projectId: 'p1', status: 'pending', ids: [7] },
    ]);
});

test('review 大写响应与零 affected 显示服务端成品，不猜跳过或移除行', async () => {
    for (const affected of [1, 0]) {
        const env = setup({
            attrs: { 'data-write-api': '/api/comment/review', 'data-write-mode': 'bulk', 'data-write-outcome': 'review', 'data-msg-refresh': '刷新列表', 'data-msg-none-selected': '未选择', 'data-msg-all-deleted': '已删除 {count}', 'data-msg-all-skipped': '跳过 {skipped}' },
            bulkRows: [{ id: '7', checked: true }, { id: '8', checked: true }],
            respond: { code: 200, message: '已驳回选中的评论。', data: { Affected: affected, Status: 'rejected' } },
        });
        env.fire(); await tick();
        assert.equal(env.status.children[0].textContent, '已驳回选中的评论。');
        assert.equal(env.status.getAttribute('data-write-affected'), String(affected));
        assert.equal(env.status.getAttribute('data-write-review-status'), 'rejected');
        assert.equal(env.status.getAttribute('data-write-state'), 'ok');
        assert.equal(env.status.querySelector('[data-write-reasons]'), null);
        assert.ok(env.rows.every((row) => row.parentNode && row.children[0].checked));
        assert.equal(env.calls.reload, 0);
        env.status.querySelector('[data-write-refresh]').click();
        assert.equal(env.calls.reload, 1);
    }
});

test('review HTML 失败保留输入选择，恢复锁按钮且无刷新入口', async () => {
    const env = setup({
        attrs: { 'data-write-api': '/api/comment/review', 'data-write-mode': 'bulk', 'data-write-outcome': 'review', 'data-write-extra': 'status', 'data-msg-failed': '操作失败', 'data-msg-refresh': '刷新列表' },
        createFields: [{ name: 'note', value: '保留的输入' }], bulkRows: [{ id: '7', checked: true }],
        buttons: [{ attrs: { name: 'status', value: 'rejected' } }], responseText: '<html>gateway failed</html>',
    });
    env.fireWith(env.buttons[0]); await tick();
    assert.equal(env.status.textContent, '操作失败');
    assert.equal(env.form.querySelector('[name="note"]').value, '保留的输入');
    assert.ok(env.rows[0].parentNode && env.rows[0].children[0].checked);
    assert.equal(env.buttons[0].value, 'rejected');
    assert.equal(env.buttons[0].disabled, false);
    assert.equal(env.form.getAttribute('aria-busy'), null);
    assert.equal(env.status.querySelector('[data-write-refresh]'), null);
    assert.equal(env.calls.reload, 0);
    assert.equal(env.requests[0].options.headers['X-CSRF-Token'], 'TOK-123');
    env.fireWith(env.buttons[0]); await tick();
    assert.equal(env.requests.length, 2);
});

test('review 继续遵守无选择、超限、确认、inFlight；失败清除上一笔回执属性', async () => {
    const attrs = { 'data-write-api': '/api/comment/review', 'data-write-mode': 'bulk', 'data-write-outcome': 'review', 'data-msg-none-selected': '未选择', 'data-msg-too-many': '最多 {max}，当前 {count}', 'data-msg-failed': '失败' };
    for (const count of [0, 201]) {
        const env = setup({ attrs, bulkRows: Array.from({ length: count }, (_, i) => ({ id: String(i), checked: true })) });
        env.fire(); await tick();
        assert.equal(env.requests.length, 0);
        assert.equal(env.status.textContent, count ? '最多 200，当前 201' : '未选择');
        assert.equal(env.form.getAttribute('aria-busy'), null);
    }
    const env = setup({ attrs: Object.assign({}, attrs, { 'data-confirm': '确认审核？' }), bulkRows: [{ id: '7', checked: true }], deferFetch: true });
    env.fire(); await tick();
    assert.equal(env.requests.length, 0);
    assert.ok(env.dialog.open);
    env.acceptConfirm(); await tick();
    env.fireEnter(); env.acceptConfirm(); await tick();
    assert.equal(env.requests.length, 1);
    env.resolveFetch({ code: 200, message: '完成', data: { Affected: 0, Status: 'approved' } });
    await tick();
    assert.equal(env.status.getAttribute('data-write-affected'), '0');
    env.fire(); env.acceptConfirm(); await tick();
    assert.equal(env.requests.length, 2);
    env.resolveFetch({ code: 409, message: '失败' }); await tick();
    assert.equal(env.status.getAttribute('data-write-affected'), null);
    assert.equal(env.status.getAttribute('data-write-review-status'), null);
    assert.ok(env.rows[0].parentNode && env.rows[0].children[0].checked);
    assert.equal(env.form.getAttribute('aria-busy'), null);
});

function openingTag(html, anchor) {
    const start = html.indexOf(anchor);
    assert.ok(start >= 0, `模板里找不到 ${anchor}`);
    const end = html.indexOf('>', start);
    assert.ok(end > start, `模板里 ${anchor} 的开标签没闭合`);
    return html.slice(start, end + 1);
}

function tagAttrs(tag) {
    const out = {};
    const re = /([\w-]+)(?:="([^"]*)")?/g;
    let m;
    while ((m = re.exec(tag))) { out[m[1]] = m[2] === undefined ? '' : m[2]; }
    return out;
}

// stripJet 把 Jet 表达式换成稳定样本：{{c.ID}} → 7（行内 id 的语义），其余 → TXT。
// 属性值与 id 都要是真数字形状，否则测的就不是「模板是否会发字符串」这件事。
function stripJet(text) {
    return String(text)
        .replace(/\{\{\s*c\.ID\s*\}\}/g, '7')
        .replace(/\{\{[^}]*\}\}/g, 'TXT');
}

function controlsOf(block) {
    const out = [];
    const re = /<(input|select|textarea)\b[^>]*>/g;
    let m;
    while ((m = re.exec(block))) {
        const attrs = tagAttrs(m[0]);
        if (!attrs.name) { continue; }
        out.push({ tag: m[1], name: attrs.name, type: attrs.type || '', value: attrs.value === undefined ? '' : attrs.value });
    }
    return out;
}

function formBlockOf(html, anchor, fromIndex) {
    const start = html.indexOf(anchor, fromIndex || 0);
    assert.ok(start >= 0, `模板里找不到 ${anchor}`);
    const end = html.indexOf('</form>', start);
    assert.ok(end > start, `模板里 ${anchor} 的 </form> 没找到`);
    const raw = html.slice(start, end + '</form>'.length);
    return { html: raw, attrs: tagAttrs(stripJet(openingTag(raw, anchor))), controls: controlsOf(stripJet(raw)) };
}

test('真模板 → payload：mail_contacts.html 的行内单删 / 两个保存抽屉发数字 id，批量仍发字符串 ids', async () => {
    const tplPath = __dirname + '/../../../admin/mail/mail_contacts.html';
    const html = fs.readFileSync(tplPath, 'utf8');

    // 真模板里的确认框由 ui/confirm.js 拦下那一次提交；这里的 submitAndConfirm 走的就是
    // 页面上真实发生的那条路（提交 → 弹确认 → 确认后重派发 → json_write 发请求）。
    const settle = async (env, action) => {
        action();
        env.acceptConfirm();
        await tick();
    };

    // ① 行内单条删除（single）：真实 form 的属性 + 真实的 hidden id
    const del = formBlockOf(html, '<form id="contact-del-');
    assert.equal(del.attrs['data-write-mode'], 'single');
    assert.equal(del.attrs['data-write-number'], 'id', '单删表单必须点名 id 是数字：严格端点拒绝 "7"');
    assert.equal(del.attrs['data-write-api'], '/api/mail/contact/delete');
    const idField = del.controls.find((c) => c.name === 'id');
    assert.ok(idField, '单删表单必须有 name="id" 的隐藏域');
    const delEnv = setup({
        attrs: del.attrs,
        createFields: del.controls,
        id: idField.value,
        respond: { code: 200, data: {} },
    });
    await settle(delEnv, () => delEnv.fire());
    assert.deepEqual(delEnv.requests[0].body, { id: 7 }, '真模板单删必须发 {"id":7}（数字）');

    // ② 编辑抽屉（create，id>0）
    const edit = formBlockOf(html, '<form data-write-api="/api/mail/contact/save" data-write-mode="create" data-write-number="id"');
    assert.equal(edit.attrs['data-write-number'], 'id');
    const editEnv = setup({ attrs: edit.attrs, createFields: edit.controls, respond: { code: 200, data: {} } });
    editEnv.fire();
    await tick();
    assert.equal(editEnv.requests[0].body.id, 7, '编辑抽屉必须发数字 id');
    assert.equal(typeof editEnv.requests[0].body.email, 'string', '其余字段保持字符串（端点按字符串白名单校验）');

    // ③ 新建抽屉（create，id=0）：两处 save 表单属性完全相同（字段清单刻意不分叉），取第二处真表单
    const saveAnchor = '<form data-write-api="/api/mail/contact/save" data-write-mode="create" data-write-number="id"';
    const firstSave = html.indexOf(saveAnchor);
    assert.ok(firstSave >= 0, '模板里找不到 save 表单');
    const newForm = formBlockOf(html, saveAnchor, firstSave + 1);
    const newId = newForm.controls.find((c) => c.name === 'id');
    assert.ok(newId, '新建抽屉必须有 name="id" 的隐藏域');
    assert.equal(Number(newId.value), 0, '新建抽屉的 id 必须是 0');
    const createEnv = setup({ attrs: newForm.attrs, createFields: newForm.controls, respond: { code: 200, data: {} } });
    createEnv.fire();
    await tick();
    assert.equal(createEnv.requests[0].body.id, 0, '新建抽屉 id=0 必须是数字 0（不是 "0"）');

    // ④ 批量删除：三个批量动作在**同一个表单**里靠按钮级声明区分（表单本身来自 bulkBarOpen，
    //    页面上没有 form 级 data-write-*），ids 是字符串数组、也不参与数字转换。
    const bulkDelBtn = tagAttrs(stripJet(openingTag(html, '<button type="submit" class="btn btn-sm btn-danger"')));
    assert.equal(bulkDelBtn['data-write-api'], '/api/mail/contact/delete');
    assert.equal(bulkDelBtn['data-write-number'], undefined, '批量档的 ids 不参与数字转换');
    const bulkEnv = setup({
        attrs: {},
        bulkRows: [{ id: '11', checked: true }, { id: '12', checked: true }],
        buttons: [{ attrs: bulkDelBtn }],
        respond: { code: 200, data: { deleted: 2, skipped: 0, items: [] } },
    });
    await settle(bulkEnv, () => bulkEnv.click(bulkEnv.buttons[0]));
    assert.deepEqual(bulkEnv.requests[0].body.ids, ['11', '12'], '批量 ids 保持字符串数组');
    assert.equal(bulkEnv.requests[0].url, '/api/mail/contact/delete');

    // ⑤ 批量打标签：ids 字符串数组 + add/remove 字符串（端点这两字段就是字符串，',' / ';' 分隔）
    const tagBtn = tagAttrs(stripJet(openingTag(html, '<button type="submit" class="btn btn-sm" data-requires-js hidden')));
    assert.equal(tagBtn['data-write-api'], '/api/mail/contact/tag');
    const tagEnv = setup({
        attrs: {},
        createFields: [{ name: 'add', value: 'vip' }, { name: 'remove', value: '' }],
        bulkRows: [{ id: '11', checked: true }],
        buttons: [{ attrs: tagBtn }],
        respond: { code: 200, data: { updated: 1, skipped: 0, items: [] } },
    });
    await settle(tagEnv, () => tagEnv.click(tagEnv.buttons[0]));
    // remove 空串也照发（点名的字段按名带上，值就是空串）：端点只把「add 与 remove 都为空」
    // 判成空差量 400，单个为空是正常操作（只加不减）。
    assert.deepEqual(tagEnv.requests[0].body, { add: 'vip', remove: '', ids: ['11'] },
        '打标签：add/remove 是字符串、ids 是字符串数组');
    assert.equal(tagEnv.requests[0].url, '/api/mail/contact/tag');
});

// —— data-write-rows：声明式行数组（整表提交） ——
//
// 这一批守的是「整表提交」这条新路上的四类**不会有人发现**的错：
//   1. 行字段同时被当成顶层标量发出去 —— 严格白名单的端点只会整条请求拒掉，
//      而页面上看到的是一句通用失败；
//   2. 容器的顺序被打乱 —— 译文会写到别的菜单文字上（哈希与译文错配）；
//   3. 「空 target 的一行」被丢掉 —— 运营清空某行译文时，那一行永远清不掉；
//   4. 畸形声明（缺选择器 / 缺字段名）被当成「发了空 rows」—— 端点会拒，页面却不知道为什么。

test('data-write-rows：容器一行一对象、行字段不进顶层标量、空译文仍发、缺字段/空容器按行跳过', async () => {
    const env = setup({
        attrs: {
            'data-write-api': '/api/navigation/update', 'data-write-mode': 'create',
            'data-write-rows': '.nav-tr-row;context,hash,target',
        },
        createFields: [
            { name: 'action', value: 'translations' },
            { name: 'lang', value: 'en-US' },
            { name: 'project', value: 'p-1' },
        ],
        rowBoxes: [
            { cls: 'nav-tr-row', fields: { context: 'navigation.label', hash: 'h-1', target: 'Home' } },
            { cls: 'nav-tr-row', fields: { context: 'navigation.label', hash: 'h-2', target: '' } },
            { cls: 'nav-tr-row', fields: { context: 'navigation.label', hash: 'h-3' } },
            { cls: 'nav-tr-row', fields: {} },
        ],
        respond: { code: 200, data: { written: 1 } },
    });
    env.fire();
    await tick();
    assert.deepEqual(env.requests[0].body, {
        action: 'translations', lang: 'en-US', project: 'p-1',
        rows: [
            { context: 'navigation.label', hash: 'h-1', target: 'Home' },
            { context: 'navigation.label', hash: 'h-2', target: '' },
            { context: 'navigation.label', hash: 'h-3' },
        ],
    }, '行数组按 DOM 顺序；空译文照发（服务端把空译文当「不改动」）；缺字段的行只带有的字段；全缺的容器不出现');
});


// 属性值编辑器那种行：name 是 values[0].key 这种**带索引的扁平名**，每行下标都不同 ——
// 固定 name 匹配不到。payload 的键也不是 rows（端点要 values），由第三个字段指定。
test('data-write-rows：带索引的扁平名按 .后缀取字段、第三段改名 payload 键、行内 sort 转数字', async () => {
    const env = setup({
        attrs: {
            'data-write-api': '/api/product/attribute/create', 'data-write-mode': 'form',
            'data-write-rows': '.attr-row;id,key,label,sort,enabled;values',
            'data-write-number': 'sort', 'data-write-bool': 'enabled',
        },
        createFields: [{ name: 'name', value: '颜色' }, { name: 'projectId', value: 'p1' }],
        rowBoxes: [
            { cls: 'attr-row', fields: { 'values[0].id': '', 'values[0].key': 'red', 'values[0].label': '红色', 'values[0].sort': '0', 'values[0].enabled': '1' } },
            { cls: 'attr-row', fields: { 'values[1].id': 'v-2', 'values[1].key': 'blue', 'values[1].label': '蓝色', 'values[1].sort': '3', 'values[1].enabled': '0' } },
        ],
        respond: { code: 200, data: { id: 'g1' } },
    });
    env.fire();
    await tick();
    assert.deepEqual(env.requests[0].body, {
        name: '颜色', projectId: 'p1',
        values: [
            { id: '', key: 'red', label: '红色', sort: 0, enabled: true },
            { id: 'v-2', key: 'blue', label: '蓝色', sort: 3, enabled: false },
        ],
    }, '带索引的名字按 .后缀取字段；索引名一律不进顶层标量；行内 sort/enabled 按点名转类型');
});

test('没有 data-write-rows：create 档一个字节都不变（重复 name 仍是后者覆盖、不发 rows）', async () => {
    const env = setup({
        attrs: { 'data-write-api': '/api/navigation/update', 'data-write-mode': 'create' },
        createFields: [{ name: 'action', value: 'translations' }, { name: 'x', value: '1' }, { name: 'x', value: '2' }],
        respond: { code: 200, data: {} },
    });
    env.fire();
    await tick();
    assert.deepEqual(env.requests[0].body, { action: 'translations', x: '2' },
        '没声明行数组的页面必须与迁移前逐字一致（「重复 name 合成数组」不是本控件的行为）');
});

test('data-write-rows 声明畸形（缺字段名 / 缺选择器）＝没声明：不发 rows，字段回落顶层', async () => {
    const noFields = setup({
        attrs: { 'data-write-api': '/api/navigation/update', 'data-write-mode': 'create', 'data-write-rows': '.nav-tr-row' },
        createFields: [{ name: 'action', value: 'translations' }],
        rowBoxes: [{ cls: 'nav-tr-row', fields: { hash: 'h-1' } }],
        respond: { code: 200, data: {} },
    });
    noFields.fire();
    await tick();
    assert.equal(noFields.requests[0].body.rows, undefined, '缺字段名的声明不该发出一个形状可疑的 rows');
    assert.equal(noFields.requests[0].body.hash, 'h-1', '解析不出声明时字段按顶层标量走（与迁移前同口径）');

    const noSelector = setup({
        attrs: { 'data-write-api': '/api/navigation/update', 'data-write-mode': 'create', 'data-write-rows': ';context,hash,target' },
        createFields: [{ name: 'action', value: 'translations' }],
        rowBoxes: [{ cls: 'nav-tr-row', fields: { hash: 'h-1' } }],
        respond: { code: 200, data: {} },
    });
    noSelector.fire();
    await tick();
    assert.equal(noSelector.requests[0].body.rows, undefined, '缺选择器的声明同样按「没声明」处理');
});

// 真模板集成：导航译文工作台整表提交。这里同时钉住三件事 ——
//   ① 表单声明的四个属性（端点 / 模式 / 行数组 / 成功回跳）与模板逐字一致；
//   ② 保存按钮**外置**（form="nav-translations-form"）时 submitter 仍能解析到表单声明；
//   ③ 无 JS 时按钮是真 disabled，有 JS 时才既可见又可点。
test('真模板 → payload：navigation_translations.html 整表提交发 rows，外置按钮关联表单，禁用 JS 时按钮真 disabled', async () => {
    const tplPath = __dirname + '/../../../admin/navigation/navigation_translations.html';
    const html = fs.readFileSync(tplPath, 'utf8');

    const form = formBlockOf(html, '<form id="nav-translations-form"');
    assert.equal(form.attrs['data-write-api'], '/api/navigation/update');
    assert.equal(form.attrs['data-write-mode'], 'create');
    assert.equal(form.attrs['data-write-rows'], '.nav-tr-row;context,hash,target',
        '行数组必须点名声明的三个字段：这就是端点严格白名单里的行键');
    assert.ok(form.attrs['data-write-nav'], '成功必须跳回带筛选的 GET（回跳地址由服务端拼好并转义）');

    // 行容器与字段名取自真模板（值被 stripJet 成 TXT，断言的是名字与形状）。
    const rowBlock = formBlockOf(html, '<div class="field nav-tr-row"').controls;
    const names = rowBlock.map((c) => c.name);
    assert.deepEqual(names.slice().sort(), ['context', 'hash', 'target'],
        '一行就是一个对象：三个具名字段，没有并行数组那种按下标对齐的隐式契约');

    const saveBtn = tagAttrs(stripJet(openingTag(html, '<button type="submit" form="nav-translations-form"')));
    assert.equal(saveBtn.form, 'nav-translations-form', '保存按钮在表单外，靠 form 属性关联');
    assert.ok(saveBtn['data-requires-js'] !== undefined, '按钮必须带 data-requires-js');
    assert.ok(saveBtn.disabled !== undefined, '无 JS 时按钮必须是真的 disabled（浏览器对 disabled 的默认提交按钮不做隐式提交）');

    const env = setup({
        attrs: form.attrs,
        createFields: form.controls,
        rowBoxes: [
            { cls: 'nav-tr-row', fields: { context: 'navigation.label', hash: 'h-1', target: 'Home' } },
            { cls: 'nav-tr-row', fields: { context: 'navigation.label', hash: 'h-2', target: 'Cart' } },
        ],
        respond: { code: 200, data: { written: 2 } },
    });
    // 桩不是浏览器：属性 → 属性的落地（disabled/hidden）要自己还原，才测得到 [data-requires-js] 的放行。
    const btn = makeEl('button', saveBtn);
    btn.disabled = true;
    btn.hidden = true;
    btn.form = env.form;
    env.WBUI.scan({ querySelectorAll: (sel) => (matches(btn, sel) ? [btn] : []) });
    assert.equal(btn.disabled, false, '有 JS 时必须摘掉 disabled，否则按钮永远点不动');
    assert.equal(btn.hidden, false, '有 JS 时必须放出来');

    // 外置按钮提交：真实浏览器里 e.target 仍是它 form 属性指向的那个表单。
    env.fireWith(btn);
    await tick();
    assert.equal(env.requests[0].url, '/api/navigation/update');
    assert.deepEqual(env.requests[0].body.rows, [
        { context: 'navigation.label', hash: 'h-1', target: 'Home' },
        { context: 'navigation.label', hash: 'h-2', target: 'Cart' },
    ], '外置按钮提交也必须带上行数组（submitter 关联解析到表单级声明）');
    assert.equal(env.requests[0].body.context, undefined, '行字段不许同时出现在顶层');
    assert.equal(env.calls.assign.length, 1, '成功后按 data-write-nav 跳回带筛选的 GET');
});

// —— 分支 3b：bulk + outcome=add（来源批量加入菜单） ——
//
// outcome 决定「成功计数键」与成品句槽：add 读 data.added 与 data-msg-all-added，
// **绝不回落 all-deleted** —— 「已加入 3 项」显示成「已删除 3 项」正是这一档存在的理由。
// 加入菜单是非破坏动作：服务端结果里即使每条都 added=true，也一**行都不许移除**。

test('outcome=add：计数读 data.added、摘要走 all-added 槽、且不移除任何行', async () => {
    const env = setup({
        attrs: {
            'data-write-api': '/api/navigation/create', 'data-write-mode': 'bulk',
            'data-write-outcome': 'add',
            'data-write-extra': 'action,projectId,kind,sourceType',
            'data-msg-all-added': '已加入 {count} 项。',
            'data-msg-all-deleted': '已删除 {count} 项。',   // 在场也不该被用（防回落）
            'data-msg-refresh': '刷新列表',
        },
        createFields: [
            { name: 'action', value: 'addSource' },
            { name: 'projectId', value: 'p-1' },
            { name: 'kind', value: 'header' },
            { name: 'sourceType', value: 'page' },
        ],
        bulkRows: [
            { id: 'pg-1', checked: true },
            { id: 'pg-2', checked: true },
            { id: 'pg-3', checked: false },
        ],
        respond: {
            code: 200,
            data: {
                added: 2, skipped: 0,
                items: [{ id: 'pg-1', added: true }, { id: 'pg-2', added: true }],
            },
        },
    });
    env.fire();
    await tick();
    assert.deepEqual(env.requests[0].body, {
        action: 'addSource', projectId: 'p-1', kind: 'header', sourceType: 'page',
        ids: ['pg-1', 'pg-2'],
    }, '批量档：extra 点名的字段 + 恒为 ids 的批量主体（后写，不被覆盖）');
    assert.equal(env.status.textContent.indexOf('已加入 2 项。'), 0, '摘要必须用 all-added 槽，而不是「已删除」');
    assert.ok(!env.status.textContent.includes('已删除'), '哪怕 all-deleted 槽在场也不许回落');
    assert.ok(env.rows[0].parentNode && env.rows[1].parentNode, 'add 是非破坏动作：行不能被移除');
    assert.ok(env.status.querySelector('[data-write-refresh]'), '不自动刷新，留一个刷新的入口');
});

test('outcome=add 部分成功：{added}/{skipped} 都填对，逐条原因全部列出', async () => {
    const env = setup({
        attrs: {
            'data-write-api': '/api/navigation/create', 'data-write-mode': 'bulk',
            'data-write-outcome': 'add',
            'data-msg-partial': '已加入 {added} 项，{skipped} 项未能加入。',
        },
        bulkRows: [{ id: 'pg-1', checked: true }, { id: 'pg-2', checked: true }, { id: 'pg-4', checked: true }],
        respond: {
            code: 200,
            data: {
                added: 1, skipped: 2,
                items: [
                    { id: 'pg-1', added: true },
                    { id: 'pg-2', added: false, message: '（暂无公开路径，先发布后再添加）' },
                    { id: 'pg-4', added: false, message: '请求资源不存在' },
                ],
            },
        },
    });
    env.fire();
    await tick();
    assert.equal(env.status.textContent.indexOf('已加入 1 项，2 项未能加入。'), 0,
        '计数必须同时读 added 与 skipped（读 deleted 会显示 0）');
    assert.match(env.status.textContent, /暂无公开路径/, '逐条原因要显示出来（否则运营不知道哪几条没进菜单）');
    assert.match(env.status.textContent, /请求资源不存在/);
});

test('outcome=add 全部被跳过：走 all-skipped 槽且 {count} 填的是总条数', async () => {
    const env = setup({
        attrs: {
            'data-write-api': '/api/navigation/create', 'data-write-mode': 'bulk',
            'data-write-outcome': 'add',
            'data-msg-all-skipped': '{count} 项都未能加入，菜单未改动。',
        },
        bulkRows: [{ id: 'pg-2', checked: true }, { id: 'pg-4', checked: true }],
        respond: {
            code: 200,
            data: {
                added: 0, skipped: 2,
                items: [
                    { id: 'pg-2', added: false, message: '（暂无公开路径，先发布后再添加）' },
                    { id: 'pg-4', added: false, message: '请求资源不存在' },
                ],
            },
        },
    });
    env.fire();
    await tick();
    assert.equal(env.status.textContent.indexOf('2 项都未能加入，菜单未改动。'), 0);
});

test('outcome=add 一条都没勾：用 none-selected 槽就地提示，不发请求', async () => {
    const env = setup({
        attrs: {
            'data-write-api': '/api/navigation/create', 'data-write-mode': 'bulk',
            'data-write-outcome': 'add',
            'data-msg-none-selected': '请至少勾选一项要加入菜单的内容。',
        },
        bulkRows: [{ id: 'pg-1', checked: false }],
        respond: { code: 200, data: { added: 0, skipped: 0, items: [] } },
    });
    env.fire();
    await tick();
    assert.equal(env.requests.length, 0, '本地就能判定的空批量不该发请求');
    assert.equal(env.status.textContent, '请至少勾选一项要加入菜单的内容。');
});

test('outcome=add 缺 all-added 槽时不给句子（宁可少说，也不回落成「已删除」）', async () => {
    const env = setup({
        attrs: {
            'data-write-api': '/api/navigation/create', 'data-write-mode': 'bulk',
            'data-write-outcome': 'add',
            'data-msg-all-deleted': '已删除 {count} 项。',   // 唯一的成品句是「删除」口径
            'data-msg-refresh': '刷新列表',
        },
        bulkRows: [{ id: 'pg-1', checked: true }],
        respond: { code: 200, data: { added: 1, skipped: 0, items: [{ id: 'pg-1', added: true }] } },
    });
    env.fire();
    await tick();
    // 结果区只剩刷新入口的文字（textContent 会拼上按钮文本）：关键是**没有**「已删除」那句。
    assert.equal(env.status.textContent, '刷新列表', '非破坏动作缺自己的成品句时不许借用删除的句子');
    assert.ok(!env.status.textContent.includes('已删除'));
    assert.ok(env.status.querySelector('[data-write-refresh]'), '结果区仍要留刷新入口，不能什么都不显示');
});

test('data-write-bool：点名的字段按布尔发（create 档），未勾选的不进 body，没点名的原样发字符串', async () => {
    // 与 data-write-number 同一条理由：DOM 里的值永远是字符串，而严格 API 的布尔字段
    // 收到 "1" 会整条 400（退货页「同意并立即完成入库 + 退款」的 autoReceive 就是）。
    const checked = setup({
        attrs: {
            'data-write-api': '/api/order/return/approve', 'data-write-mode': 'create',
            'data-write-number': 'returnId', 'data-write-bool': 'autoReceive',
        },
        createFields: [
            { name: 'returnId', value: '42' },
            { name: 'remark', value: '已确认货在仓' },
            { name: 'autoReceive', type: 'checkbox', value: '1', checked: true },
        ],
        respond: { code: 200, data: {} },
    });
    checked.fire();
    await tick();
    assert.deepEqual(checked.requests[0].body, { returnId: 42, remark: '已确认货在仓', autoReceive: true },
        '点名的布尔字段必须是 JSON true，数字字段仍按数字，文本原样');
    assert.equal(typeof checked.requests[0].body.autoReceive, 'boolean');

    const unchecked = setup({
        attrs: {
            'data-write-api': '/api/order/return/approve', 'data-write-mode': 'create',
            'data-write-number': 'returnId', 'data-write-bool': 'autoReceive',
        },
        createFields: [
            { name: 'returnId', value: '42' },
            { name: 'autoReceive', type: 'checkbox', value: '1', checked: false },
        ],
        respond: { code: 200, data: {} },
    });
    unchecked.fire();
    await tick();
    assert.deepEqual(unchecked.requests[0].body, { returnId: 42 },
        '未勾选的复选框不发 —— 点名也只处理已经存在的字段，不许凭空造出 autoReceive:false');

    const undeclared = setup({
        attrs: { 'data-write-api': '/api/order/return/approve', 'data-write-mode': 'create' },
        createFields: [{ name: 'autoReceive', type: 'checkbox', value: '1', checked: true }],
        respond: { code: 200, data: {} },
    });
    undeclared.fire();
    await tick();
    assert.deepEqual(undeclared.requests[0].body, { autoReceive: '1' },
        '没点名就不许改类型（严格 API 自己回 400，页面不替它猜）');
});

test('data-write-bool 的边界：认不出的取值原样发字符串、空串按 false、bulk 档的 extra 字段同样生效', async () => {
    const falsey = setup({
        attrs: { 'data-write-api': '/api/order/return/approve', 'data-write-mode': 'create', 'data-write-bool': 'autoReceive' },
        createFields: [
            { tag: 'select', name: 'autoReceive', value: 'no' },
        ],
        respond: { code: 200, data: {} },
    });
    falsey.fire();
    await tick();
    assert.equal(falsey.requests[0].body.autoReceive, false, 'no / off / 0 / false 都是假');
    assert.equal(typeof falsey.requests[0].body.autoReceive, 'boolean');

    const empty = setup({
        attrs: { 'data-write-api': '/api/order/return/approve', 'data-write-mode': 'create', 'data-write-bool': 'autoReceive' },
        createFields: [{ tag: 'select', name: 'autoReceive', value: '' }],
        respond: { code: 200, data: {} },
    });
    empty.fire();
    await tick();
    assert.equal(empty.requests[0].body.autoReceive, false, '空串（「不选」）就是 false，不是缺失');

    // 认不出的取值不许被悄悄判成 false：那会让「没生效」看起来像「按我说的做了」。
    for (const raw of ['maybe', '2', 'yes please']) {
        const weird = setup({
            attrs: { 'data-write-api': '/api/order/return/approve', 'data-write-mode': 'create', 'data-write-bool': 'autoReceive' },
            createFields: [{ name: 'autoReceive', value: raw }],
            respond: { code: 200, data: {} },
        });
        weird.fire();
        await tick();
        assert.equal(weird.requests[0].body.autoReceive, raw, `认不出的 ${raw} 应原样发字符串`);
    }

    const spaced = setup({
        attrs: { 'data-write-api': '/api/order/return/approve', 'data-write-mode': 'create', 'data-write-bool': 'autoReceive' },
        createFields: [{ name: 'autoReceive', value: ' TRUE ' }],
        respond: { code: 200, data: {} },
    });
    spaced.fire();
    await tick();
    assert.equal(spaced.requests[0].body.autoReceive, true, '大小写与首尾空白不影响判定');

    const bulk = setup({
        attrs: {
            'data-write-api': '/api/order/return/approve', 'data-write-mode': 'bulk',
            'data-write-extra': 'remark,autoReceive', 'data-write-bool': 'autoReceive',
        },
        bulkRows: [{ id: '7', checked: true }, { id: '8', checked: true }],
        createFields: [
            { name: 'remark', value: '批量同意' },
            { name: 'autoReceive', type: 'checkbox', value: '1', checked: true },
        ],
        respond: { code: 200, data: { updated: 2, skipped: 0, items: [] } },
    });
    bulk.fire();
    await tick();
    assert.deepEqual(bulk.requests[0].body, { remark: '批量同意', autoReceive: true, ids: ['7', '8'] },
        'bulk 档的 data-write-extra 字段同样按点名转布尔，ids 永远最后写');
});

test('named create payload builder converts fields before fetch', async () => {
    const env = setup({ attrs: { 'data-write-api': '/api/test', 'data-write-mode': 'create', 'data-write-builder': 'fixture' }, respond: { code: 200, data: {} } });
    env.sandbox.WPJsonWrite.registerPayloadBuilder('fixture', (form, submitter, payload) => ({ amount: 123, items: [{ kind: 'discount', valueInt: 5 }] }));
    env.fire();
    await tick();
    assert.deepEqual(env.requests[0].body, { amount: 123, items: [{ kind: 'discount', valueInt: 5 }] });
});

test('unserializable builder payload releases locks and permits retry', async () => {
    for (const kind of ['bigint', 'cycle']) {
        const env = setup({ attrs: { 'data-write-api': '/api/test', 'data-write-mode': 'create', 'data-write-builder': 'fixture', 'data-msg-failed': 'Failed' }, respond: { code: 200 } });
        env.sandbox.WPJsonWrite.registerPayloadBuilder('fixture', () => {
            if (kind === 'bigint') { return { id: 1n }; }
            const payload = {};
            payload.self = payload;
            return payload;
        });
        assert.doesNotThrow(() => env.fire());
        await tick();
        assert.equal(env.requests.length, 0);
        assert.equal(env.submit.disabled, false);
        assert.equal(env.form.getAttribute('aria-busy'), null);
        assert.equal(env.status.textContent, 'Failed');
        env.sandbox.WPJsonWrite.registerPayloadBuilder('fixture', () => ({ valid: true }));
        env.fire();
        await tick();
        assert.equal(env.requests.length, 1);
    }
});

test('missing or throwing payload builder blocks fetch and releases lock', async () => {
    for (const throwing of [false, true]) {
        const env = setup({ attrs: { 'data-write-api': '/api/test', 'data-write-mode': 'create', 'data-write-builder': 'fixture', 'data-msg-failed': 'Failed' }, respond: { code: 200 } });
        if (throwing) { env.sandbox.WPJsonWrite.registerPayloadBuilder('fixture', () => { throw new Error('private error'); }); }
        env.fire();
        await tick();
        assert.equal(env.requests.length, 0);
        assert.equal(env.submit.disabled, false);
        env.sandbox.WPJsonWrite.registerPayloadBuilder('fixture', () => ({ valid: true }));
        env.fire();
        await tick();
        assert.equal(env.requests.length, 1);
    }
});

// —— data-write-object：声明式嵌套对象（ruleParams 这类结构参数） ——

test('data-write-object：容器字段按驱动选项的 data-rule-keys 收成对象，容器内字段不进顶层', async () => {
    const env = setup({
        attrs: {
            'data-write-api': '/api/product/tag/create',
            'data-write-mode': 'create',
            'data-write-object': '.tag-rule-fields;ruleParams;ruleType;kind=rule',
            'data-write-number': 'sort',
        },
        createFields: [
            { name: 'name', value: '新品' },
            { name: 'slug', value: 'new' },
            { name: 'kind', value: 'rule' },
            { name: 'sort', value: '3' },
        ],
        objBox: {
            cls: 'tag-rule-fields',
            driver: 'ruleType',
            options: [
                { value: '', selected: true },
                { value: 'new_arrival', keys: 'days' },
                { value: 'price_range', keys: 'minPrice,maxPrice' },
            ],
            fields: { days: '30', minPrice: '', maxPrice: '' },
        },
        respond: { code: 200, data: { id: 't1' } },
    });
    // 选中「新品」那一项（桩里 selectedOptions 是数组，直接改它就是真实浏览器的等价动作）。
    const box = env.form.querySelector('.tag-rule-fields');
    const sel = box.querySelectorAll('[name]').find((el) => el.getAttribute('name') === 'ruleType');
    sel.options.forEach((o) => { o.selected = o.value === 'new_arrival'; });
    sel.selectedOptions = sel.options.filter((o) => o.selected);
    sel.value = 'new_arrival';

    env.fire(); await tick();
    assert.deepEqual(env.requests[0].body, {
        name: '新品', slug: 'new', kind: 'rule', sort: 3,
        ruleType: 'new_arrival', ruleParams: { days: 30 },
    });
    // 容器内没点名的输入框（minPrice/maxPrice 空）既不进对象也不进顶层。
    assert.equal('minPrice' in env.requests[0].body, false);
    assert.equal('days' in env.requests[0].body, false);
});


test('data-write-object：条件不满足时对象与驱动字段一起不发（手工标签带 ruleType 会被服务端拒）', async () => {
    for (const kind of ['manual', 'rule']) {
        const env = setup({
            attrs: {
                'data-write-api': '/api/product/tag/update',
                'data-write-mode': 'create',
                'data-write-object': '.tag-rule-fields;ruleParams;ruleType;kind=rule',
            },
            createFields: [
                { name: 'id', value: 't1' },
                { name: 'name', value: '促销' },
                { name: 'kind', value: kind },
            ],
            objBox: {
                cls: 'tag-rule-fields',
                driver: 'ruleType',
                options: [
                    { value: '', keys: '', selected: true },
                    { value: 'on_sale', keys: '' },
                ],
                fields: { days: '7' },
            },
            respond: { code: 200, data: {} },
        });
        env.fire(); await tick();
        const body = env.requests[0].body;
        if (kind === 'manual') {
            assert.equal('ruleType' in body, false);
            assert.equal('ruleParams' in body, false);
        } else {
            // on_sale 无参数：仍然发空对象 —— 缺 ruleParams 是「没给规则定义」，空对象才是「无参规则」。
            assert.equal(body.ruleType, '');
            assert.deepEqual(body.ruleParams, {});
        }
        // 容器内的 days 任何情况下都不进顶层。
        assert.equal('days' in body, false);
    }
});

// —— data-write-array / data-write-float / data-write-skip-empty：商品表单的三档字段 ——

test('data-write-array：同名复选框收成数组（不再后者覆盖），一个都没勾时发空数组', async () => {
    for (const picked of [['t1', 't2'], []]) {
        const env = setup({
            attrs: {
                'data-write-api': '/api/product/update',
                'data-write-mode': 'form',
                'data-write-array': 'tagIds,categoryIds',
            },
            createFields: [
                { name: 'name', value: '商品' },
                ...['t1', 't2', 't3'].map((id) => ({ name: 'tagIds', value: id, type: 'checkbox', checked: picked.includes(id) })),
            ],
            respond: { code: 200, data: {} },
        });
        env.fire(); await tick();
        assert.deepEqual(env.requests[0].body.tagIds, picked);
        // 点名的数组字段恒存在：nil 是「本次不改」，[] 才是「解绑全部」。
        assert.deepEqual(env.requests[0].body.categoryIds, []);
        assert.equal(env.requests[0].body.name, '商品');
    }
});

test('data-write-float 与 data-write-skip-empty：金额按小数发、点名的空字段不发、没点名的空串照发', async () => {
    const env = setup({
        attrs: {
            'data-write-api': '/api/product/update',
            'data-write-mode': 'form',
            'data-write-float': 'defaultPrice',
            'data-write-number': 'sort',
            'data-write-skip-empty': 'slug,sku,defaultPrice',
        },
        createFields: [
            { name: 'name', value: '商品' },
            { name: 'slug', value: '   ' },
            { name: 'sku', value: '' },
            { name: 'defaultPrice', value: '12.50' },
            { name: 'descriptionHtml', value: '' },
            { name: 'sort', value: '7' },
        ],
        respond: { code: 200, data: {} },
    });
    env.fire(); await tick();
    const body = env.requests[0].body;
    assert.equal(body.defaultPrice, 12.5);
    assert.equal('slug' in body, false);
    assert.equal('sku' in body, false);
    // 富文本没点名：空串是「清空正文」这个明确意图，不能一起被吞掉。
    assert.equal(body.descriptionHtml, '');
    assert.equal(body.sort, 7);
});

test('data-write-lines：多行文本藏域按行切成数组，空文本发空数组（清空而不是不发）', async () => {
    for (const [raw, want] of [['a.jpg\n  b.jpg  \n\n', ['a.jpg', 'b.jpg']], ['', []]]) {
        const env = setup({
            attrs: {
                'data-write-api': '/api/product/update',
                'data-write-mode': 'form',
                'data-write-lines': 'images,imageAlts',
            },
            createFields: [
                { name: 'name', value: '商品' },
                { name: 'images', value: raw },
                { name: 'imageAlts', value: '第一张' },
            ],
            respond: { code: 200, data: {} },
        });
        env.fire(); await tick();
        assert.deepEqual(env.requests[0].body.images, want);
        assert.deepEqual(env.requests[0].body.imageAlts, ['第一张']);
        assert.equal(env.requests[0].body.name, '商品');
    }
});

test('data-write-html：裸 HTML 包成 {html: …} 写到目标键，原键消失且不 trim', async () => {
    const env = setup({
        attrs: {
            'data-write-api': '/api/product/update',
            'data-write-mode': 'form',
            'data-write-html': 'descriptionHtml:description',
        },
        createFields: [
            { name: 'name', value: '商品' },
            { name: 'descriptionHtml', value: '  正文 <b>不加粗</b>\n' },
        ],
        respond: { code: 200, data: {} },
    });
    env.fire(); await tick();
    const body = env.requests[0].body;
    assert.deepEqual(body.description, { html: '  正文 <b>不加粗</b>\n' });
    assert.equal('descriptionHtml' in body, false);
    // 空正文也是「清空」这个明确意图，必须发出去。
    const empty = setup({
        attrs: {
            'data-write-api': '/api/product/update',
            'data-write-mode': 'form',
            'data-write-html': 'descriptionHtml:description',
        },
        createFields: [{ name: 'descriptionHtml', value: '' }],
        respond: { code: 200, data: {} },
    });
    empty.fire(); await tick();
    assert.deepEqual(empty.requests[0].body.description, { html: '' });
});

test('data-write-float 认不出的取值原样发字符串（交给严格 API 报错，而不是变成 0 或 NaN）', async () => {
    const env = setup({
        attrs: { 'data-write-api': '/api/product/update', 'data-write-mode': 'form', 'data-write-float': 'weight' },
        createFields: [{ name: 'weight', value: '1e3' }],
        respond: { code: 200, data: {} },
    });
    env.fire(); await tick();
    assert.equal(env.requests[0].body.weight, '1e3');
});

