// rich-editor/horizontal-rule.js — 水平线。
//
// 走的扩展路径：**Trix 的 attachment（附件）机制**。
// 为什么不是 blockAttributes：blockAttributes 只描述「一个文本块的标签名」，
// 没有 void 元素（空元素）的位置 —— 注册一个 tagName=hr 的块属性会让 Trix 试图往
// 这个块里放文本，产出 <hr>文本</hr> 这种非法结构。附件在 Trix 里是原子块（占一个位置、
// 不可在其内部输入），正好对应「一条自闭合的分隔线」，序列化后是
// <figure data-trix-attachment='{"contentType":"…rule+json","content":"<base64>"}'>，
// 提交时由 serializer.js 还原成 <hr>。
(function () {
    'use strict';

    var SRE = (window.SkyRichEditor = window.SkyRichEditor || {});
    if (SRE.ruleInstalled) {
        return;
    }
    SRE.ruleInstalled = true;

    function attachmentFor() {
        var Trix = window.Trix;
        if (!Trix) {
            return null;
        }
        return new Trix.Attachment({
            contentType: SRE.MIME.RULE,
            content: SRE.util.encodePayload({ type: 'rule' }),
            filename: '分隔线'
        });
    }

    function insert(editorController) {
        var att = attachmentFor();
        if (!att || !editorController) {
            return false;
        }
        editorController.insertAttachment(att);
        return true;
    }

    // 装载方向：文档 HTML 里的 <hr> → 编辑器里的附件形态。
    function hydrate(node) {
        var Trix = window.Trix;
        var figure = document.createElement('figure');
        figure.setAttribute('data-trix-attachment', JSON.stringify({
            contentType: SRE.MIME.RULE,
            content: SRE.util.encodePayload({ type: 'rule' }),
            filename: '分隔线'
        }));
        figure.setAttribute('data-trix-content-type', SRE.MIME.RULE);
        figure.className = 'attachment attachment--preview sre-attachment sre-attachment--rule';
        figure.innerHTML = '<span class="sre-attachment__badge">分隔线</span>';
        void Trix;
        void node;
        return figure;
    }

    // 提交方向：附件 → <hr>。
    function toDocumentHTML(payload) {
        if (!payload || payload.type !== 'rule') {
            return null;
        }
        return '<hr>';
    }

    SRE.rule = {
        mime: SRE.MIME.RULE,
        hydrate: hydrate,
        toDocumentHTML: toDocumentHTML,
        insert: insert,
        label: '分隔线'
    };
})();
