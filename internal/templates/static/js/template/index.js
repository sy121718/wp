// template/index.js — 模板编辑器的入口：四栏联动 + 保存 + 预览（iframe srcdoc）。
//
// 四栏（结构树 / 画布 / 属性 / 预览）的分工：
//
//   结构树（tree.js）   看见整棵树、加块删块移块
//   画布（本文件）      选中块的**子块卡片**——一键选中下一层
//   属性（attrs.js）    选中块的 attrs / text
//   预览（本文件）      iframe srcdoc ← **后端编译产物**
//
// ## 预览为什么是 iframe + srcdoc，而不是在本页拼 DOM
//
// 邮件的 HTML 是一堆 table + inline style + 一个 `<style>` 媒体查询。把它塞进当前文档
// 会污染后台样式（那是事故级的），而 srcdoc 是一个隔离的文档上下文。更要紧的是：
// 塞进去的内容必须是**后端编译产物** —— 前端拼一份就等于有两套生成器（预览对、发出去
// 不对），而这种差异没有任何测试会发现。
//
// ## 保存链路
//
// POST /admin/mail/template/save（body_json 走 JSON）→ 后端编译并落库 → 回带
// body_compiled_html / body_compiled_text → 只替换 iframe srcdoc。失败时把 NodeKey
// 定位原样显示（与画布的图校验同一约定）。

import {
    normalizeTree, createTree, addChild, removeNode, moveNode, setAttr, setText,
    findNode, toJSON, clone
} from './state.js';
import { renderTree, addableTypes } from './tree.js';
import { renderAttrs } from './attrs.js';

/* ---------- 读注入数据 ---------- */

function readJSON(id, fallback) {
    var el = document.getElementById(id);
    if (!el) { return fallback; }
    try { return JSON.parse(el.textContent || ''); } catch (e) { return fallback; }
}

var meta = readJSON('tplEditorMeta', {});
var tree = normalizeTree(meta.tree);
var history = [];
var future = [];
var selected = null;
var dirty = false;

/* ---------- DOM 引用 ---------- */

var treeBox = document.getElementById('tplTree');
var canvasBox = document.getElementById('tplCanvas');
var attrsBox = document.getElementById('tplAttrs');
var previewFrame = document.getElementById('tplPreview');
var statusEl = document.getElementById('tplStatus');
var previewTextEl = document.getElementById('tplPreviewText');

function setStatus(msg) {
    if (statusEl) { statusEl.textContent = msg || ''; }
}

function markDirty() {
    dirty = true;
    setStatus('有未保存的改动（Ctrl+S 保存并更新预览）');
}

/* ---------- 状态变更 ---------- */

/**
 * apply 接受一次树变更。
 *
 * 三个副作用都要在同一处发生：进撤销栈、标脏、重画。分散写的话，
 * 「某个入口忘了重画」的表现是「点了没反应」，而它不会让任何测试变红。
 */
function apply(next) {
    if (next === tree) { return; }
    history.push(clone(tree));
    if (history.length > 50) { history.shift(); }
    future = [];
    tree = next;
    markDirty();
    render();
}

function undo() {
    if (!history.length) { return; }
    future.push(clone(tree));
    tree = history.pop();
    markDirty();
    render();
}

function redo() {
    if (!future.length) { return; }
    history.push(clone(tree));
    tree = future.pop();
    markDirty();
    render();
}

/* ---------- 渲染 ---------- */

function render() {
    renderTreeInto();
    renderCanvasInto();
    renderAttrsInto();
    renderUndoState();
}

function renderTreeInto() {
    if (!treeBox) { return; }
    treeBox.innerHTML = '';
    treeBox.appendChild(renderTree(tree, {
        selected: selected,
        onSelect: select,
        onAdd: openAddMenu,
        onMove: function (key, dir) { apply(moveNode(tree, key, dir)); },
        onDelete: function (key) { apply(removeNode(tree, key)); if (selected === key) { selected = null; } }
    }));
}

/**
 * renderCanvasInto 画布栏：把**选中块的子块**画成卡片。
 *
 * 为什么不画整棵树的缩略图：邮件正文是纵向单列结构，整棵树画出来就是一棵树 ——
 * 结构树已经回答过那个问题。画布回答的是另一个问题：「这一层下面现在有哪些块、
 * 长什么样子」，点卡片直接跳到那个块。
 */
