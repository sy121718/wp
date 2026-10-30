const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

// seo-audit.js 的行为测试：node:test + vm + 最小 DOM 桩。
//
// 桩必须照真实 DOM 的三条语义写，否则测出来的东西与页面无关：
//   ① <template> 的 textContent 恒为空，内容在 .content（DocumentFragment）上；
//      克隆用 tpl.content.cloneNode(true)。
//   ② 表单与结果区是兄弟，关联只能经共同祖先的 closest('[data-seo-audit-scope]')。
//   ③ 嵌在 <template> 里的 <template>，其内容不在父节点的 children 上。
// 所以桩不提供「穿透 template」的后门 —— 被测代码若写错查找路径就会找不到节点。

// ── 最小 DOM 桩 ─────────────────────────────────────────────────────────────

function node(tag, attrs, text) {
    const n = {
        tagName: String(tag || 'div').toUpperCase(),
        attrs: Object.assign({}, attrs || {}),
        _text: text === undefined ? '' : String(text),
        children: [],
        parentNode: null,
        hidden: false,
        get textContent() {
            return this._text + this.children.map((c) => c.textContent).join('');
        },
        set textContent(v) { this._text = v === undefined || v === null ? '' : String(v); this.children = []; },
        getAttribute(k) { return Object.prototype.hasOwnProperty.call(this.attrs, k) ? this.attrs[k] : null; },
        setAttribute(k, v) { this.attrs[k] = String(v); },
        removeAttribute(k) { delete this.attrs[k]; },
        hasAttribute(k) { return Object.prototype.hasOwnProperty.call(this.attrs, k); },
        appendChild(c) {
            // 真实 DOM：往节点里 append 一个 DocumentFragment 是**展开**它的子节点，
            // fragment 本身不进树、随后变空。桩原先直接把它当成一个孩子推进去，
            // 于是 JS「先把行装进 fragment、再一次 append 进 tbody」的写法在测试里
            // 变成 1 行 —— 生产对、测试假红。这里如实展开。
            if (c && c.tagName === '#FRAGMENT') {
                const kids = c.children.slice();
                c.children = [];
                kids.forEach((k) => this.appendChild(k));
                return c;
            }
            if (c && c.parentNode) c.parentNode.removeChild(c);
            this.children.push(c);
            if (c) c.parentNode = this;
            return c;
        },
        replaceChild(next, old) {
            const i = this.children.indexOf(old);
            if (i < 0) return old;
            if (next.parentNode) next.parentNode.removeChild(next);
            this.children[i] = next;
            next.parentNode = this;
            old.parentNode = null;
            return old;
        },
        removeChild(c) {
            const i = this.children.indexOf(c);
            if (i >= 0) this.children.splice(i, 1);
            c.parentNode = null;
            return c;
        },
        replaceChildren() {
            this.children.forEach((c) => { c.parentNode = null; });
            this.children = [];
        },
        querySelector(sel) { return queryAll(this, sel)[0] || null; },
        querySelectorAll(sel) { return queryAll(this, sel); },
        // closest 只看祖先（不含自己）—— 与真实 DOM 的「含自己」等价于下面这种写法：
        // 面板测试里 panel 自身不带 scope，所以这里用「含自己」才与浏览器一致。
        closest(sel) {
            const spec = parse(sel);
            let cur = this;
            while (cur) {
                if (matches(cur, spec)) return cur;
                cur = cur.parentNode;
            }
            return null;
        },
        cloneNode(deep) { return clone(this, deep !== false); },
    };
    return n;
}

// 选择器只支持本控件用到的三种：[attr]、[attr="value"]、#id、tag。
function parse(sel) {
    const s = String(sel).trim();
    let m = /^#([\w-]+)$/.exec(s);
    if (m) return { id: m[1] };
    m = /^([a-zA-Z][\w-]*)$/.exec(s);
    if (m) return { tag: m[1].toUpperCase() };
    m = /^\[([\w-]+)(?:="([^"]*)")?\]$/.exec(s);
    if (m) return { name: m[1], value: m[2] };
    return null;
}

