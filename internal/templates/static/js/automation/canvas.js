// automation/canvas.js — 画布交互与保存（issue #38 P4）。
//
// 交互设计的两条主线：
//
//   1. **拖拽只负责摆放位置**。连线用侧栏下拉改 —— 鼠标 / 触屏 / 键盘都能用，
//      而「从端口拉出一条线」在触屏上要长按 + 命中判定，误操作率很高。
//      连线的真相在图数据里，画布只把它画出来。
//   2. **拖拽用 pointer events，不用 HTML5 drag and drop**。后者在触屏上完全无效。
//      配合 CSS 的 touch-action: none，触摸拖动才不会被浏览器当成页面滚动。

import {
    readJSON, layoutMissing, renderNodes, renderEdges, nodeParamText,
    parseParam, PARAM_HINT, NODE_LABEL
} from './graph.js';

var nodes = readJSON('autoNodes', []);
var meta = readJSON('autoMeta', {});

var canvas = document.getElementById('autoCanvas');
var svg = document.getElementById('autoEdges');
var side = document.getElementById('autoSide');
var form = document.getElementById('autoNodeForm');
var hint = document.getElementById('autoSideHint');
var statusEl = document.getElementById('autoStatus');
var emptyEl = document.getElementById('autoEmpty');

var selected = null;
var dirty = false;

// 兜底：容器或 SVG 层缺失时补建一个。真实模板里必定存在，但缺了不该让整个脚本抛错
// —— 脚本一抛错，页面上其它交互也跟着失效，排查时会误以为是别的问题。
if (!canvas) {
    canvas = document.createElement('div');
    canvas.className = 'auto-canvas';
    document.body.appendChild(canvas);
}
if (!svg) {
    svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.setAttribute('id', 'autoEdges');
    canvas.appendChild(svg);
}

/** setStatus 顶栏的一句话反馈（aria-live 会读出来）。 */
function setStatus(msg) {
    if (statusEl) { statusEl.textContent = msg || ''; }
}

function csrf() {
    var m = document.querySelector('meta[name="csrf-token"]');
    return m ? (m.getAttribute('content') || '') : '';
}

/**
 * stage 画布内容层。
 *
 * 用一个固定尺寸的内容层包住节点与 SVG：连线画在 SVG 里，如果 SVG 只有可视区那么大，
 * 超出视口的连线会被裁掉。内容层固定大小 + 画布滚动，连线就完整了。
 */
var stage = document.createElement('div');
stage.className = 'auto-stage';
stage.style.position = 'relative';
stage.style.width = '2400px';
stage.style.height = '1600px';
canvas.appendChild(stage);
stage.appendChild(svg);
if (emptyEl) { stage.appendChild(emptyEl); }

layoutMissing(nodes, 40, 30, 210);
if (emptyEl) { emptyEl.hidden = nodes.length > 0; }

renderNodes(stage, nodes, meta.entry, selectNode);
redraw();

function redraw() {
    renderEdges(svg, nodes);
    svg.setAttribute('width', '2400');
    svg.setAttribute('height', '1600');
}

function nodeEl(key) {
    return stage.querySelector('.auto-node[data-key="' + cssEscape(key) + '"]');
}

