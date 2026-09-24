/* Category tree state and bounded parent search. HTMX owns all data requests. */
(function () {
    'use strict';
    function syncSelection(form) {
        if (!form) return;
        var boxes = Array.from(form.querySelectorAll('[data-check-item]'));
        var visible = boxes.filter(function (box) { return !box.closest('[hidden]') && !box.disabled; });
        var checked = visible.filter(function (box) { return box.checked; }).length;
        var all = form.querySelector('[data-check-all]');
        if (all) {
            all.checked = visible.length > 0 && checked === visible.length;
            all.indeterminate = checked > 0 && checked < visible.length;
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
    function collapse(button) {
        var row = button.closest('[data-category-row]');
        var childRow = row && row.nextElementSibling;
        if (!childRow || !childRow.hasAttribute('data-category-child-row')) return;
        childRow.querySelectorAll('[data-check-item]').forEach(function (box) { box.checked = false; box.disabled = true; });
        childRow.hidden = true;
        button.setAttribute('aria-expanded', 'false');
        syncSelection(button.closest('[data-category-tree-form]'));
    }
    document.addEventListener('click', function (event) {
        var button = event.target.closest('[data-category-toggle]');
        if (button) {
            var row = button.closest('[data-category-row]');
            var childRow = row && row.nextElementSibling;
            if (!childRow || !childRow.hasAttribute('data-category-child-row')) return;
            if (button.getAttribute('aria-expanded') === 'true') {
                event.preventDefault();
                collapse(button);
            } else {
                if (childRow.querySelector('[data-category-branch]')) event.preventDefault();
                childRow.hidden = false;
                childRow.querySelectorAll('[data-check-item]').forEach(function (box) {
                    if (!box.closest('[hidden]')) box.disabled = false;
                });
                button.setAttribute('aria-expanded', 'true');
                syncSelection(button.closest('[data-category-tree-form]'));
            }
            return;
        }
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
    document.addEventListener('keydown', function (event) {
        var button = event.target.closest('[data-category-toggle]');
        if (!button || (event.key !== 'ArrowRight' && event.key !== 'ArrowLeft')) return;
        var expanded = button.getAttribute('aria-expanded') === 'true';
        if (event.key === 'ArrowRight' && !expanded) {
            event.preventDefault();
            button.click();
        } else if (event.key === 'ArrowLeft' && expanded) {
            event.preventDefault();
            collapse(button);
        }
    });
    document.addEventListener('change', function (event) {
        var box = event.target;
        if (!box.matches('[data-check-item], [data-check-all]')) return;
        var form = box.closest('[data-category-tree-form]');
        if (!form) return;
        // The generic list handler only checks the nearest tr; a nested table can have hidden ancestors.
        event.stopPropagation();
        if (box.matches('[data-check-all]')) {
            form.querySelectorAll('[data-check-item]').forEach(function (item) {
                if (!item.disabled && !item.closest('[hidden]')) item.checked = box.checked;
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
    document.addEventListener('htmx:afterSwap', function (event) {
        var target = event.detail.target;
        if (target && target.closest && target.closest('[data-category-children]')) {
            syncSelection(target.closest('[data-category-tree-form]'));
        }
    });
})();
