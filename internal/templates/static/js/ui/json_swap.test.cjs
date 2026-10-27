/* json_swap.test.cjs — htmx 扩展 ui/json_swap.js 的行为契约（node:test + vm，无浏览器）。
 *
 * 这个扩展是全仓「后端只回 {code,message,data}」这条规矩的落点：局部刷新时 DOM 由
 * 前端用 Jet 渲染好的 <template> 填充。它守的是四类**生产上不会报错、只会静默变坏**的缺陷：
 *
 *   1. 失败却把目标节点换掉了 —— 用户填的内容没了，而页面看着「有反应」；
 *      规矩是失败只 toast，一个字节都不动。
 *   2. 失败判定漏了非 0 的业务码 —— HTTP 200 + code=400 是本仓的常规形态，
 *      只看 HTTP 状态码会把业务失败当成功渲染出去。
 *   3. 用 innerHTML 拼 data —— 数据库字段里的 `<img onerror=…>` 会变成 XSS。
 *   4. 空结果没有标记 —— 表格显示成「有一张空表」，而本来该显示空态文案。
 *
 * 运行：node --test internal/templates/static/js/ui/json_swap.test.cjs
 */
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

// ── 极简 DOM 桩 ─────────────────────────────────────────────────────────────
// 只实现被测代码用到的部分：createElement / createDocumentFragment / querySelector /
// textContent / setAttribute / cloneNode(content) / insertBefore / removeChild。
// 刻意不做 innerHTML —— 谁若在扩展里写了 innerHTML，这里直接报错，等价于一条门禁。
function makeEnv(templateRow) {
    const created = [];

    function makeNode(tag) {
        const el = {
            tagName: String(tag || 'div').toUpperCase(),
            children: [],
            attrs: {},
            _text: '',
            get textContent() { return this._text; },
            set textContent(v) { this._text = String(v); },
            setAttribute(k, v) { this.attrs[k] = String(v); },
            getAttribute(k) { return Object.prototype.hasOwnProperty.call(this.attrs, k) ? this.attrs[k] : null; },
            hasAttribute(k) { return Object.prototype.hasOwnProperty.call(this.attrs, k); },
            removeAttribute(k) { delete this.attrs[k]; },
            appendChild(c) {
                // 真实 DOM：appendChild(DocumentFragment) 移动它的子节点，而不是插入 fragment 本身。
                if (c && String(c.tagName).toUpperCase() === '#FRAGMENT') {
                    c.children.slice().forEach((ch) => { this.children.push(ch); ch.parentNode = this; c.removeChild(ch); });
                    return c;
                }
                this.children.push(c);
                c.parentNode = this;
                return c;
            },
            insertBefore(c, ref) {
                // 真实 DOM：插入 DocumentFragment 时移动的是它的子节点（与 appendChild 同一语义）。
                // 少了这段，「追加 N 行」会被桩记成「插入 1 个 fragment 节点」，断言全错。
                if (c && String(c.tagName).toUpperCase() === '#FRAGMENT') {
                    const moving = c.children.slice();
                    moving.forEach((ch) => { c.removeChild(ch); this.insertBefore(ch, ref); });
                    return c;
                }
                const i = ref ? this.children.indexOf(ref) : -1;
                if (i < 0) { this.children.push(c); } else { this.children.splice(i, 0, c); }
                c.parentNode = this;
                return c;
            },
            removeChild(c) {
                const i = this.children.indexOf(c);
                if (i >= 0) { this.children.splice(i, 1); c.parentNode = null; }
                return c;
            },
            querySelector(sel) { return this._find(sel)[0] || null; },
            querySelectorAll(sel) { return this._find(sel); },
            _find(sel) {
                const out = [];
                const want = sel.replace(/^\[|\]$/g, '').split('=')[0];
                const walk = (n) => {
                    (n.children || []).forEach((c) => {
                        if (sel.startsWith('[') && c.hasAttribute(want)) { out.push(c); }
                        walk(c);
                    });
                };
                walk(this);
                return out;
            },
            get firstElementChild() { return this.children[0] || null; },
            get firstChild() { return this.children[0] || null; }
        };
        created.push(el);
        return el;
    }

    // <template> 桩：content 是一个 DocumentFragment，cloneNode 返回独立副本
    const tplRow = makeNode('tr');
    tplRow.setAttribute('data-row', '');
    (templateRow || []).forEach((f) => {
        const td = makeNode('td');
        td.setAttribute('data-field', f);
        tplRow.appendChild(td);
    });
    const tplContent = makeNode('#fragment');
    tplContent.appendChild(tplRow);
    const tpl = {
        tagName: 'TEMPLATE',
        content: Object.assign(tplContent, {
            cloneNode() {
                // 深拷贝 tplRow 得到一份独立的 <tr>，包进一个 fragment 里
                // （真实 DOM 的 template.content.cloneNode() 返回 DocumentFragment）
                const deep = (n) => {
                    const c = makeNode(n.tagName);
                    c._text = n._text;
                    Object.keys(n.attrs).forEach((k) => c.setAttribute(k, n.attrs[k]));
                    (n.children || []).forEach((ch) => c.appendChild(deep(ch)));
                    return c;
                };
                const f = makeNode('#fragment');
                f.appendChild(deep(tplRow));
                return f;
            }
        })
    };

    const doc = {
        createElement: (t) => makeNode(t),
        createDocumentFragment: () => makeNode('#fragment'),
        querySelector: (sel) => (sel === '#tpl-row' ? tpl : null)
    };

    const toasts = [];
    const errors = [];
    const win = {
        document: doc,
        WBUI: { toast: (m, o) => toasts.push({ msg: m, type: (o || {}).type }) },
        console: {
            log: () => {},
            warn: () => {},
            error: (...a) => errors.push(a.join(' '))
        }
    };
    win.window = win;

    // 捕获注册的扩展
    let ext = null;
    win.htmx = {
        registerExtension: (name, def) => { ext = { name, def }; },
        process: () => {}
    };

    const code = fs.readFileSync(__dirname + '/json_swap.js', 'utf8');
    vm.createContext(win);
    vm.runInContext(code, win);

    return { ext, toasts, errors, win, created };
}

