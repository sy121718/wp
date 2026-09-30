/* rich-editor/image.js — 富文本插图（走全站媒体库选择器）。
 *
 * 为什么不是「把 Trix 原生的图片按钮加回来」：工具栏是被 toolbar.js 整体替换的，而 Trix 的
 * 原生图片/附件能力走的是它自己的上传端点（`/attachments`）—— 本项目没有那个端点，
 * 所以附件按钮点了也不会有结果。图片要落库到站点的媒体体系（/storage + /api/media），
 * 正确的接法是**复用媒体库选择器**：选好已有图（或先上传）→ 把地址插进正文。
 *
 * 选择器只有一份 UI（ui/mediafield.js 的 openMediaPicker）：媒体字段控件与这里共用。
 * 两边各写一套的代价是立刻分叉 —— 一边能搜能翻页、另一边只能粘贴地址。
 *
 * 形态：插进去的是**真 <img> 标签**（不是 figure 附件）。服务端的正文白名单认识 img，
 * 而 table / details / hr 那几类要走附件形态是它们自己的渲染需要，与图片无关。
 */
(function () {
    'use strict';

    var SRE = (window.SkyRichEditor = window.SkyRichEditor || {});
    if (SRE.image) {
        return;
    }

    SRE.image = {
        // insert(editor)：打开媒体选择器，选中的图插到光标处。
        // editor 是 Trix 的 editor 实例（toolbar.js 传的是 editorEl.editor）。
        insert: function (editor) {
            if (!editor) {
                return;
            }
            var WBUI = window.WBUI;
            if (!WBUI || typeof WBUI.openMediaPicker !== 'function') {
                // 控件基座没加载（partials/ui_scripts.html）时静默返回：
                // 这里弹提示的收益低于"点了没反应"的可疑度，缺基座是装配问题、不是用户操作问题。
                return;
            }
            WBUI.openMediaPicker(function (url) {
                if (!url) {
                    return; // 用户取消
                }
                editor.insertHTML('<img src="' + url + '" alt="">');
            });
        }
    };

    // Trix 把 <img> 当附件：没有 src、也没有文件的附件会渲染成一块空占位，保存下来就是坏 HTML。
    // 这里兜底移除它 —— 正常插入（带 src）不受影响，也不会误伤拖入文件的那条路径（file 非空）。
    document.addEventListener('trix-attachment-add', function (event) {
        var att = event.attachment;
        if (!att || att.file) {
            return;
        }
        var url = att.getAttribute && att.getAttribute('url');
        if (!url) {
            att.remove();
        }
    });
})();
