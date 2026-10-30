const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

// write-base.js 的「迟到响应保护」：data-write-latest 的并发语义，以及**未启用表单**
// 完全不变这条共享回归。
//
// 为什么单独一个文件而不是塞进 write-base.test.cjs：那个文件测的是「整形」
// （FormData 进、FormData 出，纯函数层）；这里测的是 ②③ 层（提交与收尾）在
// 并发与节点卸载下的行为。同一个 vm 桩要同时满足两层会让两边都读不懂。
// scripts/check-js-controls.sh 递归扫 *.test.cjs，所以这个文件会被门禁自动跑到。

// ── 桩：可手动控制 resolve 时机的 fetch ─────────────────────────────────────

class FakeFormData {
    constructor() { this._v = []; }
    append(k, v) { this._v.push([k, String(v)]); }
    set(k, v) { this._v = this._v.filter(([kk]) => kk !== k); this._v.push([k, String(v)]); }
    delete(k) { this._v = this._v.filter(([kk]) => kk !== k); }
    get(k) { const hit = this._v.find(([kk]) => kk === k); return hit ? hit[1] : null; }
    getAll(k) { return this._v.filter(([kk]) => kk === k).map(([, v]) => v); }
    has(k) { return this._v.some(([kk]) => kk === k); }
    entries() { return this._v.slice(); }
}

// status 桩：记录写进去的文案、tone 与 details。
function statusBox() {
    const box = {
        textContent: '', hidden: false, tones: [], children: [], isConnected: true,
        setAttribute(k, v) { if (k === 'data-tone') box.tones.push(String(v)); },
        removeAttribute() {},
        querySelector(sel) { return sel === '[data-write-details]' ? null : null; },
        appendChild(c) { box.children.push(c); return c; },
    };
    return box;
}

function makeForm(attrs, opts) {
    opts = opts || {};
    const values = Object.assign({}, attrs);
    const status = statusBox();
    // 按钮桩：data-* 五件（get/set/has/remove/names）与 dataset 双向同步 —— 真实 DOM
    // 里两者是同一个存储，disconnected 地维护两套正是「测试绿、页面错」的典型来源。
    const buttonAttrs = Object.assign({ type: 'submit' }, opts.buttonAttrs || {});
    const button = {
        type: 'submit',
        disabled: opts.buttonDisabled === true,
        name: '',
        get dataset() {
            const out = {};
            Object.keys(buttonAttrs).forEach((k) => {
                if (!k.startsWith('data-')) return;
                out[k.slice(5).replace(/-([a-z])/g, (_, c) => c.toUpperCase())] = buttonAttrs[k];
            });
            return out;
        },
        setAttribute(k, v) { buttonAttrs[k] = String(v); },
        getAttribute(k) { return Object.prototype.hasOwnProperty.call(buttonAttrs, k) ? buttonAttrs[k] : null; },
        removeAttribute(k) { delete buttonAttrs[k]; },
        hasAttribute(k) { return Object.prototype.hasOwnProperty.call(buttonAttrs, k); },
        getAttributeNames() { return Object.keys(buttonAttrs); },
    };
    const form = {
        tagName: 'FORM',
        // isConnected 由测试改写：模拟 htmx 换出 / 抽屉关闭。
        isConnected: opts.connected === undefined ? true : opts.connected,
        getAttribute(n) { return Object.prototype.hasOwnProperty.call(values, n) ? values[n] : null; },
        setAttribute(n, v) {
            values[n] = String(v);
            if (n === 'aria-busy') form.busy = String(v);
        },
        removeAttribute(n) { delete values[n]; },
        hasAttribute(n) { return Object.prototype.hasOwnProperty.call(values, n); },
        querySelector(sel) { return sel === '[data-write-status]' ? status : null; },
        querySelectorAll(sel) {
            if (sel === 'button[type="submit"], input[type="submit"]') return [button];
            if (sel === '[name]') return [];
            if (sel === '[data-keep-empty][name]') return [];
            return [];
        },
        closest() { return null; },
        dispatchEvent() {},
    };
    return { form, status, button, values };
}

