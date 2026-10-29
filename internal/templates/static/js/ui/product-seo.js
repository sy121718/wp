/* 商品 SEO 仅消费数值事实。评分算法留在服务端，色标、文案和结构由模板与控件决定。 */
(function (global) {
    'use strict';
    var ui = global.WBUI;
    if (ui.productSEO) return;
    function label(root, attribute, key) {
        var nodes = root.querySelectorAll('[' + attribute + ']');
        for (var i = 0; i < nodes.length; i++) {
            if (nodes[i].getAttribute(attribute) === key) return nodes[i];
        }
        return null;
    }
    function grade(percent) {
        return percent >= 90 ? 'green' : percent >= 70 ? 'lightgreen' : percent >= 40 ? 'yellow' : percent > 0 ? 'red' : 'red-blocking';
    }
    function text(root, selector, value) {
        var node = root.querySelector(selector);
        if (node) node.textContent = value == null ? '' : String(value);
    }
    function render(root, data) {
        var dictionary = document.getElementById('product-seo-dictionary');
        if (!root || !dictionary || !data || !Array.isArray(data.sections)) return;
        var definitions = dictionary.content;
        text(root, '[data-seo-total]', data.total);
        var profile = label(definitions, 'data-seo-profile-label', data.profileType || '');
        text(root, '[data-seo-profile]', profile ? profile.textContent : data.profileType);
        var weights = new Intl.NumberFormat(document.documentElement && document.documentElement.lang || undefined, {style: 'percent', maximumFractionDigits: 2});
        var gradeLabel = label(definitions, 'data-seo-grade-label', grade(data.total));
        text(root, '[data-seo-grade]', gradeLabel ? gradeLabel.textContent : '');
        var sections = root.querySelector('[data-seo-sections]');
        sections.replaceChildren();
        data.sections.forEach(function (section) {
            var row = definitions.querySelector('[data-seo-section]').content.cloneNode(true);
            var name = label(definitions, 'data-seo-section-label', section.key);
            text(row, '[data-seo-section-name]', name ? name.textContent : section.key);
            text(row, '[data-seo-section-value]', section.score + '/' + section.max);
            text(row, '[data-seo-section-weight]', Number.isFinite(section.weight) ? weights.format(section.weight) : '—');
            row.querySelector('[data-seo-dot]').classList.add('is-' + grade(section.percent));
            var checks = row.querySelector('[data-seo-checks]');
            (section.checks || []).forEach(function (check) {
                if (check.skipped || check.score >= check.max) return;
                var template = label(definitions, 'data-seo-check', check.key);
                if (!template) return;
                var item = template.content.cloneNode(true);
                text(item, '[data-seo-check-value]', check.score + '/' + check.max);
                checks.appendChild(item);
            });
            sections.appendChild(row);
        });
        ['title', 'description', 'url'].forEach(function (key) {
            var node = root.querySelector('[data-seo-' + key + ']');
            node.textContent = data[key] || node.getAttribute('data-empty') || '';
        });
        var duplicates = Array.isArray(data.duplicates) ? data.duplicates : [];
        root.querySelector('[data-seo-duplicates]').hidden = duplicates.length === 0;
        text(root, '[data-seo-duplicate-count]', duplicates.length);
        var list = root.querySelector('[data-seo-duplicate-list]');
        list.replaceChildren();
        duplicates.forEach(function (duplicate) {
            var item = document.createElement('li');
            var kind = label(definitions, 'data-seo-source-label', duplicate.kind);
            item.textContent = (kind ? kind.textContent.trim() + ' ' : '') + (duplicate.title || duplicate.id) + (duplicate.path ? ' · ' + duplicate.path : '');
            list.appendChild(item);
        });
        var omitted = data.coverage && Array.isArray(data.coverage.omitted) ? data.coverage.omitted : [];
        root.querySelector('[data-seo-coverage]').hidden = omitted.length === 0;
        text(root, '[data-seo-omitted]', omitted.map(function (item) { var source = label(definitions, 'data-seo-source-label', item.source); return source ? source.textContent.trim() : item.source; }).join(', '));
        root.hidden = false;
    }
    ui.productSEO = { render: render };
    ui.writeBase.registerResultHandler('product.seo', function (context) {
        render(context.form.querySelector('[data-product-seo-panel]'), context.data);
        // 消费结果，不能继承编辑表单的关抽屉或导航动作。
        return true;
    });
    document.addEventListener('submit', function (event) {
        var form = event.target;
        var button = event.submitter;
        var handler = button && button.getAttribute('data-write-handler') || form.getAttribute('data-write-handler');
        if (handler !== 'product.seo') return;
        var panel = form.querySelector('[data-product-seo-panel]');
        if (panel) panel.hidden = true;
    }, true);
    ui.register(function (scope) {
        ui.$$('[data-product-seo-load]', scope).forEach(function (form) {
            if (ui.markOnce(form, 'ProductSEO')) form.requestSubmit();
        });
    });
})(window);
