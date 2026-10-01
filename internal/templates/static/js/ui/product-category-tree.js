/* Category tree state and bounded parent search. HTMX owns all data requests.
 *
 * 分类列表现在由服务端一次渲染整棵树（顶级分类带全部子类），所以这里没有
 * 「展开 / 折叠子树」的逻辑：折叠过的东西会让人以为页面上没有那些分类。
 * 留下的两件事：① 勾选态（批量删除的条与全选）；② 父级下拉的远程搜索。 */
(function () {
    'use strict';
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
    document.addEventListener('input', function (event) {
        var input = event.target.closest('[data-category-parent-search]');
        if (!input) return;
        var picker = input.closest('[data-category-parent-picker]');
        clearTimeout(picker._categoryTimer);
        var target = picker.querySelector('[data-category-parent-results]');
        var keyword = input.value.trim();
        if (!keyword) { target.replaceChildren(); return; }
        picker._categoryTimer = setTimeout(function () {
            if (!window.htmx) return;
            var url = '/admin/product-categories/parents?project=' + encodeURIComponent(picker.dataset.categoryProject) + '&keyword=' + encodeURIComponent(keyword);
            window.htmx.ajax('GET', url, { target: target, swap: 'innerHTML' });
        }, 180);
    });
})();
