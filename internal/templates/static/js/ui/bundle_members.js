/* ui/bundle_members.js — 捆绑商品页「按来源解析并追加成员」的交互。
 *
 * 为什么单独成文件：这段逻辑原来写在 product_bundle.html 的内联 <script> 里（约 150 行），
 * 与模板同生共死 —— 无法单测、无法被 CSP 收紧、改动散在 HTML 里。模板只该给 data-* 事实，
 * 行为在这里；形态与 ui/json_write.js / ui/product_bundle_write.js 一致（声明式 + 事件委托）。
 *
 * 模板侧契约（internal/templates/admin/product/product_bundle.html）：
 *   [data-bundle-form-host]           作用域根（成员清单与候选模板都在里面）
 *   [data-bundle-member-list]         <tbody>，成员行的容器；带 data-bundle-max（上限）
 *   [data-bundle-member-row]          一行成员
 *   [data-bundle-member-tpl]          <template>，新行结构（与服务端已存行同一种结构）
 *   [data-bundle-member-remove]       删行按钮
 *   [data-bundle-candidate]           候选 <select>（选中即追加）
 *   [data-bundle-candidate-template]  候选行的 <template>（行被删空后补回）
 *   [data-bundle-resolve]             解析按钮：data-panel / data-url / data-csrf /
 *                                     data-project / data-product / data-msg-*
 *   [data-bundle-source-status]       role=status 的文案位
 *
 * 无 JS 时解析按钮是不可用的（模板里带 data-requires-js hidden disabled，控件基座在初始化时
 * 解除）—— 这是「不落库的取数」而不是写动作，js 缺失时必须显式隐藏，不能留一个按了没反应的按钮。
 *
 * 事件挂在 document 上而不是画在模板里的行上：行由本文件追加，按钮与行可能出现在任何来源面板里。
 */
