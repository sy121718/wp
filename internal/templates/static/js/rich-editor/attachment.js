/* rich-editor/attachment.js — 富文本插入附件（走全站媒体选择面板）。

 * 为什么重做：原来这个按钮走的是 Trix 原生的 attachFiles 动作 —— 它把文件 POST 到
 * Trix 自己的附件端点，而本项目没有那个端点（附件的真源是媒体库），所以点了没有任何反应。
 * 工作台的富文本早就有自己的上传接线（controls/text.js 的 RICH_UPLOAD_ENDPOINT），
 * 后台这条一直没有 —— 同一个「插入附件」在两个编辑器里是两种结局。

 * 形态：正文里落一个 <a href="文件地址">文件名</a>。
 * 正文白名单认识 a[href]（tel/sms/ftp/相对地址都保留），所以链接形态存得下、也导得出，
 * 而图片那种 <img> 不适合附件（PDF 显示不出来，浏览器会把它当坏图）。

 * 上传与选择**只走媒体库**：本文件不持有文件输入，入口是 WBUI.openMediaPicker（fileType='all'）。
 * 非图片文件要先在媒体库里传好 —— 与图片那条路径同一条约定。
 */
(function () {
    'use strict';

    var SRE = (window.SkyRichEditor = window.SkyRichEditor || {});
    if (SRE.attachment) {
        return;
    }

    function escapeAttr(v) {
        return String(v == null ? '' : v).replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;');
    }

    function escapeText(v) {
        return String(v == null ? '' : v).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
    }

    SRE.attachment = {
        insert: function (editor) {
            if (!editor) {
                return;
            }
            var WBUI = window.WBUI;
            if (!WBUI || typeof WBUI.openMediaPicker !== 'function') {
                return; // 控件基座没加载：装配问题，不弹提示
            }
            WBUI.openMediaPicker(function (url, item) {
                if (!url) {
                    return;
                }
                // 链接文字用文件名（拿不到就退回地址）：导出的正文里只看得到文字，
                // 一串 storage 路径对读者没有任何意义。
                var label = (item && item.file_name) ? item.file_name : url;
                editor.insertHTML('<a href="' + escapeAttr(url) + '">' + escapeText(label) + '</a>&nbsp;');
            }, { fileType: 'all' });
        }
    };
})();
