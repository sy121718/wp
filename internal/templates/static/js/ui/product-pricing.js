/* 定价页只消费统一写基座的 JSON 结果；提交、CSRF、错误与按钮锁定仍由写基座负责。 */
(function () {
    'use strict';
    if (window.productPricingReady) return;
    window.productPricingReady = true;
    var revisions = new WeakMap();
    var submissions = new WeakMap();

    function text(root, selector, value) {
        var node = root.querySelector(selector);
        if (node) node.textContent = value == null ? '' : String(value);
    }

    function optionLabel(form, name, value) {
        var options = form.querySelectorAll('[name="' + name + '"] option');
        for (var i = 0; i < options.length; i++) {
            if (options[i].value === value) return options[i].textContent;
        }
        return value || '';
    }

    function label(root, attribute, value) {
        var nodes = root.querySelectorAll('[' + attribute + ']');
        for (var i = 0; i < nodes.length; i++) {
            if (nodes[i].getAttribute(attribute) === value) return nodes[i].textContent;
        }
        return '';
    }

    function render(form, data) {
        var root = form.closest('.pricing-page').querySelector('[data-pricing-preview]');
        if (!root || !data || !Array.isArray(data.lines)) return;
        var rule = optionLabel(form, 'ruleType', data.ruleType);
        if (data.ruleValues && data.ruleValues.HasValue) rule += ' ' + data.ruleValues.Value;
        text(root, '[data-preview-rule]', rule);
        text(root, '[data-preview-rounding]', optionLabel(form, 'rounding', data.rounding));
        text(root, '[data-preview-scope]', optionLabel(form, 'scope', data.scope));
        root.querySelectorAll('[data-preview-count]').forEach(function (node) {
            node.textContent = String(data[node.getAttribute('data-preview-count')]);
        });
        var body = root.querySelector('[data-preview-lines]');
        body.replaceChildren();
        var rowTemplate = root.querySelector('[data-preview-row]');
        data.lines.forEach(function (line) {
            var row = rowTemplate.content.cloneNode(true);
            row.querySelectorAll('[data-value]').forEach(function (node) {
                node.textContent = line[node.getAttribute('data-value')] || '';
            });
            row.querySelectorAll('[data-money]').forEach(function (node) {
                var field = node.getAttribute('data-money');
                var value = line[field];
                node.textContent = value == null ? '—' : (field === 'diff' && value > 0 ? '+' : '') + Number(value).toFixed(2);
            });
            var status = row.querySelector('[data-preview-status]');
            status.textContent = label(root, 'data-preview-status-label', line.status);
            status.classList.add(line.status === 'changed' ? 'badge-ok' : 'badge-mute');
            text(row, '[data-preview-reason]', label(root, 'data-preview-reason-label', line.reason));
            body.appendChild(row);
        });
        if (data.lines.length === 0) body.appendChild(root.querySelector('[data-preview-empty]').content.cloneNode(true));
        root.hidden = false;
        root.focus({ preventScroll: true });
    }

    window.WBUI.writeBase.registerResultHandler('product.pricing.preview', function (ctx) {
        var form = ctx.form;
        if (!form || !form.hasAttribute('data-pricing-form')) return false;
        if (submissions.get(form) === (revisions.get(form) || 0)) render(form, ctx.data);
        // 预览结果由当前控件消费；过期响应同样不触发写基座的默认导航。
        return true;
    });

    // 改过参数后收起旧试算，避免把另一组参数的结果当作当前结果。
    function invalidate(event) {
        var form = event.target.closest('form[data-pricing-form]');
        if (!form) return;
        revisions.set(form, (revisions.get(form) || 0) + 1);
        var preview = form.closest('.pricing-page').querySelector('[data-pricing-preview]');
        if (preview) preview.hidden = true;
    }
    document.addEventListener('input', invalidate);
    document.addEventListener('change', invalidate);
    document.addEventListener('submit', function (event) {
        var form = event.target;
        if (!form || !form.hasAttribute('data-pricing-form')) return;
        submissions.set(form, revisions.get(form) || 0);
        var preview = form.closest('.pricing-page').querySelector('[data-pricing-preview]');
        if (preview) preview.hidden = true;
    }, true);
})();
