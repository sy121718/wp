// automation/index.js — 画布的入口：串起 state / layout / render / menu 与事件绑定。
//
// ## 分工（每一层都不许越界）
//
//   state.js  图数据与命令栈（纯函数，不碰 DOM）
//   layout.js 分层自动布局（纯函数）
//   render.js 图 → DOM 片段 / SVG 字符串（纯函数，不碰全局 DOM）
//   menu.js   palette 与边菜单浮层（只造浮层，不改图）
//   index.js  ← 本文件：唯一持有「当前状态」与「DOM 引用」，把事件翻译成 state 操作
//
// ## 保存后不刷新
//
// 老画布保存成功就 location.reload()。对「新建流程」来说这是错的：地址栏还没有 id，
// 刷新会回到又一份空白草稿，用户会以为刚建的流程丢了（再点保存就多建一条）。现在保存
// 成功只吸收回执（把 id 写回地址栏）、清脏标记、更新状态栏。
//
// ## 启用中的流程图不能改结构
//
// 服务端在改图时推进版本，而**正在跑的实例读的是当前 definition** —— 改图会让它们撞到
// 已不存在的节点 key。所以 structureEditable=false 时保存要被拒绝并说清「先暂停流程」。

import { readJSON, parseParam, PARAM_HINT_FALLBACK } from './graph.js';
import {
    createState, nodeByKey, outletsOf, isTerminal, addNode, removeNode, moveNode,
    connect, disconnect, insertOnEdge, setParams, undo, redo, canUndo, canRedo,
    toDefinition, snapshot
} from './state.js';
import { layout, COL_W, ROW_H } from './layout.js';
import {
    renderNodes, renderPorts, renderEdges, renderMiniMap, edgeMidpoint,
    nodeLabel, nodeParamText, escapeHTML, NODE_W, NODE_H
} from './render.js';
import { openPalette, closePalette, openEdgeMenu } from './menu.js';

var rawNodes = readJSON('autoNodes', []);
var meta = readJSON('autoMeta', {});

// 兜底：容器或 SVG 层缺失时补建。真实模板里必定存在，但缺了不该让整个脚本抛错 ——
// 脚本一抛错，页面上其它交互也跟着失效，排查时会误以为是别的问题。
var canvasEl = document.getElementById('autoCanvas');
if (!canvasEl) {
    canvasEl = document.createElement('div');
    canvasEl.className = 'auto-canvas';
    canvasEl.id = 'autoCanvas';
    document.body.appendChild(canvasEl);
}
var svgEl = document.getElementById('autoEdges');
if (!svgEl) {
    svgEl = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svgEl.setAttribute('id', 'autoEdges');
    canvasEl.appendChild(svgEl);
}
var stageEl = document.getElementById('autoStage');
if (!stageEl) {
    stageEl = document.createElement('div');
    stageEl.id = 'autoStage';
    stageEl.className = 'auto-stage';
    canvasEl.appendChild(stageEl);
}
if (!svgEl.parentNode || svgEl.parentNode !== stageEl) { stageEl.insertBefore(svgEl, stageEl.firstChild); }

var nodesLayer = document.getElementById('autoNodesLayer');
if (!nodesLayer) {
    nodesLayer = document.createElement('div');
    nodesLayer.id = 'autoNodesLayer';
    nodesLayer.className = 'auto-nodes-layer';
    stageEl.appendChild(nodesLayer);
}
var portsLayer = document.getElementById('autoPortsLayer');
if (!portsLayer) {
    portsLayer = document.createElement('div');
    portsLayer.id = 'autoPortsLayer';
    portsLayer.className = 'auto-ports-layer';
    stageEl.appendChild(portsLayer);
}

var statusEl = document.getElementById('autoStatus');
var sideEl = document.getElementById('autoSide');
var sideHint = document.getElementById('autoSideHint');
var propForm = document.getElementById('autoNodeForm');
var miniMapEl = document.getElementById('autoMiniMap');
var undoBtn = document.getElementById('autoUndo');
var redoBtn = document.getElementById('autoRedo');
var fitBtn = document.getElementById('autoFit');
var nameEl = document.getElementById('autoName');
var triggerEl = document.getElementById('autoTrigger');
var saveBarBtn = document.getElementById('autoSave');

