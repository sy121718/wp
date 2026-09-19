// rich-editor/accordion.js — 手风琴（details / summary 折叠块）。
//
// 走的扩展路径：与表格同理，**Trix 的 attachment（附件）机制**。
// Trix 的块模型里没有「容器块」：<details> 要包住 summary 与若干段落，而一个块只能有一个标签名，
// 解析时 Trix 会把 details/summary 当普通标签、把里面的文本合并进同一个块，结构活不下来。
// 所以折叠块在编辑器里是一个原子附件，content 里放 base64 的 JSON（标题 + 正文），
// 序列化到编辑器 HTML 时是 <figure data-trix-attachment='{"contentType":"…accordion+json",…}'>，
// 提交时展开成 <details><summary>标题</summary><p>正文</p></details>。
(function () {
    'use strict';

    var SRE = (window.SkyRichEditor = window.SkyRichEditor || {});
    if (SRE.accordion) {
        return;
    }

    function attachmentFor(payload) {
        var Trix = window.Trix;
        if (!Trix) {
            return null;
        }
        var title = String((payload && payload.title) || '折叠块');
        return new Trix.Attachment({
            contentType: SRE.MIME.ACCORDION,
            content: SRE.util.encodePayload({
                type: 'accordion',
                title: title,
                body: String((payload && payload.body) || '')
            }),
            filename: title
        });
    }

    function insert(editorController) {
        SRE.dialog.open({
            title: '插入折叠块',
            fields: [
                { name: 'title', label: '标题（点击展开的那一行）', type: 'text', value: '', placeholder: '例如：常见问题一' },
                { name: 'body', label: '内容（空行分段）', type: 'textarea', rows: 6, value: '' }
            ],
            onSubmit: function (values) {
                var att = attachmentFor({ title: values.title || '折叠块', body: values.body || '' });
                if (att && editorController) {
                    editorController.insertAttachment(att);
                }
            }
        });
    }

    function edit(editorController, range, payload) {
        SRE.dialog.open({
            title: '编辑折叠块',
            fields: [
                { name: 'title', label: '标题（点击展开的那一行）', type: 'text', value: (payload && payload.title) || '' },
                { name: 'body', label: '内容（空行分段）', type: 'textarea', rows: 8, value: (payload && payload.body) || '' }
            ],
            onSubmit: function (values) {
                var att = attachmentFor({ title: values.title || '折叠块', body: values.body || '' });
                if (!att || !editorController) {
                    return;
                }
                if (range) {
                    editorController.setSelectedRange(range);
                }
                editorController.insertAttachment(att);
            }
        });
    }

    // hydrate：文档 HTML 里的 <details> → 附件形态。
    function hydrate(detailsEl) {
        var summary = detailsEl.querySelector('summary');
        var title = summary ? String(summary.textContent || '').replace(/^\s+|\s+$/g, '') : '折叠块';
        var clone = detailsEl.cloneNode(true);
        var cloneSummary = clone.querySelector('summary');
        if (cloneSummary) {
            cloneSummary.parentNode.removeChild(cloneSummary);
        }
        var body = SRE.util.toPlainText(clone.innerHTML);
        var figure = document.createElement('figure');
        figure.setAttribute('data-trix-attachment', JSON.stringify({
            contentType: SRE.MIME.ACCORDION,
            content: SRE.util.encodePayload({ type: 'accordion', title: title, body: body }),
            filename: title
        }));
        figure.setAttribute('data-trix-content-type', SRE.MIME.ACCORDION);
        figure.className = 'attachment attachment--preview sre-attachment sre-attachment--accordion';
        figure.innerHTML = '<span class="sre-attachment__badge">折叠块：' + SRE.util.escapeHTML(title) + '</span>';
        return figure;
    }

    // toDocumentHTML：附件 payload → <details><summary>…</summary>…</details>。
    function toDocumentHTML(payload) {
        if (!payload || payload.type !== 'accordion') {
            return null;
        }
        var title = SRE.util.escapeHTML(payload.title || '折叠块');
        var body = SRE.util.plainTextToHTML(payload.body || '');
        return '<details><summary>' + title + '</summary>' + body + '</details>';
    }

    SRE.accordion = {
        mime: SRE.MIME.ACCORDION,
        insert: insert,
        edit: edit,
        hydrate: hydrate,
        toDocumentHTML: toDocumentHTML
    };
})();
