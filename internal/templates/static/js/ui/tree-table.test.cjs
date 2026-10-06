// Run with: node --test internal/templates/static/js/ui/tree-table.test.cjs
//
// tree-table.js 的行为回归：折叠以「DFS 前序 + depth 区间」表达，看不见的东西全靠
// tr.hidden —— 而列表页的批量选择模块（admin.js）只认这个属性（隐藏行不参与全选、
// 不计入「已选 N 项」）。所以这里盯三件事：
//
//   1. 折叠只影响「本行之后、更深且连续」的行（DFS 前序的子树区间），不误伤下一棵树；
//   2. 折叠把行藏起来时，顺手取消它的勾选 —— 否则页面显示「已选 0 项」而提交带着 id；
//   3. 展开 / 折叠全部按钮与单个三角走同一套状态（不多造一份折叠实现）。
//
// 手写 fake DOM（与 ui/drawer.test.cjs 同一套做法）：控件脚本用的是十几行 DOM API，
// 引 jsdom 只为跑这一条不划算，而且真实浏览器里跑不出「同一行既 hidden 又被勾选」这种态。

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

const utilSrc = fs.readFileSync(__dirname + '/_util.js', 'utf8');
const treeSrc = fs.readFileSync(__dirname + '/tree-table.js', 'utf8');

function makeNode(attrs) {
    return {
        attrs: Object.assign({}, attrs),
        hidden: false,
        checked: false,
        disabled: false,
        dataset: {},
        events: [],
        classes: {},
        getAttribute(k) { return this.attrs[k] === undefined ? null : this.attrs[k]; },
        setAttribute(k, v) { this.attrs[k] = v; },
        removeAttribute(k) { delete this.attrs[k]; },
        classList: null,
        querySelector() { return null; },
        querySelectorAll() { return []; },
        addEventListener(type, fn) { this.events.push({ type, fn }); },
        dispatchEvent(ev) { this.dispatched = (this.dispatched || []).concat(ev.type); return true; },
        fire(type, target) {
            this.events.filter((e) => e.type === type).forEach((e) => e.fn({ target: target || this }));
        },
    };
}

// makeRow 造一行：attrs 给 data-tree-id / data-tree-depth；expanded 给定时造折叠按钮。
function makeRow(id, depth, expanded) {
    const row = makeNode({ 'data-tree-id': id, 'data-tree-depth': String(depth) });
    row.classList = {
        toggled: [],
        toggle(name, on) { this.toggled.push([name, on]); },
    };
    const box = makeNode({ type: 'checkbox', name: 'ids' });
    box.checked = false;
    row.box = box;
    row.querySelectorAll = (sel) => (sel === 'input[type="checkbox"]' ? [box] : []);
    // 控件走事件委托：监听在 <table> 上，从 e.target 用 closest 找到三角与所在行。
    row.closest = (sel) => (sel === '[data-tree-id]' ? row : null);
    if (expanded !== undefined) {
        const btn = makeNode({ 'aria-expanded': expanded ? 'true' : 'false' });
        btn.closest = (sel) => (sel === '[data-tree-toggle]' ? btn : sel === '[data-tree-id]' ? row : null);
        row.btn = btn;
        row.querySelector = (sel) => (sel === '[data-tree-toggle]' ? btn : null);
    }
    return row;
}

function setup(rows, opts) {
    const buttons = {};
    const table = makeNode({});
    table.querySelectorAll = (sel) => (sel === '[data-tree-id]' ? rows : []);
    const documentScope = {
        querySelectorAll(sel) {
            const key = sel === '[data-tree-expand-all]' ? 'expand' : sel === '[data-tree-collapse-all]' ? 'collapse' : null;
            if (!key || !opts || !opts[key]) { return []; }
            buttons[key] = buttons[key] || makeNode({});
            return [buttons[key]];
        },
    };
    const window = {};
    const ctx = { window, document: documentScope, Event: class { constructor(type) { this.type = type; } } };
    vm.createContext(ctx);
    vm.runInContext(utilSrc, ctx);
    vm.runInContext(treeSrc, ctx);
    ctx.window.WBUI.scan({ querySelectorAll: (sel) => (sel === '[data-tree-table]' ? [table] : []) });
    return { table, buttons, rows };
}

test('折叠只隐藏本行之后更深且连续的行', () => {
    // 两棵树：1 → 2 → 3，4 → 5。初始：服务端给的态是「顶级折叠」（aria-expanded=false）。
    const rows = [makeRow(1, 0, false), makeRow(2, 1, false), makeRow(3, 2, undefined), makeRow(4, 0, false), makeRow(5, 1, undefined)];
    setup(rows);

    assert.equal(rows[0].hidden, false, '顶级行自己必须可见');
    assert.equal(rows[1].hidden, true, '被折叠的子树隐藏');
    assert.equal(rows[2].hidden, true, '更深的后代一起隐藏');
    assert.equal(rows[4].hidden, true, '第二棵树的子行同样被折叠');
    assert.equal(rows[0].box.checked, false);
});

test('点三角展开子树，再点收起；跨树不误伤', () => {
    // 1 初始展开（它的子 2 可见），2 初始收起（孙行 3 隐藏）；4 是另一棵树的顶级。
    const rows = [makeRow(1, 0, true), makeRow(2, 1, false), makeRow(3, 2, undefined), makeRow(4, 0, undefined)];
    const { table } = setup(rows);

    assert.equal(rows[1].hidden, false, '祖先展开时子行可见');
    assert.equal(rows[2].hidden, true, '孙行的可见性由它自己的父（2）决定');

    // 展开 2：3 露出来。
    table.fire('click', rows[1].btn);
    assert.equal(rows[1].btn.getAttribute('aria-expanded'), 'true');
    assert.equal(rows[2].hidden, false);

    // 再点收起 2：3 又藏起来，同层的另一棵树不受影响。
    table.fire('click', rows[1].btn);
    assert.equal(rows[1].btn.getAttribute('aria-expanded'), 'false');
    assert.equal(rows[2].hidden, true);
    assert.equal(rows[3].hidden, false, '同层的另一棵树不受影响');
});

test('折叠把行藏起来时取消它的勾选并派发 change', () => {
    const rows = [makeRow(1, 0, true), makeRow(2, 1, undefined)];
    const { table } = setup(rows);
    rows[1].box.checked = true;

    table.fire('click', rows[0].btn);

    assert.equal(rows[1].hidden, true);
    assert.equal(rows[1].box.checked, false, '被折叠藏起来的行不能继续带着勾选提交');
    assert.deepEqual(rows[1].box.dispatched, ['change'], '派发 change 让批量条重算「已选 N 项」');
});

test('展开 / 折叠全部与单个三角共用同一套状态', () => {
    const rows = [makeRow(1, 0, false), makeRow(2, 1, undefined), makeRow(3, 0, false), makeRow(4, 1, undefined)];
    const { buttons } = setup(rows, { expand: true, collapse: true });

    buttons.expand.fire('click');
    assert.equal(rows[1].hidden, false, '展开全部后子行可见');
    assert.equal(rows[3].hidden, false);
    assert.equal(rows[0].btn.getAttribute('aria-expanded'), 'true');
    assert.equal(rows[2].btn.getAttribute('aria-expanded'), 'true');

    buttons.collapse.fire('click');
    assert.equal(rows[1].hidden, true, '折叠全部后又藏起来');
    assert.equal(rows[3].hidden, true);
    assert.equal(rows[0].btn.getAttribute('aria-expanded'), 'false');
});
