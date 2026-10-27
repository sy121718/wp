/**
 * json_swap.js — htmx 扩展：响应是 JSON 时，用页面里已有的 Jet <template> 渲染 DOM。
 *
 * ## 规矩（本仓的唯一一条）
 *
 * **后端只准返回 {code, message, data}，不产 HTML 片段。** 局部刷新时，DOM 由前端
 * 用「页面已经渲染好的 Jet 模板」填充 —— 模板仍在 Jet 里，JS 里没有 HTML 字面量，
 * 也没有第二份词表（界面文案在渲染期取好，随 <template> 一起下发）。
 *
 * 这样 htmx 的 hx-get / hx-post / hx-target / hx-swap / hx-trigger / 同步策略全部照常，
 * 唯一变化是「DOM 从哪来」：从后端的 HTML 变成前端的模板 + data。
 *
 * ## 用法
 *
 *   {{* 模板写在 Jet 里，声明式；data-field 就是 {{ }} 的等价物 *}}
 *   <template id="tpl-lang-row">
 *     <tr>
 *       <td data-field="lang"></td>
 *       <td data-field="statusText"></td>
 *     </tr>
 *   </template>
 *
 *   <div hx-get="/api/page/langs?pageId=3"
 *        hx-trigger="load"
 *        hx-ext="json-swap"
 *        hx-target="this"
 *        hx-swap="innerHTML"
 *        data-json-template="#tpl-lang-row"
 *        data-json-rows="items"></div>
 *
 * 属性契约：
 *   data-json-template  行模板选择器（必填，除非 data-json-norender）
 *   data-json-rows      data 里数组所在的键；省略时 data 本身当作数组
 *   data-json-key       可选：行的唯一键字段，用于 data-json-patch 定位已有节点
 *   data-json-patch     可选："replace"（默认，整块重渲染）| "append" | "prepend"
 *   data-json-remove    可选：成功响应里 data 的某个键 → 其值匹配 data-json-key 时删掉那行
 *   data-json-norender  可选：不渲染任何内容（用于「删除成功只需移除节点」的场景）
 *   data-json-more      可选：页码参数名（如 "page"）—— 响应成功后把本元素的 hx-get
 *                       里的该参数推进到 data.page + 1（累计加载 = 取最新 N 条，
 *                       重复请求同一页是幂等的）。与下面两条配合时兼管可见性。
 *   data-json-more-target 可选：选择器 —— 把「推进后的 URL」写到这个元素的 hx-get 上。
 *                       触发按钮与「加载更多」是两个元素（前者带上查询参数、后者在面板里），
 *                       由扩展把参数搬过去，页面就不必为每行生成一个更多按钮。
 *   data-json-more-hide 可选：与 data-json-more 同用 —— hasMore 为假时隐藏本元素。
 *                       只给「加载更多」那个按钮用：触发按钮不该在没有下一页时消失。
 *   data-json-reveal    可选：选择器（可用逗号给多个）—— 响应成功后把这些元素从
 *                       hidden 变可见（行内那个「历史」按钮用它把本行的历史行放出来）。
 *   data-json-empty-target 可选：选择器 —— 空结果时把它从 hidden 放出、非空时收回。
 *                       「这个商品还没有记录」这类文案要留在 Jet 里（词表在前端只此一份），
 *                       所以由扩展统一管显隐，不写第二条脚本。
 *
 * ## 文案怎么来（关键设计）
 *
 * `<template>` 里**预置每种状态的节点**，用 `data-when="值"` 标出它属于哪个状态：
 *
 *   <td>
 *     <span data-when="excluded">{{tr("admin.page.langs.status.excluded", "已排除")}}</span>
 *     <span data-when="published">{{tr("admin.page.langs.status.published", "已发布")}}</span>
 *     <span data-when="draft">{{tr("admin.page.langs.status.draft", "未发布")}}</span>
 *   </td>
 *
 * JSON 只给状态码（`{"status":"excluded"}`），扩展按 `data-when` 显示命中的那个、隐藏其余。
 * 这样**译文仍然全部在 Jet 里按请求语言渲染**（既不违反「业务层只交事实」，
 * 也不需要在前端存第二份词表 —— 那是既有代码明确避免的事，见 admin.js 的 label()）。
 *
 * 一个节点同时可以有 `data-when` 与 `data-field`：先按 data-field 填文本（如果给了），
 * 再按 data-when 决定显隐。
 *
 * ## 失败怎么走
 *
 * `code !== 0` 或 HTTP >= 400：**不 swap**，message 走 toast，目标节点与用户
 * 已填内容一个字节都不动。这是「失败只提示」这条规矩的落点。
 *
 * ## 为什么不用 innerHTML
 *
 * 渲染只走 textContent / setAttribute，绝不拼 HTML 字符串。后端不产 HTML 是前提，
 * 但 data 来自数据库字段（标题、路径…），一旦有人往里塞 `<img onerror=…>`，
 * innerHTML 那条路会把它变成 XSS。属性名也走白名单（见 SAFE_ATTRS）。
 */