/** 造一次 before_swap 的 detail：text 是响应体，target 是目标节点 */
function swapDetail(env, text, status = 200) {
    const target = env.win.document.createElement('div');
    const elt = env.win.document.createElement('div');
    elt.setAttribute('data-json-template', '#tpl-row');
    elt.setAttribute('data-json-rows', 'items');
    elt.setAttribute('data-json-key', 'lang');
    const tasks = [{ type: 'main', fragment: env.win.document.createDocumentFragment(), target, swapSpec: { style: 'innerHTML' } }];
    return { target, elt, tasks, detail: { ctx: { text, response: { status }, target }, tasks } };
}

test('注册名为 json-swap', () => {
    const env = makeEnv(['lang', 'statusText']);
    assert.ok(env.ext, '必须调用 htmx.registerExtension');
    assert.equal(env.ext.name, 'json-swap');
});

test('成功：按 template 填充行，字段走 textContent', () => {
    const env = makeEnv(['lang', 'statusText']);
    const s = swapDetail(env, JSON.stringify({
        code: 0, message: '',
        data: { items: [{ lang: 'zh-CN', statusText: '已发布' }, { lang: 'en-US', statusText: '未发布' }] }
    }));
    env.ext.def.htmx_before_swap(s.elt, s.detail);

    assert.equal(s.tasks.length, 1, '应保留 main 任务让它 swap');
    const frag = s.tasks[0].fragment;
    assert.equal(frag.children.length, 2, '两行数据应产出两个 <tr>');
    const firstCells = frag.children[0].children;
    assert.equal(firstCells[0].textContent, 'zh-CN');
    assert.equal(firstCells[1].textContent, '已发布');
    assert.equal(s.target.hasAttribute('data-json-empty'), false);
});

test('成功但空数组：清空并打空态标记', () => {
    const env = makeEnv(['lang']);
    const s = swapDetail(env, JSON.stringify({ code: 0, data: { items: [] } }));
    env.ext.def.htmx_before_swap(s.elt, s.detail);
    assert.equal(s.tasks[0].fragment.children.length, 0);
    assert.equal(s.target.getAttribute('data-json-empty'), '1', '空结果必须标记，否则显示成一张空表');
});

test('失败（HTTP 200 + code=400）：不 swap、只 toast', () => {
    const env = makeEnv(['lang']);
    const s = swapDetail(env, JSON.stringify({ code: 400, message: '该语言已排除' }), 200);
    const before = s.tasks.length;
    env.ext.def.htmx_before_swap(s.elt, s.detail);

    assert.equal(s.tasks.length, 0, '失败必须清空 tasks —— 一个字节都不许换');
    assert.equal(before, 1);
    assert.equal(env.toasts.length, 1);
    assert.equal(env.toasts[0].type, 'error');
    assert.equal(env.toasts[0].msg, '该语言已排除');
});

test('失败（HTTP 500）：同样不 swap', () => {
    const env = makeEnv(['lang']);
    const s = swapDetail(env, JSON.stringify({ code: 500, message: '内部错误' }), 500);
    env.ext.def.htmx_before_swap(s.elt, s.detail);
    assert.equal(s.tasks.length, 0);
    assert.equal(env.toasts[0].type, 'error');
});

