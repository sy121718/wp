/* 标签命中列表和分页只消费一个JSON结果，计数与行数据保持同一口径。 */
(function (global) {
    'use strict';
    var active = null;
    function text(root, selector, value) { root.querySelector(selector).textContent = value == null ? '' : String(value); }
    function render(panel, data, state) {
        text(panel, '[data-tag-name]', data.tagName);
        text(panel, '[data-tag-total]', data.total);
        var rows = panel.querySelector('[data-tag-rows]');
        rows.replaceChildren();
        data.items.forEach(function (product) {
            var row = panel.querySelector('[data-tag-row]').content.cloneNode(true);
            text(row, '[data-tag-product-name]', product.name);
            text(row, '[data-tag-product-slug]', product.slug || '—');
            text(row, '[data-tag-product-status]', product.status);
            rows.appendChild(row);
        });
        if (!data.items.length) rows.appendChild(panel.querySelector('[data-tag-empty]').content.cloneNode(true));
        var from = data.items.length > 0 ? (data.page - 1) * data.pageSize + 1 : 0;
        var to = data.items.length ? from + data.items.length - 1 : 0;
        var values = [data.total, from, to];
        text(panel, '[data-tag-page-info]', panel.getAttribute('data-page-info').replace(/%s/g, function () { return values.shift(); }));
        panel.querySelector('[data-tag-page="-1"]').disabled = data.page <= 1;
        panel.querySelector('[data-tag-page="1"]').disabled = data.page * data.pageSize >= data.total;
        state.page = data.page;
        state.size = data.pageSize;
        var trigger = document.getElementById('tag-hit-count-' + data.tagId);
        if (trigger) trigger.textContent = data.total + panel.getAttribute('data-count-tail');
        panel.querySelector('[data-tag-result]').hidden = false;
    }
    function load(id, project, page, size) {
        var panel = document.querySelector('[data-product-tag-panel]');
        if (!panel) return;
        if (active) active.abort.abort();
        var state = {id: id, project: project, page: page, size: size || 20, abort: new AbortController()};
        active = state;
        panel.hidden = false;
        panel.setAttribute('aria-busy', 'true');
        panel.querySelector('[data-tag-result]').hidden = true;
        var status = panel.querySelector('[data-tag-status]');
        status.hidden = true;
        var query = new URLSearchParams({id: id, projectId: project, page: page, size: state.size});
        fetch('/api/product/tag/products?' + query.toString(), {credentials: 'same-origin', redirect: 'error', signal: state.abort.signal, headers: {'Accept': 'application/json'}})
            .then(function (response) {
                if (!(response.headers.get('content-type') || '').includes('application/json')) throw new Error(panel.getAttribute('data-failed'));
                return response.json().then(function (payload) {
                    if (!payload || typeof payload !== 'object' || typeof payload.code !== 'number') throw new Error();
                    if (!response.ok || payload.code >= 400) {
                        var error = new Error(typeof payload.message === 'string' ? payload.message : '');
                        error.productReadMessage = true;
                        throw error;
                    }
                    if (!payload.data || !Array.isArray(payload.data.items)) throw new Error();
                    return payload.data;
                });
            }).then(function (data) {
                if (active === state && panel.isConnected) render(panel, data, state);
            }).catch(function (error) {
                if (error.name === 'AbortError' || active !== state || !panel.isConnected) return;
                status.textContent = error.productReadMessage && error.message || panel.getAttribute('data-failed');
                status.hidden = false;
            }).finally(function () { if (active === state) panel.removeAttribute('aria-busy'); });
    }
    document.addEventListener('click', function (event) {
        var trigger = event.target.closest('[data-product-tag-hits]');
        if (trigger) { load(trigger.getAttribute('data-product-tag-hits'), trigger.getAttribute('data-project-id'), 1, 20); return; }
        var next = event.target.closest('[data-tag-page]');
        if (next && active && !next.disabled) load(active.id, active.project, active.page + Number(next.getAttribute('data-tag-page')), active.size);
    });
    global.WBUI.productTagHits = {render: render};
})(window);
