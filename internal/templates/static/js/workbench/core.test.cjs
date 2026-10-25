/* workbench_core.test.cjs — 工作台前端内核 core.js 的行为契约（node:test + vm）。
 *
 * 为什么这块过去一个测试都没有：workbench/ 下有 5 个文件、几千行 JS，此前**零行为测试**，
 * 只有几条 strings.Contains 守着字面量。而这里坏掉的表现几乎都是静默的 ——
 * CSRF 头没带上 = /api 组 403，页面上只看到一句通用失败；路径判定错了 = 链接点不动，
 * 而画布看起来完全正常。
 *
 * 手法照 ui/json_write.test.cjs：vm 里跑真文件（不桩掉被测函数），只桩 DOM 与
 * sessionStorage 这些环境依赖。core.js 是 ES module，vm 里用「去掉 export 关键字」
 * 的方式加载 —— 比引入转译器轻，而它本来就是无副作用的纯函数集合（文件头自己声明
 * 「无 DOM 环境（node 契约测试直接 import 本模块求值纯函数）」）。
 *
 * 运行：node --test internal/templates/static/js/workbench/core.test.cjs
 */
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const source = fs.readFileSync(__dirname + '/core.js', 'utf8');

/**
 * 在受控环境里求值 core.js，取回它导出的函数。
 *
 * @param {object} opts
 * @param {object|null} opts.metaContent  <meta name="csrf-token"> 的 content（null = 没有这个标签）
 * @param {string|null} opts.sessionToken sessionStorage 里的 csrf_token（null = 未设置）
 * @param {object} opts.globals          额外注入的全局（window / WBUI 等）
 */
function load(opts = {}) {
    const { metaContent = null, sessionToken = null, globals = {} } = opts;

    // 最小 DOM：只需 querySelector('meta[name="csrf-token"]') 一条查询。
    const document = {
        querySelector(sel) {
            if (sel !== 'meta[name="csrf-token"]') return null;
            if (metaContent === null) return null;
            return { getAttribute: (name) => (name === 'content' ? metaContent : null) };
        },
        // core.js 顶层还会读 #wb-meta / #wb-bootstrap；返回 null 时两者退化为空对象，
        // 这正是文件头承诺的「无 DOM 环境不抛错」路径，顺带被这里覆盖。
        getElementById() {
            return null;
        },
    };

    const store = new Map();
    if (sessionToken !== null) store.set('csrf_token', sessionToken);
    const sessionStorage = {
        getItem: (k) => (store.has(k) ? store.get(k) : null),
        setItem: (k, v) => store.set(k, String(v)),
    };

    // core.js 是 ES module：vm 里跑需要两处改写（都不是"改被测代码"，只是换加载方式）。
    //   1. `export function` → `function`（顶层声明挂到 context 上即可取回）；
    //   2. re-export 语句整块删掉 —— 它把 palette.js 的符号转发出去，与这里被测的
    //      纯函数无关，而 vm 不解析模块说明符。palette.js 自己的契约由
    //      internal/builder/defaults_contract_test.go 覆盖（那里的注释写了这条分工）。
    const plain = source
        .replace(/^export\s*\{[^}]*\}\s*from\s*'[^']*';?/gm, '')
        .replace(/^export /gm, '');
    const sandbox = {
        document,
        sessionStorage,
        console,
        JSON,
        window: globals.window || {},
        ...globals,
    };
    sandbox.globalThis = sandbox;

    const ctx = vm.createContext(sandbox);
    vm.runInContext(plain, ctx, { filename: 'core.js' });

    // 取回本测试用到的导出（vm 里顶层函数声明会挂到 context 上）。
    return {
        getCSRFToken: ctx.getCSRFToken,
        csrfHeaders: ctx.csrfHeaders,
        wbInternalPathOf: ctx.wbInternalPathOf,
        wbAnchorIDOf: ctx.wbAnchorIDOf,
        wbActionValue: ctx.wbActionValue,
        clone: ctx.clone,
        controlLabel: ctx.controlLabel,
        optionLabel: ctx.optionLabel,
        meta: ctx.meta,
        initialDoc: ctx.initialDoc,
    };
}

// ── CSRF ────────────────────────────────────────────────────────────────────

