// 事件委托契约探针：属性来自真实 HTTP 响应，操作来自真实工作台模块。
// 此适配器只覆盖绑定与文档变更，不声称覆盖浏览器渲染、触屏和焦点。
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { bindRepeaterPanel } from '../../../../internal/templates/static/js/workbench/methods/controls/repeater.js';
import { alignKeyOf } from '../../../../internal/templates/static/js/workbench/palette.js';

const input = JSON.parse(readFileSync(0, 'utf8'));
const copy = value => JSON.parse(JSON.stringify(value));
const node = input.node;
const key = alignKeyOf(node.type);
const field = input.root['data-wb-rep-field'];
assert.ok(key && field);
const snapshots = [];
const results = [];
let sequence = 0;
const elements = input.elements.map(attrs => ({
    attrs,
    value: attrs.value || '',
    getAttribute(name) { return Object.hasOwn(this.attrs, name) ? this.attrs[name] : null; },
    closest() { return this; }
}));
const root = {
    attrs: input.root,
    listeners: {},
    getAttribute(name) { return this.attrs[name] ?? null; },
    contains(element) { return elements.includes(element); },
    addEventListener(type, fn) { (this.listeners[type] ||= []).push(fn); },
    dispatch(type, target) {
        for (const fn of this.listeners[type] || []) fn({ target });
        this['on' + type]?.({ target });
    }
};
const ctx = {
    node,
    panel: { querySelector() { return root; } },
    self: {
        snapshot() { snapshots.push(copy(node)); },
        renderTree() {}, renderUI() {}, syncInspector() {}, patchCanvas() {},
        makeIdAllocator() { return base => base + '-probe-' + (++sequence); }
    }
};
const record = name => results.push({ name, node: copy(node) });
const find = (name, value) => {
    const el = elements.find(el => el.attrs[name] === value);
    assert.ok(el, 'HTTP 片段缺少 ' + name + '=' + value);
    return el;
};

// 重复绑定同一根节点，模拟 morph 保留 DOM 后再次增强。
bindRepeaterPanel(ctx);
bindRepeaterPanel(ctx);
const text = find('data-wb-rep-input', '0');
text.value = '修改后的标题';
root.dispatch('change', text);
assert.equal(snapshots.length, 1, '一次编辑只能记录一次快照');
assert.equal(snapshots[0].props[key][0][field], '第一项', '快照必须保留修改前的文案');
assert.equal(node.props[key][0][field], '修改后的标题');
record('编辑文案');

text.value = '第二次修改';
root.dispatch('change', text);
assert.equal(snapshots.at(-1).props[key][0][field], '修改后的标题', '绑定必须读取最新数组');
record('连续编辑');
const extra = elements.find(el => el.attrs['data-wb-rep-extra']);
if (extra) {
    extra.checked = true;
    root.dispatch('change', extra);
    const extraKey = extra.attrs['data-wb-rep-extra'];
    assert.equal(Boolean(snapshots.at(-1).props[key][0][extraKey]), false);
    assert.equal(node.props[key][0][extraKey], true);
    record('默认展开');
}

const beforeAdd = snapshots.length;
root.dispatch('click', find('data-wb-rep-op', 'add'));
assert.equal(snapshots.length, beforeAdd + 1, '重复绑定不得造成双重添加');
assert.equal(node.props[key].length, 3);
assert.equal(node.children.length, 3);
record('添加');
const firstChild = node.children[0].id;
root.dispatch('click', find('data-wb-rep-op', 'move'));
assert.equal(node.children[1].id, firstChild);
assert.equal(node.props[key][1][field], '第二次修改');
record('调序');
root.dispatch('click', find('data-wb-rep-op', 'remove'));
assert.equal(node.children.length, 2);
assert.equal(node.props[key].length, 2);
record('删除');
Object.assign(node, copy(snapshots.at(-1)));
record('撤销删除');
assert.equal(node.children.length, 3);
assert.equal(node.props[key].length, 3);

// 缺骨架/旧字段名不能回退到另一套 DOM 后假装成功。
assert.throws(() => bindRepeaterPanel({ ...ctx, panel: { querySelector() { return null; } } }), /契约/);
input.root['data-wb-rep-field'] = 'wrongField';
assert.throws(() => bindRepeaterPanel(ctx), /契约/);
console.log(JSON.stringify(results));