// env 把「一次提交」封装成可编排的动作：每次 fetch 都挂起，测试自己决定谁先返回。
function env(attrs, opts) {
    const listeners = {};
    const pending = [];
    const rejecters = [];
    const { form, status, button, values } = makeForm(attrs, opts);
    const document = {
        readyState: 'complete',
        addEventListener(n, fn) { listeners[n] = fn; },
        querySelector() { return null; },
        createElement() { return { setAttribute() {}, appendChild() {}, remove() {}, querySelector() { return null; }, textContent: '', hidden: false }; },
    };
    // window.WBUI.toast 必须存在且可观测：否则「迟到响应不 toast」这条主张
    // 无论实现对不对都会通过 —— 那种绿灯什么也没证明。
    const toasts = [];
    const window = {
        document,
        location: { assign() {}, reload() {} },
        confirm: () => true,
        WBUI: { toast: (text, opts) => { toasts.push({ text, opts }); } },
    };
    const ctx = vm.createContext({
        window, document, FormData: FakeFormData, CustomEvent: class { constructor(t) { this.type = t; } },
        console,
        // 每个请求都挂起，由测试决定 resolve 还是 reject。reject 是 catch 分支的唯一
        // 入口（断网、redirect:'error' 被浏览器拒绝）—— 没有它，「迟到的 catch 不解锁」
        // 就只能靠一句空话蒙过去。
        fetch: () => new Promise((resolve, reject) => { pending.push(resolve); rejecters.push(reject); }),
    });
    vm.runInContext(fs.readFileSync(__dirname + '/write-base.js', 'utf8'), ctx);

    return {
        form, status, button, values, listeners, pending, rejecters, toasts,
        submit() {
            listeners.submit({ target: form, submitter: null, preventDefault() {} });
            return this;
        },
        // respond 让第 index 个未决请求返回给定响应。
        respond(index, reply) {
            const opts = Object.assign({ ok: true, code: 200, message: '已保存', status: 200 }, reply || {});
            const resolve = pending[index];
            assert.ok(resolve, '第 ' + index + ' 个请求应当已发出');
            resolve({
                ok: opts.ok,
                status: opts.status,
                headers: { get: (n) => (n === 'Content-Type' ? 'application/json' : null) },
                json: async () => ({ code: opts.code, message: opts.message, data: { id: 'x' } }),
            });
            return this;
        },
        // settle 让微任务队列跑完（两次 setImmediate 足以覆盖 .then 链）。
        async settle() {
            await new Promise(setImmediate);
            await new Promise(setImmediate);
            await new Promise(setImmediate);
        },
    };
}

const LATEST = {
    'data-write': 'custom',
    'data-write-url': '/api/publication/seo-audit',
    'data-write-latest': '',
};

// ── 判据 ────────────────────────────────────────────────────────────────────

test('启用 data-write-latest：先发的成功晚到时既不写结论也不 toast', async () => {
    const e = env(LATEST).submit().submit();
    assert.equal(e.pending.length, 2, '连点两次应当发出两次请求');
    // 后发的先回（第二次）→ 正常写结论。
    e.respond(1, { message: '后一次' });
    await e.settle();
    assert.equal(e.status.textContent, '后一次');
    // 先发的迟到 → 必须被丢弃。
    e.respond(0, { message: '前一次' });
    await e.settle();
    assert.equal(e.status.textContent, '后一次', '迟到响应不得覆盖新结论');
    // toast 也要没有：只断言 status 会让「其实弹了两条」漏网。
    assert.equal(e.toasts.length, 1, '只有最新那次该 toast');
    assert.equal(e.toasts[0].text, '后一次');
});

test('启用 data-write-latest：先发的失败晚到时不得把新成功改成错误', async () => {
    const e = env(LATEST).submit().submit();
    e.respond(1, { message: '体检完成' });
    await e.settle();
    assert.equal(e.status.textContent, '体检完成');
    e.respond(0, { ok: false, code: 500, message: '服务端错误', status: 500 });
    await e.settle();
    assert.equal(e.status.textContent, '体检完成', '迟到的失败不渲染、不提示');
    assert.equal(e.status.tones.includes('error'), false, '迟到失败连 tone 都不该写');
    assert.equal(e.toasts.length, 1, '迟到失败不弹错误 toast');
    assert.equal(e.toasts[0].opts.type, 'ok');
});

test('启用 data-write-latest：迟到的成功不得解锁更新的请求（按钮保持禁用）', async () => {
    const e = env(LATEST).submit();
    assert.equal(e.button.disabled, true, '提交期间按钮禁用');
    e.submit();                              // 第二次提交，第二次请求在飞
    assert.equal(e.pending.length, 2);
    assert.equal(e.button.disabled, true);
    // 第一次（迟到的那个）返回：不能把按钮放开 —— 否则用户可以再点第三次。
    e.respond(0, { message: '前一次' });
    await e.settle();
    assert.equal(e.button.disabled, true, '旧响应不得解锁新请求');

    // 真正最新的那次返回后才解锁。
    e.respond(1, { message: '后一次' });
    await e.settle();
    assert.equal(e.button.disabled, false);
});

