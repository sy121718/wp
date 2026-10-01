// rich-editor/block-level.js — 块级元素：标题 h1~h5 与普通段落。
//
// 走的扩展路径：**Trix 的一等扩展点 Trix.config.blockAttributes**
// （vendor 源码 trix.umd.js:131 的 attributes 对象，经 config 导出为 blockAttributes）。
// 为什么是这条：块级格式在 Trix 里本来就是这块配置承载的 —— 每个块属性给一个 tagName，
// Trix 的 HTML 序列化（text/html）就是「DocumentView.render(document).innerHTML」，
// 块元素的标签名直接取自这里，不需要碰 Trix 内部类。Trix 原生只定义了 heading1，
// 我们在原配置上补齐 heading2~heading5 与 paragraph。
//
// 与 h1 有关的既有约定：本项目原先的服务端白名单把 h1 降级为 h2（一页一个 H1 由页面标题承担）。
// 本轮产品要求明确「编辑器支持 h1~h5 且白名单放行」，故 core.SanitizeRichHTML 同步改为
// 保留 h1（见 internal/builder/core/richtext.go 的注释）。
(function () {
    'use strict';

    var SRE = (window.SkyRichEditor = window.SkyRichEditor || {});

    // 就绪判断必须在安装标记**之前**：模块由 index.js 动态加载，可能跑在 vendor 的
    // trix.umd.js 之前（partial 嵌在文档中间，vendor 脚本在文末）。先打标记再 return，
    // 后续任何一次重跑都会在标记处早退 —— 扩展永久不生效，商品编辑页的工具条因此
    // 一直停留在 Trix 默认那套（2026-10-01 实测：SRE.blocks 缺失 → boot 永远失败）。
    var Trix = window.Trix;
    if (!Trix || !Trix.config || !Trix.config.blockAttributes) {
        return;
    }
    if (SRE.blockLevelInstalled) {
        return;
    }
    SRE.blockLevelInstalled = true;

    // 互斥的块级属性：Trix 的块属性是「属性集合」，多个同时激活时标签名由内部取值顺序决定，
    // 不保证是我们点的那个。所以切换一律走 switchBlock()：先把这一组全部取消，再激活目标。
    var BLOCK_ATTRS = ['heading1', 'heading2', 'heading3', 'heading4', 'heading5', 'paragraph'];

    // 标题级别 → 标签名。h1~h5 全部保留（不再降级）。
    var LEVELS = [
        { name: 'heading1', tag: 'h1', label: '标题 1' },
        { name: 'heading2', tag: 'h2', label: '标题 2' },
        { name: 'heading3', tag: 'h3', label: '标题 3' },
        { name: 'heading4', tag: 'h4', label: '标题 4' },
        { name: 'heading5', tag: 'h5', label: '标题 5' }
    ];

    // terminal + breakOnReturn：标题是「回车即离开」的块，与 Trix 原生 heading1 的配置一致。
    // 不设 group：Trix 2.1.19 内部没有用 group 做互斥（全仓只有 group===false 的一处判断），
    // 互斥由本文件的 switchBlock 显式完成。
    for (var i = 0; i < LEVELS.length; i++) {
        Trix.config.blockAttributes[LEVELS[i].name] = {
            tagName: LEVELS[i].tag,
            terminal: true,
            breakOnReturn: true
        };
    }

    // 段落：Trix 原生没有可激活的「段落」块属性（默认块 default 走的是 div）。
    // 这里补一个 tagName=p 的块属性，并把默认块的 tagName 也改成 p ——
    // 编辑器产出的段落直接就是 <p>，与白名单、与存量纯文本段落化的 <p> 形状一致。
    Trix.config.blockAttributes.paragraph = {
        tagName: 'p',
        terminal: true,
        breakOnReturn: true
    };
    Trix.config.blockAttributes['default'].tagName = 'p';

    // switchBlock 把某个块级属性设为当前块的唯一属性。
    // name 传 'paragraph' 表示「取消所有标题，回到段落」。
    function switchBlock(editorController, name) {
        if (!editorController) {
            return;
        }
        for (var i = 0; i < BLOCK_ATTRS.length; i++) {
            editorController.deactivateAttribute(BLOCK_ATTRS[i]);
        }
        // 段落是「没有块属性」的状态，default 块的 tagName 已经是 p，不需要再激活。
        if (name && name !== 'paragraph') {
            editorController.activateAttribute(name);
        }
    }

    // currentBlock 返回当前选区所在的块级属性名（没有则返回 'paragraph'）。
    function currentBlock(editorController) {
        if (!editorController) {
            return 'paragraph';
        }
        for (var i = 0; i < LEVELS.length; i++) {
            if (editorController.attributeIsActive(LEVELS[i].name)) {
                return LEVELS[i].name;
            }
        }
        return 'paragraph';
    }

    SRE.blocks = {
        LEVELS: LEVELS,
        ATTRS: BLOCK_ATTRS,
        switchBlock: switchBlock,
        currentBlock: currentBlock
    };
})();
