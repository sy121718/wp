// rich-editor/util.js — 富文本扩展的公共工具。
//
// 目录约定（见 index.js 顶部说明）：每个扩展一个文件，普通脚本（非 ES module），
// 统一挂在 window.SkyRichEditor 命名空间下，由 index.js 按顺序动态注入。
// 所有文件都必须「幂等」：被重复执行时先看挂载点是否已存在，已存在就直接返回。
(function () {
    'use strict';

    var SRE = (window.SkyRichEditor = window.SkyRichEditor || {});
    if (SRE.util) {
        return;
    }

    // MIME 常量：Trix 附件（attachment）的 contentType 用它区分「这个原子块是什么」。
    // 表格与手风琴在 Trix 的扁平文档模型里没有对应结构，只能作为原子附件存在文档里，
    // 提交时再由本目录的 toDocumentHTML 展开成真标签（详见 table.js / accordion.js 顶部说明）。
    var MIME = {
        TABLE: 'application/vnd.go-wp.rich-editor.table+json',
        ACCORDION: 'application/vnd.go-wp.rich-editor.accordion+json',
        RULE: 'application/vnd.go-wp.rich-editor.rule+json'
    };

    // 是否「像一段 HTML」：与 Go 侧 internal/builder/core.HasRichMarkup 同口径 ——
    // 出现任意标签 token 就算富文本。用于区分「存量纯文本」与「富文本」。
    var TAG_RE = /<[a-zA-Z!/?][^>]*>/;

    function hasRichMarkup(src) {
        return TAG_RE.test(String(src == null ? '' : src));
    }

    // HTML 文本转义（与 Go 侧 html.EscapeString 同口径）。
    function escapeHTML(src) {
        return String(src == null ? '' : src)
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&#34;')
            .replace(/'/g, '&#39;');
    }

    // 存量纯文本 → 段落化 HTML：先按空行分段，段内转义后再把换行换成 <br>。
    // 顺序不可颠倒（反过来会把 <br> 自身转义成可见文本）。口径与 Go 侧 core.plainTextToHTML 一致：
    // 后端渲染存量纯文本走的是同一条规则，编辑器里看到的形状必须与发布后一致。
    function plainTextToHTML(src) {
        var s = String(src == null ? '' : src).replace(/\r\n/g, '\n').replace(/\r/g, '\n');
        var blocks = s.split(/\n[ \t]*\n+/);
        var out = [];
        for (var i = 0; i < blocks.length; i++) {
            var block = blocks[i].replace(/^[ \t\n]+|[ \t\n]+$/g, '');
            if (!block) {
                continue;
            }
            out.push('<p>' + escapeHTML(block).replace(/\n/g, '<br>') + '</p>');
        }
        return out.join('');
    }

    // 富文本 HTML 的安全化入口：不含标签的输入一律当作纯文本转义 + 分段。
    // 「存量纯文本先转义再交给编辑器」这条要求落在这里 —— 绝不把旧纯文本当 HTML 直接塞进 Trix。
    function safeEditorHTML(src) {
        var s = String(src == null ? '' : src);
        if (!s) {
            return '';
        }
        return hasRichMarkup(s) ? s : plainTextToHTML(s);
    }

    // UTF-8 ↔ base64：附件 content 里放的是 JSON，base64 掉可以避开引号/尖括号在
    // data-trix-attachment 属性与 Trix 预览渲染里的所有转义陷阱。
    function encodePayload(obj) {
        var json = JSON.stringify(obj);
        var bytes = new TextEncoder().encode(json);
        var bin = '';
        for (var i = 0; i < bytes.length; i++) {
            bin += String.fromCharCode(bytes[i]);
        }
        return btoa(bin);
    }

    function decodePayload(text) {
        try {
            var bin = atob(String(text || ''));
            var bytes = new Uint8Array(bin.length);
            for (var i = 0; i < bin.length; i++) {
                bytes[i] = bin.charCodeAt(i);
            }
            return JSON.parse(new TextDecoder().decode(bytes));
        } catch (err) {
            return null;
        }
    }

    // 富文本片段 → 纯文本（细节：br/块级标签换算行，供摘要与对话框预填用）。
    function toPlainText(html) {
        var holder = document.createElement('div');
        holder.innerHTML = String(html == null ? '' : html);
        var blocks = holder.querySelectorAll('p,div,h1,h2,h3,h4,h5,h6,li,blockquote,pre,tr,summary');
        for (var i = 0; i < blocks.length; i++) {
            blocks[i].appendChild(document.createTextNode('\n'));
        }
        return String(holder.textContent || '').replace(/\n{3,}/g, '\n\n').replace(/^\n+|\n+$/g, '');
    }

    // 在光标处插入 HTML 的兜底路径（Trix 的 insertHTML 不可用时）。
    function focusEnd(el) {
        if (!el) {
            return;
        }
        var range = document.createRange();
        range.selectNodeContents(el);
        range.collapse(false);
        var sel = window.getSelection();
        sel.removeAllRanges();
        sel.addRange(range);
    }

    SRE.MIME = MIME;
    SRE.util = {
        hasRichMarkup: hasRichMarkup,
        escapeHTML: escapeHTML,
        plainTextToHTML: plainTextToHTML,
        safeEditorHTML: safeEditorHTML,
        encodePayload: encodePayload,
        decodePayload: decodePayload,
        toPlainText: toPlainText,
        focusEnd: focusEnd
    };
})();