function matches(n, spec) {
    if (!n || !spec) return false;
    if (spec.id !== undefined) return n.getAttribute('id') === spec.id;
    if (spec.tag !== undefined) return n.tagName === spec.tag;
    if (spec.value !== undefined) return n.getAttribute(spec.name) === spec.value;
    return n.hasAttribute(spec.name);
}

// 后代查找：普通子树往下走；遇到 <template> 不进入它的 .content
// （真实 DOM 里 querySelector 不会穿透 template —— 这正是 ②③ 要考的点）。
function queryAll(root, sel) {
    const spec = parse(sel);
    const out = [];
    const walk = (n) => {
        for (const c of n.children || []) {
            if (matches(c, spec)) out.push(c);
            walk(c);
        }
    };
    walk(root);
    return out;
}

// <template>：本体 textContent 恒为空，内容活在 .content 这个 DocumentFragment 上。
function tpl(attrs, build) {
    const frag = node('#fragment', {});
    build(frag);
    const t = node('template', attrs, '');
    Object.defineProperty(t, 'textContent', { get() { return ''; }, set() {} });
    t.content = frag;
    return t;
}

function clone(n, deep) {
    if (n.tagName === '#FRAGMENT') {
        const frag = node('#fragment', {});
        if (deep) n.children.forEach((c) => frag.appendChild(clone(c, true)));
        return frag;
    }
    const c = node(n.tagName.toLowerCase(), n.attrs, n._text);
    c.hidden = n.hidden;
    if (n.content) {
        // 克隆 <template> 时内容也克隆（真实 DOM 的默认 deep=true）。
        c.content = clone(n.content, true);
    }
    if (deep) n.children.forEach((child) => c.appendChild(clone(child, true)));
    return c;
}

// ── 生产模板夹具 ────────────────────────────────────────────────────────────
//
// 文案与结构照抄 admin/analytics/seo.html 当前版本，节点名、嵌套关系、级别类都一致。
// 唯一简化：Jinja 的 {{ .["t"](...) }} 直接写中文 —— 取词结果与页面一致即可。

const ISSUE_TEXT = {
    title_missing: '页面没有标题',
    title_duplicate: '多个页面使用了同一个标题',
    description_missing: '页面没有 meta description',
    canonical_missing: '页面没有 canonical',
    image_alt_missing: '图片缺少替代文本',
    internal_link_broken: '站内链接指向不存在的路径',
    hreflang_incomplete: 'hreflang 只有一条，缺少互指',
    unknown: '未识别的检查项（后端返回了界面尚未覆盖的标识）',
};
const LEVELS = [
    { level: 'error', cls: 'badge-danger', text: '严重' },
    { level: 'warning', cls: 'badge-warning', text: '警告' },
    { level: 'info', cls: 'badge-info', text: '提示' },
];
const LEVEL_MISSING = { cls: 'badge-mute', text: '未标注级别' };