test('非本仓信封（无 code 字段）：完全不管，交回 htmx', () => {
    const env = makeEnv(['lang']);
    const s = swapDetail(env, JSON.stringify({ items: [{ lang: 'x' }] }));
    const frag = s.tasks[0].fragment;
    env.ext.def.htmx_before_swap(s.elt, s.detail);
    assert.equal(s.tasks.length, 1);
    assert.equal(s.tasks[0].fragment, frag, '不应替换 fragment');
});

test('找不到模板：清空 tasks 并报错，不静默换掉节点', () => {
    const env = makeEnv(['lang']);
    const s = swapDetail(env, JSON.stringify({ code: 0, data: { items: [{ lang: 'a' }] } }));
    s.elt.setAttribute('data-json-template', '#tpl-missing');
    env.ext.def.htmx_before_swap(s.elt, s.detail);
    assert.equal(s.tasks.length, 0);
    assert.ok(env.errors.some((e) => e.indexOf('找不到行模板') >= 0), '接线错误必须报出来');
});

test('data-json-remove：按 key 摘掉那一行，其余不动', () => {
    const env = makeEnv(['lang']);
    const target = env.win.document.createElement('tbody');
    const rowA = env.win.document.createElement('tr');
    rowA.setAttribute('data-row-key', 'zh-CN');
    const rowB = env.win.document.createElement('tr');
    rowB.setAttribute('data-row-key', 'en-US');
    target.appendChild(rowA);
    target.appendChild(rowB);

    const elt = env.win.document.createElement('div');
    elt.setAttribute('data-json-remove', 'removed');
    elt.setAttribute('data-json-key', 'lang');
    const tasks = [{ type: 'main', fragment: env.win.document.createDocumentFragment(), target, swapSpec: {} }];

    env.ext.def.htmx_before_swap(elt, {
        ctx: { text: JSON.stringify({ code: 0, message: '已恢复', data: { removed: 'zh-CN' } }), response: { status: 200 }, target },
        tasks
    });

    assert.equal(tasks.length, 0, '删除不应走整块 swap');
    assert.equal(target.children.length, 1);
    assert.equal(target.children[0].getAttribute('data-row-key'), 'en-US');
    assert.equal(env.toasts[0].type, 'ok');
});