test('CSRF：优先取 meta 里的 token（每次渲染新鲜）', () => {
    const wb = load({ metaContent: 'meta-token', sessionToken: 'session-token' });
    // meta 必须赢：它是服务端本次渲染注入的；sessionStorage 里那份可能来自上一次登录，
    // 会话续期后可能已过期。取错的后果是 /api 组 403，而页面只显示一句通用失败。
    assert.equal(wb.getCSRFToken(), 'meta-token');
});

test('CSRF：meta 缺失时回退 sessionStorage', () => {
    const wb = load({ metaContent: null, sessionToken: 'session-token' });
    assert.equal(wb.getCSRFToken(), 'session-token');
});

test('CSRF：两处都没有时返回空串，不返回 undefined / null', () => {
    const wb = load({ metaContent: null, sessionToken: null });
    // 返回 undefined 会让 csrfHeaders 里的 `if (t)` 也走同一分支，但调用方若直接
    // 拼 header 就会得到 "undefined" 字符串。空串是这里唯一安全的零值。
    assert.equal(wb.getCSRFToken(), '');
});

test('CSRF：meta 存在但 content 为空串时继续回退', () => {
    const wb = load({ metaContent: '', sessionToken: 'session-token' });
    // 空 content 是「模板渲染了标签但没取到 token」的形态，等同于没有。
    assert.equal(wb.getCSRFToken(), 'session-token');
});

test('csrfHeaders：有 token 时附加 X-CSRF-Token', () => {
    const wb = load({ metaContent: 'tok' });
    const h = wb.csrfHeaders();
    assert.equal(h['X-CSRF-Token'], 'tok');
});

test('csrfHeaders：保留调用方已有的头（不覆盖 Content-Type）', () => {
    const wb = load({ metaContent: 'tok' });
    const h = wb.csrfHeaders({ 'Content-Type': 'application/json' });
    assert.equal(h['Content-Type'], 'application/json');
    assert.equal(h['X-CSRF-Token'], 'tok');
});

test('csrfHeaders：没有 token 时不写 X-CSRF-Token（写空值会被服务端当成「提供了错误 token」）', () => {
    const wb = load({ metaContent: null, sessionToken: null });
    const h = wb.csrfHeaders({ 'Content-Type': 'application/json' });
    assert.equal('X-CSRF-Token' in h, false);
    assert.equal(h['Content-Type'], 'application/json');
});

test('csrfHeaders：无参调用也返回可写对象（不返回 null / undefined）', () => {
    const wb = load({ metaContent: 'tok' });
    const h = wb.csrfHeaders();
    assert.equal(typeof h, 'object');
    assert.notEqual(h, null);
});

// ── 路径判定 ────────────────────────────────────────────────────────────────

test('wbInternalPathOf：站内相对路径原样保留', () => {
    const wb = load();
    assert.equal(wb.wbInternalPathOf('/about'), '/about');
    assert.equal(wb.wbInternalPathOf('/products/phone'), '/products/phone');
});

test('wbInternalPathOf：查询串被清洗成路径字符（不是原样保留）', () => {
    const wb = load();
    // ? 与 = 不在路径白名单里，会被替换成 "-" —— 这个函数的产物是**路径**，
    // 不是完整 URL：它要能安全拼进 href 与选择器，查询串不属于那里。
    assert.equal(wb.wbInternalPathOf('/products/phone?v=1'), '/products/phone-v-1');
});

test('wbInternalPathOf：外链被归一成站内路径（不是原样保留）', () => {
    const wb = load();
    // 设计意图：编辑器画布上点链接**不能跳出编辑器**（草稿会丢），所以外链的
    // path 部分被取出来当作站内路径处理。把外链原样返回才是缺陷。
    assert.equal(wb.wbInternalPathOf('https://example.com/a'), '/a');
    // 没有任何 path 的外链 → 根路径。
    assert.equal(wb.wbInternalPathOf('https://example.com'), '/');
});

test('wbInternalPathOf：非法字符被归一成连字符并截断', () => {
    const wb = load();
    // 路径要能安全拼进 URL/选择器：空格、引号、尖括号一律换成 "-"。
    assert.equal(wb.wbInternalPathOf('/a b'), '/a-b');
    const long = wb.wbInternalPathOf('/' + 'a'.repeat(300));
    assert.ok(long.length <= 200, `超长路径应被截断到 200，实际 ${long.length}`);
});