test('启用 data-write-latest：迟到的 catch（网络失败）不提示、不解锁', async () => {
    const e = env(LATEST).submit().submit();
    e.respond(1, { message: '后一次' });
    await e.settle();
    assert.equal(e.status.textContent, '后一次');
    // 第 0 个请求以 reject 结束 —— 断网 / redirect:'error' 被浏览器拒绝的真实形态。
    e.rejecters[0](new Error('network down'));
    await e.settle();
    assert.equal(e.status.textContent, '后一次', '迟到的 catch 不得盖住新结论');
    const tonesAfter = e.status.tones.length;

    // 反向对照：**没有**保护的表单在同一条路径上会把结论改成失败提示 ——
    // 这条对照证明上面那条断言不是「因为 catch 本来就不写状态」而侥幸绿的。
    const plain = env({ 'data-write': 'custom', 'data-write-url': '/api/product/update' }).submit().submit();
    plain.rejecters[0](new Error('network down'));
    await plain.settle();
    assert.equal(plain.status.tones.at(-1), 'error', '未启用保护：catch 照常写失败提示');
    assert.equal(e.status.tones.length, tonesAfter, '启用保护：迟到的 catch 连 tone 都不写');
});

test('启用 data-write-latest：节点已摘出文档时迟到响应不写 DOM', async () => {
    const e = env(LATEST, { connected: true }).submit();
    // 模拟 htmx 局部替换：表单被换出文档。
    e.form.isConnected = false;
    e.respond(0, { message: '旧结论' });
    await e.settle();
    assert.equal(e.status.textContent, '', '节点已卸载：不得写入结论');
    // setStatus 写的是 textContent/hidden/data-tone 三件；节点卸载后一件都不该被改。
    assert.equal(e.status.hidden, false, '卸载后连 hidden 都不该动（setStatus 会置 true）');
    assert.equal(e.status.tones.length, 0);
});

test('启用 data-write-latest：节点摘出文档后失败响应也不写', async () => {
    const e = env(LATEST).submit();
    e.form.isConnected = false;
    e.respond(0, { ok: false, code: 500, message: '服务端错误', status: 500 });
    await e.settle();
    assert.equal(e.status.textContent, '', '卸载后失败也不提示');
    assert.equal(e.status.tones.length, 0);
});

test('未启用 data-write-latest 的表单：基座行为一个字节都不变（迟到响应照常生效）', async () => {
    const e = env({
        'data-write': 'custom',
        'data-write-url': '/api/product/update',
    }).submit().submit();
    assert.equal(e.pending.length, 2);
    e.respond(1, { message: '后一次' });
    await e.settle();
    assert.equal(e.status.textContent, '后一次');
    // 未启用保护：先发的迟到响应仍会写入 —— 这是存量表单的既有行为，本批不改。
    e.respond(0, { message: '前一次' });
    await e.settle();
    assert.equal(e.status.textContent, '前一次', '未启用保护的表单保持原行为');
});

test('未启用 data-write-latest 的表单：成功路径、解锁与失败提示全部照旧', async () => {
    const e = env({ 'data-write': 'custom', 'data-write-url': '/api/product/update' }).submit();
    assert.equal(e.button.disabled, true);
    e.respond(0, { ok: false, code: 400, message: '参数不合法', status: 400 });
    await e.settle();
    assert.equal(e.status.textContent, '参数不合法');
    assert.equal(e.status.tones.at(-1), 'error');
    assert.equal(e.button.disabled, false, '失败后照常解锁');
});