test('不写 innerHTML：扩展源码里不许出现 innerHTML（XSS 面）', () => {
    const code = fs.readFileSync(__dirname + '/json_swap.js', 'utf8')
        .replace(/\/\*[\s\S]*?\*\//g, '')   // 去掉块注释（注释里提到 innerHTML 是说明，不是用法）
        .replace(/\/\/.*$/gm, '');
    assert.equal(/\.innerHTML\s*=/.test(code), false,
        'data 来自数据库字段，拼 innerHTML 等于把 XSS 换个位置；只准 textContent / setAttribute');
});

test('属性白名单：data-field-attr 非白名单属性被拒绝，且不写入', () => {
    const env = makeEnv(['lang']);
    // 用 WBJSON.fillRow 直接测（不必绕一圈 swap）
    const tpl = env.win.document.querySelector('#tpl-row');
    // 桩里把第一格改成写属性的形态
    const cell = tpl.content.children[0].children[0];
    cell.setAttribute('data-field-attr', 'onclick');
    cell.setAttribute('data-field', 'lang');

    const root = env.win.WBJSON.fillRow(tpl, { lang: 'zh-CN' }, 'lang');
    assert.equal(root.children[0].getAttribute('onclick'), null,
        '非白名单属性一律不写 —— 否则等于把 XSS 换个位置');
    assert.ok(env.errors.some((e) => e.indexOf('不允许的属性') >= 0), '拒绝必须留下可诊断的错误');
});

test('属性白名单：白名单内的属性正常写入', () => {
    const env = makeEnv(['lang']);
    const tpl = env.win.document.querySelector('#tpl-row');
    const cell = tpl.content.children[0].children[0];
    cell.setAttribute('data-field-attr', 'data-id');
    cell.setAttribute('data-field', 'lang');

    const root = env.win.WBJSON.fillRow(tpl, { lang: 'zh-CN' }, 'lang');
    assert.equal(root.children[0].getAttribute('data-id'), 'zh-CN');
    assert.equal(root.children[0].textContent, '', '走属性时不该同时写 textContent');
});

test('data-when：只保留命中状态的那个节点（译文由 Jet 预置，前端只做显隐）', () => {
    const env = makeEnv([]);
    // 手工造一个带三种状态文案的行模板
    const tpl = env.win.document.querySelector('#tpl-row');
    const cell = env.win.document.createElement('td');
    ['excluded', 'published', 'draft'].forEach((st) => {
        const span = env.win.document.createElement('span');
        span.setAttribute('data-when', st);
        span.textContent = '文案-' + st;   // 相当于 Jet 渲染期取好的译文
        cell.appendChild(span);
    });
    tpl.content.children[0].appendChild(cell);

    const root = env.win.WBJSON.fillRow(tpl, { status: 'published' }, null);
    const spans = root.children[0].children;
    assert.equal(spans.length, 1, '应只留下命中的那一个');
    assert.equal(spans[0].getAttribute('data-when'), 'published');
    assert.equal(spans[0].textContent, '文案-published', '译文原样保留，前端不参与取词');
});

test('data-when：全部不命中时该单元格为空（不残留别的状态文案）', () => {
    const env = makeEnv([]);
    const tpl = env.win.document.querySelector('#tpl-row');
    const cell = env.win.document.createElement('td');
    ['excluded', 'published'].forEach((st) => {
        const span = env.win.document.createElement('span');
        span.setAttribute('data-when', st);
        span.textContent = '文案-' + st;
        cell.appendChild(span);
    });
    tpl.content.children[0].appendChild(cell);

    const root = env.win.WBJSON.fillRow(tpl, { status: 'draft' }, null);
    assert.equal(root.children[0].children.length, 0);
});


// ── 累计加载（data-json-more）──────────────────────────────────────────────
// 这两条守的是「加载更多」的静默退化：重复点击会插重复行、没有下一页时按钮还在，
// 两者在界面上都不报错，只是越点越长。

test('data-json-patch=append：按 data-json-key 去重，重复页不产生重复行', () => {
    const env = makeEnv(['lang']);
    const { target, elt, tasks, detail } = swapDetail(env, JSON.stringify({
        code: 0, message: '', data: { items: [{ lang: 'zh' }, { lang: 'en' }], page: 1, hasMore: true }
    }));
    elt.setAttribute('data-json-patch', 'append');
    env.ext.def.htmx_before_swap(elt, detail);

    assert.equal(target.children.length, 2, '首次追加两行');

    // 同一页再来一次（用户连点）：一行都不该新增
    const again = swapDetail(env, JSON.stringify({
        code: 0, message: '', data: { items: [{ lang: 'zh' }, { lang: 'en' }], page: 1, hasMore: true }
    }));
    again.elt.setAttribute('data-json-patch', 'append');
    // 把已渲染的两行搬到新 target 上，模拟「目标里已有这两行」
    target.children.slice().forEach((c) => again.target.appendChild(c));
    env.ext.def.htmx_before_swap(again.elt, again.detail);
    assert.equal(again.target.children.length, 2, '重复页不该新增行');
});

test('data-json-more：hasMore 为假时隐藏按钮，不再推进页码', () => {
    const env = makeEnv(['lang']);
    const { target, elt, detail } = swapDetail(env, JSON.stringify({
        code: 0, message: '', data: { items: [{ lang: 'zh' }], page: 3, hasMore: false }
    }));
    elt.setAttribute('data-json-patch', 'append');
    elt.setAttribute('data-json-more', 'page');
    elt.setAttribute('data-json-more-hide', '');
    elt.setAttribute('hx-get', '/api/product/publish-attempts?productId=p1&page=3');
    env.ext.def.htmx_before_swap(elt, detail);

    assert.equal(elt.hidden, true, '没有下一页必须隐藏「加载更多」按钮');
    assert.equal(elt.getAttribute('hx-get'), '/api/product/publish-attempts?productId=p1&page=3', '页码不推进');
    assert.equal(target.children.length, 1);
});

test('data-json-more：hasMore 为真时把 hx-get 的页码推进到 page+1', () => {
    const env = makeEnv(['lang']);
    const { target, elt, detail } = swapDetail(env, JSON.stringify({
        code: 0, message: '', data: { items: [{ lang: 'zh' }], page: 1, hasMore: true }
    }));
    elt.setAttribute('data-json-patch', 'append');
    elt.setAttribute('data-json-more', 'page');
    elt.setAttribute('hx-get', '/api/product/publish-attempts?productId=p1&page=1');
    env.ext.def.htmx_before_swap(elt, detail);

    assert.equal(elt.hidden, false, '有下一页（且已显式放出来）');
    assert.equal(elt.getAttribute('hx-get'), '/api/product/publish-attempts?productId=p1&page=2');
    assert.equal(target.children.length, 1);
});

test('data-json-more：hx-get 里还没有 page 参数时补上', () => {
    const env = makeEnv(['lang']);
    const { elt, detail } = swapDetail(env, JSON.stringify({
        code: 0, message: '', data: { items: [{ lang: 'zh' }], page: 1, hasMore: true }
    }));
    elt.setAttribute('data-json-patch', 'append');
    elt.setAttribute('data-json-more', 'page');
    elt.setAttribute('hx-get', '/api/product/publish-attempts?productId=p1');
    env.ext.def.htmx_before_swap(elt, detail);
    assert.equal(elt.getAttribute('hx-get'), '/api/product/publish-attempts?productId=p1&page=2');
});