function dictionary() {
    const dict = node('template', { id: 'seo-audit-dictionary' });
    const frag = node('#fragment', {});

    // 级别：一项一个 <template>，名字与颜色类成对写在里面
    // （ui.css 只有 badge-* 类，没有 data-tone 映射）。
    // 结构与 seo.html 逐字一致：外层 span.badge[data-seo-issue-badge]，内层 span.badge.badge-*，
    // 内层**没有**任何 data-* 钩子。夹具不许给自己造生产中不存在的定位属性。
    LEVELS.forEach((l) => {
        frag.appendChild(tpl({ 'data-seo-level-item': l.level }, (f) => {
            const outer = node('span', { class: 'badge', 'data-seo-issue-badge': '' });
            outer.appendChild(node('span', { class: 'badge ' + l.cls }, l.text));
            f.appendChild(outer);
        }));
    });
    frag.appendChild(tpl({ 'data-seo-level-missing': '' }, (f) => {
        const mute = node('span', { class: 'badge', 'data-seo-issue-badge': '' });
        mute.appendChild(node('span', { class: 'badge ' + LEVEL_MISSING.cls }, LEVEL_MISSING.text));
        f.appendChild(mute);
    }));

    // 行模板：级别列留空 td，文案预置在 [data-seo-issue-label] 里，details 只给 title_duplicate。
    Object.entries(ISSUE_TEXT).forEach(([item, label]) => {
        const t = tpl({ 'data-seo-issue-item': item }, (f) => {
            const tr = node('tr', {});
            tr.appendChild(node('td', { 'data-seo-level-cell': '' }));       // 级别列（空，由 JS 填徽章）
            const pathCell = node('td', {});
            pathCell.appendChild(node('code', { 'data-seo-issue-path': '' }));
            tr.appendChild(pathCell);
            const issueCell = node('td', {});
            issueCell.appendChild(node('span', { 'data-seo-issue-label': '' }, label));
            const details = node('div', { 'data-seo-issue-details': '' });
            issueCell.appendChild(details);
            if (item === 'title_duplicate') {
                // 只有 title_duplicate 带 details/summary —— unknown 没有，与生产模板一致。
                const d = node('details', {});
                d.appendChild(node('summary', {}, '受影响的路径'));
                d.appendChild(node('ul', { 'data-seo-issue-paths': '' }));
                issueCell.appendChild(d);
            }
            tr.appendChild(issueCell);
            f.appendChild(tr);
        });
        frag.appendChild(t);
    });

    [['item', '检查项标识：'], ['title', '标题：'], ['src', '图片：'], ['href', '链接：']].forEach(([name, prefix]) => {
        frag.appendChild(tpl({ 'data-seo-issue-detail': name }, (f) => {
            f.appendChild(node('span', {}, prefix));
            f.appendChild(node('code', { 'data-seo-detail-value': '' }));
        }));
    });

    frag.appendChild(tpl({ 'data-seo-path-template': '' }, (f) => {
        const li = node('li', {});
        li.appendChild(node('code', { 'data-seo-path': '' }));
        f.appendChild(li);
    }));

    // 摘要成品句：不可变的格式来源。
    frag.appendChild(tpl({ 'data-seo-summary-template': '' }, (f) => {
        f.appendChild(node('span', {}, '已扫描 {scanned} 个产物，发现 {count} 个问题。'));
    }));

    dict.content = frag;
    return dict;
}

// 结果区：四档状态，与 seo.html 同构。
function panelFixture() {
    const panel = node('div', { class: 'stack', 'data-seo-audit-result': '' });
    panel.setAttribute('role', 'region');
    panel.setAttribute('aria-live', 'polite');
    panel.setAttribute('aria-busy', 'false');

    // 坏契约的落点：独立一条，不混进四档。hidden 由 JS 切。
    const failed = node('p', {
        class: 'badge badge-danger',
        'data-seo-audit-failed': '',
        // 静态文案存在属性上：JS 改 textContent 时不污染它。
        'data-seo-audit-message': '体检结果格式异常，本次未更新结论，请重试。',
    });
    failed.setAttribute('role', 'alert');
    failed.hidden = true;
    panel.appendChild(failed);

    const placeholder = node('div', { class: 'empty-state', 'data-seo-audit-state': 'placeholder' });
    placeholder.appendChild(node('p', {}, '运行体检后，此处显示当前产物中的 SEO 问题。'));
    panel.appendChild(placeholder);

    const clean = node('div', { class: 'empty-state', 'data-seo-audit-state': 'clean' });
    clean.hidden = true;
    const cleanP = node('p', {});
    cleanP.appendChild(node('span', {}, '已扫描 '));
    cleanP.appendChild(node('span', { 'data-seo-audit-scanned': '' }));
    cleanP.appendChild(node('span', {}, ' 个产物，未发现 SEO 问题。'));
    clean.appendChild(cleanP);
    panel.appendChild(clean);

    const empty = node('div', { class: 'empty-state', 'data-seo-audit-state': 'empty' });
    empty.hidden = true;
    empty.appendChild(node('p', {}, '当前站点还没有可检查的产物。'));
    panel.appendChild(empty);

    const issues = node('div', { 'data-seo-audit-state': 'issues' });
    issues.hidden = true;
    issues.appendChild(node('p', { 'data-seo-audit-summary': '' }, ''));
    const table = node('table', {});
    const tbody = node('tbody', { 'data-seo-audit-rows': '' });
    table.appendChild(tbody);
    issues.appendChild(table);
    panel.appendChild(issues);
    return panel;
}

