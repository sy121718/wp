// workbench/methods/controls/color.js — 颜色控件（从 methods/inspector.js 提取）。

import { wbColorPicker } from '../../core.js';
import { commit, get } from './base.js';

// colorControl 颜色控件：色板 + 文本（支持 var(--token)）。
export function colorControl(ctx, label, path, ctl) {
    var wrap = document.createElement('div'); wrap.className = 'wb-field';
    var caption = document.createElement('label'); caption.textContent = label; wrap.appendChild(caption);
    // 内联取色器：拖动时 150ms 防抖提交（实时预览），松手/输入结束立即提交。
    var cpTimer = null;
    var picker = wbColorPicker({
        value: get(ctx, path) == null ? '' : String(get(ctx, path)),
        onInput: function (v, final) {
            if (cpTimer) { clearTimeout(cpTimer); cpTimer = null; }
            if (final) { commit(ctx, path, v); return; }
            cpTimer = setTimeout(function () { cpTimer = null; commit(ctx, path, v); }, 150);
        }
    });
    wrap.appendChild(picker);
    ctx.panel.appendChild(wrap);
}

