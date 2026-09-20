/*
 * rich-editor/live-preview.js — 文章新建/编辑页的实时预览（纯客户端，零服务端往返）。
 *
 * 约定（两页共用）：
 *   · 页面里有 <iframe id="article-live-preview">（右侧「实时预览」卡）；
 *   · 页面里有一个或多个 trix-editor；
 *   · 脚本在 Trix 本体与扩展入口之后加载（文末 <script src>，非 defer 时序最稳）。
 *
 * 行为：监听每个编辑器的 trix-change，把编辑器 HTML 包进一份「最小站点观感」
 * 的独立文档写进 iframe 的 srcdoc —— iframe 是独立文档，后台的深色主题、
 * 布局样式不会渗进预览；输入做 ~200ms 防抖，避免每个按键都整帧重建。
 * 打开页面时先同步一次（编辑页要展示已保存正文，新建页给空壳）。
 *
 * iframe 带 sandbox（不含 allow-scripts）：预览只展示，正文里的脚本不运行。
 */
(function () {
  "use strict";

  var DEBOUNCE_MS = 200;

  /* 预览文档骨架：白底 + 正文字号/行高 + h1~h5/p/ul/ol/img/blockquote/pre/code/table 的基础排版。 */
  var BASE_HEAD =
    '<!doctype html><html><head><meta charset="utf-8"><style>' +
    'body{background:#fff;color:#1f2328;margin:0;padding:24px;' +
    'font:16px/1.75 -apple-system,"Segoe UI",Roboto,"Helvetica Neue","PingFang SC","Microsoft YaHei",sans-serif;}' +
    'h1,h2,h3,h4,h5{line-height:1.3;margin:1.2em 0 .5em;}' +
    'p{margin:.6em 0;}ul,ol{margin:.6em 0;padding-left:1.6em;}' +
    'img{max-width:100%;height:auto;}' +
    'blockquote{margin:1em 0;padding:.4em 1em;border-left:4px solid #d0d7de;color:#57606a;}' +
    'pre{background:#f6f8fa;padding:12px;border-radius:6px;overflow:auto;}' +
    'code{font-family:ui-monospace,SFMono-Regular,Consolas,monospace;}' +
    'table{border-collapse:collapse;width:100%;}td,th{border:1px solid #d0d7de;padding:6px 10px;}' +
    '</style></head><body>';
  var BASE_TAIL = "</body></html>";

  function editorHTML(el) {
    // Trix 就绪后 editor 可用，getDocument().toString() 是权威序列化；
    // 尚未就绪时回退隐藏 input 的 value（rich_editor.html 的装载流程会先填好）。
    if (el.editor && el.editor.getDocument) {
      try {
        return el.editor.getDocument().toString();
      } catch (e) { /* 落回 input */ }
    }
    var input = document.getElementById(el.getAttribute("input"));
    return input ? input.value : "";
  }

  function render(frame, editors) {
    var html = "";
    for (var i = 0; i < editors.length; i++) {
      html += editorHTML(editors[i]);
    }
    frame.srcdoc = BASE_HEAD + html + BASE_TAIL;
  }

  function init() {
    var frame = document.getElementById("article-live-preview");
    if (!frame) return;
    var editors = Array.prototype.slice.call(document.querySelectorAll("trix-editor"));
    if (!editors.length) return;

    var timer = null;
    function schedule() {
      if (timer) return;
      timer = setTimeout(function () {
        timer = null;
        render(frame, editors);
      }, DEBOUNCE_MS);
    }

    editors.forEach(function (el) {
      el.addEventListener("trix-change", schedule);
    });

    // 首帧同步：编辑页把已保存正文直接画出来；新建页给一个空白文档。
    render(frame, editors);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
})();