(function (global) {
    'use strict';

    var htmx = global.htmx;
    if (!htmx || typeof htmx.registerExtension !== 'function') { return; }

    // 允许通过 data-field-attr 写进模板节点的属性白名单。
    // 不放开任意属性：`href` / `src` 之类若能任意注入，等于把 XSS 换个位置。
    // 需要链接时在模板里写固定 href，把**变量部分**用 data-field-attr="data-id"。
    var SAFE_ATTRS = {
        'data-id': 1, 'data-lang': 1, 'data-value': 1, 'data-state': 1,
        'value': 1, 'title': 1, 'aria-label': 1, 'class': 1
    };

    function parseEnvelope(text) {
        var t = (text || '').trim();
        if (!t || t.charAt(0) !== '{') { return null; }
        var body;
        try { body = JSON.parse(t); } catch (e) { return null; }
        if (!body || typeof body !== 'object' || !('code' in body)) { return null; }
        return body;
    }

    // 本仓两种成功码：writeJSON 用 0，response.Success 也是 0；防御性接受 200。
    function isFailure(body, status) {
        if (status >= 400) { return true; }
        var code = Number(body.code);
        if (isNaN(code)) { return true; }
        return !(code === 0 || code === 200);
    }

    function toast(message, ok) {
        var t = global.WBUI && global.WBUI.toast;
        if (typeof t === 'function') { t(message || '', { type: ok ? 'ok' : 'error' }); return; }
        if (message) { (ok ? console.log : console.warn).call(console, message); }
    }

    function readPath(obj, path) {
        if (!path) { return obj; }
        var cur = obj;
        var parts = String(path).split('.');
        for (var i = 0; i < parts.length; i++) {
            if (cur === null || cur === undefined) { return undefined; }
            cur = cur[parts[i]];
        }
        return cur;
    }

    /** setField 把一个标量值写进节点：data-field 走 textContent，data-field-attr 走属性。 */
    function setField(node, key, value) {
        var text = value === null || value === undefined ? '' : String(value);
        var attr = node.getAttribute && node.getAttribute('data-field-attr');
        if (attr) {
            if (!SAFE_ATTRS[attr]) {
                console.error('json-swap: 不允许的属性 ' + attr + '（见 SAFE_ATTRS）');
                return;
            }
            node.setAttribute(attr, text);
            return;
        }
        node.textContent = text;
    }

    /**
     * fillRow 用 template 克隆一份，把 data 上的同名字段填进去（按 data-field）。
     *
     * 返回**行根元素**（不是 DocumentFragment）：调用方直接 appendChild 它，
     * 不依赖 `appendChild(DocumentFragment)` 的「移动子节点」语义 ——
     * 那条语义在真实 DOM 里成立，但它让「返回的是什么」变得含糊，
     * 而含糊的地方一定会有人接错。
     *
     * @param {HTMLTemplateElement} tpl
     * @param {object} item
     * @param {string} keyField 行唯一键字段名（可空）：有值时写进 [data-row-key]。
     */
    function fillRow(tpl, item, keyField) {
        var frag = tpl.content.cloneNode(true);
        var fields = frag.querySelectorAll ? frag.querySelectorAll('[data-field]') : [];
        for (var i = 0; i < fields.length; i++) {
            var f = fields[i];
            setField(f, f.getAttribute('data-field'), item ? item[f.getAttribute('data-field')] : '');
        }
        // data-when：模板里预置了每种状态的节点，按 data 的取值显隐。
        // 文案在 Jet 渲染期就已取好，这里只做显隐 —— 前端不存词表。
        var whens = frag.querySelectorAll ? frag.querySelectorAll('[data-when]') : [];
        for (var w = 0; w < whens.length; w++) {
            var node = whens[w];
            var want = node.getAttribute('data-when');
            var fieldName = node.getAttribute('data-when-field') || 'status';
            var actual = item ? item[fieldName] : undefined;
            if (String(actual) !== want) {
                if (node.parentNode) { node.parentNode.removeChild(node); }
            }
        }

        // 行根：[data-row] 缺省取第一个元素。
        var root = (frag.querySelector && frag.querySelector('[data-row]')) || frag.firstElementChild;
        if (keyField && item && root && item[keyField] !== undefined) {
            root.setAttribute('data-row-key', String(item[keyField]));
        }
        return root;
    }

    function rowsOf(data, rowsKey) {
        var v = readPath(data, rowsKey);
        if (Array.isArray(v)) { return v; }
        if (Array.isArray(data)) { return data; }
        return [];
    }

    htmx.registerExtension('json-swap', {
        init: function () { /* 只用 swap 钩子，不需要 API */ },

        // htmx 4 的扩展事件名：htmx_ + 事件路径（冒号换下划线）。
        htmx_before_swap: function (elt, detail) {
            var ctx = (detail && detail.ctx) || {};
            var body = parseEnvelope(ctx.text);
            if (!body) { return; }            // 不是本仓信封：交回 htmx 默认处理

            var status = (ctx.response && Number(ctx.response.status)) || 0;
            var tasks = detail.tasks;
            var target = ctx.target;

            // 失败：不 swap，只提示。
            if (isFailure(body, status)) {
                tasks.length = 0;
                toast(body.message, false);
                return;
            }

            if (!target) { tasks.length = 0; return; }

            var tplSel = elt.getAttribute('data-json-template') ||
                (target.getAttribute && target.getAttribute('data-json-template'));
            var rowsKey = elt.getAttribute('data-json-rows');
            var patch = elt.getAttribute('data-json-patch') || 'replace';
            var removeKey = elt.getAttribute('data-json-remove');
            var keyField = elt.getAttribute('data-json-key');
            var noRender = elt.hasAttribute('data-json-norender');

            // 「删除一行」这类：只按 key 摘掉对应节点，其余不动。
            if (removeKey) {
                var removed = readPath(body.data, removeKey);
                removeRow(target, keyField, removed);
                tasks.length = 0;
                if (body.message) { toast(body.message, true); }
                return;
            }

            if (noRender) {
                tasks.length = 0;
                if (body.message) { toast(body.message, true); }
                return;
            }

            var tpl = tplSel ? document.querySelector(tplSel) : null;
            if (!tpl) {
                tasks.length = 0;
                console.error('json-swap: 找不到行模板 ' + tplSel + '（data-json-template 写错或模板未渲染）');
                return;
            }

            var rows = rowsOf(body.data, rowsKey);
            var holder = document.createDocumentFragment();
            for (var i = 0; i < rows.length; i++) {
                holder.appendChild(fillRow(tpl, rows[i], keyField));
            }

            // 复用 htmx 已经算好的 main 任务，只换 fragment —— 不自己拼 swapSpec：
            // 那里有 hx-swap 修饰符（settle / focus-scroll / outer）的解析结果，
            // 手写一份必然与原语义分叉。
            var main = null;
            for (var j = 0; j < tasks.length; j++) {
                if (tasks[j].type === 'main') { main = tasks[j]; break; }
            }
            if (!main) { return; }   // htmx 判了不该 swap（空响应等）：按它的判断

            if (patch === 'append' || patch === 'prepend') {
                // 增量：不要 htmx 换整块，只把新行插进去。
                tasks.length = 0;
                // 去重：累计接口重复请求同一页时不产生重复行（data-json-key 为行唯一键）。
                if (keyField) { holder = dropKnownRows(holder, target, keyField); }
                var anchor = patch === 'append' ? null : target.firstChild;
                target.insertBefore(holder, anchor);
                if (global.htmx && typeof global.htmx.process === 'function') { global.htmx.process(target); }
            } else {
                main.fragment = holder;
                // 空结果标记：模板/样式据此显示空态。
                if (rows.length === 0) { target.setAttribute('data-json-empty', '1'); }
                else { target.removeAttribute('data-json-empty'); }
            }
            // 空态文案的显隐：只在「整块重渲染」时判（append 只会让行变多，不会变空）。
            if (patch !== 'append' && patch !== 'prepend') {
                toggleEmpty(elt, rows.length === 0);
            }

            // 累计加载的两件收尾（两种 patch 都要跑）：推进页码 / 放出被隐藏的面板。
            advanceMore(elt, body.data);
            revealTargets(elt);

            if (body.message) { toast(body.message, true); }
        }
    });

    /**
     * dropKnownRows 摘掉「目标里已经有同 key 的行」，返回新的 fragment。
     *
     * 为什么前端也要去重：累计接口（取最新 N 条）在重复点击「加载更多」时会重复下发
     * 已经渲染过的行。靠后端做去重就得放弃累计语义、改成游标分页；而游标会让前端
     * 存一个刷新即丢的隐形状态。行唯一键本来就在 data-json-key 里，这里顺手用掉。
     */
    function dropKnownRows(frag, target, keyField) {
        // 只按「属性存在」取节点、再比 value：属性选择器里带值（[k="v"]）在这个仓的
        // 测试桩（vm + 手写 DOM）里不成立，而这里只需要一个键集合。
        var known = {};
        var existing = target.querySelectorAll ? target.querySelectorAll('[data-row-key]') : [];
        for (var e = 0; e < existing.length; e++) {
            known[existing[e].getAttribute('data-row-key')] = 1;
        }
        var next = document.createDocumentFragment();
        // slice：appendChild 会把节点从 frag 里移走，边遍历边移动会跳过元素。
        var kids = (frag.children || []).slice();
        for (var i = 0; i < kids.length; i++) {
            var row = kids[i];
            var key = row.getAttribute ? row.getAttribute('data-row-key') : null;
            if (key === null || key === '' || !Object.prototype.hasOwnProperty.call(known, key)) {
                next.appendChild(row);
            }
        }
        return next;
    }

    /**
     * toggleEmpty 按 data-json-empty-target 显示/收回空态文案节点。
     *
     * 文案本身留在 Jet 模板里（前端不存第二份词表）；扩展只做显隐。
     */
    function toggleEmpty(elt, empty) {
        var sel = elt.getAttribute ? elt.getAttribute('data-json-empty-target') : null;
        if (!sel || !document.querySelectorAll) { return; }
        var nodes = document.querySelectorAll(sel);
        for (var i = 0; i < nodes.length; i++) {
            if (empty) { nodes[i].removeAttribute('hidden'); }
            else { nodes[i].setAttribute('hidden', ''); }
        }
    }

    /**
     * revealTargets 把 elt 用 data-json-reveal 点名的元素从 hidden 放出来。
     *
     * 用途是「打开面板」：面板与「加载更多」都渲染在页面里但初始 hidden，
     * 第一次请求成功后由这个属性放出 —— 比另写一段脚本管可见性省一整套状态。
     */
    function revealTargets(elt) {
        var sel = elt.getAttribute ? elt.getAttribute('data-json-reveal') : null;
        if (!sel) { return; }
        var nodes = document.querySelectorAll ? document.querySelectorAll(sel) : [];
        for (var i = 0; i < nodes.length; i++) {
            nodes[i].removeAttribute('hidden');
        }
    }

    /**
     * advanceMore 推进「加载更多」按钮：页码 +1，没有下一页就自己隐藏。
     *
     * 触发元素上声明 data-json-more="page"，响应里给 page / hasMore 两个事实 ——
     * 前端不需要另写脚本管页码状态，这一点与「DOM 由模板填充」是同一个思路。
     */
    function advanceMore(elt, data) {
        var param = elt.getAttribute && elt.getAttribute('data-json-more');
        if (!param) { return; }
        var carry = elt.getAttribute && elt.getAttribute('data-json-more-target');
        if (carry) {
            var host = document.querySelector ? document.querySelector(carry) : null;
            if (host) {
                host.setAttribute('hx-get', nextPageURL(elt, param, data));
                if (data && data.hasMore) { host.removeAttribute('hidden'); }
            }
        }
        var hasMore = !!(data && data.hasMore);
        var page = Number(data && data.page) || 0;
        if (!hasMore) {
            if (elt.hasAttribute('data-json-more-hide')) {
                elt.hidden = true;
                elt.setAttribute('aria-disabled', 'true');
            }
            return;
        }
        elt.hidden = false;
        elt.removeAttribute('aria-disabled');
        elt.setAttribute('hx-get', nextPageURL(elt, param, data));
    }

    /** nextPageURL 把 elt 的 hx-get 里的页码推进到 data.page + 1（缺参数时补上）。 */
    function nextPageURL(elt, param, data) {
        var url = elt.getAttribute('hx-get') || '';
        var next = String((Number(data && data.page) || 0) + 1);
        var re = new RegExp('([?&])' + param + '=\\d+');
        if (re.test(url)) { return url.replace(re, '$1' + param + '=' + next); }
        return url + (url.indexOf('?') >= 0 ? '&' : '?') + param + '=' + next;
    }

    function removeRow(target, keyField, value) {
        if (!keyField || value === null || value === undefined) { return; }
        var rows = target.querySelectorAll('[data-row-key]');
        for (var i = 0; i < rows.length; i++) {
            if (rows[i].getAttribute('data-row-key') === String(value)) {
                rows[i].parentNode.removeChild(rows[i]);
                return;
            }
        }
    }

    // 渲染器也可以按名字注册（表格/表单这类复杂结构，模板拷贝不够用时用）。
    var RENDERERS = {};
    function registerRenderer(name, render) { RENDERERS[name] = render; }

    global.WBJSON = {
        registerRenderer: registerRenderer,
        renderers: RENDERERS,
        parseEnvelope: parseEnvelope,
        isFailure: isFailure,
        fillRow: fillRow
    };
}(window));
