/* 分类树勾选态与父级候选搜索。搜索只消费JSON，选择与展示留在前端。 */
(function () {
    'use strict';
    var searches = new WeakMap();
    function searchParents(input) {
        var picker = input.closest('[data-category-parent-picker]');
        var previous = searches.get(picker);
        if (previous) { clearTimeout(previous.timer); if (previous.abort) previous.abort.abort(); }
        var state = {timer: null, abort: null};
        searches.set(picker, state);
        var results = picker.querySelector('[data-category-parent-results]');
        results.replaceChildren();
        state.timer = setTimeout(function () {
            state.abort = new AbortController();
            var query = new URLSearchParams({projectId: picker.getAttribute('data-category-project'), keyword: input.value, page: '1', size: '50'});
            fetch('/api/product/category/parents?' + query.toString(), {credentials: 'same-origin', redirect: 'error', signal: state.abort.signal, headers: {'Accept': 'application/json'}})
                .then(function (response) {
                    if (!(response.headers.get('content-type') || '').includes('application/json')) throw new Error(picker.getAttribute('data-category-failed'));
                    return response.json().then(function (payload) {
                        if (!payload || typeof payload !== 'object' || typeof payload.code !== 'number') throw new Error();
                        if (!response.ok || payload.code >= 400) {
                            var error = new Error(typeof payload.message === 'string' ? payload.message : '');
                            error.productReadMessage = true;
                            throw error;
                        }
                        if (!Array.isArray(payload.data)) throw new Error();
                        return payload.data;
                    });
                }).then(function (categories) {
                    if (searches.get(picker) !== state || !picker.isConnected) return;
                    var own = picker.closest('form').querySelector('[name="id"]');
                    categories.forEach(function (category) {
                        // 自身不可被选作父级；循环与工程边界仍由后端校验。
                        if (own && own.value === category.id) return;
                        var choice = document.createElement('button');
                        choice.type = 'button'; choice.className = 'btn btn-sm';
                        choice.setAttribute('data-category-parent-choice', category.id);
                        choice.textContent = '　'.repeat(Math.max(0, Math.min(32, category.depth || 0))) + category.name;
                        results.appendChild(choice);
                    });
                    if (!results.children.length) results.textContent = picker.getAttribute('data-category-empty');
                }).catch(function (error) {
                    if (error.name !== 'AbortError' && searches.get(picker) === state && picker.isConnected) results.textContent = error.productReadMessage && error.message || picker.getAttribute('data-category-failed');
                });
        }, 180);
    }
    function searchEvent(event) {
        if (event.target.matches('[data-category-parent-search]')) searchParents(event.target);
    }
    document.addEventListener('input', searchEvent);
    document.addEventListener('search', searchEvent);
    function syncSelection(form) {
        if (!form) return;
        var boxes = Array.from(form.querySelectorAll('[data-check-item]'));
        var checked = boxes.filter(function (box) { return box.checked && !box.disabled; }).length;
        var all = form.querySelector('[data-check-all]');
        if (all) {
            all.checked = boxes.length > 0 && checked === boxes.length;
            all.indeterminate = checked > 0 && checked < boxes.length;
        }
        var bar = form.querySelector('[data-bulk-bar]');
        if (bar) bar.hidden = checked === 0;
        var count = form.querySelector('[data-bulk-count]');
        if (count) count.textContent = (count.dataset.bulkTemplate || '已选 {n} 项').replace('{n}', String(checked));
        boxes.forEach(function (box) {
            var row = box.closest('[data-category-row]');
            if (row) row.classList.toggle('is-selected', box.checked && !box.disabled);
        });
    }
    document.addEventListener('click', function (event) {
        var choice = event.target.closest('[data-category-parent-choice]');
        if (!choice) return;
        var picker = choice.closest('[data-category-parent-picker]');
        var select = picker && picker.querySelector('[data-category-parent-select]');
        if (!select) return;
        var id = choice.getAttribute('data-category-parent-choice');
        var own = picker.closest('form').querySelector('[name="id"]');
        if (own && own.value === id) return;
        var option = Array.from(select.options).find(function (entry) { return entry.value === id; });
        if (!option) {
            option = new Option(choice.textContent, id);
            select.add(option);
        }
        select.value = id;
        select.dispatchEvent(new Event('change', { bubbles: true }));
    });
    document.addEventListener('change', function (event) {
        var box = event.target;
        if (!box.matches('[data-check-item], [data-check-all]')) return;
        var form = box.closest('[data-category-tree-form]');
        if (!form) return;
        // 通用列表处理器只看最近的 tr；分类树是一张平表，这里统一接管整张表的勾选态。
        event.stopPropagation();
        if (box.matches('[data-check-all]')) {
            form.querySelectorAll('[data-check-item]').forEach(function (item) {
                if (!item.disabled) item.checked = box.checked;
            });
        }
        syncSelection(form);
    }, true);
})();
