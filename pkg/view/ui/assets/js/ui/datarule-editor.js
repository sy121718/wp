/* 条件组只操作本地模板；白名单事实来自 schema JSON，业务校验仍在 service。 */
(function (global) {
    'use strict';
    var WBUI = global.WBUI || (global.WBUI = {});
    function clone(root, selector) { return root.querySelector(selector).content.cloneNode(true); }
    function fields(root) { return Array.from(root.querySelector('[data-datarule-fields]').children); }
    function option(root, value, label) {
        var fragment = clone(root, '[data-rule-option-template]');
        var node = fragment.firstElementChild;
        node.value = value;
        node.textContent = label;
        return node;
    }
    function fillOperators(root, row, selected) {
        var field = row.querySelector('[data-row-field]').value;
        var target = row.querySelector('[data-row-op]');
        target.replaceChildren();
        var source = fields(root).find(function (item) { return item.querySelector('input').value === field; });
        if (source) {
            source.querySelector('[data-field-operators]').content.querySelectorAll('option').forEach(function (op) { target.appendChild(op.cloneNode(true)); });
            if (Array.from(target.options).some(function (op) { return op.value === selected; })) target.value = selected;
        } else target.appendChild(clone(root, '[data-rule-op-placeholder]'));
    }
    function fillRow(root, row) {
        var field = row.querySelector('[data-row-field]');
        var selected = field.getAttribute('data-initial-field') || field.value;
        var op = row.querySelector('[data-row-op]');
        var selectedOp = op.getAttribute('data-initial-op') || op.value;
        field.removeAttribute('data-initial-field');
        op.removeAttribute('data-initial-op');
        field.replaceChildren(clone(root, '[data-rule-field-placeholder]'));
        fields(root).forEach(function (item) {
            field.appendChild(option(root, item.querySelector('input').value, item.querySelector('[data-field-label]').textContent));
        });
        if (Array.from(field.options).some(function (item) { return item.value === selected; })) field.value = selected;
        fillOperators(root, row, selectedOp);
    }
    function reindex(root) {
        root.querySelectorAll('[data-datarule-group]').forEach(function (group, gi) {
            group.querySelector('[data-group-index]').textContent = gi;
            // 组逻辑与条件行都是**静态平行键**（groupLogic / condField / condOp /
            // condValue / condGroup）：同名控件按出现顺序保留多值，服务端按顺序配对。
            // 不再拼 groups[<gi>].conditions[<ri>] 这类动态下标 —— 字段门禁与契约
            // 才能静态对账，服务端也不需要按正则拆键。
            group.querySelector('[data-group-logic]').name = 'groupLogic';
            group.querySelectorAll('[data-datarule-row]').forEach(function (row) {
                ['field', 'op', 'value'].forEach(function (name) {
                    var node = row.querySelector('[data-row-' + name + ']');
                    if (node) node.name = 'cond' + name.charAt(0).toUpperCase() + name.slice(1);
                });
                var groupNode = row.querySelector('[data-cond-group]');
                if (groupNode) groupNode.value = String(gi);
            });
        });
        root.querySelector('[data-datarule-empty]').hidden = fields(root).length > 0;
    }
    function addRow(root, group) {
        var fragment = clone(root, '[data-rule-row-template]');
        var row = fragment.firstElementChild;
        fillRow(root, row);
        group.querySelector('[data-group-rows]').appendChild(fragment);
        return row;
    }
    function scan(root) { if (WBUI.scan) WBUI.scan(root); }
    function addGroup(root) {
        var fragment = clone(root, '[data-rule-group-template]');
        var group = fragment.firstElementChild;
        addRow(root, group);
        root.querySelector('[data-datarule-groups]').appendChild(fragment);
        return group;
    }
    function schemaAccepts(root, schema) {
        var declared = schema.fields || [];
        var omitted = fields(root).filter(function (item) { return item.querySelector('input').checked; });
        if (!omitted.every(function (item) {
            return declared.some(function (field) { return field.field === item.querySelector('input').value; });
        })) return false;
        return Array.from(root.querySelectorAll('[data-datarule-row]')).every(function (row) {
            var name = row.querySelector('[data-row-field]').value;
            var op = row.querySelector('[data-row-op]').value;
            if (!name) return true;
            return declared.some(function (field) { return field.field === name && (!op || (field.operators || []).includes(op)); });
        });
    }
    function schemaFields(root, schema) {
        var checked = fields(root).filter(function (item) { return item.querySelector('input').checked; }).map(function (item) { return item.querySelector('input').value; });
        var fragment = document.createDocumentFragment();
        (schema.fields || []).forEach(function (field) {
            var item = clone(root, '[data-rule-field-template]').firstElementChild;
            item.querySelector('input').value = field.field;
            item.querySelector('input').checked = checked.includes(field.field);
            var label = clone(root, '[data-rule-label-template]');
            label.querySelector('[data-schema-label]').textContent = field.label;
            label.querySelector('[data-schema-field]').textContent = field.field;
            item.querySelector('[data-field-label]').appendChild(label);
            (field.operators || []).forEach(function (op) { item.querySelector('[data-field-operators]').content.appendChild(option(root, op, op)); });
            fragment.appendChild(item);
        });
        root.querySelector('[data-datarule-fields]').replaceChildren(fragment);
        root.querySelectorAll('[data-datarule-row]').forEach(function (row) { fillRow(root, row); });
        reindex(root);
        scan(root);
    }
    function setBusy(form, busy) {
        var save = form.querySelector('button[type="submit"]');
        if (save) save.disabled = busy;
    }
    async function changeDomain(select, root) {
        var form = select.closest('form');
        var status = root.querySelector('[data-rule-status]');
        var previous = select.getAttribute('data-current-domain');
        if (root._schemaRequest) root._schemaRequest.abort();
        var controller = new AbortController();
        root._schemaRequest = controller;
        root.setAttribute('aria-busy', 'true');
        status.textContent = root.getAttribute('data-loading');
        setBusy(form, true);
        try {
            var response = await fetch('/api/datarule/schema/detail?domain=' + encodeURIComponent(select.value), {credentials: 'same-origin', signal: controller.signal});
            var payload = await response.json();
            if (root._schemaRequest !== controller) return;
            if (!response.ok || payload.code !== 200 || !payload.data) {
                select.value = previous;
                status.textContent = payload.message || root.getAttribute('data-load-error');
                return;
            }
            if (!schemaAccepts(root, payload.data)) {
                select.value = previous;
                status.textContent = root.getAttribute('data-domain-conflict');
                return;
            }
            schemaFields(root, payload.data);
            select.setAttribute('data-current-domain', select.value);
            status.textContent = '';
        } catch (error) {
            if (root._schemaRequest !== controller) return;
            select.value = previous;
            status.textContent = root.getAttribute('data-load-error');
        } finally {
            if (root._schemaRequest === controller) {
                root._schemaRequest = null;
                root.removeAttribute('aria-busy');
                setBusy(form, false);
            }
        }
    }
    global.addEventListener('submit', function (event) {
        var root = event.target.querySelector('[data-datarule-editor]');
        if (root && root._schemaRequest) { event.preventDefault(); event.stopImmediatePropagation(); }
    }, true);
    document.addEventListener('click', function (event) {
        var button = event.target.closest('[data-rule-action]');
        if (!button) return;
        var root = button.closest('[data-datarule-editor]');
        var group = button.closest('[data-datarule-group]');
        var action = button.getAttribute('data-rule-action');
        if (action === 'add-group') addGroup(root).querySelector('[data-group-logic]').focus();
        else if (action === 'add-row') addRow(root, group).querySelector('[data-row-field]').focus();
        else if (action === 'del-group') { group.remove(); root.querySelector('[data-rule-action="add-group"]').focus(); }
        else if (action === 'del-row') { button.closest('[data-datarule-row]').remove(); group.querySelector('[data-rule-action="add-row"]').focus(); }
        reindex(root);
        scan(root);
    });
    document.addEventListener('change', function (event) {
        var target = event.target;
        if (target.matches('[data-datarule-domain]')) {
            var root = target.closest('form').querySelector('[data-datarule-editor]');
            if (root) changeDomain(target, root);
        } else if (target.matches('[data-row-field]')) {
            var editor = target.closest('[data-datarule-editor]');
            fillOperators(editor, target.closest('[data-datarule-row]'), '');
            scan(editor);
        }
    });
    if (WBUI.register) WBUI.register(function (scope) {
        (scope || document).querySelectorAll('[data-datarule-editor]').forEach(function (root) {
            if (!WBUI.markOnce(root, 'DataruleEditor')) return;
            root.querySelectorAll('[data-datarule-row]').forEach(function (row) { fillRow(root, row); });
            if (!root.querySelector('[data-datarule-group]')) addGroup(root);
            var domain = root.closest('form').querySelector('[data-datarule-domain]');
            domain.setAttribute('data-current-domain', domain.value);
            reindex(root);
        });
    });
})(window);
