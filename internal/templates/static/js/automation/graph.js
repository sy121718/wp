// automation/graph.js — 画布的数据与渲染（issue #38 P4）。
//
// 与 canvas.js 的分工：这个文件只负责「把图数据变成 DOM 与连线」，不碰任何输入事件。
// 拆开的意义是渲染逻辑可以单独读、单独验证 —— 事件处理最容易把渲染代码搅成一团。

/** readJSON 从页面的 script[type=application/json] 里读数据（服务端注入，避免内联脚本）。 */
export function readJSON(id, fallback) {
    var el = document.getElementById(id);
    if (!el) { return fallback; }
    try {
        return JSON.parse(el.textContent || '');
    } catch (e) {
        return fallback;
    }
}

/** NODE_LABEL 节点类型的中文名（与表单页保持同一套说法）。 */
export var NODE_LABEL = {
    trigger: '入口',
    delay: '等待',
    email: '发邮件',
    branch: '条件分支',
    tag: '打标签',
    end: '结束'
};

/** PARAM_HINT 各类型的参数提示（与表单页同一份文案）。 */
export var PARAM_HINT = {
    trigger: '入口不需要参数。',
    delay: '等待分钟数，例如 1440 表示一天。',
    email: '邮件模板的模板 key。',
    branch: '条件（逗号分隔）：opened / clicked / subscribed / has_tag:标签。',
    tag: '要加的标签（逗号分隔）。',
    end: '结束不需要参数。'
};

/**
 * layoutMissing 给没有坐标的节点排一个默认位置。
 *
 * 为什么要这一步：老流程（表单页建的）没有 x / y，如果不排就会全部叠在 (0,0)，
 * 表现为「画布上只有一个节点」—— 用户会以为数据丢了。默认横向排开，一眼能看全。
 */
export function layoutMissing(nodes, startX, startY, stepX) {
    var x = startX, y = startY, col = 0;
    for (var i = 0; i < nodes.length; i++) {
        var n = nodes[i];
        if (typeof n.x !== 'number' || typeof n.y !== 'number') {
            n.x = x;
            n.y = y;
        }
        col++;
        if (col >= 4) { col = 0; x = startX; y += 140; } else { x += stepX; }
    }
    return nodes;
}

/**
 * nodeEdges 返回一个节点的所有出边：[{ to, kind }]。
 * kind 为 '' | 'yes' | 'no'，用于给连线加不同的颜色（分支两臂一眼可分）。
 */
export function nodeEdges(n) {
    var out = [];
    if (n.type === 'branch') {
        if (n.yes) { out.push({ to: n.yes, kind: 'yes' }); }
        if (n.no) { out.push({ to: n.no, kind: 'no' }); }
    } else if (n.next) {
        out.push({ to: n.next, kind: '' });
    }
    return out;
}

/** nodeParamText 把一个节点的参数还原成一行文本（与表单页同一套规则）。 */
export function nodeParamText(n) {
    var p = n.params || {};
    if (n.type === 'delay') { return p.minutes ? (p.minutes + ' 分钟') : ''; }
    if (n.type === 'email') { return p.template_key || ''; }
    if (n.type === 'tag') { return joinList(p.add); }
    if (n.type === 'branch') { return joinList(p.conditions); }
    return '';
}

/**
 * parseParam 把侧栏里的一行文本解析成该类型的 params 对象。
 *
 * **与表单页 parseAutomationForm 规则一致** —— 两处不一致的话，同一个流程
 * 在画布改一次、在表单改一次，参数会走出不同的形状。返回 { ok, params, error }。
 */
export function parseParam(type, raw) {
    var v = (raw || '').trim();
    if (type === 'delay') {
        var n = parseInt(v, 10);
        if (!isFinite(n) || n <= 0) { return { ok: false, error: '等待分钟数要填正整数' }; }
        return { ok: true, params: { minutes: n } };
    }
    if (type === 'email') {
        if (!v) { return { ok: false, error: '发信节点要选邮件模板' }; }
        return { ok: true, params: { template_key: v } };
    }
    if (type === 'tag') {
        var tags = splitList(v);
        if (!tags.length) { return { ok: false, error: '标签节点要填要加的标签' }; }
        return { ok: true, params: { add: tags } };
    }
    if (type === 'branch') {
        var conds = splitList(v);
        if (!conds.length) { return { ok: false, error: '条件分支至少填一个条件' }; }
        return { ok: true, params: { conditions: conds } };
    }
    return { ok: true, params: {} };
}