function renderCanvasInto() {
    if (!canvasBox) { return; }
    canvasBox.innerHTML = '';
    var doc = document;
    var host = selected ? findNode(tree, selected) : { node: tree.root };
    if (!host) { host = { node: tree.root }; }
    var kids = host.node.children || [];
    if (!kids.length) {
        var empty = doc.createElement('p');
        empty.className = 'tpl-canvas-empty';
        empty.textContent = '这一层还没有块。用左栏的 + 添加。';
        canvasBox.appendChild(empty);
        return;
    }
    kids.forEach(function (kid) {
        var card = doc.createElement('button');
        card.type = 'button';
        card.className = 'tpl-canvas-card';
        card.setAttribute('data-key', kid.key);
        if (kid.key === selected) { card.setAttribute('data-selected', '1'); }
        var type = doc.createElement('span');
        type.className = 'tpl-canvas-type';
        type.textContent = kid.type;
        card.appendChild(type);
        if (kid.text) {
            var text = doc.createElement('span');
            text.className = 'tpl-canvas-text';
            text.textContent = kid.text.length > 40 ? kid.text.slice(0, 40) + '…' : kid.text;
            card.appendChild(text);
        }
        card.addEventListener('click', function () { select(kid.key); });
        canvasBox.appendChild(card);
    });
}

function renderAttrsInto() {
    if (!attrsBox) { return; }
    attrsBox.innerHTML = '';
    var hit = selected ? findNode(tree, selected) : null;
    if (!hit) {
        var hint = document.createElement('p');
        hint.className = 'tpl-attr-hint';
        hint.textContent = '在左栏或画布选一个块，这里显示它的属性。';
        attrsBox.appendChild(hint);
        return;
    }
    attrsBox.appendChild(renderAttrs(hit.node, {
        onText: function (key, v) { apply(setText(tree, key, v)); },
        onAttr: function (key, attr, v) { apply(setAttr(tree, key, attr, v)); }
    }));
}

function renderUndoState() {
    var undoBtn = document.getElementById('tplUndo');
    var redoBtn = document.getElementById('tplRedo');
    if (undoBtn) { undoBtn.disabled = history.length === 0; }
    if (redoBtn) { redoBtn.disabled = future.length === 0; }
}

function select(key) {
    selected = key;
    render();
}

/* ---------- 加块菜单 ---------- */

function openAddMenu(parentKey) {
    var hit = findNode(tree, parentKey);
    if (!hit) { return; }
    var types = addableTypes(hit.node.type);
    if (!types.length) { setStatus('这个块不能再放子块。'); return; }

    var layer = document.createElement('div');
    layer.className = 'tpl-add-pop';
    layer.setAttribute('role', 'menu');
    types.forEach(function (t) {
        var b = document.createElement('button');
        b.type = 'button';
        b.className = 'tpl-add-item';
        b.textContent = t;
        b.setAttribute('role', 'menuitem');
        b.addEventListener('click', function () {
            close();
            apply(addChild(tree, parentKey, t));
        });
        layer.appendChild(b);
    });
    document.body.appendChild(layer);

    var rect = treeBox.getBoundingClientRect();
    layer.style.left = (rect.left + 24) + 'px';
    layer.style.top = (rect.top + 40) + 'px';

    function onDoc(ev) { if (!layer.contains(ev.target)) { close(); } }
    function onKey(ev) { if (ev.key === 'Escape') { close(); } }
    document.addEventListener('pointerdown', onDoc, true);
    document.addEventListener('keydown', onKey);

    var closed = false;
    function close() {
        if (closed) { return; }
        closed = true;
        document.removeEventListener('pointerdown', onDoc, true);
        document.removeEventListener('keydown', onKey);
        if (layer.parentNode) { layer.parentNode.removeChild(layer); }
    }
    var first = layer.querySelector('button');
    if (first) { first.focus(); }
}

/* ---------- 预览 ---------- */

