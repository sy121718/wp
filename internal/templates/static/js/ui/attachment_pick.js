/* ui/attachment_pick.js — 凭证附件字段（多选媒体库 → 同名多值隐藏域里的 media id）。

 * 为什么需要它：调拨页的「申请凭证 / 出货凭证 / 签收凭证」要的是**媒体 id**
 * （inventory_transfer_attachments.attachment_id 是 bigint），而既有的
 * ui/media-gallery.js 写回的是**地址文本**（图集字段的形状）。两者不能混用：
 * 把地址塞进 attachmentIds 会让服务端绑成 0，落库后指向一条不存在的附件 ——
 * 而出货那一刻才发现（服务端 resolveAttachmentIDs 会拒，但那时人已经在仓库门口）。
 *
 * 真正的问题在模板层：调拨页原来写的是 <div data-media-picker ...>，
 * 而**没有任何脚本认领 data-media-picker**（媒体字段的契约是 data-media-field，
 * 图集是 data-media-gallery）—— 于是三个抽屉里的凭证区是空 div，
 * 「至少上传一张凭证」这条硬规则在前端根本没有入口。
 *
 * 唯一真值来源是隐藏域（每选中一个附件一个 <input name="attachmentIds">，
 * 同名多值 → Go 侧 []int64 直接绑上）。缩略图只是这些 id 的视图，
 * 每次变更都从视图写回隐藏域，页面的表单收集逻辑一行都不用改。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});

    function cellsOf(root) {
        var grid = root.querySelector('[data-attach-grid]');
        return grid ? Array.prototype.slice.call(grid.querySelectorAll('[data-attach-cell]')) : [];
    }

    // syncInputs 按缩略图重写隐藏域（顺序 = 选择顺序）。
    function syncInputs(root) {
        var box = root.querySelector('[data-attach-values]');
        if (!box) { return; }
        var name = box.getAttribute('data-attach-name') || 'attachmentIds';
        box.innerHTML = '';
        cellsOf(root).forEach(function (cell) {
            var id = cell.getAttribute('data-id');
            if (!id) { return; }
            var input = document.createElement('input');
            input.type = 'hidden';
            input.name = name;
            input.value = id;
            box.appendChild(input);
        });
    }

    function syncEmpty(root) {
        var empty = root.querySelector('[data-attach-empty]');
        if (empty) { empty.hidden = cellsOf(root).length > 0; }
    }

    function addCell(root, id, url, label, size) {
        if (cellsOf(root).some(function (c) { return c.getAttribute('data-id') === id; })) { return; }
        var grid = root.querySelector('[data-attach-grid]');
        if (!grid) { return; }
        var cell = document.createElement('figure');
        cell.className = 'attach-cell';
        cell.setAttribute('data-attach-cell', '');
        cell.setAttribute('data-id', id);

        var link = document.createElement('a');
        link.href = url || '#';
        link.target = '_blank';
        link.rel = 'noopener';
        var isImage = /\.(png|jpe?g|webp|gif|avif|bmp)$/i.test(url || '');
        if (isImage) {
            var img = document.createElement('img');
            img.alt = label || '';
            img.loading = 'lazy';
            img.src = url;
            link.appendChild(img);
        } else {
            // 非图片（PDF 交货单 / 拍照压缩包）显示文件名而不是一个碎图图标。
            var tip = document.createElement('span');
            tip.className = 'attach-file';
            tip.textContent = label || url || id;
            link.appendChild(tip);
        }
        cell.appendChild(link);

        var meta = document.createElement('figcaption');
        meta.className = 'attach-meta';
        meta.textContent = isImage ? (label || '') : '';
        if (size) {
            meta.textContent = (meta.textContent ? meta.textContent + ' · ' : '') + size;
        }
        cell.appendChild(meta);

        var del = document.createElement('button');
        del.type = 'button';
        del.className = 'btn btn-sm btn-ghost attach-del';
        del.setAttribute('data-attach-remove', '');
        del.setAttribute('aria-label', label ? '移除 ' + label : '移除');
        del.textContent = '×';
        cell.appendChild(del);
        grid.appendChild(cell);
    }

    function sizeText(bytes) {
        if (!bytes || bytes <= 0) { return ''; }
        if (bytes < 1024) { return bytes + ' B'; }
        if (bytes < 1024 * 1024) { return Math.round(bytes / 1024) + ' KB'; }
        return (bytes / 1024 / 1024).toFixed(1) + ' MB';
    }

    WBUI.register(function (scope) {
        WBUI.$$('[data-attach-pick]', scope).forEach(function (root) {
            if (!WBUI.markOnce(root, 'AttachPick')) { return; }
            var add = root.querySelector('[data-attach-add]');
            if (add) {
                add.addEventListener('click', function () {
                    if (typeof WBUI.openMediaPickerMulti !== 'function') { return; }
                    // fileType 'all'：交货单常是 PDF、拍照可能是 HEIC —— 只筛图片会把真实凭证挡在外面。
                    WBUI.openMediaPickerMulti(function (urls, items) {
                        var picked = items && items.length ? items : (urls || []).map(function (u) { return { url: u }; });
                        picked.forEach(function (it) {
                            if (!it || !it.id) { return; }
                            addCell(root, String(it.id), it.url, it.file_name || it.name || '', sizeText(it.file_size));
                        });
                        syncInputs(root);
                        syncEmpty(root);
                    }, { fileType: 'all' });
                });
            }
            root.addEventListener('click', function (e) {
                var del = e.target && e.target.closest ? e.target.closest('[data-attach-remove]') : null;
                if (!del) { return; }
                var cell = del.closest('[data-attach-cell]');
                if (cell) { cell.remove(); }
                syncInputs(root);
                syncEmpty(root);
            });
            syncEmpty(root);
        });
    });
})(window);
