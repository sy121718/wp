// workbench/methods/controls/selects.js — 服务端渲染 select → 自定义下拉升级 + 字段联动提示。
//
// 根因（实测）：检查器面板由服务端片段渲染，select 控件是**原生 <select>**
// （fragments/inspector_panel.html），而原生 select 在 Linux/Chromium 上是
// 「按下展开、松开即选」——弹层首项恰好压在控件原位置，松手就选中并收起，
// 表现为「下拉点开一瞬间就自动选择/关闭，无法选择」。这正是 core.js 的
// wbDropdown 当初存在的理由，但面板改成服务端渲染后只有手写面板（动效等）
// 用了 wb-dd，schema 驱动的 select 全部退回原生控件，老问题因此回归。
//
// 面板只调用公共 WBUI.scan；基座缺失或初始化失败会进入面板错误提示。

import { wbFieldHints } from '../../core.js';

// upgradeNativeSelects 面板重渲染后，把面板里的原生 select 交给**基座**接管。
//
// 这里原来是自己把 select 升级成 wb-dd（core.js 的 wbDropdown）。那份实现与基座
// ui/select.js 解决的是同一个问题（见本文件头部实测记录：原生 select 在
// Linux/Chromium 上「点开一瞬间就自动选择/关闭」），属于同一件事的第三份实现。
// 现在只保留一个实现：
//   · 基座保留原生 select 作为取值/回写载体（.wbs-native 视觉隐藏）；
//   · 选中后派发冒泡 change，仍由检查器既有的 panel.onchange（[data-wb-path] 委托）
//     回写 AST —— 回写通道一个没变；
//   · option 动态增删由基座的 MutationObserver 同步（工作台那版没有这个能力）。
//
// 本函数保留为「面板增强阶段」的入口：morphHTML 之后由 inspector 调用；
// 幂等由基座的 markOnce 保证（morph 后是新节点会重新增强）。
export function upgradeNativeSelects(ctx) {
    var panel = ctx.panel;
    if (!panel) return;
    if (!window.WBUI || typeof window.WBUI.scan !== 'function') {
        throw new Error('工作台缺少控件基座：请通过 partials/ui_scripts.html 加载');
    }
    if (window.WBUI.scan(panel).length) {
        throw new Error('工作台控件初始化失败，详情见 WBUI 错误日志');
    }
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