// 页面夹具：section[data-seo-audit-scope] > (form, panel, dict) —— 三者是兄弟。
function page() {
    const scope = node('section', { class: 'card card-body', 'data-seo-audit-scope': '' });
    const form = node('form', {
        method: 'post',
        action: '/api/publication/seo-audit',
        'data-seo-audit-action': '',
        'data-write': 'custom',
        'data-write-url': '/api/publication/seo-audit',
        'data-write-handler': 'publication.seo-audit',
        'data-write-latest': '',
    });
    const status = node('p', { 'data-write-status': '', role: 'status' }, '');
    status.hidden = true;
    form.appendChild(status);
    const panel = panelFixture();
    const dict = dictionary();
    scope.appendChild(form);
    scope.appendChild(panel);
    scope.appendChild(dict);
    return { scope, form, status, panel, dict };
}

// ── 加载被测模块 ────────────────────────────────────────────────────────────

function load() {
    const handlers = Object.create(null);
    const { scope, form, status, panel, dict } = page();

    const document = {
        readyState: 'complete',
        addEventListener() {},
        // 词典已挪进本 section：这里刻意**不**提供全局回落，页面上也查不到它。
        getElementById: () => null,
        querySelector: () => null,
        createDocumentFragment: () => node('#fragment', {}),
    };
    const window = {
        document,
        WBUI: {
            writeBase: {
                registerResultHandler: (name, fn) => {
                    if (handlers[name]) throw new Error('写结果处理能力重复注册: ' + name);
                    handlers[name] = fn;
                },
            },
        },
    };
    const ctx = vm.createContext({ window, document, console });
    vm.runInContext(fs.readFileSync(__dirname + '/seo-audit.js', 'utf8'), ctx);

    const submit = (data) => handlers['publication.seo-audit']({
        form, submitter: null, data, payload: { code: 200, data }, text: '已保存',
        instanceId: '',
    });

    // submitCatch 抓处理器抛出的错 —— 基座就是靠这个抛错走 catch 写 error 的。
    const submitCatch = (data) => {
        try {
            return { threw: false, value: submit(data) };
        } catch (err) {
            return { threw: true, error: err };
        }
    };

    return {
        window, document, scope, form, status, panel, dict, handlers, submit, submitCatch,
        state: (name) => queryAll(panel, `[data-seo-audit-state="${name}"]`)[0] || null,
        rows: () => queryAll(panel, '[data-seo-audit-rows]')[0].children,
        summary: () => queryAll(panel, '[data-seo-audit-summary]')[0],
        failed: () => queryAll(panel, '[data-seo-audit-failed]')[0],
    };
}

const issue = (path, item, level, params) => ({ path, item, level, params: params || {} });

function rowTextOf(row) { return row.textContent; }

// ── 判据 ────────────────────────────────────────────────────────────────────

test('模板与结果区是兄弟：只经 scope 关联也能渲染（form 里查不到结果区）', () => {
    const app = load();
    // 前提断言：形式化地证明这条结构事实 —— 若这里能查到，本用例就失去意义。
    assert.equal(app.form.querySelector('[data-seo-audit-result]'), null, '表单里不该有结果区');
    assert.equal(app.panel.closest('[data-seo-audit-scope]'), app.scope, '结果区祖先里有 scope');

    const ok = app.submit({ scanned: 3, count: 1, issues: [issue('/a', 'title_missing', 'error')] });
    assert.equal(ok, true);
    assert.equal(app.rows().length, 1);
});

