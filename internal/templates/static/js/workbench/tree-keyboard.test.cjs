const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

function treeEvents() {
    const source = fs.readFileSync(__dirname + '/methods/tree.js', 'utf8')
        .replace(/^import\s*\{[\s\S]*?\}\s*from\s*'[^']*';/gm, '')
        .replace('export const treeMethods', 'var treeMethods');
    const context = vm.createContext({});
    vm.runInContext(source, context);
    const listeners = new Map();
    const calls = [];
    let focused;
    const rows = ['first', 'hidden', 'last'].map(id => {
        let collapsed = false;
        const li = { classList: { contains: () => collapsed } };
        const caret = { textContent: '▾', click() { collapsed = !collapsed; calls.push(['toggle', id]); } };
        const row = {
            dataset: { id }, offsetParent: id === 'hidden' ? null : {},
            closest(selector) { return selector === '.wb-node' ? row : selector === 'li' ? li : null; },
            querySelector() { return caret; },
            focus() { focused = id; }
        };
        return row;
    });
    const root = {
        dataset: {}, contains: row => rows.includes(row),
        querySelectorAll: () => rows,
        addEventListener: (type, handler) => listeners.set(type, handler)
    };
    const wb = {
        localizeTree() {}, select(id) { calls.push(['select', id]); },
        moveNodeOrder(id, delta) { calls.push(['move', id, delta]); },
        duplicate() { calls.push(['duplicate', this.selectedId]); },
        deleteSelected() { calls.push(['delete', this.selectedId]); }
    };
    context.treeMethods.bindTreeHtmx.call(wb, root);
    const descendant = (row, operation) => ({
        closest(selector) {
            if (selector === '.wb-node') return row;
            if (selector === '.wb-node-action' && operation) return this;
            return null;
        },
        getAttribute: () => operation
    });
    function key(target, value) {
        const event = { target, key: value, defaultPrevented: false,
            preventDefault() { this.defaultPrevented = true; } };
        listeners.get('keydown')(event);
        return event;
    }
    function click(target) {
        listeners.get('click')({ target, stopPropagation() {} });
    }
    return { rows, calls, descendant, key, click, focused: () => focused };
}

test('结构树子动作按钮保留Enter和空格默认激活，点击委托仍执行原动作', () => {
    for (const key of ['Enter', ' ', 'Spacebar']) {
        for (const [operation, expected] of [
            ['up', ['move', 'first', -1]], ['down', ['move', 'first', 1]],
            ['dup', ['duplicate', 'first']], ['del', ['delete', 'first']]
        ]) {
            const tree = treeEvents();
            const button = tree.descendant(tree.rows[0], operation);
            const event = tree.key(button, key);
            assert.equal(event.defaultPrevented, false, operation + ': ' + key);
            assert.deepEqual(tree.calls, []);
            // 浏览器默认激活随后派发click；验证实际绑定的点击委托仍可到达动作。
            if (!event.defaultPrevented) tree.click(button);
            assert.deepEqual(tree.calls, [expected]);
        }
    }
});

test('结构树后代输入等交互元素的方向键与空格不被行导航接管', () => {
    const tree = treeEvents();
    for (const kind of ['input', 'select', 'a', 'contenteditable']) {
        const control = { ...tree.descendant(tree.rows[0]), kind };
        for (const key of ['Enter', ' ', 'ArrowDown', 'ArrowUp', 'ArrowLeft', 'ArrowRight']) {
            assert.equal(tree.key(control, key).defaultPrevented, false, kind + ': ' + key);
        }
    }
    assert.deepEqual(tree.calls, []);
    assert.equal(tree.focused(), undefined);
});

test('树行自身保留选中、折叠展开和跳过隐藏行的方向导航', () => {
    const tree = treeEvents();
    const first = tree.rows[0];
    const last = tree.rows[2];
    assert.equal(tree.key(first, 'Enter').defaultPrevented, true);
    assert.deepEqual(tree.calls, [['select', 'first']]);
    assert.equal(tree.key(first, ' ').defaultPrevented, true);
    assert.equal(tree.key(first, 'ArrowRight').defaultPrevented, true);
    assert.equal(tree.key(first, 'ArrowLeft').defaultPrevented, true);
    assert.deepEqual(tree.calls.slice(1), [['toggle', 'first'], ['toggle', 'first'], ['toggle', 'first']]);
    assert.equal(tree.key(first, 'ArrowDown').defaultPrevented, true);
    assert.equal(tree.focused(), 'last');
    assert.equal(tree.key(last, 'ArrowUp').defaultPrevented, true);
    assert.equal(tree.focused(), 'first');
    assert.equal(tree.key(first, 'x').defaultPrevented, false);
});