/** state 当前画布状态（唯一真相）。 */
var state = createState({
    entry: meta.entry,
    nodes: rawNodes,
    nodeTypes: meta.nodeTypes || []
});
var savedDef = snapshot(state);
var selected = null;
var dirty = false;
/** disabledTypes 由服务端 meta 给出（例如未配置营销 SMTP 时禁用 email 节点）。 */
var disabledTypes = {};
(meta.nodeTypes || []).forEach(function (t) {
    if (t.disabled) { disabledTypes[t.value] = t.disabledReason || true; }
});

/* ---------- 状态栏与脏标记 ---------- */

function setStatus(msg) {
    if (statusEl) { statusEl.textContent = msg || ''; }
}

function markDirty() {
    dirty = true;
    setStatus('有未保存的改动（Ctrl+S 保存）');
}

/* ---------- 渲染 ---------- */

/**
 * draw 重画整张图。
 *
 * 坐标系：节点与端口层用绝对定位（left/top = 节点坐标），内容层尺寸按最右/最下的节点
 * 动态撑开 —— 固定 2400×1600 的内容层在节点变多之后会把连线裁掉。
 */
function draw() {
    var pos = layout(state.nodes, state.entry, existingPositions());
    // 只有**新节点**（没有坐标）会被 layout 排位；已有坐标原样保留。
    state.nodes.forEach(function (n) {
        var p = pos[n.key];
        if (p) { n.x = p.x; n.y = p.y; }
    });

    var frag = renderNodes(state, { selected: selected, entry: state.entry, disabledTypes: disabledTypes });
    if (frag) {
        nodesLayer.innerHTML = '';
        nodesLayer.appendChild(frag);
    }
    var ports = renderPorts(state, {});
    if (ports) {
        portsLayer.innerHTML = '';
        portsLayer.appendChild(ports);
    }
    svgEl.innerHTML = renderEdges(state, {});
    var w = 0, h = 0;
    state.nodes.forEach(function (n) {
        w = Math.max(w, (n.x || 0) + NODE_W + 60);
        h = Math.max(h, (n.y || 0) + NODE_H + 60);
    });
    stageEl.style.width = Math.max(600, w) + 'px';
    stageEl.style.height = Math.max(400, h) + 'px';
    svgEl.setAttribute('width', String(Math.max(600, w)));
    svgEl.setAttribute('height', String(Math.max(400, h)));

    if (miniMapEl) { miniMapEl.innerHTML = renderMiniMap(state, { selected: selected }); }
    if (undoBtn) { undoBtn.disabled = !canUndo(state); }
    if (redoBtn) { redoBtn.disabled = !canRedo(state); }
    fillSidePanel();
}

/** existingPositions 当前实际坐标（layout 用它保住已摆好的位置）。 */
function existingPositions() {
    var keep = {};
    state.nodes.forEach(function (n) {
        if (typeof n.x === 'number' && typeof n.y === 'number') {
            keep[n.key] = { x: n.x, y: n.y };
        }
    });
    return keep;
}

function nodeEl(key) {
    return nodesLayer.querySelector('.auto-node[data-key="' + cssEscape(key) + '"]');
}