(function (global) {
    'use strict';

    var ROW_SEL = '[data-bundle-member-row]';
    var MAX_DEFAULT = 20;
    // 行内具名字段 → 服务端 members 项的字段名（只有三个前后缀不同，其余同名）。
    var FIELDS = ['variantId', 'sourceKind', 'memberWarehouseId', 'memberWarehouseSku', 'memberExternalSku'];

    // planAppend 纯函数：算出本次真正要追加的成员（去空、去重、按上限截断）。
    // 上限与去重都在服务端也会再做一次 —— 这里只负责「别把明显超限的行塞进 DOM」。
    function planAppend(existing, members, max) {
        var cap = Number(max);
        if (!(cap > 0)) { cap = MAX_DEFAULT; }
        var seen = {};
        var count = 0;
        var i, id;
        for (i = 0; i < (existing || []).length; i++) {
            id = String(existing[i] == null ? '' : existing[i]).trim();
            if (id !== '' && !seen[id]) { seen[id] = true; count++; }
        }
        var out = [];
        for (i = 0; i < (members || []).length; i++) {
            var member = members[i] || {};
            id = String(member.variantId == null ? '' : member.variantId).trim();
            if (id === '' || seen[id]) { continue; }
            if (count >= cap) { break; }
            seen[id] = true;
            count++;
            out.push(member);
        }
        return out;
    }

    // receiptText 纯函数：把「追加了几条 / 跳过几条 + 逐条原因」拼成一句结论。
    // 文案全部来自模板的 data-msg-*（服务端按当前语言渲染），这里不写死中文。
    function receiptText(added, skips, msg) {
        var text = added > 0
            ? String(msg.added || '').replace('%d', String(added))
            : String(msg.none || '');
        var list = skips || [];
        if (list.length) {
            var lines = [];
            for (var i = 0; i < list.length; i++) {
                var skip = list[i] || {};
                lines.push((skip.label ? skip.label + '：' : '') + (skip.text || ''));
            }
            text += ' ' + String(msg.skipped || '').replace('%d', String(list.length)) + lines.join('；');
        }
        return text;
    }

    function hostOf(root) {
        return (root || document).querySelector('[data-bundle-form-host]');
    }

    function setStatus(root, text) {
        var nodes = (root || document).querySelectorAll('[data-bundle-source-status]');
        for (var i = 0; i < nodes.length; i++) {
            nodes[i].textContent = text || '';
        }
    }

    // existingVariantIds 已在清单里的变体 id：同一名字可能有隐藏域（已存行）与
    // 候选下拉（新加的空行）两个控件，按「下拉优先、否则隐藏域」取。
    function existingVariantIds(host) {
        var out = [];
        if (!host) { return out; }
        var rows = host.querySelectorAll('[data-bundle-member-list] ' + ROW_SEL);
        for (var i = 0; i < rows.length; i++) {
            var select = rows[i].querySelector('select[name="variantId"]');
            var hidden = rows[i].querySelector('input[name="variantId"]');
            var value = select ? select.value : (hidden ? hidden.value : '');
            if (value) { out.push(value); }
        }
        return out;
    }

    // setCandidateEnabled 候选行随「是否已到上限」开合。到上限时整行隐藏并禁用控件，
    // 删掉一行后由 remove 分支重新打开（否则用户会卡在「加不了也删不掉」的错觉里）。
    function setCandidateEnabled(host, enabled) {
        var body = host && host.querySelector('[data-bundle-member-list]');
        if (!body) { return; }
        var candidate = body.querySelector('[data-bundle-candidate]');
        var row = candidate && candidate.closest('tr');
        if (!row) { return; }
        row.hidden = !enabled;
        var fields = row.querySelectorAll('input, select');
        for (var i = 0; i < fields.length; i++) {
            fields[i].disabled = !enabled;
        }
        if (!enabled) { candidate.value = ''; }
    }

    function memberField(member, name) {
        switch (name) {
            case 'memberWarehouseId': return member.warehouseId || '';
            case 'memberWarehouseSku': return member.warehouseSku || '';
            case 'memberExternalSku': return member.externalSku || '';
            default: return member[name] || '';
        }
    }

    function appendMembers(host, members) {
        if (!host) { return 0; }
        var body = host.querySelector('[data-bundle-member-list]');
        var tpl = host.querySelector('[data-bundle-member-tpl]');
        if (!body || !tpl) { return 0; }
        var cap = Number(body.getAttribute('data-bundle-max')) || MAX_DEFAULT;
        var todo = planAppend(existingVariantIds(host), members, cap);
        var candidate = body.querySelector('[data-bundle-candidate]');
        var beforeRow = candidate && candidate.closest('tr');
        for (var i = 0; i < todo.length; i++) {
            var member = todo[i];
            var frag = tpl.content.cloneNode(true);
            var tr = frag.querySelector('tr');
            if (!tr) { continue; }
            for (var f = 0; f < FIELDS.length; f++) {
                var field = tr.querySelector('[name="' + FIELDS[f] + '"]');
                if (field) { field.value = memberField(member, FIELDS[f]); }
            }
            var sku = tr.querySelector('[data-member-sku]');
            if (sku) { sku.textContent = member.skuCode || member.variantId || ''; }
            var source = tr.querySelector('[data-member-source]');
            if (source) { source.textContent = member.sourceLabel || ''; }
            if (beforeRow && beforeRow.parentNode === body) {
                body.insertBefore(frag, beforeRow);
            } else {
                body.appendChild(frag);
            }
        }
        setCandidateEnabled(host, existingVariantIds(host).length < cap);
        return todo.length;
    }

    // —— 事件委托 ——

    function onChange(event) {
        var candidate = event.target.closest('[data-bundle-candidate]');
        if (!candidate || !candidate.value) { return; }
        var option = candidate.options[candidate.selectedIndex];
        var id = candidate.value;
        candidate.value = '';
        appendMembers(hostOf(document), [{ variantId: id, skuCode: option ? option.textContent : '' }]);
    }

    function onRemove(event) {
        var remove = event.target.closest('[data-bundle-member-remove]');
        if (!remove) { return false; }
        var row = remove.closest(ROW_SEL);
        if (row) { row.remove(); }
        var host = hostOf(document);
        if (!host) { return true; }
        var body = host.querySelector('[data-bundle-member-list]');
        var candidate = body && body.querySelector('[data-bundle-candidate]');
        if (candidate && candidate.closest('tr')) {
            setCandidateEnabled(host, true);
        } else if (!candidate && body) {
            // 候选行已被整行删掉时，从模板补回一行（删空后仍要能继续加）。
            var tpl = host.querySelector('[data-bundle-candidate-template]');
            if (tpl) { body.appendChild(tpl.content.cloneNode(true)); }
        }
        return true;
    }

    function onResolve(event, trigger) {
        var panel = document.querySelector(trigger.getAttribute('data-panel') || '');
        if (!panel) { return; }
        var body = new FormData();
        body.append('projectId', trigger.getAttribute('data-project') || '');
        body.append('productId', trigger.getAttribute('data-product') || '');
        var sourceSelect = panel.querySelector('[data-bundle-source-select]');
        body.append('source', sourceSelect ? sourceSelect.value : '');
        var productSelect = panel.querySelector('select[name="sourceProduct"]');
        body.append('sourceProductId', productSelect ? productSelect.value : '');
        var warehouseSelect = panel.querySelector('select[name="sourceWarehouse"]');
        // 字段名与 handler 的 c.PostForm("sourceWarehouse") 逐字一致 —— 这里写错只会让服务端收到空串。
        body.append('sourceWarehouse', warehouseSelect ? warehouseSelect.value : '');
        var picked = panel.querySelectorAll('input[name="warehouseSku"]:checked');
        for (var i = 0; i < picked.length; i++) { body.append('warehouseSku', picked[i].value); }
        var attrs = panel.querySelectorAll('input[type="checkbox"][name^="attr:"]:checked');
        for (var j = 0; j < attrs.length; j++) { body.append(attrs[j].name, attrs[j].value); }
        var existing = existingVariantIds(hostOf(document));
        for (var k = 0; k < existing.length; k++) { body.append('existingVariantId', existing[k]); }

        setStatus(document, '');
        fetch(trigger.getAttribute('data-url') || '', {
            method: 'POST',
            headers: { 'X-CSRF-Token': trigger.getAttribute('data-csrf') || '' },
            body: body
        }).then(function (res) {
            return res.json().catch(function () { return null; });
        }).then(function (data) {
            if (!data || !data.ok) {
                var reason = data && data.message ? data.message : '';
                setStatus(document, (trigger.getAttribute('data-msg-failed') || '') + reason);
                return;
            }
            var added = appendMembers(hostOf(document), data.members || []);
            setStatus(document, receiptText(added, data.skips || [], {
                added: trigger.getAttribute('data-msg-added'),
                none: trigger.getAttribute('data-msg-none'),
                skipped: trigger.getAttribute('data-msg-skipped')
            }));
        }).catch(function () {
            setStatus(document, trigger.getAttribute('data-msg-failed') || '');
        });
    }

    function onClick(event) {
        if (onRemove(event)) { return; }
        var trigger = event.target.closest('[data-bundle-resolve]');
        if (!trigger) { return; }
        onResolve(event, trigger);
    }

    function bind(root) {
        var scope = root || document;
        scope.addEventListener('change', onChange);
        scope.addEventListener('click', onClick);
    }

    global.WPBundleMembers = {
        planAppend: planAppend,
        receiptText: receiptText,
        appendMembers: appendMembers,
        bind: bind
    };

    if (typeof document !== 'undefined') { bind(document); }
})(typeof window !== 'undefined' ? window : this);