/**
 * paintPreview 把后端编译产物塞进 iframe（srcdoc）。
 *
 * `srcdoc` 里的内容来自**服务端编译器**，前端只搬运字节 —— 这是「所见即所发」的唯一
 * 可行做法：一旦预览由前端拼，发信时后端拼，两者必然漂移。
 */
function paintPreview(html) {
    if (previewFrame && typeof html === 'string') {
        previewFrame.setAttribute('srcdoc', html);
    }
}

function paintText(text) {
    if (!previewTextEl) { return; }
    previewTextEl.textContent = text || '';
}

/* ---------- 保存 ---------- */

function csrf() {
    var m = document.querySelector('meta[name="csrf-token"]');
    return m ? (m.getAttribute('content') || '') : '';
}

function saveTemplate() {
    var keyEl = document.getElementById('tplKey');
    var subjectEl = document.getElementById('tplSubject');
    var nameEl = document.getElementById('tplName');
    var localeEl = document.getElementById('tplLocale');
    var varsEl = document.getElementById('tplVars');
    var key = keyEl ? String(keyEl.value || '').trim() : '';
    var subject = subjectEl ? String(subjectEl.value || '').trim() : '';
    if (!key) { setStatus('请先填模板标识'); if (keyEl) { keyEl.focus(); } return; }
    if (!subject) { setStatus('请先填主题'); if (subjectEl) { subjectEl.focus(); } return; }

    setStatus('保存中…');
    fetch('/admin/mail/template/save', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf() },
        credentials: 'same-origin',
        body: JSON.stringify({
            templateKey: key,
            locale: localeEl ? String(localeEl.value || '').trim() : '',
            name: nameEl ? String(nameEl.value || '').trim() : '',
            subject: subject,
            variables: varsEl ? String(varsEl.value || '').split(',').map(function (s) { return s.trim(); }).filter(Boolean) : [],
            // 可视化正文：块树。后端编译并回带产物（前端不拼 HTML）。
            bodyJson: toJSON(tree)
        })
    }).then(function (r) {
        return r.json().catch(function () { return {}; });
    }).then(function (body) {
        if (body && body.code === 200 && body.data) {
            dirty = false;
            paintPreview(body.data.bodyCompiledHtml || '');
            paintText(body.data.bodyCompiledText || '');
            setStatus('已保存，预览已更新');
        } else {
            // 编译失败时后端回带 NodeKey 定位（哪一块、为什么）——原样显示。
            setStatus('保存失败：' + ((body && body.message) || '未知错误'));
        }
    })['catch'](function () {
        setStatus('保存失败：网络错误');
    });
}

/* ---------- 事件绑定 ---------- */

document.addEventListener('keydown', function (ev) {
    if ((ev.ctrlKey || ev.metaKey) && (ev.key === 's' || ev.key === 'S')) {
        ev.preventDefault();
        saveTemplate();
        return;
    }
    if ((ev.ctrlKey || ev.metaKey) && (ev.key === 'z' || ev.key === 'Z')) {
        ev.preventDefault();
        if (ev.shiftKey) { redo(); } else { undo(); }
        return;
    }
    if ((ev.key === 'Delete' || ev.key === 'Backspace') && selected) {
        var tag = (ev.target && ev.target.tagName) || '';
        if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') { return; }
        ev.preventDefault();
        apply(removeNode(tree, selected));
        selected = null;
        render();
    }
});

var undoBtn = document.getElementById('tplUndo');
if (undoBtn) { undoBtn.addEventListener('click', undo); }
var redoBtn = document.getElementById('tplRedo');
if (redoBtn) { redoBtn.addEventListener('click', redo); }
var saveBtn = document.getElementById('tplSave');
if (saveBtn) { saveBtn.addEventListener('click', saveTemplate); }

window.addEventListener('beforeunload', function (ev) {
    if (!dirty) { return; }
    ev.preventDefault();
    ev.returnValue = '';
});

/* ---------- 启动 ---------- */

render();
// 首屏预览直接吃服务端的编译产物（打开编辑器就能看见现在长什么样）。
paintPreview(meta.compiledHtml || '');
paintText(meta.compiledText || '');
if (!meta.tree || !meta.tree.root || !meta.tree.root.children || !meta.tree.root.children.length) {
    setStatus('从左栏的 + 开始添加第一个块。保存后预览会更新。');
}
