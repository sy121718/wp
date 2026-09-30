/* ui/perm-tree.js — 控件：角色权限分配树（父子联动 / 折叠 / 搜索 / 勾选计数）。
 *
 * 为什么它住在基座而不是片段里：这棵树现在由抽屉按需取回（data-drawer-url），
 * 而 ui/drawer.js 的 fragmentRoot 校验**拒绝一切 <script>**（片段里带脚本就等于把
 * XSS 通道开在 fetch 回来才执行的路径上）。所以脚本必须由基座的扫描认领：
 * 抽屉插入片段后会调 WBUI.scan(body)，本控件就在那一步初始化。
 *
 * 锚点是 data-perm-tree 而不是固定 id：抽屉关一次就把 DOM 全丢了，再打开是全新的一棵，
 * 用属性锚点才能在同一页里反复初始化。幂等标记**落在树容器上** —— 容器每次重建
 * （抽屉 body 被 innerHTML 清空），所以不会漏初始化；这点与「htmx 复用容器只换内容」
 * 的场景不同（那种情况必须把标记落在内容节点上）。
 *
 * 不变式只有一条：**子勾 ⇒ 父勾**。服务端保存时同样补齐（withAncestorMenuIDs），
 * 所以这里的联动是即时反馈而不是唯一防线 —— 脚本没跑（禁用 JS / 加载失败）时片段
 * 仍然显示正确的勾选态，提交上去的数据也会被服务端补正。
 *
 * 两个方向的动作不对称，这是刻意的：
 *   - 勾选子项 → 向上补齐祖先（补的是「入口」，不是新能力，安全）；
 *   - 取消父项 → 整棵子树一起取消（不这么做就会造出「子勾着、父没勾」的非法状态，
 *     提交后又被服务端补齐回来，表现为「取消父级点了没反应」）。
 * 反方向的「勾父项自动勾全部子项」不实现：那等于一次「页面管理」顺手给出
 * 「删除页面 / 发布页面」—— 权限系统最不该有的默认行为。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});
    global.WBUI = WBUI;
    WBUI.controls = WBUI.controls || [];

    function init(scope) {
        WBUI.each(WBUI.$$('[data-perm-tree]', scope), function (tree) {
            if (!WBUI.markOnce(tree, 'PermTree')) { return; }
            var form = tree.closest('form');
            if (!form) { return; }

            var rows = Array.prototype.slice.call(tree.querySelectorAll('[data-perm-row]'));
            var boxes = Array.prototype.slice.call(tree.querySelectorAll('[data-perm-node]'));
            if (!boxes.length) { return; }

            var boxById = Object.create(null);
            var rowById = Object.create(null);
            var childBoxes = Object.create(null);
            boxes.forEach(function (box) {
                var id = box.getAttribute('data-id');
                var parent = box.getAttribute('data-parent');
                boxById[id] = box;
                (childBoxes[parent] = childBoxes[parent] || []).push(box);
            });
            rows.forEach(function (row) { rowById[row.getAttribute('data-perm-row')] = row; });

            /* disabled 的勾选框（管理员的「来自角色」继承项）不参与任何联动：
               它们**不提交**，勾了也不产生授权、取消了也不撤销任何东西 —— 让它们跟着联动
               只会制造「看起来取消了、其实没有」的假象。 */
            function checkAncestors(box) {
                var parent = box.getAttribute('data-parent');
                while (parent && parent !== '0' && boxById[parent]) {
                    if (!boxById[parent].disabled) { boxById[parent].checked = true; }
                    parent = boxById[parent].getAttribute('data-parent');
                }
            }

            function uncheckSubtree(box) {
                if (box.disabled) { return; }
                box.checked = false;
                (childBoxes[box.getAttribute('data-id')] || []).forEach(uncheckSubtree);
            }

            /* 部分选中标记：父项自己在授权集合里（check 为真），但还有子项没勾。
               刻意不用 checkbox 的 indeterminate：那会让一个「确实已授权」的父项看起来没勾，
               而它保存后不仅保留，还会把权限码写进策略。小圆点只表示「没选全」。 */
            function refreshPartial() {
                rows.forEach(function (row) { row.removeAttribute('data-partial'); });
                rows.slice().sort(function (a, b) {
                    return Number(b.getAttribute('data-depth')) - Number(a.getAttribute('data-depth'));
                }).forEach(function (row) {
                    var box = row.querySelector('[data-perm-node]');
                    if (!box || !box.checked) { return; }
                    var kids = childBoxes[box.getAttribute('data-id')] || [];
                    if (!kids.length) { return; }
                    for (var i = 0; i < kids.length; i++) {
                        if (!kids[i].checked) { row.setAttribute('data-partial', '1'); return; }
                    }
                });
                refreshCount();
            }

            var collapsed = Object.create(null);
            var searchEl = form.querySelector('[data-perm-search]');
            var countEl = form.querySelector('[data-selected-count]');

            /* 勾选计数：与权限树同源，任何改变勾选的路径都必须过这里
               （父子联动的 change、全选 / 清空、初始渲染）。{n} 由 data-selected-template 提供，
               与列表页批量条（admin.js 的 refresh）同一约定。
               aria-live="polite" 让勾选变化被读屏播报，用户不必回头去找计数。 */
            function refreshCount() {
                if (!countEl) { return; }
                var n = 0;
                boxes.forEach(function (box) { if (box.checked && !box.disabled) { n++; } });
                var tpl = countEl.getAttribute('data-selected-template') || '已选 {n} 项';
                countEl.textContent = tpl.replace('{n}', String(n));
            }

            /* 折叠与搜索都靠「DFS 展平后同一父级的子树连续」这一性质：折叠 = 隐藏本行之后
               连续出现、且 depth 更大的所有行。搜索优先于折叠，且会把命中行的祖先一并显示 ——
               否则搜索结果会挂在看不见的层级下面。 */
            function applyVisibility() {
                rows.forEach(function (row) { row.hidden = false; });
                var query = searchEl ? (searchEl.value || '').trim().toLowerCase() : '';
                if (query) {
                    var keep = Object.create(null);
                    rows.forEach(function (row) {
                        if (row.textContent.toLowerCase().indexOf(query) < 0) { return; }
                        keep[row.getAttribute('data-perm-row')] = true;
                        var parent = row.getAttribute('data-parent');
                        while (parent && parent !== '0' && rowById[parent]) {
                            keep[parent] = true;
                            parent = rowById[parent].getAttribute('data-parent');
                        }
                    });
                    rows.forEach(function (row) { row.hidden = !keep[row.getAttribute('data-perm-row')]; });
                    return;
                }
                rows.forEach(function (row, i) {
                    if (!collapsed[row.getAttribute('data-perm-row')]) { return; }
                    var depth = Number(row.getAttribute('data-depth'));
                    for (var j = i + 1; j < rows.length; j++) {
                        if (Number(rows[j].getAttribute('data-depth')) <= depth) { break; }
                        rows[j].hidden = true;
                    }
                });
            }

            boxes.forEach(function (box) {
                box.addEventListener('change', function () {
                    if (box.checked) { checkAncestors(box); } else { uncheckSubtree(box); }
                    refreshPartial();
                });
            });

            tree.addEventListener('click', function (e) {
                var btn = e.target && e.target.closest ? e.target.closest('[data-perm-toggle]') : null;
                if (!btn) { return; }
                var row = btn.closest('[data-perm-row]');
                if (!row) { return; }
                var id = row.getAttribute('data-perm-row');
                collapsed[id] = !collapsed[id];
                row.classList.toggle('is-collapsed', !!collapsed[id]);
                btn.setAttribute('aria-expanded', collapsed[id] ? 'false' : 'true');
                applyVisibility();
            });

            function setAll(checked) {
                boxes.forEach(function (box) { if (!box.disabled) { box.checked = checked; } });
                refreshPartial();
            }

            function collapseAll(on) {
                collapsed = Object.create(null);
                rows.forEach(function (row) {
                    var btn = row.querySelector('[data-perm-toggle]');
                    if (!btn) { return; }
                    var id = row.getAttribute('data-perm-row');
                    collapsed[id] = on;
                    row.classList.toggle('is-collapsed', on);
                    btn.setAttribute('aria-expanded', on ? 'false' : 'true');
                });
                applyVisibility();
            }

            [['[data-perm-all]', function () { setAll(true); }],
             ['[data-perm-none]', function () { setAll(false); }],
             ['[data-perm-expand]', function () { collapseAll(false); }],
             ['[data-perm-collapse]', function () { collapseAll(true); }]].forEach(function (pair) {
                WBUI.each(WBUI.$$(pair[0], form), function (el) { el.addEventListener('click', pair[1]); });
            });

            if (searchEl) { searchEl.addEventListener('input', applyVisibility); }

            /* 目录层默认折叠：116 个节点全展开时整棵树很高，而决策路径是「先看有哪几类权限，
               再展开其中一类去勾」—— 全展开反而让首屏只剩噪声。
               机制完全复用上面的 collapsed 字典 + applyVisibility()（不新造状态、不新造折叠实现），
               只把最上层（树里 depth 最小的那一层，实测是目录 = depth 1）预置为收起。
               为什么按「最小 depth」而不是写死 depth==0：层级编号由 Go 侧的 DFS 深度决定
               （实测为 1/2/3），写死一个数字会在层级口径变化时静默失效（折叠看起来「没生效」）。
               禁用 JS 时回到服务端渲染的全展开态，勾选状态仍完整（勾选态来自服务端，不依赖本脚本）。 */
            var topDepth = null;
            rows.forEach(function (row) {
                var d = Number(row.getAttribute('data-depth'));
                if (isNaN(d)) { return; }
                if (topDepth === null || d < topDepth) { topDepth = d; }
            });
            rows.forEach(function (row) {
                if (topDepth === null || Number(row.getAttribute('data-depth')) !== topDepth) { return; }
                var btn = row.querySelector('[data-perm-toggle]');
                if (!btn) { return; }
                var id = row.getAttribute('data-perm-row');
                collapsed[id] = true;
                row.classList.add('is-collapsed');
                btn.setAttribute('aria-expanded', 'false');
            });
            applyVisibility();

            refreshPartial();
        });
    }

    WBUI.register(init);
})(window);