test('wbInternalPathOf：空值给根路径而不是空串', () => {
    const wb = load();
    // 始终返回一个能用的路径：调用方拿它去 fetch / 填 href，空串会让请求打到当前页。
    assert.equal(wb.wbInternalPathOf(''), '/');
    assert.equal(wb.wbInternalPathOf(null), '/');
});

test('wbInternalPathOf：纯锚点被当成路径段（# 前缀被剥掉）', () => {
    const wb = load();
    // 调用方若想要「只要锚点」应该用 wbAnchorIDOf；本函数一律产出路径。
    assert.equal(wb.wbInternalPathOf('#section'), '/section');
});

// ── 锚点 ID ─────────────────────────────────────────────────────────────────

test('wbAnchorIDOf：合法标识符原样返回（含 # 前缀）', () => {
    const wb = load();
    assert.equal(wb.wbAnchorIDOf('team'), 'team');
    assert.equal(wb.wbAnchorIDOf('#team'), 'team');
});

test('wbAnchorIDOf：路径取末段派生，且清洗成合法 id', () => {
    const wb = load();
    // 路径末段被当作 id 派生来源（弹窗/锚点目标可以直接填一条路径）。
    assert.equal(wb.wbAnchorIDOf('/about'), 'about');
    assert.equal(wb.wbAnchorIDOf('/a/b/team'), 'team');
    // 非标识符字符被清掉；清完不合法则返回空串（宁可没有 id，也不给一个非法 id）。
    assert.equal(wb.wbAnchorIDOf('1-bad-start'), '');
    assert.equal(wb.wbAnchorIDOf('/a b'), 'ab');
});

// ── 动作值 ──────────────────────────────────────────────────────────────────

test('wbActionValue：internal 动作返回 { value, hint } 结构', () => {
    const wb = load();
    const r = wb.wbActionValue('internal', '/x');
    // 返回的是联动结果对象（供 wbLinkedPatch 写回字段 + 给面板提示），不是裸值。
    assert.equal(r.value, '/x');
    assert.equal(r.hint, '');
});

test('wbActionValue：internal 动作对非法路径归一', () => {
    const wb = load();
    assert.equal(wb.wbActionValue('internal', 'no-slash').value, '/no-slash');
});

// ── clone ───────────────────────────────────────────────────────────────────

test('clone：深拷贝，改副本不影响原件', () => {
    const wb = load();
    const src = { a: { b: [1, 2] } };
    const copy = wb.clone(src);
    assert.deepEqual(copy, src);
    copy.a.b.push(3);
    assert.deepEqual(src.a.b, [1, 2]);
});

test('clone：undefined 保持 undefined（不是 JSON.stringify 的 null）', () => {
    const wb = load();
    // JSON.parse(JSON.stringify(undefined)) 会抛错；这里必须短路。
    assert.equal(wb.clone(undefined), undefined);
});

// ── 标签回退 ────────────────────────────────────────────────────────────────

test('controlLabel：未登记的 key 原样返回（不返回 undefined）', () => {
    const wb = load();
    // 返回 undefined 会让检查器渲染出 "undefined" 字样 —— 静默的界面缺陷。
    assert.equal(wb.controlLabel('some.unknown.label'), 'some.unknown.label');
});

test('optionLabel：未登记的 value 原样返回', () => {
    const wb = load();
    assert.equal(wb.optionLabel('some-unknown-option'), 'some-unknown-option');
});

// ── 无 DOM 环境 ─────────────────────────────────────────────────────────────

test('无 DOM 环境不抛错，且纯函数照常可用', () => {
    // core.js 的文件头承诺「无 DOM 环境（node 契约测试直接 import 本模块求值纯函数）
    // 下不抛错」。这条承诺是上面所有测试的前提，单独钉一次。
    //
    // 注意 vm 里 `export const` 会变成普通赋值、不挂到 context 顶层，所以这里不取
    // meta / initialDoc 的值，而是断言「加载没抛 + 依赖 DOM 的函数仍然可用」。
    const wb = load();
    assert.equal(typeof wb.getCSRFToken, 'function');
    assert.equal(wb.getCSRFToken(), '');
    assert.equal(wb.wbInternalPathOf('/x'), '/x');
});
