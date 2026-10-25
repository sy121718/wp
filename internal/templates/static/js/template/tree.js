// template/tree.js — 结构树（左栏）：把块树画成一棵可点、可增、可删、可移的列表。
//
// 结构树是**唯一**能看见整棵树的地方（画布只显示当前选中的那一层及其子块的预览）。
// 它也承担「加块」的入口：`+` 按当前块的 accepts 白名单给出可选项 —— 白名单来自
// state.js 的 BLOCK_SPEC，而 BLOCK_SPEC 与 Go 侧的 validChildren 是同一份规则的两次
// 抄写……不，这里刻意只在前端写一份：后端校验在 CompileTemplate / ValidateTree，
// 前端这一份只用于**渲染可选项**，不参与合法性判定（后端才是判据）。
//
// 为什么不做「拖拽重排」：树的重排只影响同一父节点内的顺序，而按钮（上移 / 下移）
// 在键盘与触屏上都可用。拖拽要一套命中判定与吸附逻辑，收益远小于成本。

import { BLOCK_SPEC, BLOCK_ORDER, accepts } from './state.js';

/** renderTree 生成结构树的 DOM 片段。 */
export function renderTree(tree, opts) {
    var o = opts || {};
    var doc = o.document || document;
    var frag = doc.createDocumentFragment();
    walkInto(frag, doc, tree.root, 0, o);
    return frag;
}

function walkInto(frag, doc, node, depth, opts) {
    if (node.type !== 'body') {
        frag.appendChild(rowOf(doc, node, depth, opts));
    }
    for (var i = 0; i < node.children.length; i++) {
        walkInto(frag, doc, node.children[i], node.type === 'body' ? 0 : depth + 1, opts);
    }
}

function rowOf(doc, node, depth, opts) {
    var spec = BLOCK_SPEC[node.type] || { label: node.type };
    var row = doc.createElement('div');
    row.className = 'tpl-tree-row';
    row.setAttribute('data-key', node.key);
    row.setAttribute('data-depth', String(depth));
    row.style.paddingLeft = (depth * 14 + 6) + 'px';
    if (node.key === opts.selected) { row.setAttribute('data-selected', '1'); }
    row.setAttribute('role', 'treeitem');
    row.setAttribute('tabindex', '0');

    var label = doc.createElement('span');
    label.className = 'tpl-tree-label';
    label.textContent = spec.label || node.type;
    row.appendChild(label);

    // 文本块给一眼可见的摘要：结构树里所有块都叫「文本」的话，找不到要改的那一段。
    if (spec.hasText && node.text) {
        var brief = doc.createElement('span');
        brief.className = 'tpl-tree-brief';
        brief.textContent = node.text.length > 24 ? node.text.slice(0, 24) + '…' : node.text;
        row.appendChild(brief);
    }

    var actions = doc.createElement('span');
    actions.className = 'tpl-tree-actions';

    // 只有「能放子块」的块才给 +（其余块的 accepts 是空数组）。
    if (accepts(node.type).length) {
        actions.appendChild(actionBtn(doc, '+', 'add child block', function () {
            if (opts.onAdd) { opts.onAdd(node.key); }
        }));
    }
    actions.appendChild(actionBtn(doc, '↑', 'move up', function () {
        if (opts.onMove) { opts.onMove(node.key, -1); }
    }));
    actions.appendChild(actionBtn(doc, '↓', 'move down', function () {
        if (opts.onMove) { opts.onMove(node.key, 1); }
    }));
    actions.appendChild(actionBtn(doc, '×', 'delete block', function () {
        if (opts.onDelete) { opts.onDelete(node.key); }
    }));
    row.appendChild(actions);

    row.addEventListener('click', function (ev) {
        if (ev.target.closest && ev.target.closest('.tpl-tree-actions')) { return; }
        if (opts.onSelect) { opts.onSelect(node.key); }
    });
    row.addEventListener('keydown', function (ev) {
        // Enter / Space 选中（树本身是 role=treeitem，键盘可达是硬要求）。
        if (ev.key === 'Enter' || ev.key === ' ') {
            ev.preventDefault();
            if (opts.onSelect) { opts.onSelect(node.key); }
        }
    });
    return row;
}

function actionBtn(doc, text, label, fn) {
    var b = doc.createElement('button');
    b.type = 'button';
    b.className = 'tpl-tree-btn';
    b.textContent = text;
    b.setAttribute('aria-label', label);
    b.title = label;
    b.addEventListener('click', function (ev) {
        ev.stopPropagation();
        fn();
    });
    return b;
}

/** addableTypes 某块可加的子类型（palette 与结构树共用这份顺序）。 */
export function addableTypes(parentType) {
    var allowed = accepts(parentType);
    return BLOCK_ORDER.filter(function (t) { return allowed.indexOf(t) >= 0; });
}