function cssEscape(s) {
    return String(s).replace(/(["\\])/g, '\\$1');
}

function markDirty() {
    dirty = true;
    setStatus('位置有改动，记得保存');
}

/* ---------- 选中与侧栏 ---------- */

function selectNode(n) {
    selected = n;
    var els = stage.querySelectorAll('.auto-node');
    for (var i = 0; i < els.length; i++) {
        els[i].setAttribute('data-selected', els[i].getAttribute('data-key') === n.key ? '1' : '0');
    }
    fillForm(n);
}

/** fillForm 把节点填进侧栏。出边用下拉：选项是全部节点 key + 「（无）」。 */
function fillForm(n) {
    if (!form) { return; }
    form.hidden = false;
    if (hint) { hint.hidden = true; }
    document.getElementById('fKey').value = n.key;
    document.getElementById('fType').value = NODE_LABEL[n.type] || n.type;
    document.getElementById('fParam').value = nodeParamText(n);
    document.getElementById('fParamHint').textContent = PARAM_HINT[n.type] || '';

    // 顺序要紧：先按「流程是否启用」整体禁用 / 启用，**再**按节点类型设出边可用性。
    // 反过来的话，整体启用那一遍会把「分支节点的 next 本来就该禁用」给覆盖掉
    // （表现为分支节点上 next 可编辑，但保存时根本不读它 —— 用户白改一遍）。
    var locked = meta.structureEditable === false;
    var inputs = form.querySelectorAll('input, select, button');
    for (var i = 0; i < inputs.length; i++) { inputs[i].disabled = locked; }

    // 分支节点的 next 无意义（它走 yes / no），非分支节点的 yes / no 无意义。
    fillSelect('fNext', n.next || '', locked || n.type === 'branch');
    fillSelect('fYes', n.yes || '', locked || n.type !== 'branch');
    fillSelect('fNo', n.no || '', locked || n.type !== 'branch');
    var lockNote = document.getElementById('autoLockNote');
    if (locked) {
        if (!lockNote) {
            lockNote = document.createElement('p');
            lockNote.id = 'autoLockNote';
            lockNote.className = 'auto-hint';
            form.appendChild(lockNote);
        }
        lockNote.textContent = '流程处于『启用中』，结构不可改。先暂停流程再改 —— 改图会让正在跑的人走岔。';
    } else if (lockNote) {
        lockNote.textContent = '';
    }
}

function fillSelect(id, current, disabled) {
    var sel = document.getElementById(id);
    if (!sel) { return; }
    var html = '<option value="">（无）</option>';
    for (var i = 0; i < nodes.length; i++) {
        var k = nodes[i].key;
        html += '<option value="' + k.replace(/"/g, '&quot;') + '"'
            + (k === current ? ' selected' : '') + '>' + k + '</option>';
    }
    sel.innerHTML = html;
    sel.value = current;
    sel.disabled = !!disabled;
}

/* ---------- 拖拽（pointer events，触屏可用） ---------- */

var drag = null;

stage.addEventListener('pointerdown', function (ev) {
    var el = ev.target.closest ? ev.target.closest('.auto-node') : null;
    if (!el) { return; }
    var n = nodeByKey(el.getAttribute('data-key'));
    if (!n) { return; }
    drag = { node: n, el: el, startX: ev.clientX, startY: ev.clientY, origX: n.x, origY: n.y };
    el.setAttribute('data-dragging', '1');
    // setPointerCapture：指针移出元素甚至移出画布后仍然收得到事件。
    if (el.setPointerCapture) { try { el.setPointerCapture(ev.pointerId); } catch (e) {} }
    ev.preventDefault();
});

stage.addEventListener('pointermove', function (ev) {
    if (!drag) { return; }
    var dx = ev.clientX - drag.startX;
    var dy = ev.clientY - drag.startY;
    moveTo(drag.node, drag.origX + dx, drag.origY + dy);
    ev.preventDefault();
});

function endDrag() {
    if (!drag) { return; }
    drag.el.removeAttribute('data-dragging');
    drag = null;
    markDirty();
}

stage.addEventListener('pointerup', endDrag);
stage.addEventListener('pointercancel', endDrag);

/** moveTo 移动节点到指定坐标并重画连线（坐标夹到内容层内，不许拖丢）。 */
function moveTo(n, x, y) {
    n.x = Math.max(0, Math.min(2320, Math.round(x)));
    n.y = Math.max(0, Math.min(1540, Math.round(y)));
    var el = nodeEl(n.key);
    if (el) {
        el.style.left = n.x + 'px';
        el.style.top = n.y + 'px';
    }
    redraw();
}

function nodeByKey(key) {
    for (var i = 0; i < nodes.length; i++) {
        if (nodes[i].key === key) { return nodes[i]; }
    }
    return null;
}

/* ---------- 键盘（方向键移动 / Enter 编辑） ---------- */

stage.addEventListener('keydown', function (ev) {
    var el = ev.target.closest ? ev.target.closest('.auto-node') : null;
    if (!el) { return; }
    var n = nodeByKey(el.getAttribute('data-key'));
    if (!n) { return; }
    var step = ev.shiftKey ? 40 : 8;
    var dx = 0, dy = 0;
    if (ev.key === 'ArrowLeft') { dx = -step; }
    else if (ev.key === 'ArrowRight') { dx = step; }
    else if (ev.key === 'ArrowUp') { dy = -step; }
    else if (ev.key === 'ArrowDown') { dy = step; }
    else if (ev.key === 'Enter' || ev.key === ' ') {
        selectNode(n);
        var p = document.getElementById('fParam');
        if (p && !p.disabled) { p.focus(); }
        ev.preventDefault();
        return;
    } else {
        return;
    }
    moveTo(n, n.x + dx, n.y + dy);
    el.setAttribute('data-moved', '1');
    window.setTimeout(function () { el.removeAttribute('data-moved'); }, 220);
    markDirty();
    ev.preventDefault();
});

/* ---------- 保存位置 ---------- */

var saveBtn = document.getElementById('autoSavePos');
if (saveBtn) {
    saveBtn.addEventListener('click', function () {
        saveBtn.disabled = true;
        setStatus('保存中…');
        var positions = {};
        for (var i = 0; i < nodes.length; i++) {
            positions[nodes[i].key] = { x: nodes[i].x, y: nodes[i].y };
        }
        fetch('/api/mail/automation/layout', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf() },
            credentials: 'same-origin',
            body: JSON.stringify({ id: meta.automationId, positions: positions })
        }).then(function (r) {
            return r.json().catch(function () { return {}; });
        }).then(function (body) {
            saveBtn.disabled = false;
            // 项目的成功码是 HTTP 200（见 pkg/response.Success），不是 0。
            // 写错不会影响保存本身，但会显示「保存失败：保存成功」这种自相矛盾的提示。
            if (body && body.code === 200) {
                dirty = false;
                setStatus('位置已保存（版本号不变）');
            } else {
                setStatus('保存失败：' + ((body && body.message) || '未知错误'));
            }
        })['catch'](function () {
            saveBtn.disabled = false;
            setStatus('保存失败：网络错误');
        });
    });
}

/* ---------- 保存结构（走与表单页同一套图校验） ---------- */

if (form) {
    form.addEventListener('submit', function (ev) {
        ev.preventDefault();
        if (!selected) { return; }
        var parsed = parseParam(selected.type, document.getElementById('fParam').value);
        if (!parsed.ok) {
            setStatus('参数有问题：' + parsed.error);
            return
        }
        selected.params = parsed.params;
        if (selected.type === 'branch') {
            selected.yes = document.getElementById('fYes').value;
            selected.no = document.getElementById('fNo').value;
            selected.next = '';
        } else {
            selected.next = document.getElementById('fNext').value;
            selected.yes = '';
            selected.no = '';
        }

        var def = { entry: meta.entry, nodes: nodes };
        setStatus('保存中…');
        fetch('/api/mail/automation/save', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf() },
            credentials: 'same-origin',
            body: JSON.stringify({
                id: meta.automationId,
                name: meta.name || '',
                description: meta.description || '',
                triggerType: meta.triggerType || 'manual',
                definition: def
            })
        }).then(function (r) {
            return r.json().catch(function () { return {}; });
        }).then(function (body) {
            if (body && body.code === 200) {
                setStatus('结构已保存，正在刷新…');
                window.location.reload();
            } else {
                // 图校验失败时把服务端的定位信息原样显示（它会说清是哪一条边 / 哪个节点）。
                setStatus('保存失败：' + ((body && body.message) || '未知错误'));
            }
        })['catch'](function () {
            setStatus('保存失败：网络错误');
        });
    });
}

// 离开前提醒未保存的位置改动。
window.addEventListener('beforeunload', function (ev) {
    if (!dirty) { return; }
    ev.preventDefault();
    ev.returnValue = '';
});