test('真实 Fragment 落地后仍是三列：级别 / 路径 / 问题都在，问题列没被级别清空', () => {
    const app = load();
    app.submit({ scanned: 2, count: 2, issues: [
        issue('/p/one', 'title_duplicate', 'error', { title: '同一个标题', paths: ['/p/one', '/p/two'] }),
        issue('/p/three', 'image_alt_missing', 'warning', { src: '/img/a.png' }),
    ] });
    const rows = app.rows();
    assert.equal(rows.length, 2);

    rows.forEach((tr, i) => {
        assert.equal(tr.tagName, 'TR', 'body 里必须是 <tr>，不是 DocumentFragment');
        assert.equal(tr.children.length, 3, `第 ${i} 行必须完整保留三列`);
    });

    const first = rows[0];
    const [levelCell, pathCell, issueCell] = first.children;
    assert.equal(queryAll(levelCell, '[data-seo-issue-badge]').length, 1, '级别列只放徽章（生产里内层没有 data-* 钩子）');
    assert.equal(levelCell.textContent, '严重');
    assert.equal(queryAll(pathCell, '[data-seo-issue-path]')[0].textContent, '/p/one', '路径列没被清掉');
    assert.ok(queryAll(issueCell, '[data-seo-issue-label]')[0].textContent.includes('多个页面使用了同一个标题'),
        '问题列文案没被清掉');
    assert.ok(issueCell.textContent.includes('同一个标题'), '详细参数（标题）没被清掉');
    const paths = queryAll(issueCell, '[data-seo-issue-paths]')[0];
    assert.equal(paths.children.length, 2, '完整路径仍完整可见');
    assert.equal(paths.children.map((li) => li.textContent).join('|'), '/p/one|/p/two');
});

test('词典只在本 section 内查：document 上没有回落入口', () => {
    const app = load();
    // 夹具里 document.getElementById 恒返回 null —— 若实现仍做全局回落，渲染必然失败。
    assert.equal(app.document.getElementById('seo-audit-dictionary'), null);
    assert.equal(app.submit({ scanned: 1, count: 0, issues: [] }), true, '仅靠 scope 内词典也要成功');
    // 词典确实在 scope 内。
    assert.equal(queryAll(app.scope, '#seo-audit-dictionary').length, 1);
});

test('词典是 template：文案必须从 .content 取到，行模板嵌套的 template 也能找到', () => {
    const app = load();
    // 前提：panel 与 dict 平级，panel 内部找不到词典（证明不是从 panel 里瞎凑）。
    assert.equal(queryAll(app.panel, '#seo-audit-dictionary').length, 0);
    app.submit({ scanned: 2, count: 2, issues: [
        issue('/a', 'title_missing', 'warning'),
        issue('/b', 'title_duplicate', 'warning', { title: '同一个标题', paths: ['/b', '/c'] }),
    ] });
    const rows = app.rows();
    assert.equal(rows.length, 2);
    assert.ok(rowTextOf(rows[0]).includes('页面没有标题'), '内层 template 的 .content 文案要取到');
    assert.ok(rowTextOf(rows[1]).includes('多个页面使用了同一个标题'));
});

test('文案沿用克隆体里的预置文本：不把整行 textContent 回读进来', () => {
    const app = load();
    app.submit({ scanned: 1, count: 1, issues: [
        issue('/x', 'title_duplicate', 'error', { title: '标题', paths: ['/p1', '/p2'] }),
    ] });
    const label = queryAll(app.rows()[0], '[data-seo-issue-label]')[0];
    // 若实现改成「按整行 textContent 再写回 label」，这里会混入级别名 / 路径 / summary。
    assert.equal(label.textContent, '多个页面使用了同一个标题');
    assert.equal(label.textContent.includes('严重'), false);
    assert.equal(label.textContent.includes('/p1'), false);
    assert.equal(label.textContent.includes('受影响的路径'), false);
});

test('七类问题各自用预置文案，级别名与颜色类都来自模板', () => {
    const app = load();
    const items = Object.keys(ISSUE_TEXT).filter((k) => k !== 'unknown');
    app.submit({ scanned: 9, count: items.length, issues: items.map((it, i) => issue('/p' + i, it, 'warning')) });
    const rows = app.rows();
    assert.equal(rows.length, items.length);
    rows.forEach((row, i) => {
        assert.ok(rowTextOf(row).includes(ISSUE_TEXT[items[i]]), `第 ${i} 行文案`);
        const badge = queryAll(row, '[data-seo-issue-badge]')[0];
        assert.ok(badge, '级别徽章必须来自模板');
        assert.equal(badge.textContent, '警告');
        // 颜色类挂在内层 span 上 —— 生产模板里那一层没有任何 data-* 钩子。
        assert.equal((badge.children[0].getAttribute('class') || '').includes('badge-warning'), true,
            '颜色类来自模板，不把业务 level 当类名');
    });
    assert.equal(app.state('issues').hidden, false);
    assert.equal(app.state('placeholder').hidden, true);
});

