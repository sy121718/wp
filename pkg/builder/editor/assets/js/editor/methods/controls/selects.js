// 检查器的原生字段和复杂字段都使用公共 UI Kit 下拉。
// 本模块只把字段路径转成通用状态键，并将初始化错误交给面板显示。
import { wbFieldHints } from '../../core.js';

export function upgradeNativeSelects(ctx) {
    var panel = ctx.panel;
    if (!panel) return;
    if (!window.WBUI || typeof window.WBUI.scan !== 'function') {
        throw new Error('工作台缺少控件基座：请通过 partials/ui_scripts.html 加载');
    }
    panel.querySelectorAll('select[data-wb-path]').forEach(function (sel) { sel.dataset.uiKey = sel.dataset.wbPath; });
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
