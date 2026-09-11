// workbench/methods/controls/color.js — 颜色控件（从 methods/inspector.js 提取）。

import { wbColorPicker } from '../../core.js';
import { commit, get } from './base.js';

// cssPropOf 由字段路径推断要读取的计算样式属性。
//
// 只做提示用，认不出来就返回空（不猜、不报错）。
function cssPropOf(path) {
    var key = String(path || '').split('.').pop().toLowerCase();
    if (key.indexOf('background') === 0 || key === 'bg' || key === 'bgcolor') return 'bg';
    if (key.indexOf('border') === 0) return 'border-color';
    if (key === 'color' || key === 'textcolor' || key === 'fill' || key === 'stroke') return 'color';
    return '';
}

// effectiveColorOf 读画布上该节点**当前真实生效**的颜色。
//
// 为什么读计算样式而不是在面板里推「这个字段对应哪个令牌」：
// getComputedStyle 拿到的是已经算完的最终值 —— 主题、页面级覆盖、组件自身 CSS 全都算进去了，
// 而令牌映射表既不完备（各组件字段名不同）也会随主题结构变化而失真。
// 背景色要沿祖先上溯到第一个不透明的元素，否则组件外层没有底色时只会显示 transparent。
function effectiveColorOf(node, path) {
    if (!node || !node.id) return '';
    var frame = document.getElementById('wb-canvas');
    var win = frame && frame.contentWindow;
    var doc = win && win.contentDocument;
    if (!doc) return '';
    var el = doc.querySelector('.sky-c-' + node.id);
    if (!el) return '';
    var kind = cssPropOf(path);
    if (!kind) return '';
    if (kind === 'color') {
        var c = win.getComputedStyle(el).color;
        return c || '';
    }
    // border-color：沿自身向上找第一条可见边框。
    if (kind === 'border-color') {
        var cur = el;
        while (cur) {
            var s = win.getComputedStyle(cur);
            if (s.borderTopWidth && parseFloat(s.borderTopWidth) > 0) {
                if (s.borderTopColor && s.borderTopColor !== 'rgba(0, 0, 0, 0)') return s.borderTopColor;
            }
            cur = cur.parentElement;
        }
        return '';
    }
    var node2 = el;
    while (node2) {
        var bg = win.getComputedStyle(node2).backgroundColor;
        if (bg && bg !== 'transparent' && bg !== 'rgba(0, 0, 0, 0)') return bg;
        node2 = node2.parentElement;
    }
    return '';
}

// colorControl 颜色控件：色板 + 文本（支持 var(--token)）。
//
// 属性为空 = 继承：此时把画布上当前生效的颜色显示出来（含"继承"小字提示），
// 只作展示不写模型 —— 用户改一下才落到组件属性上。
// 否则从组件面板拖出来的组件颜色字段是空框，看起来像"没应用主题"。
export function colorControl(ctx, label, path, ctl) {
    var wrap = document.createElement('div'); wrap.className = 'wb-field';
    var caption = document.createElement('label'); caption.textContent = label; wrap.appendChild(caption);
    var raw = get(ctx, path);
    var empty = raw == null || String(raw).trim() === '';
    var inherited = empty ? effectiveColorOf(ctx.node, path) : '';
    // 内联取色器：拖动时 150ms 防抖提交（实时预览），松手/输入结束立即提交。
    var cpTimer = null;
    var picker = wbColorPicker({
        value: empty ? '' : String(raw),
        placeholder: inherited ? '继承 ' + inherited : undefined,
        onInput: function (v, final) {
            if (cpTimer) { clearTimeout(cpTimer); cpTimer = null; }
            if (final) { commit(ctx, path, v); return; }
            cpTimer = setTimeout(function () { cpTimer = null; commit(ctx, path, v); }, 150);
        }
    });
    wrap.appendChild(picker);
    if (inherited) {
        var hint = document.createElement('div'); hint.className = 'wb-field-hint';
        hint.textContent = '当前继承自主题/页面：' + inherited + '（改动后只影响本组件）';
        wrap.appendChild(hint);
    }
    ctx.panel.appendChild(wrap);
}
