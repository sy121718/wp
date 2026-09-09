// workbench/methods/controls/selects.js — 服务端渲染 select → 自定义下拉升级 + 字段联动提示。
//
// 根因（实测）：检查器面板由服务端片段渲染，select 控件是**原生 <select>**
// （fragments/inspector_panel.html），而原生 select 在 Linux/Chromium 上是
// 「按下展开、松开即选」——弹层首项恰好压在控件原位置，松手就选中并收起，
// 表现为「下拉点开一瞬间就自动选择/关闭，无法选择」。这正是 core.js 的
// wbDropdown 当初存在的理由，但面板改成服务端渲染后只有手写面板（动效等）
// 用了 wb-dd，schema 驱动的 select 全部退回原生控件，老问题因此回归。
//
// 修法：在面板增强阶段把原生 select 就地升级为 wb-dd（同一份交互实现，三端一致）。
// 原生 select 保留在 DOM 里作为取值/回写载体（.wb-dd-src 隐藏），选择后派发冒泡
// change 事件，仍由检查器既有的 [data-wb-path] 委托回写 AST —— 不新增回写通道。

import { wbDropdown, wbFieldHints } from '../../core.js';

// upgradeNativeSelects 把面板内的原生 select 升级为自定义下拉。
// 幂等：同一 DOM 上重复调用只升级一次（morph 后是新节点，会重新升级）。
export function upgradeNativeSelects(ctx) {
    var panel = ctx.panel;
    if (!panel || !panel.querySelectorAll) return;
    var list = panel.querySelectorAll('select[data-wb-path][data-wb-kind="select"]');
    Array.prototype.forEach.call(list, function (sel) {
        if (sel.dataset.wbDdUpgraded === '1') return;
        var choices = [];
        Array.prototype.forEach.call(sel.options, function (o) {
            choices.push([o.value, o.textContent]);
        });
        var key = sel.getAttribute('data-wb-path') || '';
        var dd = wbDropdown(choices, sel.value, {
            key: key,
            onChange: function (v) {
                if (sel.value === v) return;
                sel.value = v;
                // 交给既有委托（inspector.js bindInspectorHtmx 的 panel.onchange）回写 AST。
                sel.dispatchEvent(new Event('change', { bubbles: true }));
            }
        });
        sel.dataset.wbDdUpgraded = '1';
        sel.classList.add('wb-dd-src');
        if (sel.parentNode) sel.parentNode.insertBefore(dd.root, sel.nextSibling);
    });
}

// refreshFieldHints 按当前模型刷新提示：先清掉旧提示再重算。
// 值补全后提示必须立刻消失，而属性提交只 patch 画布、不重渲染面板，
// 所以每次提交后都要显式刷新一次（不是等下一次面板重渲染）。
export function refreshFieldHints(ctx) {
    var panel = ctx.panel;
    if (!panel || !panel.querySelectorAll) return;
    Array.prototype.forEach.call(panel.querySelectorAll('.wb-field-hint'), function (el) {
        if (el.parentNode) el.parentNode.removeChild(el);
    });
    renderFieldHints(ctx);
}

// renderFieldHints 展示「字段联动后仍需人工补全」的提示（如 native 动作缺号码）。
// 提示与联动规则同源（core.js wbFieldHints → wbActionValue），不会两处漂移。
export function renderFieldHints(ctx) {
    var panel = ctx.panel;
    if (!panel || !panel.querySelector) return;
    wbFieldHints(ctx.node).forEach(function (h) {
        var key = String(h.path).replace(/^props\./, '');
        var el = panel.querySelector('[data-wb-path="' + key.replace(/"/g, '') + '"]');
        var wrap = el && el.closest ? el.closest('.wb-field') : null;
        if (!wrap) return;
        var p = document.createElement('p');
        p.className = 'wb-field-hint';
        p.textContent = h.text;
        wrap.appendChild(p);
    });
}
