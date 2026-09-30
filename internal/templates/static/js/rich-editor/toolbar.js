// rich-editor/toolbar.js — 自定义工具条（Trix 的 Trix.config.toolbar 扩展点）。
//
// 扩展路径：Trix.config.toolbar.getDefaultHTML()（vendor 源码 trix.umd.js:663、13398 ——
// TrixToolbarElement 在挂载时把这段 HTML 填进工具条元素）。我们整体替换它，
// 而不是往默认 HTML 后面追加：默认按钮是图标 + 英文 title，且 tabindex="-1"（只走
// Trix 自己的 roving focus），既不好读也不满足「Tab 可达」的要求。
//
// 按钮分三类：
//   1. data-trix-attribute / data-trix-action —— Trix 原生属性与动作（加粗、列表、引用、
//      代码块、链接、附件、撤销重做），点击与激活态由 Trix 自己处理，不重复实现；
//   2. data-sre-block —— 标题 h1~h5 与段落。Trix 的块属性是一组集合，多个同时激活时
//      取哪个标签名不确定，所以这里不由 Trix 直接 toggle，而是走 block-level.js 的
//      switchBlock（先清空这一组再激活目标），按钮的 aria-pressed 由 selection-change 同步；
//   3. data-sre-insert —— 水平线 / 表格 / 手风琴，点击后走各自的插入流程（见同名文件）。
//
// 键盘可达：所有按钮都是原生 <button type="button">，不设 tabindex="-1"（因此 Tab 能到、
// Enter/Space 原生触发 click）；方向键在同一按钮组内移动焦点、Home/End 跳首尾，
// 由本文件在工具条上委托实现（不依赖 Trix 内部的方向键逻辑，它只认它自己的两类属性）。
(function () {
    'use strict';

    var SRE = (window.SkyRichEditor = window.SkyRichEditor || {});
    if (SRE.toolbar) {
        return;
    }

    function attrButton(attribute, label, title) {
        return '<button type="button" class="trix-button sre-toolbar-btn" data-trix-attribute="' + attribute +
            '" title="' + title + '">' + label + '</button>';
    }

    function actionButton(action, label, title) {
        return '<button type="button" class="trix-button sre-toolbar-btn" data-trix-action="' + action +
            '" title="' + title + '">' + label + '</button>';
    }

    // toolbarHTML 生成整段工具条 HTML。
    function toolbarHTML() {
        var html = [];
        html.push('<div class="trix-button-row sre-toolbar-row">');

        // 行内格式
        html.push('<span class="trix-button-group" data-trix-button-group="text-tools">');
        html.push(attrButton('bold', 'B', '加粗'));
        html.push(attrButton('italic', 'I', '斜体'));
        html.push(attrButton('strike', 'S', '删除线'));
        html.push(attrButton('href', '链接', '插入或编辑链接'));
        html.push('</span>');

        // 块级格式：标题 1~5 与段落（互斥组，见 block-level.js）
        html.push('<span class="trix-button-group" data-trix-button-group="block-tools">');
        for (var i = 0; i < SRE.blocks.LEVELS.length; i++) {
            var level = SRE.blocks.LEVELS[i];
            html.push('<button type="button" class="trix-button sre-toolbar-btn" data-sre-block="' + level.name +
                '" title="' + level.label + '" aria-pressed="false">H' + (i + 1) + '</button>');
        }
        html.push('<button type="button" class="trix-button sre-toolbar-btn" data-sre-block="paragraph"' +
            ' title="普通段落" aria-pressed="true">段落</button>');
        html.push('</span>');

        // 列表 / 引用 / 代码块（Trix 原生属性）
        html.push('<span class="trix-button-group" data-trix-button-group="block-tools-extra">');
        html.push(attrButton('bullet', '• 列表', '无序列表'));
        html.push(attrButton('number', '1. 列表', '有序列表'));
        html.push(attrButton('quote', '引用', '引用块'));
        html.push(attrButton('code', '</> 代码', '代码块'));
        html.push('</span>');

        // 扩展块：水平线 / 表格 / 手风琴 / 附件
        html.push('<span class="trix-button-group" data-trix-button-group="ext-tools">');
        html.push('<button type="button" class="trix-button sre-toolbar-btn" data-sre-insert="rule" title="插入水平线">水平线</button>');
        html.push('<button type="button" class="trix-button sre-toolbar-btn" data-sre-insert="table" title="插入表格">表格</button>');
        html.push('<button type="button" class="trix-button sre-toolbar-btn" data-sre-insert="accordion" title="插入折叠块">折叠块</button>');
        // 图片走媒体库选择器（rich-editor/image.js），不是 Trix 原生的附件上传：
        // 原生那条走 Trix 自己的 /attachments 端点，本项目没有它。
        html.push('<button type="button" class="trix-button sre-toolbar-btn" data-sre-insert="image" title="插入图片">图片</button>');
        // 附件也走统一分派（原来用 actionButton('attachFiles') 走 Trix 原生上传，
        // 而本项目没有那个端点 —— 点了没反应）。行为见 rich-editor/attachment.js。
        html.push('<button type="button" class="trix-button sre-toolbar-btn" data-sre-insert="attachment" title="插入附件">附件</button>');
        html.push('</span>');

        // 撤销 / 重做
        html.push('<span class="trix-button-group" data-trix-button-group="history-tools">');
        html.push(actionButton('undo', '撤销', '撤销'));
        html.push(actionButton('redo', '重做', '重做'));
        html.push('</span>');

        html.push('</div>');
        return html.join('');
    }

    // editorOfToolbar 由工具条元素反查它服务的编辑器（Trix 的 trix-toolbar 用 toolbar 属性关联）。
    function editorOfToolbar(toolbarEl) {
        var editor = toolbarEl.closest('trix-editor');
        if (editor) {
            return editor;
        }
        var editors = document.querySelectorAll('trix-editor');
        for (var i = 0; i < editors.length; i++) {
            if (editors[i].toolbarElement === toolbarEl) {
                return editors[i];
            }
        }
        return null;
    }

    // syncToolbar 按当前选区更新块按钮的 aria-pressed（标题与段落互斥，同一时刻只有一个为真）。
    function syncToolbar(editorEl) {
        if (!editorEl || !editorEl.editor) {
            return;
        }
        var toolbarEl = editorEl.toolbarElement;
        if (!toolbarEl) {
            return;
        }
        var current = SRE.blocks.currentBlock(editorEl.editor);
        var buttons = toolbarEl.querySelectorAll('[data-sre-block]');
        for (var i = 0; i < buttons.length; i++) {
            var on = buttons[i].getAttribute('data-sre-block') === current;
            buttons[i].setAttribute('aria-pressed', on ? 'true' : 'false');
            buttons[i].classList.toggle('sre-toolbar-btn--active', on);
        }
    }

    // bindEvents 在 document 上做一次委托：工具条的点击、方向键与 mousedown 全部在这里处理。
    function bindEvents() {
        if (SRE.__toolbarBound) {
            return;
        }
        SRE.__toolbarBound = true;

        // mousedown 阻止默认：点工具条不应该让编辑器失焦（失焦会丢选区）。
        document.addEventListener('mousedown', function (event) {
            if (event.target.closest && event.target.closest('trix-toolbar')) {
                event.preventDefault();
            }
        }, true);

        document.addEventListener('click', function (event) {
            var target = event.target;
            if (!target || !target.closest) {
                return;
            }
            var toolbarEl = target.closest('trix-toolbar');
            if (!toolbarEl) {
                return;
            }
            var editorEl = editorOfToolbar(toolbarEl);
            if (!editorEl || !editorEl.editor) {
                return;
            }

            var blockBtn = target.closest('[data-sre-block]');
            if (blockBtn) {
                event.preventDefault();
                editorEl.focus();
                SRE.blocks.switchBlock(editorEl.editor, blockBtn.getAttribute('data-sre-block'));
                syncToolbar(editorEl);
                return;
            }

            var insertBtn = target.closest('[data-sre-insert]');
            if (insertBtn) {
                event.preventDefault();
                editorEl.focus();
                var kind = insertBtn.getAttribute('data-sre-insert');
                if (kind === 'image') {
                    SRE.image.insert(editorEl.editor);
                } else if (kind === 'attachment') {
                    SRE.attachment.insert(editorEl.editor);
                } else if (kind === 'rule') {
                    SRE.rule.insert(editorEl.editor);
                } else if (kind === 'table') {
                    SRE.table.insert(editorEl.editor);
                } else if (kind === 'accordion') {
                    SRE.accordion.insert(editorEl.editor);
                }
            }
        });

        // 方向键在按钮组内移动焦点，Home/End 跳首尾。
        document.addEventListener('keydown', function (event) {
            var target = event.target;
            if (!target || !target.closest) {
                return;
            }
            var toolbarEl = target.closest('trix-toolbar');
            if (!toolbarEl || !target.matches || !target.matches('button')) {
                return;
            }
            var group = target.closest('.trix-button-group') || toolbarEl;
            var buttons = Array.prototype.slice.call(group.querySelectorAll('button'));
            var index = buttons.indexOf(target);
            if (index < 0) {
                return;
            }
            var next = -1;
            if (event.key === 'ArrowRight' || event.key === 'ArrowDown') {
                next = (index + 1) % buttons.length;
            } else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') {
                next = (index - 1 + buttons.length) % buttons.length;
            } else if (event.key === 'Home') {
                next = 0;
            } else if (event.key === 'End') {
                next = buttons.length - 1;
            }
            if (next >= 0) {
                event.preventDefault();
                buttons[next].focus();
            }
        });
    }

    SRE.toolbar = {
        html: toolbarHTML,
        bindEvents: bindEvents,
        sync: syncToolbar
    };
})();