test('error / info 分别取「严重」「提示」，且不出现原始 level 原值', () => {
    const app = load();
    app.submit({ scanned: 2, count: 2, issues: [
        issue('/a', 'title_missing', 'error'),
        issue('/b', 'canonical_missing', 'info'),
    ] });
    const [e1, e2] = app.rows();
    assert.ok(rowTextOf(e1).includes('严重'));
    assert.ok(rowTextOf(e2).includes('提示'));
    assert.equal(rowTextOf(e1).includes('error'), false);
    assert.equal((queryAll(e1, '[data-seo-issue-badge]')[0].children[0].getAttribute('class')).includes('badge-danger'), true);
    assert.equal((queryAll(e2, '[data-seo-issue-badge]')[0].children[0].getAttribute('class')).includes('badge-info'), true);
});

test('未收录的 level 走「未标注级别」兜底，不把原值当文案', () => {
    const app = load();
    app.submit({ scanned: 1, count: 1, issues: [issue('/a', 'title_missing', 'fatal')] });
    const row = app.rows()[0];
    // data-seo-level-missing 挂在 <template> 外壳上，选中它之后克隆的是 .content ——
    // 克隆体里不会有这个属性。所以只按真实克隆结果断言：一个徽章、内层颜色类、
    // 兜底文案，且不把原 level 原值当文案。
    const badges = queryAll(row, '[data-seo-issue-badge]');
    assert.equal(badges.length, 1, '未收录 level 也只放一个徽章');
    assert.equal(badges[0].textContent, LEVEL_MISSING.text);
    assert.equal((badges[0].children[0].getAttribute('class') || '').includes('badge-mute'), true,
        '兜底用 badge-mute，不拿原 level 当类名');
    assert.equal(rowTextOf(row).includes('fatal'), false, '原 level 原值不得出现在文案里');
});

test('未知 item：通用提示 + 原标识纯文本，不丢行，且没有出现模板里不存在的 details', () => {
    const app = load();
    app.submit({ scanned: 1, count: 1, issues: [issue('/new', 'some_future_check', 'info')] });
    const rows = app.rows();
    assert.equal(rows.length, 1);
    const text = rowTextOf(rows[0]);
    assert.ok(text.includes(ISSUE_TEXT.unknown));
    assert.ok(text.includes('some_future_check'));
    assert.ok(text.includes('检查项标识：'));
    // 生产模板的 unknown 行没有 details —— 夹具也必须没有。
    assert.equal(rows[0].tagName === 'TR', true);
    assert.equal(queryAll(rows[0], 'details').length, 0);
});

test('重复标题：全部命中路径在 details 内逐个完整可见，不用 title 属性', () => {
    const app = load();
    const paths = ['/a', '/b', '/c'];
    app.submit({ scanned: 3, count: 1, issues: [
        issue('', 'title_duplicate', 'warning', { title: '同一个标题', paths }),
    ] });
    const row = app.rows()[0];
    const list = queryAll(row, '[data-seo-issue-paths]')[0];
    assert.ok(list, '重复标题必须有路径容器');
    assert.equal(list.children.map((li) => li.textContent).join('|'), paths.join('|'));
    assert.ok(rowTextOf(row).includes('同一个标题'));
    const details = queryAll(row, 'details')[0];
    assert.ok(details, '路径用 <details> 承载，原生可键盘展开');
    assert.equal(details.getAttribute('title'), null);
});

test('图片与死链分别带 src / href 原值', () => {
    const app = load();
    app.submit({ scanned: 2, count: 2, issues: [
        issue('/p', 'image_alt_missing', 'warning', { src: '/img/hero.png' }),
        issue('/p', 'internal_link_broken', 'error', { href: '/old/page' }),
    ] });
    const [img, link] = app.rows();
    assert.ok(rowTextOf(img).includes('图片：') && rowTextOf(img).includes('/img/hero.png'));
    assert.ok(rowTextOf(link).includes('链接：') && rowTextOf(link).includes('/old/page'));
});

