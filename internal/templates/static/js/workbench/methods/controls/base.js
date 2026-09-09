// workbench/methods/controls/base.js — 检查器基础字段原语（从 methods/inspector.js 的 syncInspector 闭包提取）。
// 约定：所有函数首参为 ctx = { panel, node, self }；ctx.panel 可被临时改写（slot 填充 / 分组挂载）。

import { wbDropdown, wbColorPicker, optionLabel } from '../../core.js';

export function get(ctx, path) {
    return path.split('.').reduce(function (value, key) { return value == null ? undefined : value[key]; }, ctx.node);
}

export function set(ctx, path, value) {
    var parts = path.split('.'); var target = ctx.node;
    for (var i = 0; i < parts.length - 1; i++) {
        if (!target[parts[i]]) target[parts[i]] = {};
        target = target[parts[i]];
    }
    target[parts[parts.length - 1]] = value;
}

export function commit(ctx, path, value, after) {
    ctx.self.snapshot();
    set(ctx, path, value);
    if (after) after(ctx, value);
    // 字段联动：源字段写入后，把 WB_FIELD_LINKS 声明过的目标字段归一为新值
    // （如 button 的 action → value），与 inspector.js 的提交路径共用同一实现。
    var linked = ctx.self.applyLinkedFields ? ctx.self.applyLinkedFields(path) : false;
    ctx.self.renderTree(); ctx.self.renderUI();
    // 属性改动只 patch 当前节点（局部刷新，不重载 iframe）；
    // 无节点上下文（页面设置等）回退整页刷新。
    var nid = (typeof ctx.node !== 'undefined' && ctx.node && ctx.node.id) ? ctx.node.id : '';
    if (nid) ctx.self.patchCanvas(nid); else ctx.self.refreshCanvas();
    // 联动改写了其它字段：重渲染面板，让输入框与提示与模型一致。
    if (linked && ctx.self.syncInspector) ctx.self.syncInspector();
    else if (ctx.self.refreshFieldHints) ctx.self.refreshFieldHints();
}

export function heading(ctx, text) {
    var h = document.createElement('h3'); h.className = 'wb-inspector-section'; h.textContent = text; ctx.panel.appendChild(h);
}