/** splitList 逗号 / 中文逗号 / 顿号分隔 → 去空白去空项（与后端 splitFormList 一致）。 */
export function splitList(raw) {
    return (raw || '').replace(/[，、]/g, ',').split(',')
        .map(function (s) { return s.trim(); })
        .filter(function (s) { return s !== ''; });
}

function joinList(v) {
    if (Object.prototype.toString.call(v) === '[object Array]') {
        return v.join(', ');
    }
    return '';
}

/**
 * renderNodes 在画布内容层里渲染节点。
 *
 * 每个节点是一个可聚焦的 div（tabindex=0）：键盘用户 Tab 得进去、方向键能移动、
 * Enter 能打开侧栏。这不是锦上添花 —— 无障碍是硬要求，而且触屏上拖拽体验本来就差，
 * 键盘路径是它的等价替代。
 */
export function renderNodes(stage, nodes, entryKey, onSelect) {
    for (var i = 0; i < nodes.length; i++) {
        var n = nodes[i];
        var el = document.createElement('div');
        el.className = 'auto-node';
        el.setAttribute('data-key', n.key);
        el.setAttribute('data-type', n.type);
        el.setAttribute('tabindex', '0');
        el.setAttribute('role', 'button');
        el.style.left = n.x + 'px';
        el.style.top = n.y + 'px';
        el.setAttribute('aria-label', nodeAria(n, n.key === entryKey));

        var entry = n.key === entryKey ? '<span class="auto-node-entry">入口</span>' : '';
        var param = nodeParamText(n);
        el.innerHTML = entry
            + '<div class="auto-node-key">' + escapeHTML(n.key) + '</div>'
            + '<div class="auto-node-type">' + escapeHTML(NODE_LABEL[n.type] || n.type) + '</div>'
            + (param ? '<div class="auto-node-param">' + escapeHTML(param) + '</div>' : '');

        el.addEventListener('click', function (node, ev) {
            ev.stopPropagation();
            onSelect(node);
        }.bind(null, n));
        stage.appendChild(el);
    }
}

function nodeAria(n, isEntry) {
    var parts = ['节点 ' + n.key, NODE_LABEL[n.type] || n.type];
    if (isEntry) { parts.push('流程入口'); }
    var p = nodeParamText(n);
    if (p) { parts.push('参数 ' + p); }
    return parts.join('，');
}

/**
 * renderEdges 画出所有连线。
 *
 * 从源节点右边中点连到目标节点左边中点，用三次贝塞尔。点在节点中心算，
 * 所以拖动节点后重画即可（不需要维护端口位置）。
 */
export function renderEdges(svg, nodes) {
    var byKey = {};
    for (var i = 0; i < nodes.length; i++) { byKey[nodes[i].key] = nodes[i]; }
    var sel = document.querySelectorAll('.auto-node');
    var box = {};
    for (var j = 0; j < sel.length; j++) {
        var el = sel[j];
        box[el.getAttribute('data-key')] = {
            x: el.offsetLeft, y: el.offsetTop, w: el.offsetWidth, h: el.offsetHeight
        };
    }

    var html = '<defs><marker id="autoArrow" viewBox="0 0 10 10" refX="9" refY="5"'
        + ' markerWidth="7" markerHeight="7" orient="auto-start-reverse">'
        + '<path d="M 0 0 L 10 5 L 0 10 z"></path></marker></defs>';
    for (var k = 0; k < nodes.length; k++) {
        var n = nodes[k];
        var from = box[n.key];
        if (!from) { continue; }
        var edges = nodeEdges(n);
        for (var e = 0; e < edges.length; e++) {
            var to = box[edges[e].to];
            if (!to) { continue; }
            var x1 = from.x + from.w, y1 = from.y + from.h / 2;
            var x2 = to.x, y2 = to.y + to.h / 2;
            // 目标在左边时（回绕），从源节点左边出发，避免线穿过节点。
            if (x2 < x1) { x1 = from.x; x2 = to.x + to.w; }
            var dx = Math.max(40, Math.abs(x2 - x1) / 2);
            var d = 'M ' + x1 + ' ' + y1 + ' C ' + (x1 + dx) + ' ' + y1 + ', '
                + (x2 - dx) + ' ' + y2 + ', ' + x2 + ' ' + y2;
            var cls = edges[e].kind ? (' edge-' + edges[e].kind) : '';
            html += '<path class="edge' + cls + '" d="' + d + '" marker-end="url(#autoArrow)"></path>';
        }
    }
    svg.innerHTML = html;
}

/** escapeHTML 文本进 DOM 前转义（节点 key / 参数可能含用户输入）。 */
export function escapeHTML(s) {
    return String(s).replace(/[&<>"']/g, function (c) {
        return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c];
    });
}