test('恶意文本只走 textContent：不产生子元素、不注入属性', () => {
    const app = load();
    const evil = '<img src=x onerror=alert(1)>" onclick="steal()';
    app.submit({ scanned: 1, count: 1, issues: [
        issue('/x<script>bad()</script>', 'image_alt_missing', 'warning', { src: evil }),
    ] });
    const row = app.rows()[0];
    const code = queryAll(row, '[data-seo-issue-path]')[0];
    assert.equal(code.children.length, 0);
    assert.equal(code.textContent, '/x<script>bad()</script>');
    const detail = queryAll(row, '[data-seo-detail-value]')[0];
    assert.equal(detail.textContent, evil);
    assert.equal(detail.children.length, 0);
    assert.equal(detail.getAttribute('onclick'), null);
});

test('两种空态互斥：scanned=0 是没有产物，scanned>0 且 count=0 才是干净结论', () => {
    const empty = load();
    empty.submit({ scanned: 0, count: 0, issues: [] });
    assert.equal(empty.state('empty').hidden, false);
    assert.equal(empty.state('clean').hidden, true);
    assert.equal(empty.state('issues').hidden, true);

    const clean = load();
    clean.submit({ scanned: 12, count: 0, issues: [] });
    assert.equal(clean.state('clean').hidden, false);
    assert.equal(clean.state('empty').hidden, true);
    assert.equal(queryAll(clean.state('clean'), '[data-seo-audit-scanned]')[0].textContent, '12');
});

test('摘要每轮从不可变模板重算：第二轮数字照样更新', () => {
    const app = load();
    app.submit({ scanned: 5, count: 2, issues: [issue('/a', 'title_missing', 'error'), issue('/b', 'canonical_missing', 'warning')] });
    assert.equal(app.summary().textContent, '已扫描 5 个产物，发现 2 个问题。');
    app.submit({ scanned: 6, count: 1, issues: [issue('/a', 'title_missing', 'error')] });
    assert.equal(app.summary().textContent, '已扫描 6 个产物，发现 1 个问题。', '第二轮占位符必须还在');
    assert.equal(app.rows().length, 1, '每次重建行，不追加');
    // count 必须与 issues 条数相符（契约校验，不是业务补算）——所以这里真给 7 条。
    const seven = [];
    for (let i = 0; i < 7; i++) seven.push(issue('/p' + i, 'title_missing', 'error'));
    app.submit({ scanned: 31, count: 7, issues: seven });
    assert.equal(app.summary().textContent, '已扫描 31 个产物，发现 7 个问题。');
});

test('重复渲染同一份数据：行不累积、级别徽章不叠加', () => {
    const app = load();
    const data = { scanned: 2, count: 2, issues: [issue('/a', 'title_missing', 'error'), issue('/b', 'title_missing', 'error')] };
    app.submit(data);
    app.submit(data);
    app.submit(data);
    assert.equal(app.rows().length, 2, '行数始终等于 issues 条数');
    app.rows().forEach((row) => {
        assert.equal(queryAll(row, '[data-seo-issue-badge]').length, 1, '级别徽章只被替换一次');
        assert.equal(queryAll(row, '[data-seo-issue-label]').length, 1);
    });
});

test('成功不导航不刷新：处理器消费结果', () => {
    const app = load();
    let navigated = 0;
    app.window.location = { assign: () => { navigated++; }, reload: () => { navigated++; } };
    assert.equal(app.submit({ scanned: 0, count: 0, issues: [] }), true);
    assert.equal(navigated, 0);
});

