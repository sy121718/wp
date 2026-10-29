const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');

function numberEditor(props = {}) {
    const node = { id: 'list-1', type: 'core.productList', props };
    const panel = { contains: el => el.connected !== false };
    const stripImports = source => source.replace(/^import\s*\{[\s\S]*?\}\s*from\s*'[^']*';/gm, '');
    const context = vm.createContext({ console, WeakSet, Number, document: { getElementById: () => panel } });
    vm.runInContext(stripImports(fs.readFileSync(__dirname + '/methods/controls/base.js', 'utf8'))
        .replace(/export function /g, 'function '), context);
    vm.runInContext(stripImports(fs.readFileSync(__dirname + '/methods/inspector.js', 'utf8'))
        .replace('export const inspectorMethods', 'var inspectorMethods'), context);
    const wb = {
        selectedId: node.id, current: node, history: [], saveState: 'saved',
        findNode(id) { return this.current.id === id ? this.current : null; },
        snapshot() { this.history.push(JSON.stringify(this.current)); this.saveState = 'dirty'; },
        applyLinkedFields() { return false; },
        renderTree() {}, refreshCanvas() {}, renderUI() {}, refreshFieldHints() {}
    };
    context.inspectorMethods.bindInspectorHtmx.call(wb);
    function field(path, value) {
        return {
            type: 'number', value, validity: { badInput: false },
            getAttribute(key) { return key === 'data-wb-path' ? path : key === 'data-wb-kind' ? 'number' : null; },
            hasAttribute(key) { return this.getAttribute(key) !== null; },
            closest() { return this; }
        };
    }
    return { wb, panel, node, field };
}

test('数值input及时进入草稿，后续change和失焦不重复历史', () => {
    const { wb, panel, node, field } = numberEditor();
    const input = field('pageSize', '2');
    panel.oninput({ target: input });
    assert.equal(node.props.pageSize, 2);
    assert.equal(wb.saveState, 'dirty');
    assert.equal(wb.history.length, 1);
    assert.equal(JSON.parse(wb.history[0]).props.pageSize, undefined);
    panel.onchange({ target: input });
    panel.onfocusout({ target: input });
    assert.equal(wb.history.length, 1);
    assert.equal(JSON.parse(JSON.stringify(node)).props.pageSize, 2);
});

test('嵌套数值支持负数和小数，不改写输入框原文本', () => {
    const { panel, node, field } = numberEditor();
    const input = field('advanced.offset', '-1.250');
    panel.oninput({ target: input });
    assert.equal(node.props.advanced.offset, -1.25);
    assert.equal(input.value, '-1.250');
});

test('半输入和IME中间态不提交，组合结束后提交有效数值', () => {
    const { wb, panel, node, field } = numberEditor({ pageSize: 8 });
    const input = field('pageSize', '');
    input.validity.badInput = true;
    panel.oninput({ target: input });
    panel.onchange({ target: input });
    panel.onfocusout({ target: input });
    assert.equal(node.props.pageSize, 8);
    assert.equal(wb.history.length, 0);
    assert.equal(input.value, '');
    input.validity.badInput = false;
    input.value = '2';
    panel.oninput({ target: input, isComposing: true });
    panel.oncompositionstart({ target: input });
    panel.oninput({ target: input });
    panel.onchange({ target: input });
    assert.equal(node.props.pageSize, 8);
    panel.oncompositionend({ target: input });
    panel.oninput({ target: input });
    assert.equal(node.props.pageSize, 2);
    assert.equal(wb.history.length, 1);
});

test('清空在结束编辑时保留既有空字符串语义，不推断默认或必填', () => {
    const { wb, panel, node, field } = numberEditor({ pageSize: 8 });
    const input = field('pageSize', '');
    panel.oninput({ target: input });
    assert.equal(node.props.pageSize, 8);
    panel.onfocusout({ target: input });
    panel.onchange({ target: input });
    assert.equal(node.props.pageSize, '');
    assert.equal(wb.history.length, 1);
});

test('旧节点或已移除控件的迟到输入不得提交', () => {
    for (const detached of [false, true]) {
        const { wb, panel, node, field } = numberEditor({ pageSize: 8 });
        const input = field('pageSize', '2');
        if (detached) input.connected = false;
        else wb.current = { ...node, props: { pageSize: 10 } };
        panel.oninput({ target: input });
        panel.onfocusout({ target: input });
        panel.onchange({ target: input });
        assert.equal(wb.history.length, 0);
        assert.equal(node.props.pageSize, 8);
    }
});