test('两次并发提交：旧响应返回后按钮保持禁用，最新那次返回后才解锁', async () => {
    const e = env(LATEST).submit();
    assert.equal(e.button.disabled, true, '首次提交即禁用');
    assert.equal(e.button.getAttribute('data-was-disabled'), '', '原本启用 → 记为启用（空串）');
    e.submit();
    // 第二次 lock(on) 不得把「原本启用」改写为 disabled —— 这是永久锁死的入口。
    assert.equal(e.button.getAttribute('data-was-disabled'), '', '重复上锁保留首次记录');
    assert.equal(e.button.disabled, true);

    e.respond(0, { message: '第一次' });     // 旧的那次先回来（迟到分支）
    await e.settle();
    assert.equal(e.button.disabled, true, '旧响应不解锁');
    assert.equal(e.button.getAttribute('data-was-disabled'), '', '旧响应的 unlock 被跳过，记录还在');

    e.respond(1, { message: '第二次' });
    await e.settle();
    assert.equal(e.button.disabled, false, '最新那次解锁后必须真的可用');
    assert.equal(e.button.hasAttribute('data-was-disabled'), false, '解锁后清掉记录');
});

test('原本就 disabled 的按钮：锁与解锁都保持 disabled', async () => {
    const e = env(LATEST, { buttonDisabled: true }).submit().submit();
    assert.equal(e.button.getAttribute('data-was-disabled'), '1', '原本禁用 → 记为 1');
    e.respond(1, { message: '第二次' });
    await e.settle();
    assert.equal(e.button.disabled, true, '原本禁用的不能被解锁激活');
    assert.equal(e.button.hasAttribute('data-was-disabled'), false);
});

test('未启用 data-write-latest 的表单：lock 沿用原逻辑，每次上锁都按当前值重记', async () => {
    const e = env({ 'data-write': 'custom', 'data-write-url': '/api/product/update' });
    e.submit();
    assert.equal(e.button.getAttribute('data-was-disabled'), '', '原本启用：记空串');
    // 未启用保护的表单本来就不会二次上锁；直接再调一次提交模拟「确实重入」，
    // 此时 guarded=false，必须照原逻辑按当前（已禁用）值重记 —— 保持冻结前行为。
    e.submit();
    assert.equal(e.button.getAttribute('data-was-disabled'), '1', '未启用：重记当前值，行为与冻结前一致');
});

test('连点三次后乱序返回：按钮与结论都以最新那次为准', async () => {
    const e = env(LATEST).submit().submit().submit();
    e.respond(2, { message: '第三次' });
    await e.settle();
    assert.equal(e.button.disabled, false);
    e.respond(0, { message: '第一次' });
    await e.settle();
    assert.equal(e.button.disabled, false, '迟到响应不动 disabled');
    e.respond(1, { message: '第二次' });
    await e.settle();
    assert.equal(e.button.disabled, false);
    assert.equal(e.status.textContent, '第三次');
});

test('启用与未启用并存：一个页面上两种表单各走各的（保护不外溢）', async () => {
    const guarded = env(LATEST).submit().submit();
    const plain = env({ 'data-write': 'custom', 'data-write-url': '/api/product/update' }).submit().submit();
    assert.equal(guarded.pending.length, 2);
    assert.equal(plain.pending.length, 2);
    assert.equal(guarded.form.__wbWriteGeneration, 2);
    assert.equal(plain.form.__wbWriteGeneration, undefined, '未启用的那份不建代号');

    // 未启用的先回新的、再回旧的：两次都写（这就是存量行为，本批不改）。
    plain.respond(1, { message: '普通后' });
    await plain.settle();
    plain.respond(0, { message: '普通前' });
    await plain.settle();
    assert.equal(plain.status.textContent, '普通前');

    // 启用的只认最新那次。
    guarded.respond(1, { message: '保护后' });
    await guarded.settle();
    guarded.respond(0, { message: '保护前' });
    await guarded.settle();
    assert.equal(guarded.status.textContent, '保护后');
});

test('未声明保护时 form 上不产生请求代号（不污染存量的数据属性）', async () => {
    const e = env({ 'data-write': 'custom', 'data-write-url': '/api/product/update' }).submit();
    await e.settle();
    assert.equal(e.form.__wbWriteGeneration, undefined, '未启用时不建代号');
    const guarded = env(LATEST).submit();
    await guarded.settle();
    assert.equal(guarded.form.__wbWriteGeneration, 1, '启用时每提交一次递增');
});

test('启用保护后每次提交都递增代号（同一表单可被连点重跑）', async () => {
    const e = env(LATEST).submit().submit().submit();
    assert.equal(e.pending.length, 3);
    assert.equal(e.form.__wbWriteGeneration, 3);
    e.respond(2, { message: '第三次' });
    await e.settle();
    assert.equal(e.status.textContent, '第三次');
    e.respond(0, { message: '第一次' });
    e.respond(1, { message: '第二次' });
    await e.settle();
    assert.equal(e.status.textContent, '第三次');
});