test('坏契约显式失败：处理器抛错（基座据此写 error），旧结论保留', () => {
    const app = load();
    const good = { scanned: 3, count: 1, issues: [issue('/a', 'title_missing', 'error')] };
    app.submit(good);
    const before = app.summary().textContent;
    const beforeRows = app.rows().length;

    // 坏契约必须让处理器抛错 —— 不能 return true 把「没拿到结论」当成功。
    const bad = app.submitCatch({ count: 4, issues: [] });
    assert.equal(bad.threw, true, '处理器必须抛 Error');
    assert.match(String(bad.error && bad.error.message), /格式异常/,
        'Error 文案取自模板的 data-seo-audit-failed 节点，不是 context.text');
    assert.equal(app.panel.getAttribute('data-seo-audit-error'), 'true');
    assert.equal(app.failed().hidden, false, '失败提示要真的显示出来');
    assert.equal(app.summary().textContent, before, '旧结论不被清空');
    assert.equal(app.rows().length, beforeRows);

    // 契约形状的各种破裂：count 与 issues 不等、scanned=0 却带 issues、负数、非整数、字符串。
    const rejects = [
        { scanned: 1, count: 1, issues: 'nope' },
        { scanned: 3, count: 1, issues: [] },                       // count !== issues.length
        { scanned: 0, count: 0, issues: [issue('/a', 'title_missing', 'error')] },
        { scanned: 1, count: -1, issues: [] },
        { scanned: '3', count: 0, issues: [] },
        { scanned: 1.5, count: 0, issues: [] },
        { scanned: 1, count: 0 },
    ];
    rejects.forEach((d, i) => {
        assert.equal(app.window.WBUI.seoAudit.render(app.panel, d), false, '应拒绝第 ' + i + ' 份坏契约');
        assert.equal(app.submitCatch(d).threw, true, '处理器应抛错: ' + i);
    });
});

test('null issue / 缺行模板：整批作废，旧结果一行不少', () => {
    const app = load();
    app.submit({ scanned: 2, count: 2, issues: [
        issue('/a', 'title_missing', 'error'),
        issue('/b', 'canonical_missing', 'warning'),
    ] });
    const beforeRows = app.rows().map((r) => r.textContent);

    // 中间混一个 null —— 先离线造行，失败就不动 body。
    const bad = app.submitCatch({ scanned: 2, count: 2, issues: [issue('/x', 'title_missing', 'error'), null] });
    assert.equal(bad.threw, true);
    assert.equal(app.rows().length, beforeRows.length, '旧行必须原样保留');
    assert.equal(app.rows().map((r) => r.textContent).join('|'), beforeRows.join('|'));

    // 整个词典里没有对应行模板（连 unknown 都没有）同样作废。
    const dict = app.dict;
    const saved = dict.content.children.slice();
    dict.content.replaceChildren();
    const missing = app.submitCatch({ scanned: 1, count: 1, issues: [issue('/y', 'title_missing', 'error')] });
    assert.equal(missing.threw, true, '缺模板也要显式失败，不能静默丢行');
    assert.equal(app.rows().length, beforeRows.length, '且不动旧结果');
    saved.forEach((c) => dict.content.appendChild(c));
});

test('好的渲染会清掉失败标记', () => {
    const app = load();
    // 坏契约：count 与 issues 对不上 —— 处理器抛错、面板打标记。
    assert.equal(app.submitCatch({ scanned: 1, count: 1, issues: [] }).threw, true);
    assert.equal(app.panel.getAttribute('data-seo-audit-error'), 'true');
    app.submit({ scanned: 1, count: 0, issues: [] });
    assert.equal(app.panel.getAttribute('data-seo-audit-error'), null);
    assert.equal(queryAll(app.panel, '[data-seo-audit-failed]')[0].hidden, true);
});

test('不再自带 submit 监听改 aria-busy：并发状态归基座管', () => {
    const app = load();
    // 被测脚本只注册了结果处理器，没有向 document 加 submit 监听。
    assert.equal(typeof app.handlers['publication.seo-audit'], 'function');
    assert.equal(app.panel.getAttribute('aria-busy'), 'false', '初始值来自模板');
});

test('重复加载不重复注册处理器', () => {
    const app = load();
    const before = app.handlers['publication.seo-audit'];
    assert.equal(typeof before, 'function');
    // 再跑一次脚本：ui.seoAudit 已存在 → 直接返回，不再调 registerResultHandler。
    const src = fs.readFileSync(__dirname + '/seo-audit.js', 'utf8');
    const ctx = vm.createContext({ window: app.window, document: app.document, console });
    vm.runInContext(src, ctx);
    assert.equal(app.handlers['publication.seo-audit'], before);
});
