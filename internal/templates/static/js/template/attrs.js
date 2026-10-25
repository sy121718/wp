// template/attrs.js — 属性面板（第三栏）：把选中块的 attrs / text 变成输入控件。
//
// 一个关键约束：**这里不决定「哪个属性合法」**。属性清单来自 state.js 的 BLOCK_SPEC，
// 而块类型能不能放在父块下面由**后端**（CompileTemplate / ValidateTree）判定。
// 前端做的是「按规格列出控件」，不做第二份合法性判据 —— 两份判据必然漂移，
// 而漂移的表现是「界面上能填、保存被拒」或反过来。
//
// 值一律当字符串：空串表示「用默认」，与 TemplateNode.Attrs 的契约一致。

import { BLOCK_SPEC } from './state.js';

/** ATTR_LABEL 属性名的界面文案（key + 兜底，模板取词后再注入也行；这里给兜底）。 */
var ATTR_LABEL = {
    'background-color': '背景色',
    'padding': '内边距',
    'width': '宽度',
    'color': '文字颜色',
    'font-size': '字号',
    'line-height': '行高',
    'text-align': '对齐',
    'src': '图片地址',
    'alt': '替代文字',
    'href': '链接',
    'height': '高度'
};

/** ATTR_PLACEHOLDER 每个属性的填法提示（比 label 更能省一次试错）。 */
var ATTR_PLACEHOLDER = {
    'background-color': '#ffffff',
    'padding': '16px',
    'width': '100%',
    'color': '#22303f',
    'font-size': '15px',
    'line-height': '1.7',
    'text-align': 'left / center / right',
    'src': 'https://…（必填）',
    'alt': '图片加载不出来时显示这句',
    'href': 'https://…',
    'height': '24'
};

/** ATTRIBUTE_KEYS 可编辑的文本字段（与 BLOCK_SPEC.attrs 无关的那些）。 */
export function attrFields(node) {
    var spec = BLOCK_SPEC[node.type] || { attrs: [], hasText: false };
    return {
        attrs: spec.attrs || [],
        hasText: !!spec.hasText
    };
}

/** renderAttrs 生成属性面板的 DOM 片段（先清空再由调用方挂上）。 */
export function renderAttrs(node, opts) {
    var o = opts || {};
    var doc = o.document || document;
    var frag = doc.createDocumentFragment();
    if (!node) { return frag; }

    var spec = BLOCK_SPEC[node.type] || { label: node.type, attrs: [], hasText: false };
    var head = doc.createElement('p');
    head.className = 'tpl-attr-head';
    head.textContent = spec.label + '（' + node.key + '）';
    frag.appendChild(head);

    if (spec.hasText) {
        frag.appendChild(fieldOf(doc, {
            label: '内容',
            value: node.text || '',
            placeholder: node.type === 'raw' ? '原样输出的 HTML 片段' : '支持 {{.变量}}',
            multiline: true,
            onChange: function (v) { if (o.onText) { o.onText(node.key, v); } }
        }));
    }

    (spec.attrs || []).forEach(function (attr) {
        frag.appendChild(fieldOf(doc, {
            label: ATTR_LABEL[attr] || attr,
            value: node.attrs[attr] || '',
            placeholder: ATTR_PLACEHOLDER[attr] || '',
            multiline: false,
            onChange: function (v) { if (o.onAttr) { o.onAttr(node.key, attr, v); } }
        }));
    });

    if (!spec.hasText && !(spec.attrs || []).length) {
        var note = doc.createElement('p');
        note.className = 'tpl-attr-hint';
        note.textContent = '这个块没有可填的属性。';
        frag.appendChild(note);
    }
    return frag;
}

function fieldOf(doc, cfg) {
    var wrap = doc.createElement('p');
    wrap.className = 'tpl-attr-field';
    var label = doc.createElement('label');
    label.textContent = cfg.label;
    // 基座类在创建时立刻挂上（外观真源只有 ui.css 一处）：动态控件忘带基座类的
    // 表现是「控件在页面上长得像浏览器默认样式」，而它不会让任何测试变红 ——
    // 所以把它们写在 createElement 的同一行，并由门禁盯着（js-base-skip 之外必须有类）。
    var input;
    if (cfg.multiline) {
        input = doc.createElement('textarea'); // 基座类：form-textarea
        input.rows = 4;
    } else {
        input = doc.createElement('input'); // 基座类：form-input
        input.type = 'text';
    }
    input.className = cfg.multiline ? 'form-textarea' : 'form-input';
    input.value = cfg.value;
    input.placeholder = cfg.placeholder;
    // input 事件（不是 change）：属性面板要**边打边更新**画布预览，否则每改一次都要
    // 失焦才生效，而用户的期待是「打进去就看见」。
    input.addEventListener('input', function () { cfg.onChange(input.value); });
    label.appendChild(input);
    wrap.appendChild(label);
    return wrap;
}
