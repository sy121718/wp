/* ui/tree-table.js — 控件：树状表格行（按层级折叠 / 展开）。
 *
 * 服务端把整棵树按 DFS 前序摊平成表格行并算好三件事：每行的 depth、有没有子行、
 * 初始是不是被某个祖先折叠着。本控件只做「用户点了一下之后，可见性怎么变」——
 * 不做第二套层级推算（父子关系只有服务端那份），也不猜初始态。
 *
 * 锚点是 data-tree-table（表格本身）而不是固定 id：同一形态的树表可能出现在多个列表页，
 * 属性锚点让控件能同时服务多处，也让 htmx 局部替换后重扫能重新认领新表格。
 *
 * 行隐藏统一走 **tr.hidden**，这是与列表页批量选择模块（admin.js）的既有约定：
 * 它只认 tr.hidden —— 被隐藏的行不参与全选、也不计入「已选 N 项」。
 * 因此折叠时必须顺手取消被藏起来那几行的勾选（见 apply），否则会出现
 * 「页面上显示已选 2 项、提交上去 7 个 id」的静默越界。
 *
 * 禁用 JS 时不会更糟：服务端渲染的就是「浏览态只显示顶级 / 搜索态展开命中路径」，
 * 脚本只提供切换能力，初始那一屏不依赖它。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});
    global.WBUI = WBUI;
    WBUI.controls = WBUI.controls || [];

    function init(scope) {
        WBUI.each(WBUI.$$('[data-tree-table]', scope), function (table) {
            if (!WBUI.markOnce(table, 'TreeTable')) { return; }
            var rows = Array.prototype.slice.call(table.querySelectorAll('[data-tree-id]'));
            if (!rows.length) { return; }

            function depthOf(row) {
                return Number(row.getAttribute('data-tree-depth')) || 0;
            }
            function idOf(row) {
                return row.getAttribute('data-tree-id');
            }

            var collapsed = Object.create(null);

            /* 初始折叠态直接读服务端给的渲染结果（折叠按钮的 aria-expanded），
               不在这里重新推导「哪些该折叠」—— 浏览态与搜索态的差别是服务端的判断
               （搜索态要展开命中路径），脚本再推一遍就是第二份会漂移的规则。 */
            rows.forEach(function (row) {
                var btn = row.querySelector('[data-tree-toggle]');
                if (btn && btn.getAttribute('aria-expanded') === 'false') {
                    collapsed[idOf(row)] = true;
                }
            });

            function uncheckRow(row) {
                WBUI.each(row.querySelectorAll('input[type="checkbox"]'), function (box) {
                    if (!box.checked || box.disabled) { return; }
                    box.checked = false;
                    // 派发 change 而不是直接改计数：批量条的计数 / 全选态由 admin.js 统一维护，
                    // 这里只把「勾选变了」这件事实报出去。
                    box.dispatchEvent(new Event('change', { bubbles: true }));
                });
            }

            /* 折叠 = 隐藏本行之后连续出现、且 depth 更大的所有行。
               这个区间式判定成立的前提是 DFS 前序（子树在行序里连续），由服务端保证。 */
            function apply() {
                var hidden = Object.create(null);
                rows.forEach(function (row, i) {
                    if (!collapsed[idOf(row)]) { return; }
                    var depth = depthOf(row);
                    for (var j = i + 1; j < rows.length; j++) {
                        if (depthOf(rows[j]) <= depth) { break; }
                        hidden[idOf(rows[j])] = true;
                    }
                });
                rows.forEach(function (row) {
                    var hide = !!hidden[idOf(row)];
                    if (hide && !row.hidden) { uncheckRow(row); }
                    row.hidden = hide;
                });
            }

            function setCollapsed(row, on) {
                var btn = row.querySelector('[data-tree-toggle]');
                if (!btn) { return; }
                collapsed[idOf(row)] = on;
                btn.setAttribute('aria-expanded', on ? 'false' : 'true');
                row.classList.toggle('is-collapsed', on);
            }

            table.addEventListener('click', function (e) {
                var btn = e.target && e.target.closest ? e.target.closest('[data-tree-toggle]') : null;
                if (!btn) { return; }
                var row = btn.closest('[data-tree-id]');
                if (!row) { return; }
                setCollapsed(row, !collapsed[idOf(row)]);
                apply();
            });

            /* 展开 / 折叠全部按钮在表格外（筛选行里），所以按文档查找；
               markOnce 打在按钮上：同一页面反复扫描时不会叠出第二套监听。 */
            [['[data-tree-expand-all]', false], ['[data-tree-collapse-all]', true]].forEach(function (pair) {
                WBUI.each(WBUI.$$(pair[0], document), function (el) {
                    if (!WBUI.markOnce(el, 'TreeTableAll')) { return; }
                    el.addEventListener('click', function () {
                        rows.forEach(function (row) { setCollapsed(row, pair[1]); });
                        apply();
                    });
                });
            });

            apply();
        });
    }

    WBUI.register(init);
})(window);
