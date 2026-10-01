// rich-editor/table.js — 表格（插入 + 行列增删）。
//
// 走的扩展路径：**Trix 的 attachment（附件）机制**，而不是 blockAttributes。
// 原因是实测出来的：Trix 的 HTMLParser 对表格有专门分支（vendor 源码 trix.umd.js:8644-8652）——
// 解析时 <tr> 被换成 parser.tableRowSeparator（"\n"）、<td> 被换成 tableCellSeparator（" | "），
// 表格结构在进入文档模型前就被压平成一行文本。也就是说 Trix 的块模型（一串扁平块）
// 根本存不下「行 × 列」的嵌套结构，任何基于块属性的表格扩展都会静默丢结构。
//
// 因此表格在 Trix 里是一个**原子附件**：contentType 标记它是表格，content 里放 base64 的
// JSON 数据（行/列/单元格），编辑器里显示为一张可点击的卡片，点击卡片打开对话框增删行列。
// 序列化到编辑器 HTML 时是：
//   <figure data-trix-attachment='{"contentType":"…table+json","content":"<base64>"}' data-trix-content-type="…">
// 提交时由 index.js 的 toDocumentHTML 展开成真正的 <table><thead>…</thead><tbody>…</tbody></table>，
// 服务端白名单只需要认识真表格标签。
(function () {
    'use strict';

    var SRE = (window.SkyRichEditor = window.SkyRichEditor || {});
    if (SRE.table) {
        return;
    }

    function maxColumns(rows) {
        var n = 1;
        for (var i = 0; i < rows.length; i++) {
            if (rows[i].length > n) {
                n = rows[i].length;
            }
        }
        return n;
    }

    function normalize(data) {
        var rows = (data && data.rows) || [];
        var cols = Math.max(1, Math.min(12, parseInt(data && data.cols, 10) || maxColumns(rows)));
        var out = [];
        for (var i = 0; i < rows.length; i++) {
            var row = [];
            for (var j = 0; j < cols; j++) {
                row.push(typeof rows[i][j] === 'string' ? rows[i][j] : '');
            }
            out.push(row);
        }
        return {
            type: 'table',
            header: !(data && data.header === false),
            rows: out
        };
    }

    function emptyPayload(rows, cols) {
        var data = [];
        for (var i = 0; i < rows; i++) {
            var row = [];
            for (var j = 0; j < cols; j++) {
                row.push('');
            }
            data.push(row);
        }
        return { type: 'table', header: true, rows: data };
    }

    function attachmentFor(payload) {
        var Trix = window.Trix;
        if (!Trix) {
            return null;
        }
        return new Trix.Attachment({
            contentType: SRE.MIME.TABLE,
            content: SRE.util.encodePayload(normalize(payload)),
            filename: '表格 ' + normalize(payload).rows.length + ' × ' + maxColumns(normalize(payload).rows)
        });
    }

    function insert(editorController) {
        SRE.dialog.open({
            title: '插入表格',
            fields: [
                { name: 'rows', label: '行数（含表头行）', type: 'number', value: 3, min: 1, max: 12 },
                { name: 'cols', label: '列数', type: 'number', value: 3, min: 1, max: 12 }
            ],
            onSubmit: function (values) {
                var att = attachmentFor(emptyPayload(values.rows || 3, values.cols || 3));
                if (att && editorController) {
                    editorController.insertAttachment(att);
                }
            }
        });
    }

    // edit 打开已有表格的编辑框：可以在里面改单元格、增删行列。
    // range 是点击卡片时记录的文档位置范围，确定后用新附件替换该范围。
    function edit(editorController, range, payload) {
        var data = normalize(payload);
        var lines = [];
        for (var i = 0; i < data.rows.length; i++) {
            lines.push(data.rows[i].join(' | '));
        }
        var headerLine = data.header && data.rows.length ? data.rows[0].join(' | ') : '';

        function apply(rows, header, headerOn) {
            var att = attachmentFor({ type: 'table', header: headerOn, rows: rows });
            if (!att || !editorController) {
                return;
            }
            if (range) {
                editorController.setSelectedRange(range);
            }
            editorController.insertAttachment(att);
        }

        SRE.dialog.open({
            title: '编辑表格',
            fields: [
                { name: 'header', label: '表头行（单元格用竖线分隔）', type: 'text', value: headerLine },
                { name: 'body', label: '正文行（每行一条，单元格用竖线分隔）', type: 'textarea', rows: 8, value: lines.slice(data.header ? 1 : 0).join('\n') }
            ],
            extra: [
                {
                    label: '＋ 列',
                    onClick: function () {
                        var rows = data.rows.map(function (row) { return row.concat(['']); });
                        apply(rows, true, data.header);
                    }
                },
                {
                    label: '－ 列',
                    onClick: function () {
                        var rows = data.rows.map(function (row) {
                            return row.length > 1 ? row.slice(0, row.length - 1) : row;
                        });
                        apply(rows, true, data.header);
                    }
                },
                {
                    label: '＋ 行',
                    onClick: function () {
                        var rows = data.rows.concat([data.rows[0].map(function () { return ''; })]);
                        apply(rows, true, data.header);
                    }
                },
                {
                    label: '－ 行',
                    onClick: function () {
                        var rows = data.rows.length > 1 ? data.rows.slice(0, data.rows.length - 1) : data.rows;
                        apply(rows, true, data.header);
                    }
                }
            ],
            onSubmit: function (values) {
                var headerOn = String(values.header || '').replace(/^\s+|\s+$/g, '') !== '';
                var headerCells = splitCells(values.header);
                var rows = [];
                if (headerOn) {
                    rows.push(headerCells);
                }
                var bodyLines = String(values.body || '').split('\n');
                for (var i = 0; i < bodyLines.length; i++) {
                    if (!bodyLines[i].replace(/^\s+|\s+$/g, '')) {
                        continue;
                    }
                    rows.push(splitCells(bodyLines[i]));
                }
                if (!rows.length) {
                    rows.push(['']);
                }
                apply(rows, headerOn, headerOn);
            }
        });
    }

    function splitCells(line) {
        return String(line == null ? '' : line).split('|').map(function (cell) {
            return cell.replace(/^\s+|\s+$/g, '');
        });
    }

    // hydrate：文档 HTML 里的 <table> → 编辑器里的附件形态（figure）。
    // Trix 自己在解析表格时会压平，所以必须在把 HTML 交给 Trix 之前转换（见 index.js 的装载流程）。
    function hydrate(tableEl) {
        var rows = [];
        var header = false;
        var trs = tableEl.querySelectorAll('tr');
        for (var i = 0; i < trs.length; i++) {
            var cells = trs[i].querySelectorAll('th,td');
            if (!cells.length) {
                continue;
            }
            var row = [];
            for (var j = 0; j < cells.length; j++) {
                row.push(String(cells[j].textContent || '').replace(/^\s+|\s+$/g, ''));
            }
            if (!rows.length && trs[i].querySelector('th')) {
                header = true;
            }
            rows.push(row);
        }
        if (!rows.length) {
            return null;
        }
        var figure = document.createElement('figure');
        figure.setAttribute('data-trix-attachment', JSON.stringify({
            contentType: SRE.MIME.TABLE,
            content: SRE.util.encodePayload({ type: 'table', header: header, rows: rows }),
            filename: '表格 ' + rows.length + ' × ' + maxColumns(rows)
        }));
        figure.setAttribute('data-trix-content-type', SRE.MIME.TABLE);
        figure.className = 'attachment attachment--preview sre-attachment';
        figure.innerHTML = '<span class="sre-attachment__badge">表格 ' + rows.length + ' × ' + maxColumns(rows) + '</span>';
        return figure;
    }

    // toDocumentHTML：附件 payload → 真表格 HTML（提交时写回隐藏 input）。
    function toDocumentHTML(payload) {
        if (!payload || payload.type !== 'table') {
            return null;
        }
        var data = normalize(payload);
        if (!data.rows.length) {
            return null;
        }
        var cols = maxColumns(data.rows);
        var out = ['<table>'];
        var start = 0;
        if (data.header) {
            out.push('<thead><tr>');
            for (var c = 0; c < cols; c++) {
                out.push('<th>' + SRE.util.escapeHTML(data.rows[0][c] || '') + '</th>');
            }
            out.push('</tr></thead>');
            start = 1;
        }
        out.push('<tbody>');
        for (var i = start; i < data.rows.length; i++) {
            out.push('<tr>');
            for (var j = 0; j < cols; j++) {
                out.push('<td>' + SRE.util.escapeHTML(data.rows[i][j] || '') + '</td>');
            }
            out.push('</tr>');
        }
        out.push('</tbody></table>');
        return out.join('');
    }

    SRE.table = {
        mime: SRE.MIME.TABLE,
        insert: insert,
        edit: edit,
        hydrate: hydrate,
        toDocumentHTML: toDocumentHTML
    };
})();