function cssEscape(s) {
    return String(s).replace(/(["\\])/g, '\\$1');
}

/* ---------- 侧栏属性面板 ---------- */

function fillSidePanel() {
    if (!propForm) { return; }
    var locked = meta.structureEditable === false;
    if (!selected) {
        propForm.hidden = true;
        if (sideHint) { sideHint.hidden = false; }
        return;
    }
    var n = nodeByKey(state, selected);
    if (!n) { selected = null; propForm.hidden = true; return; }
    propForm.hidden = false;
    if (sideHint) { sideHint.hidden = true; }
    var kEl = document.getElementById('fKey');
    var tEl = document.getElementById('fType');
    var pEl = document.getElementById('fParam');
    var pHint = document.getElementById('fParamHint');
    if (kEl) { kEl.value = n.key; }
    if (tEl) { tEl.value = nodeLabel(state, n.type); }
    if (pEl) {
        pEl.value = paramInputText(n);
        pEl.disabled = locked;
    }
    if (pHint) { pHint.textContent = PARAM_HINT_FALLBACK[n.type] || ''; }

    // 出边下拉：只列该类型**有**的出口，选项是全部节点 key + 「（无）」。
    var outletBox = document.getElementById('fOutlets');
    if (outletBox) {
        outletBox.innerHTML = '';
        var names = outletsOf(state, n.type);
        names.forEach(function (name) {
            var row = document.createElement('p');
            row.className = 'auto-outlet-row';
            var label = document.createElement('label');
            label.textContent = name;
            var sel = document.createElement('select');
            sel.className = 'form-select';
            sel.setAttribute('data-outlet', name);
            sel.disabled = locked;
            var none = document.createElement('option');
            none.value = '';
            none.textContent = '—';
            sel.appendChild(none);
            state.nodes.forEach(function (other) {
                if (other.key === n.key) { return; }
                var opt = document.createElement('option');
                opt.value = other.key;
                opt.textContent = other.key;
                if (n.out[name] === other.key) { opt.selected = true; }
                sel.appendChild(opt);
            });
            sel.addEventListener('change', function () {
                if (sel.value) {
                    apply(connect(state, n.key, name, sel.value));
                } else {
                    apply(disconnect(state, n.key, name));
                }
            });
            label.appendChild(sel);
            row.appendChild(label);
            outletBox.appendChild(row);
        });
    }
    var delBtn = document.getElementById('autoDeleteNode');
    if (delBtn) { delBtn.hidden = locked || isTerminal(n.type) === false && n.key === state.entry; }
    if (delBtn) { delBtn.disabled = locked || n.key === state.entry; }
}

/** paramInputText 参数 → 侧栏输入框里的一行文本（与 parseParam 互逆）。 */
function paramInputText(n) {
    var p = n.params || {};
    if (n.type === 'delay') { return p.minutes ? String(p.minutes) : ''; }
    if (n.type === 'email') { return String(p.template_key || ''); }
    if (n.type === 'tag') {
        var add = Array.isArray(p.add) ? p.add : [];
        var remove = Array.isArray(p.remove) ? p.remove : [];
        return add.join(', ') + (remove.length ? ' - ' + remove.join(', ') : '');
    }
    if (n.type === 'filter') {
        var conds = Array.isArray(p.conditions) ? p.conditions : [];
        return conds.map(function (c) { return typeof c === 'string' ? c : (c.field + ' ' + c.op + ' ' + c.value); }).join(', ');
    }
    if (n.type === 'webhook') { return String(p.url || ''); }
    return '';
}

/** apply 接受一次状态变更（并同步脏标记与重画）。 */
function apply(next) {
    if (next === state) { return; }
    state = next;
    markDirty();
    draw();
}

function select(key) {
    selected = key;
    draw();
}

/* ---------- 事件：建节点 / 出口圆钮 / 边菜单 ---------- */

/** paletteTypes 把服务端类型表翻成 palette 需要的形状（禁用的带上原因）。 */
function paletteTypes() {
    return (meta.nodeTypes || []).map(function (t) {
        return {
            value: t.value,
            label: t.label,
            hint: t.hint,
            disabled: !!disabledTypes[t.value],
            disabledReason: typeof disabledTypes[t.value] === 'string' ? disabledTypes[t.value] : ''
        };
    });
}

function viewportCenter() {
    var rect = canvasEl.getBoundingClientRect();
    return {
        x: canvasEl.scrollLeft + rect.width / 2 - NODE_W / 2,
        y: canvasEl.scrollTop + rect.height / 2 - NODE_H / 2
    };
}

function addAt(type, at, fromOutlet) {
    if (disabledTypes[type]) {
        setStatus('该节点类型当前不可用：' + (typeof disabledTypes[type] === 'string' ? disabledTypes[type] : '未配置'));
        return;
    }
    var res = addNode(state, type, at, fromOutlet);
    state = res.state;
    selected = res.key;
    markDirty();
    draw();
}

function openPaletteAt(x, y, fromOutlet, anchor) {
    openPalette({
        types: paletteTypes(),
        x: x, y: y, anchor: anchor,
        title: fromOutlet ? ('连接出口 ' + fromOutlet.outlet) : '添加节点',
        onPick: function (type) {
            var at = fromOutlet ? null : viewportCenter();
            if (fromOutlet) {
                var src = nodeByKey(state, fromOutlet.from);
                at = src ? { x: (src.x || 0) + COL_W, y: src.y || 0 } : viewportCenter();
            }
            addAt(type, at, fromOutlet);
        }
    });
}

// 左栏 palette（模板里的 .auto-palette 按钮）
document.addEventListener('click', function (ev) {
    var btn = ev.target.closest ? ev.target.closest('[data-palette-type]') : null;
    if (!btn) { return; }
    ev.preventDefault();
    addAt(btn.getAttribute('data-palette-type'), viewportCenter(), null);
});

// 出口圆钮：点开 palette，选中后自动从这条出口连到新节点。
portsLayer.addEventListener('click', function (ev) {
    var btn = ev.target.closest ? ev.target.closest('.auto-port') : null;
    if (!btn) { return; }
    ev.preventDefault();
    ev.stopPropagation();
    var rect = btn.getBoundingClientRect();
    openPaletteAt(rect.left, rect.bottom + 4, {
        from: btn.getAttribute('data-from'),
        outlet: btn.getAttribute('data-outlet')
    }, btn);
});

// 边：悬停出 [+] / [×]（用一个跟随鼠标的浮层菜单，点 + 在中间插入节点）
var edgeHot = null;
svgEl.addEventListener('pointermove', function (ev) {
    var g = ev.target.closest ? ev.target.closest('.auto-edge') : null;
    if (!g) { return; }
    edgeHot = { from: g.getAttribute('data-from'), outlet: g.getAttribute('data-outlet') };
    if (edgeMenuOpen) { return; }
    var rect = canvasEl.getBoundingClientRect();
    showEdgeMenu(edgeHot, {
        x: Math.min(ev.clientX + 8, rect.right - 60),
        y: Math.min(ev.clientY + 8, rect.bottom - 44)
    });
});

var edgeMenuOpen = null;
function showEdgeMenu(edge, at) {
    var from = nodeByKey(state, edge.from);
    if (!from) { return; }
    edgeMenuOpen = openEdgeMenu({
        x: at.x, y: at.y,
        insertTitle: '在这条边中间插入节点',
        deleteTitle: '断开这条边',
        onInsert: function () {
            edgeMenuOpen = null;
            openPalette({
                types: paletteTypes(),
                x: at.x, y: at.y, title: '插入节点',
                onPick: function (type) {
                    var res = insertOnEdge(state, edge.from, edge.outlet, type);
                    if (res.state === state) { return; }
                    state = res.state;
                    selected = res.key || null;
                    markDirty();
                    draw();
                }
            });
        },
        onDelete: function () {
            edgeMenuOpen = null;
            apply(disconnect(state, edge.from, edge.outlet));
        }
    });
}

/* ---------- 事件：拖拽（pointer events，触屏可用） ---------- */

var drag = null;
nodesLayer.addEventListener('pointerdown', function (ev) {
    var el = ev.target.closest ? ev.target.closest('.auto-node') : null;
    if (!el) { return; }
    if (meta.structureEditable === false) { select(el.getAttribute('data-key')); return; }
    var n = nodeByKey(state, el.getAttribute('data-key'));
    if (!n) { return; }
    drag = { key: n.key, el: el, startX: ev.clientX, startY: ev.clientY, origX: n.x, origY: n.y, moved: false };
    el.setAttribute('data-dragging', '1');
    if (el.setPointerCapture) { try { el.setPointerCapture(ev.pointerId); } catch (e) { /* 老浏览器无 capture */ } }
    select(n.key);
});

nodesLayer.addEventListener('pointermove', function (ev) {
    if (!drag) { return; }
    var dx = ev.clientX - drag.startX;
    var dy = ev.clientY - drag.startY;
    if (!drag.moved && Math.abs(dx) < 3 && Math.abs(dy) < 3) { return; }
    drag.moved = true;
    state = moveNode(state, drag.key, drag.origX + dx, drag.origY + dy);
    var n = nodeByKey(state, drag.key);
    if (n) {
        drag.el.style.left = n.x + 'px';
        drag.el.style.top = n.y + 'px';
    }
    svgEl.innerHTML = renderEdges(state, {});
    var ports = renderPorts(state, {});
    if (ports) { portsLayer.innerHTML = ''; portsLayer.appendChild(ports); }
    ev.preventDefault();
});

function endDrag() {
    if (!drag) { return; }
    drag.el.removeAttribute('data-dragging');
    var moved = drag.moved;
    drag = null;
    if (moved) { markDirty(); }
    draw();
}
nodesLayer.addEventListener('pointerup', endDrag);
nodesLayer.addEventListener('pointercancel', endDrag);

/* ---------- 事件：键盘 ---------- */

document.addEventListener('keydown', function (ev) {
    // Ctrl/Cmd+S 保存：浏览器默认是「另存网页」，这里必须拦掉。
    if ((ev.ctrlKey || ev.metaKey) && (ev.key === 's' || ev.key === 'S')) {
        ev.preventDefault();
        saveStructure();
        return;
    }
    // Ctrl+Z / Ctrl+Shift+Z（含 Ctrl+Y）撤销重做。
    if ((ev.ctrlKey || ev.metaKey) && (ev.key === 'z' || ev.key === 'Z')) {
        ev.preventDefault();
        if (ev.shiftKey) { doRedo(); } else { doUndo(); }
        return;
    }
    if ((ev.ctrlKey || ev.metaKey) && (ev.key === 'y' || ev.key === 'Y')) {
        ev.preventDefault();
        doRedo();
        return;
    }
    // 输入框里不接管 Del / 方向键。
    var tag = (ev.target && ev.target.tagName) || '';
    if (tag === 'INPUT' || tag === 'SELECT' || tag === 'TEXTAREA') { return; }

    if ((ev.key === 'Delete' || ev.key === 'Backspace') && selected) {
        ev.preventDefault();
        deleteSelected();
        return;
    }
    if (!selected) { return; }
    var step = ev.shiftKey ? 40 : 8;
    var dx = 0, dy = 0;
    if (ev.key === 'ArrowLeft') { dx = -step; }
    else if (ev.key === 'ArrowRight') { dx = step; }
    else if (ev.key === 'ArrowUp') { dy = -step; }
    else if (ev.key === 'ArrowDown') { dy = step; }
    else { return; }
    ev.preventDefault();
    var n = nodeByKey(state, selected);
    if (!n) { return; }
    state = moveNode(state, n.key, n.x + dx, n.y + dy);
    markDirty();
    draw();
});

function deleteSelected() {
    if (!selected) { return; }
    if (meta.structureEditable === false) { setStatus('流程启用中，结构不可改。先暂停流程再改。'); return; }
    var n = nodeByKey(state, selected);
    if (!n) { return; }
    if (n.key === state.entry) { setStatus('入口节点不能删除（换个入口再删它）。'); return; }
    state = removeNode(state, selected);
    selected = null;
    markDirty();
    draw();
}

/* ---------- 撤销 / 重做 / 自动布局 ---------- */

function doUndo() {
    var next = undo(state);
    if (next === state) { return; }
    state = next;
    if (selected && !nodeByKey(state, selected)) { selected = null; }
    markDirty();
    draw();
}

function doRedo() {
    var next = redo(state);
    if (next === state) { return; }
    state = next;
    if (selected && !nodeByKey(state, selected)) { selected = null; }
    markDirty();
    draw();
}

if (undoBtn) { undoBtn.addEventListener('click', doUndo); }
if (redoBtn) { redoBtn.addEventListener('click', doRedo); }
if (fitBtn) {
    fitBtn.addEventListener('click', function () {
        canvasEl.scrollTo(0, 0);
        var pos = layout(state.nodes, state.entry, {});
        // 重新布局 = **清掉全部坐标**再排（这是唯一会覆盖已有位置的入口，用户显式点了它）。
        state.nodes.forEach(function (n) {
            if (pos[n.key]) { n.x = pos[n.key].x; n.y = pos[n.key].y; }
        });
        markDirty();
        draw();
    });
}
if (miniMapEl) {
    miniMapEl.addEventListener('click', function (ev) {
        var rect = miniMapEl.getBoundingClientRect();
        var fx = Math.max(0, Math.min(1, (ev.clientX - rect.left) / rect.width));
        var fy = Math.max(0, Math.min(1, (ev.clientY - rect.top) / rect.height));
        var maxX = 0, maxY = 0;
        state.nodes.forEach(function (n) {
            maxX = Math.max(maxX, (n.x || 0) + NODE_W);
            maxY = Math.max(maxY, (n.y || 0) + NODE_H);
        });
        canvasEl.scrollTo(Math.max(0, fx * maxX - canvasEl.clientWidth / 2),
            Math.max(0, fy * maxY - canvasEl.clientHeight / 2));
    });
}

/* ---------- 保存 ---------- */

function csrf() {
    var m = document.querySelector('meta[name="csrf-token"]');
    return m ? (m.getAttribute('content') || '') : '';
}

/** foldSidePanel 把侧栏里**正在编辑**的参数与出边折进图（保存前必须先做）。 */
function foldSidePanel() {
    if (!selected || !propForm) { return true; }
    var n = nodeByKey(state, selected);
    if (!n) { return true; }
    var pEl = document.getElementById('fParam');
    if (!pEl || pEl.disabled) { return true; }
    var parsed = parseParam(n.type, pEl.value);
    if (!parsed.ok) {
        setStatus('参数有问题：' + parsed.error);
        return false;
    }
    var changed = JSON.stringify(n.params) !== JSON.stringify(parsed.params);
    if (changed) { state = setParams(state, n.key, parsed.params); }
    return true;
}

function saveStructure() {
    if (meta.structureEditable === false) {
        setStatus('流程启用中，结构不可改。先暂停流程再保存 —— 改图会让正在跑的人走岔。');
        return;
    }
    if (!foldSidePanel()) { return; }
    var name = nameEl ? String(nameEl.value || '').trim() : (meta.name || '');
    if (!name) {
        setStatus('请先填流程名称');
        if (nameEl) { nameEl.focus(); }
        return;
    }
    var triggerType = (triggerEl && triggerEl.value) || meta.triggerType || 'manual';
    setStatus('保存中…');
    fetch('/api/mail/automation/save', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf() },
        credentials: 'same-origin',
        body: JSON.stringify({
            id: meta.automationId || 0,
            name: name,
            description: meta.description || '',
            triggerType: triggerType,
            // 触发条件**只在触发方式没变时**回传：它是流程级配置，不属于图。
            // 触发方式一改，旧条件就是错的（tag_added 的 {tag:vip} 留在 contact_created
            // 上会让 triggerParamsMatch 恒为 false —— 该流程此后永不触发）。
            triggerParams: triggerType === (meta.triggerType || 'manual') ? (meta.triggerParams || {}) : {},
            definition: toDefinition(state)
        })
    }).then(function (r) {
        return r.json().catch(function () { return {}; });
    }).then(function (body) {
        if (body && body.code === 200) {
            adoptSaved(body.data || {});
            dirty = false;
            setStatus('已保存' + (body.data && body.data.version ? '（版本 ' + body.data.version + '）' : ''));
        } else {
            setStatus('保存失败：' + ((body && body.message) || '未知错误'));
        }
    })['catch'](function () {
        setStatus('保存失败：网络错误');
    });
}

/** adoptSaved 吸收保存回执（不刷新页面）：新建时把 id 写回地址栏，此后就是「更新」。 */
function adoptSaved(item) {
    if (item.id) {
        meta.automationId = item.id;
        if (typeof window.history !== 'undefined' && window.history.replaceState) {
            var url = new URL(window.location.href);
            url.searchParams.set('id', String(item.id));
            window.history.replaceState({}, '', url.toString());
        }
    }
    if (item.version) { meta.version = item.version; }
    if (item.status) { meta.status = item.status; }
    if (item.name) { meta.name = item.name; }
    if (item.triggerType) { meta.triggerType = item.triggerType; }
    var badge = document.getElementById('autoVersionBadge');
    if (badge) { badge.textContent = 'v' + (item.version || 1) + ' · ' + (item.status || 'draft'); }
    savedDef = snapshot(state);
}

if (saveBarBtn) { saveBarBtn.addEventListener('click', saveStructure); }

/* ---------- 位置保存（不推进版本号） ---------- */

var savePosBtn = document.getElementById('autoSavePos');
if (savePosBtn) {
    savePosBtn.addEventListener('click', function () {
        var id = meta.automationId || 0;
        if (!id) { setStatus('流程还没保存过，先按「保存结构」建出流程。'); return; }
        savePosBtn.disabled = true;
        setStatus('保存中…');
        var positions = {};
        state.nodes.forEach(function (n) { positions[n.key] = { x: n.x, y: n.y }; });
        fetch('/api/mail/automation/layout', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf() },
            credentials: 'same-origin',
            body: JSON.stringify({ id: id, positions: positions })
        }).then(function (r) {
            return r.json().catch(function () { return {}; });
        }).then(function (body) {
            savePosBtn.disabled = false;
            if (body && body.code === 200) {
                setStatus('位置已保存（版本号不变）');
            } else {
                setStatus('保存失败：' + ((body && body.message) || '未知错误'));
            }
        })['catch'](function () {
            savePosBtn.disabled = false;
            setStatus('保存失败：网络错误');
        });
    });
}

/* ---------- 侧栏表单提交（回车 / 表单内按钮） ---------- */

if (propForm) {
    propForm.addEventListener('submit', function (ev) {
        ev.preventDefault();
        saveStructure();
    });
}

// 离开前提醒未保存的改动。
window.addEventListener('beforeunload', function (ev) {
    if (!dirty) { return; }
    ev.preventDefault();
    ev.returnValue = '';
});

/* ---------- 启动 ---------- */

draw();
if (meta.isNew || !state.nodes.length) {
    setStatus('按左栏添加节点，或点节点右侧的 + 连下一步。');
}
