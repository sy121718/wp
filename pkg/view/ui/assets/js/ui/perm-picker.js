/* ui/perm-picker.js — 控件：权限点复选清单（搜索过滤 / 清空 / 勾选计数）。
 *
 * 为什么需要它：一个菜单要绑的权限点在候选里近 300 个（31 个模块）。两种现成做法都不行：
 *   - 原生 <select multiple> 在这种规模下没有搜索，触屏上要逐项长按；
 *   - ui/select.js 对 multiple 是**显式跳过**的（`sel.multiple || sel.size > 1` 直接 return），
 *     它接管的是单选的弹层，不是多选清单。
 *
 * 锚点是 data-perm-picker 而不是固定 id：菜单新建抽屉内嵌在页面里，编辑抽屉每次
 * 按需取回片段都会重建 DOM —— 用属性锚点才能在同一页里反复初始化（与 perm-tree 同理）。
 *
 * 不变式：勾选框是原生的 input[type=checkbox][name=permission_codes]，
 * 所以禁用 JS 时清单照常可勾、表单照常提交多值，本控件只做过滤与计数这类增强 ——
 * 服务端不信任何前端状态（提交上来的是完整集合，空项由 handler 丢掉）。
 *
 * 搜索只影响显示、不影响提交：保存始终提交**全部勾选**（清单里没搜到但勾着的项照常提交），
 * 与角色权限抽屉的搜索同一约定 —— 否则「搜完点保存」会被误当成「只保存搜到的那些」。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});
    global.WBUI = WBUI;
    WBUI.controls = WBUI.controls || [];

    function init(scope) {
        WBUI.each(WBUI.$$('[data-perm-picker]', scope), function (picker) {
            if (!WBUI.markOnce(picker, 'PermPicker')) { return; }

            var boxes = Array.prototype.slice.call(picker.querySelectorAll('[data-perm-picker-item]'));
            if (!boxes.length) { return; }
            var rows = Array.prototype.slice.call(picker.querySelectorAll('[data-perm-picker-row]'));
            var groups = Array.prototype.slice.call(picker.querySelectorAll('[data-perm-picker-group]'));
            var searchEl = picker.querySelector('[data-perm-picker-search]');
            var countEl = picker.querySelector('[data-selected-count]');
            var emptyEl = picker.querySelector('[data-perm-picker-empty]');

            /* 计数：任何改变勾选的路径都要过这里（逐项 change、清空、初始渲染）。
               {n} 由 data-selected-template 提供，与列表页批量条、角色权限抽屉同一约定。 */
            function refreshCount() {
                if (!countEl) { return; }
                var n = 0;
                for (var i = 0; i < boxes.length; i++) { if (boxes[i].checked) { n++; } }
                var tpl = countEl.getAttribute('data-selected-template') || '已选 {n} 项';
                countEl.textContent = tpl.replace('{n}', String(n));
            }

            /* 过滤按行（label）而不是按勾选框：行里除了码还有中文名，
               管理员通常是按中文名找的（「删除商品」比 product:delete 好记）。 */
            function applyFilter() {
                var query = searchEl ? (searchEl.value || '').trim().toLowerCase() : '';
                var visible = 0;
                rows.forEach(function (row) {
                    var text = row.getAttribute('data-perm-picker-text') || row.textContent || '';
                    var hit = !query || text.toLowerCase().indexOf(query) >= 0;
                    row.hidden = !hit;
                    if (hit) { visible++; }
                });
                /* 分组标题：整组没有可见行就一起藏起来，否则搜完之后满屏空标题。
                   标题不参与搜索本身（它是模块名，用户搜的是权限名或码）。 */
                groups.forEach(function (group) {
                    var any = false;
                    var groupRows = group.querySelectorAll('[data-perm-picker-row]');
                    for (var i = 0; i < groupRows.length; i++) {
                        if (!groupRows[i].hidden) { any = true; break; }
                    }
                    group.hidden = !any;
                });
                if (emptyEl) { emptyEl.hidden = visible > 0; }
            }

            boxes.forEach(function (box) { box.addEventListener('change', refreshCount); });

            var noneBtn = picker.querySelector('[data-perm-picker-none]');
            if (noneBtn) {
                noneBtn.addEventListener('click', function () {
                    boxes.forEach(function (box) { box.checked = false; });
                    refreshCount();
                });
            }

            if (searchEl) { searchEl.addEventListener('input', applyFilter); }

            refreshCount();
            applyFilter();
        });
    }

    WBUI.register(init);
})(window);
