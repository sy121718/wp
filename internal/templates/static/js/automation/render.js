// automation/render.js — 把图渲染成 DOM 片段与 SVG 连线（纯函数，不碰全局 DOM）。
//
// 一条纪律：**这里不读 document、不挂事件**。渲染函数只吃数据、吐片段/字符串，
// 由 index.js 决定挂到哪、挂什么事件。这样渲染本身可以直接在 node 里跑（画布门禁就是
// 这么验的：layout 与 render 都是纯函数，不需要浏览器）。
//
// 连线用 SVG path，节点用 DOM。两者分两层（SVG 在下、节点在上），因为连线要能穿过
// 节点之间的空隙，而节点要能接收指针事件 —— 混在一层里必然要处理 z-index 与命中穿透。

import { outletsOf, isTerminal, outletLabel } from './state.js';

/** NODE_W / NODE_H 节点盒尺寸（与 automation.css 的 .auto-node 一致）。 */
export var NODE_W = 180;
export var NODE_H = 64;

/** escapeHTML 文本进 DOM 前转义（节点 key / 参数可能含用户输入）。 */
export function escapeHTML(s) {
    return String(s === undefined || s === null ? '' : s).replace(/[&<>"']/g, function (c) {
        return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
}

/** nodeLabel 节点类型的界面名（来自服务端注入的类型表；缺省回落到类型值本身）。 */
export function nodeLabel(state, type) {
    for (var i = 0; i < state.nodeTypes.length; i++) {
        if (state.nodeTypes[i].value === type) { return state.nodeTypes[i].label || type; }
    }
    return type;
}

/** nodeParamText 节点参数的摘要（节点盒上的一行小字）。 */
export function nodeParamText(node) {
    var p = node.params || {};
    if (node.type === 'delay') { return p.minutes ? (p.minutes + ' min') : ''; }
    if (node.type === 'email') { return String(p.template_key || ''); }
    if (node.type === 'tag') { return joinList(p.add) + (joinList(p.remove) ? ' / -' + joinList(p.remove) : ''); }
    if (node.type === 'filter') {
        var conds = Array.isArray(p.conditions) ? p.conditions : [];
        return (p.mode === 'any' ? 'any: ' : '') + conds.map(conditionText).join(' & ');
    }
    if (node.type === 'list_status_branch') { return 'status'; }
    if (node.type === 'ab_test') {
        var vs = Array.isArray(p.variants) ? p.variants : [];
        return vs.map(function (v) { return v.id + '=' + v.weight + '%'; }).join(' ');
    }
    if (node.type === 'webhook') { return String(p.url || ''); }
    return '';
}

function joinList(v) {
    if (Object.prototype.toString.call(v) === '[object Array]') { return v.join(', '); }
    return '';
}

function conditionText(c) {
    if (typeof c === 'string') { return c; }
    if (c && typeof c === 'object') {
        return String(c.field || '') + ' ' + String(c.op || '') + ' ' + String(c.value === undefined ? '' : c.value);
    }
    return '';
}

/**
 * renderNodes 生成节点层片段。
 *
 * 每个节点是可聚焦的 div（tabindex=0）：键盘用户 Tab 得进去、方向键能移动、Delete 能删。
 * 这不是锦上添花 —— 拖拽在触屏上体验本来就差，键盘路径是它的等价替代。
 *
 * @param {object} state 画布状态
 * @param {object} opts  {selected, entry, disabledTypes:{type:true}, document}
 */
export function renderNodes(state, opts) {
    var o = opts || {};
    var doc = o.document || (typeof document !== 'undefined' ? document : null);
    if (!doc) { return null; }
    var frag = doc.createDocumentFragment();
    for (var i = 0; i < state.nodes.length; i++) {
        var n = state.nodes[i];
        var el = doc.createElement('div');
        el.className = 'auto-node';
        el.setAttribute('data-key', n.key);
        el.setAttribute('data-type', n.type);
        el.setAttribute('tabindex', '0');
        el.setAttribute('role', 'button');
        el.style.left = (n.x || 0) + 'px';
        el.style.top = (n.y || 0) + 'px';
        el.style.width = NODE_W + 'px';
        if (n.key === o.selected) { el.setAttribute('data-selected', '1'); }
        if (o.disabledTypes && o.disabledTypes[n.type]) { el.setAttribute('data-disabled', '1'); }
        el.setAttribute('aria-label', ariaOf(state, n, n.key === (o.entry || state.entry)));

        var entryMark = n.key === (o.entry || state.entry)
            ? '<span class="auto-node-entry">' + escapeHTML(o.entryLabel || 'entry') + '</span>'
            : '';
        var param = nodeParamText(n);
        el.innerHTML = entryMark
            + '<div class="auto-node-key">' + escapeHTML(n.key) + '</div>'
            + '<div class="auto-node-type">' + escapeHTML(nodeLabel(state, n.type)) + '</div>'
            + (param ? '<div class="auto-node-param">' + escapeHTML(param) + '</div>' : '');
        frag.appendChild(el);
    }
    return frag;
}

function ariaOf(state, n, isEntry) {
    var parts = [n.key, nodeLabel(state, n.type)];
    if (isEntry) { parts.push('entry'); }
    var p = nodeParamText(n);
    if (p) { parts.push(p); }
    return parts.join(', ');
}

/**
 * renderPorts 给每个节点的**未连出口**画一个 `+` 圆钮（已连的出口不画 —— 它的用法是
 * 悬停边上的 [×] 断开，而不是再连一条）。
 *
 * 圆钮是画布唯一的「新建并连线」入口：点它弹出 palette，选中类型后自动从这条出口连到
 * 新节点。老画布只能先建节点、再去侧栏下拉改边，两步骤都属于「让用户理解图」，
 * 而这正是他没有的上下文。
 */
export function renderPorts(state, opts) {
    var o = opts || {};
    var doc = o.document || (typeof document !== 'undefined' ? document : null);
    if (!doc) { return null; }
    var frag = doc.createDocumentFragment();
    for (var i = 0; i < state.nodes.length; i++) {
        var n = state.nodes[i];
        if (isTerminal(n.type)) { continue; }
        var names = outletsOf(state, n.type);
        for (var j = 0; j < names.length; j++) {
            var name = names[j];
            if (n.out[name]) { continue; }
            var btn = doc.createElement('button');
            btn.type = 'button';
            btn.className = 'auto-port';
            btn.setAttribute('data-from', n.key);
            btn.setAttribute('data-outlet', name);
            btn.setAttribute('aria-label', 'add node after ' + name);
            btn.textContent = '+';
            // 半径 12（直径 24）：触屏上小圆钮命中率低，24 是可用下限。
            btn.style.left = ((n.x || 0) + NODE_W) + 'px';
            btn.style.top = ((n.y || 0) + portOffset(state, n, j, names.length)) + 'px';
            frag.appendChild(btn);
        }
    }
    return frag;
}

/** portOffset 多出口时纵向错开，避免两个圆钮叠在一起。 */
function portOffset(state, node, index, total) {
    if (total <= 1) { return NODE_H / 2 - 12; }
    var span = NODE_H - 24;
    return 12 + (span * index) / (total - 1);
}

/**
 * renderEdges 生成连线层的 SVG 字符串（调用方写成 svg.innerHTML）。
 *
 * 从源节点右边中点连到目标节点左边中点，用三次贝塞尔。目标在左边时（回绕）改从左边出发，
 * 避免线穿过节点本身。
 */
export function renderEdges(state, opts) {
    var o = opts || {};
    var byKey = {};
    var nodes = state.nodes;
    for (var i = 0; i < nodes.length; i++) { byKey[nodes[i].key] = nodes[i]; }

    var html = '<defs><marker id="autoArrow" viewBox="0 0 10 10" refX="9" refY="5"'
        + ' markerWidth="7" markerHeight="7" orient="auto-start-reverse">'
        + '<path d="M 0 0 L 10 5 L 0 10 z"></path></marker></defs>';
    for (var k = 0; k < nodes.length; k++) {
        var n = nodes[k];
        var names = Object.keys(n.out || {});
        for (var e = 0; e < names.length; e++) {
            var outlet = names[e];
            var to = n.out[outlet];
            var target = byKey[to];
            if (!target) { continue; }
            var d = edgePath(n, target);
            var cls = 'edge' + (outlet === 'yes' ? ' edge-yes' : (outlet === 'no' ? ' edge-no' : ''));
            html += '<g class="auto-edge" data-from="' + escapeHTML(n.key) + '" data-outlet="'
                + escapeHTML(outlet) + '">'
                + '<path class="' + cls + '" d="' + d + '" marker-end="url(#autoArrow)"></path>'
                + '<path class="edge-hit" d="' + d + '"></path>'
                + '</g>';
        }
    }
    return html;
}

/** edgePath 两点之间的贝塞尔路径（导出以便单测几何）。 */
export function edgePath(from, to) {
    var x1 = (from.x || 0) + NODE_W;
    var y1 = (from.y || 0) + NODE_H / 2;
    var x2 = (to.x || 0);
    var y2 = (to.y || 0) + NODE_H / 2;
    if (x2 < x1) {
        x1 = (from.x || 0);
        x2 = (to.x || 0) + NODE_W;
    }
    var dx = Math.max(40, Math.abs(x2 - x1) / 2);
    return 'M ' + x1 + ' ' + y1 + ' C ' + (x1 + dx) + ' ' + y1 + ', '
        + (x2 - dx) + ' ' + y2 + ', ' + x2 + ' ' + y2;
}

/**
 * renderMiniMap 缩略图：把整张图等比缩放成一个 160×100 的 SVG 字符串。
 *
 * 缩略图不是为了好看：图一大就要靠滚动找节点，而「我现在在哪、整张图长什么样」
 * 是唯一能靠缩略图回答的问题（Fit View 按钮点它，视口居中到点击处）。
 */
export function renderMiniMap(state, opts) {
    var o = opts || {};
    var w = o.width || 160;
    var h = o.height || 100;
    var nodes = state.nodes || [];
    if (!nodes.length) { return ''; }

    var minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    for (var i = 0; i < nodes.length; i++) {
        var n = nodes[i];
        minX = Math.min(minX, n.x || 0);
        minY = Math.min(minY, n.y || 0);
        maxX = Math.max(maxX, (n.x || 0) + NODE_W);
        maxY = Math.max(maxY, (n.y || 0) + NODE_H);
    }
    var pad = 20;
    var spanX = Math.max(1, maxX - minX + pad * 2);
    var spanY = Math.max(1, maxY - minY + pad * 2);
    var scale = Math.min(w / spanX, h / spanY);
    var offsetX = (w - spanX * scale) / 2;
    var offsetY = (h - spanY * scale) / 2;

    var box = '';
    for (var j = 0; j < nodes.length; j++) {
        var m = nodes[j];
        var rx = offsetX + ((m.x || 0) - minX + pad) * scale;
        var ry = offsetY + ((m.y || 0) - minY + pad) * scale;
        var cls = m.key === (o.selected) ? 'mini-node mini-selected' : 'mini-node';
        box += '<rect class="' + cls + '" x="' + rx.toFixed(1) + '" y="' + ry.toFixed(1)
            + '" width="' + (NODE_W * scale).toFixed(1) + '" height="' + (NODE_H * scale).toFixed(1)
            + '" rx="2"></rect>';
    }
    return '<svg class="auto-mini-svg" viewBox="0 0 ' + w + ' ' + h + '" width="' + w + '" height="' + h
        + '" aria-hidden="true">' + box + '</svg>';
}

/** edgeMidpoint 边的中点（插入按钮与断开按钮画在这里）。 */
export function edgeMidpoint(from, to) {
    var x1 = (from.x || 0) + NODE_W;
    var y1 = (from.y || 0) + NODE_H / 2;
    var x2 = (to.x || 0);
    var y2 = (to.y || 0) + NODE_H / 2;
    if (x2 < x1) {
        x1 = (from.x || 0);
        x2 = (to.x || 0) + NODE_W;
    }
    return { x: (x1 + x2) / 2, y: (y1 + y2) / 2 };
}
