// rich-editor/index.js — 富文本扩展入口。
//
// ## 为什么是这几个扩展点（Trix 2.1.19，vendor 在 /static/vendor/trix/）
//
// 先在 vendor 源码里确认了 2.1.19 真正暴露的扩展面，再按「元素形态」分配路径：
//   · Trix.config —— 冻结对象，含 blockAttributes / textAttributes / toolbar / lang / parser / css；
//     config.elements 在 2.1.x 已经被移除，所以「自定义 element 类」这条路在当前版本不存在；
//   · Trix.config.blockAttributes —— 块级元素（标签名 + terminal/breakOnReturn），标题与段落走这里；
//   · Trix.config.toolbar.getDefaultHTML() —— 工具条整体替换，自定义按钮走这里；
//   · Trix 的 Attachment（[data-trix-attachment]）—— 原子块，表格/折叠块/水平线走这里；
//   · trixEditor.editor（EditorController）—— 公开的 insertHTML / insertAttachment /
//     activateAttribute / deactivateAttribute / getSelectedRange / setSelectedRange。
//
// 为什么不把表格做成 blockAttributes：Trix 的 HTMLParser 对 <tr>/<td> 有专门分支
// （trix.umd.js:8644），解析期就把表格压平成 "a | b" 文本，嵌套结构进不了文档模型。
// 折叠块同理（详情见各文件顶部）。
//
// ## 装载 / 提交的双向转换
//
// 编辑器里的附件形态与服务端要存的 HTML 形态不同，转换点固定在两处：
//   · 装载（hydrate）：把隐藏 input 里的 <table>/<details>/<hr> 换成 figure 附件，
//     纯文本先转义再分段 —— 存量纯文本绝不当作 HTML 直接塞给 Trix；
//   · 提交（serialize）：form submit 的捕获阶段把 figure 附件展开回真标签写回隐藏 input，
//     服务端白名单只认识真标签。
(function () {
    'use strict';

    var SRE = (window.SkyRichEditor = window.SkyRichEditor || {});
    var BASE = '/static/js/rich-editor/';

    // 重复执行（partial 里被多次输出、抽屉每次打开）时只做一次启动，之后只补扫新编辑器。
    // 但「启动过」不等于「启动成功」：页面把 vendor 的 trix.umd.js 放在文末时，本脚本会先于
    // Trix 执行，boot 只能推迟。这里在重复执行时补一次 boot（boot 自带就绪判断与幂等），
    // 否则扩展会静默失效 —— 工具条、表格与折叠块全都不出现，控制台之外没有任何提示。
    if (SRE.__booting) {
        if (SRE.boot) {
            SRE.boot();
        }
        if (SRE.rescan) {
            SRE.rescan();
        }
        return;
    }

    // 模块按依赖顺序加载：util 最先，toolbar 依赖 block-level 的 LEVELS。
    var MODULES = ['util.js', 'block-level.js', 'dialog.js', 'horizontal-rule.js', 'table.js', 'accordion.js', 'image.js', 'attachment.js', 'toolbar.js'];

    function loadOne(name) {
        return new Promise(function (resolve) {
            var script = document.createElement('script');
            script.src = BASE + name;
            script.async = false; // 动态脚本默认 async，关掉才能保证按插入顺序执行
            script.onload = resolve;
            script.onerror = function () {
                console.error('[rich-editor] 模块加载失败：' + name);
                resolve();
            };
            document.head.appendChild(script);
        });
    }

    function loadAll() {
        return MODULES.reduce(function (chain, name) {
            return chain.then(function () {
                return loadOne(name);
            });
        }, Promise.resolve());
    }

    SRE.__booting = true;
    // 暴露 boot 供重复执行时补启动（见文件顶部 __booting 分支）。
    SRE.boot = boot;

    loadAll().then(boot).catch(function (err) {
        console.error('[rich-editor] 启动失败', err);
    });

    // ── 装载：隐藏 input 的 HTML → 编辑器能接受的 HTML ─────────────────────────
    function hydrateValue(raw) {
        // 纯文本走「转义 + 分段」，与 Go 侧 core.RichTextHTML 同口径。
        var html = SRE.util.safeEditorHTML(raw);
        if (!html || !SRE.util.hasRichMarkup(html) || !html.match(/<(table|details|hr)[\s/>]/i)) {
            return html;
        }
        var doc = new DOMParser().parseFromString('<div id="sre-hydrate-root">' + html + '</div>', 'text/html');
        var root = doc.getElementById('sre-hydrate-root');
        if (!root) {
            return html;
        }
        // 先折叠块、后表格：折叠块的 hydrate 会整体接管 details 内部的节点。
        var details = root.querySelectorAll('details');
        for (var i = 0; i < details.length; i++) {
            var acc = SRE.accordion.hydrate(details[i]);
            if (acc) {
                details[i].parentNode.replaceChild(acc, details[i]);
            }
        }
        var tables = root.querySelectorAll('table');
        for (var j = 0; j < tables.length; j++) {
            var fig = SRE.table.hydrate(tables[j]);
            if (fig) {
                tables[j].parentNode.replaceChild(fig, tables[j]);
            }
        }
        var rules = root.querySelectorAll('hr');
        for (var k = 0; k < rules.length; k++) {
            var rule = SRE.rule.hydrate(rules[k]);
            if (rule) {
                rules[k].parentNode.replaceChild(rule, rules[k]);
            }
        }
        return root.innerHTML;
    }

    // ── 提交：编辑器的 HTML → 服务端认识的 HTML ────────────────────────────────
    var ATTACHMENT_HANDLERS = {
        'application/vnd.go-wp.rich-editor.table+json': function () { return SRE.table; },
        'application/vnd.go-wp.rich-editor.accordion+json': function () { return SRE.accordion; },
        'application/vnd.go-wp.rich-editor.rule+json': function () { return SRE.rule; }
    };

    function documentHTML(editorValue) {
        var value = String(editorValue == null ? '' : editorValue);
        if (value.indexOf('data-trix-attachment') < 0) {
            return value;
        }
        var doc = new DOMParser().parseFromString('<div id="sre-doc-root">' + value + '</div>', 'text/html');
        var root = doc.getElementById('sre-doc-root');
        if (!root) {
            return value;
        }
        var figures = root.querySelectorAll('figure[data-trix-attachment]');
        for (var i = 0; i < figures.length; i++) {
            var figure = figures[i];
            var attributes;
            try {
                attributes = JSON.parse(figure.getAttribute('data-trix-attachment') || '{}');
            } catch (err) {
                continue;
            }
            var factory = ATTACHMENT_HANDLERS[attributes.contentType];
            if (!factory) {
                continue;
            }
            var payload = SRE.util.decodePayload(attributes.content);
            var html = factory().toDocumentHTML(payload);
            if (html == null) {
                continue;
            }
            var holder = doc.createElement('div');
            holder.innerHTML = html;
            var fragment = doc.createDocumentFragment();
            while (holder.firstChild) {
                fragment.appendChild(holder.firstChild);
            }
            figure.parentNode.replaceChild(fragment, figure);
        }
        return root.innerHTML;
    }

    // ── 编辑器装配 ────────────────────────────────────────────────────────────
    // hydrateElement 把隐藏 input 的值换成编辑器形态，返回「是否真的变了」。
    // 变了且编辑器已经初始化时，调用方要用 loadHTML 把新值灌回去 ——
    // Trix 只在初始化时从 input 读一次，之后是「编辑器 → input」单向写，
    // 直接改 input.value 不会反映到编辑器里。
    function hydrateElement(editorEl) {
        if (!editorEl || editorEl.__sreHydrated) {
            return false;
        }
        var input = editorEl.inputElement;
        if (!input) {
            return false;
        }
        var next = hydrateValue(input.value);
        var changed = next !== input.value;
        if (changed) {
            input.value = next;
        }
        editorEl.__sreHydrated = true;
        return changed;
    }

    function adopt(editorEl) {
        if (!editorEl || editorEl.tagName !== 'TRIX-EDITOR') {
            return;
        }
        if (editorEl.editor) {
            // index.js 晚于 Trix 初始化（编辑器已经写在页面 HTML 里）时，装载要手动补一次：
            // 改完 input 值再 loadHTML 灌回编辑器，否则表格/折叠块会以原始标签形态出现。
            if (hydrateElement(editorEl)) {
                editorEl.editor.loadHTML(editorEl.inputElement.value);
            }
            SRE.toolbar.sync(editorEl);
        }
        if (!editorEl.__sreAdopted) {
            editorEl.__sreAdopted = true;
            editorEl.addEventListener('trix-selection-change', function () {
                SRE.toolbar.sync(editorEl);
            });
        }
    }

    // 表格 / 折叠块卡片：单击打开对应的编辑对话框（新增行列就在那个对话框里）。
    function bindAttachmentEditing() {
        document.addEventListener('click', function (event) {
            var target = event.target;
            if (!target || !target.closest) {
                return;
            }
            var figure = target.closest('figure[data-trix-attachment]');
            if (!figure) {
                return;
            }
            var editorEl = figure.closest('trix-editor');
            if (!editorEl || !editorEl.editor) {
                return;
            }
            var attributes;
            try {
                attributes = JSON.parse(figure.getAttribute('data-trix-attachment') || '{}');
            } catch (err) {
                return;
            }
            var isTable = attributes.contentType === SRE.MIME.TABLE;
            var isAccordion = attributes.contentType === SRE.MIME.ACCORDION;
            if (!isTable && !isAccordion) {
                return;
            }
            var payload = SRE.util.decodePayload(attributes.content);
            // 等 Trix 把选区更新到被点击的附件上，再取范围用于替换。
            setTimeout(function () {
                var range = editorEl.editor.getSelectedRange();
                if (isTable) {
                    SRE.table.edit(editorEl.editor, range, payload);
                } else {
                    SRE.accordion.edit(editorEl.editor, range, payload);
                }
            }, 0);
        });
    }

    // 提交前把附件展开成真标签（捕获阶段，保证早于表单序列化）。
    function bindFormSubmit() {
        document.addEventListener('submit', function (event) {
            var form = event.target;
            if (!form || form.tagName !== 'FORM') {
                return;
            }
            var editors = document.querySelectorAll('trix-editor');
            for (var i = 0; i < editors.length; i++) {
                var editorEl = editors[i];
                var input = editorEl.inputElement;
                if (!input || input.form !== form) {
                    continue;
                }
                input.value = documentHTML(input.value);
            }
        }, true);
    }

    function scanTree(node) {
        if (!node || node.nodeType !== 1) {
            return;
        }
        if (node.matches && node.matches('trix-editor')) {
            adopt(node);
        }
        if (node.querySelectorAll) {
            var found = node.querySelectorAll('trix-editor');
            for (var i = 0; i < found.length; i++) {
                adopt(found[i]);
            }
        }
    }

    // rebootTries / scheduleReboot：模块比 Trix 先执行完时的自愈路径。
    // 「等下一次补启动」只对**在文末又引了一次 index.js** 的调用方成立（partial 注释里的推荐做法），
    // 而并非每个调用方都引 —— 商品编辑页就没有，于是它的富文本一直是 Trix 默认工具条。
    // 这里自己把模块重跑一遍（模块都幂等），最多 3 次，间隔递增。
    var rebootTries = 0;

    function scheduleReboot() {
        if (rebootTries >= 3) {
            return;
        }
        rebootTries++;
        setTimeout(function () {
            loadAll().then(boot);
        }, 100 * rebootTries);
    }

    function boot() {
        if (SRE.__booted) {
            return;
        }

        var Trix = window.Trix;
        if (!Trix || !Trix.config || !SRE.toolbar || !SRE.blocks) {
            // 刻意不在这里置 __booted：Trix 晚于本脚本加载是正常顺序（vendor 脚本在文末），
            // 在这里定局就再也没有重试机会。
            console.error('[rich-editor] Trix 未加载或扩展模块缺失，重跑扩展模块');
            scheduleReboot();
            return;
        }
        SRE.__booted = true;

        // 工具条整体替换：已经有工具条的元素要重建一次（它们可能在本次 boot 之前就渲染过了）。
        Trix.config.toolbar.getDefaultHTML = SRE.toolbar.html;
        var toolbars = document.querySelectorAll('trix-toolbar');
        for (var i = 0; i < toolbars.length; i++) {
            toolbars[i].innerHTML = SRE.toolbar.html();
        }

        SRE.toolbar.bindEvents();
        bindAttachmentEditing();
        bindFormSubmit();

        // 关键时机：trix-before-initialize 在 TrixTemplate 读取 input.value 之前同步派发
        // （vendor 源码 trix.umd.js:13886 -> 13889），在这里改 value 才是「装载」。
        document.addEventListener('trix-before-initialize', function (event) {
            var editorEl = event.target;
            if (!editorEl || editorEl.tagName !== 'TRIX-EDITOR') {
                return;
            }
            hydrateElement(editorEl);
        });
        document.addEventListener('trix-initialize', function (event) {
            adopt(event.target);
        });

        var observer = new MutationObserver(function (records) {
            for (var j = 0; j < records.length; j++) {
                var added = records[j].addedNodes;
                for (var k = 0; k < added.length; k++) {
                    scanTree(added[k]);
                }
            }
        });
        observer.observe(document.documentElement, { childList: true, subtree: true });

        SRE.rescan = function () {
            var editors = document.querySelectorAll('trix-editor');
            for (var n = 0; n < editors.length; n++) {
                adopt(editors[n]);
            }
            var bars = document.querySelectorAll('trix-toolbar');
            for (var m = 0; m < bars.length; m++) {
                if (!bars[m].querySelector('.sre-toolbar-row')) {
                    bars[m].innerHTML = SRE.toolbar.html();
                }
            }
        };

        SRE.rescan();
    }

    // 加载完成但 boot 还没跑（脚本被重复注入）时，rescan 是空实现，先兜住。
    SRE.rescan = SRE.rescan || function () {};
})();