export function field(ctx, label, path, kind, choices, after, extra) {
    var wrap = document.createElement('div'); wrap.className = 'wb-field';
    if (kind === 'segment') {
        // 分段按钮组:choices = [[value, text]],当前值高亮。
        var seg = document.createElement('div'); seg.className = 'wb-seg';
        var current = get(ctx, path) == null ? '' : String(get(ctx, path));
        var segBtns = [];
        choices.forEach(function (ch) {
            var b = document.createElement('button');
            b.type = 'button'; b.className = 'wb-seg-btn'; b.textContent = ch[1]; b.title = ch[0];
            if (ch[0] === current || (!current && ch[2])) b.classList.add('is-active');
            b.addEventListener('click', function () {
                commit(ctx, path, ch[0], after);
                segBtns.forEach(function (x) { x.classList.toggle('is-active', x === b); });
            });
            segBtns.push(b); seg.appendChild(b);
        });
        var cap = document.createElement('label'); cap.textContent = label;
        wrap.appendChild(cap); wrap.appendChild(seg); ctx.panel.appendChild(wrap);
        return;
    }
    var caption = document.createElement('label'); caption.textContent = label; wrap.appendChild(caption);
    if (kind === 'select') {
        // 自定义下拉：跨平台一致交互（原生 select 在 Linux 为按下即选）；
        // 点选即 commit，程序化赋值不触发（对齐原生 change 语义）。
        wrap.appendChild(wbDropdown(choices, get(ctx, path) == null ? '' : String(get(ctx, path)), {
            onChange: function (v) { commit(ctx, path, v, after); }
        }).root);
        ctx.panel.appendChild(wrap);
        return;
    }
    // 颜色类字段：内联取色器（可调透明度 → transparent，文本框支持 var(--token)）。
    var colorKeyMatch = path.match(/\.(color|bgColor|borderColor|titleColor|border|bg|fill)$/i) || /^props\.(color|bgColor|background|bg|border|fill)$/.test(path);
    if (kind === 'input' && colorKeyMatch) {
        // 拖动取色：150ms 防抖合并提交，避免连续整帧刷新与 undo 栈污染；
        // 松手 / 文本输入结束立即提交。
        var cpTimer2 = null;
        wrap.appendChild(wbColorPicker({
            value: get(ctx, path) == null ? '' : String(get(ctx, path)),
            onInput: function (v, final) {
                if (cpTimer2) { clearTimeout(cpTimer2); cpTimer2 = null; }
                if (final) { commit(ctx, path, v, after); return; }
                cpTimer2 = setTimeout(function () { cpTimer2 = null; commit(ctx, path, v, after); }, 150);
            }
        }));
        ctx.panel.appendChild(wrap);
        return;
    }
    var input = document.createElement(kind === 'textarea' ? 'textarea' : 'input');
    if (kind === 'textarea') input.rows = 4;
    if (kind === 'number') { input.type = 'number'; }
    if (extra) { if (extra.min !== undefined) input.min = extra.min; if (extra.max !== undefined) input.max = extra.max; if (extra.step !== undefined) input.step = extra.step; }
    input.value = get(ctx, path) == null ? '' : String(get(ctx, path));
    input.addEventListener('change', function () {
        var value = input.value;
        if (kind === 'number') value = input.value === '' ? '' : Number(input.value);
        commit(ctx, path, value, after);
    });
    wrap.appendChild(input);
    // 媒体类字段：缩略图预览 + 媒体库选择 + 清除（对齐 Elementor 图片控件）。
    // src/bgImage 尾缀：直接回填 URL（画布/产物直出，构建期零解析）。
    if (kind === 'input' && /\.(src|bgImage)$/.test(path)) {
        var setPreview = function (url) {
            previewImg.src = url || '';
            previewImg.classList.toggle('is-empty', !previewImg.src);
            previewTip.textContent = previewImg.src ? '' : '点击选择图片';
        };
        var applyPick = function (url) {
            commit(ctx, path, url);
            input.value = url;
            setPreview(url);
        };
        var previewBox = document.createElement('div');
        previewBox.className = 'wb-media-field' + (input.value ? ' has-image' : '');
        var previewImg = document.createElement('img');
        previewImg.alt = '';
        var initialURL = ctx.self.resolveAssetUrl(input.value);
        if (initialURL) previewImg.src = initialURL; else previewImg.classList.add('is-empty');
        var pickFromPreview = function () {
            ctx.self.openMediaPicker(applyPick);
        };
        previewBox.appendChild(previewImg);
        var previewTip = document.createElement('span');
        previewTip.className = 'wb-media-tip';
        previewTip.textContent = initialURL ? '' : '点击选择图片';
        previewBox.appendChild(previewTip);
        previewBox.addEventListener('click', pickFromPreview);
        var pickBtn = document.createElement('button');
        pickBtn.type = 'button'; pickBtn.className = 'wb-btn wb-btn-secondary wb-btn-sm';
        pickBtn.textContent = '媒体库';
        pickBtn.addEventListener('click', pickFromPreview);
        var clearBtn = document.createElement('button');
        clearBtn.type = 'button'; clearBtn.className = 'wb-btn wb-btn-ghost wb-btn-sm';
        clearBtn.textContent = '清除';
        clearBtn.addEventListener('click', function () {
            input.value = '';
            commit(ctx, path, '');
            previewImg.src = '';
            previewImg.classList.add('is-empty');
            previewTip.textContent = '点击选择图片';
        });
        var row = document.createElement('div'); row.className = 'wb-media-row';
        row.appendChild(pickBtn); row.appendChild(clearBtn);
        wrap.removeChild(input);
        wrap.appendChild(previewBox);
        wrap.appendChild(row);
        ctx.panel.appendChild(wrap);
        return;
    }
    ctx.panel.appendChild(wrap);
}

export function checkbox(ctx, label, path) {
    var wrap = document.createElement('label'); wrap.className = 'wb-check-field';
    var input = document.createElement('input'); input.type = 'checkbox'; input.checked = !!get(ctx, path);
    input.addEventListener('change', function () { commit(ctx, path, input.checked); });
    wrap.appendChild(input); wrap.appendChild(document.createTextNode(label)); ctx.panel.appendChild(wrap);
}

// 分段按钮组(WP 式):短枚举平铺,点选即提交,当前值高亮。
export function segmentedField(ctx, label, path, ctl, after) {
    var wrap = document.createElement('div'); wrap.className = 'wb-field wb-field-seg';
    var caption = document.createElement('label'); caption.textContent = label; wrap.appendChild(caption);
    var seg = document.createElement('div'); seg.className = 'wb-seg';
    var current = get(ctx, path) == null ? '' : String(get(ctx, path));
    var buttons = [];
    // 选项支持 {value,label} 对象（ct tag 声明中文标签）或纯字符串。
    (ctl.options || []).forEach(function (o) {
        var value = (o && typeof o === 'object') ? o.value : o;
        var text = (o && typeof o === 'object' && o.label) ? o.label : optionLabel(value);
        var b = document.createElement('button');
        b.type = 'button'; b.className = 'wb-seg-btn';
        b.textContent = text;
        b.title = value;
        if (value === current || (!current && ctl.default && value === ctl.default)) b.classList.add('is-active');
        b.addEventListener('click', function () {
            commit(ctx, path, value, after);
            buttons.forEach(function (x) { x.classList.toggle('is-active', x === b); });
        });
        buttons.push(b);
        seg.appendChild(b);
    });
    wrap.appendChild(seg); ctx.panel.appendChild(wrap);
}

