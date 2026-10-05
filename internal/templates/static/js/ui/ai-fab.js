/* ui/ai-fab.js — 全局 AI 悬浮球：开合、焦点、页面上下文。
 *
 * 这个控件只做**三件事**：
 *   1. 开合面板（含 aria-expanded / focus 归还 —— 关掉面板后焦点必须回到那个球，
 *      否则键盘用户会掉到页面开头）；
 *   2. 提交前把当前页面上下文塞进 htmx 请求（htmx:configRequest）；
 *   3. Enter 发送、Shift+Enter 换行。
 *
 * **不在这里拼任何文案**：面板结构由服务端模板给出（admin/partials/ai_fab.html），
 * 回答片段由服务端返回（admin/partials/ai_fab_result.html）。在 JS 里拼第二份
 * 结构迟早与模板分叉，而分叉的表现是「某一种状态下面板长得不一样」。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});
    WBUI.controls = WBUI.controls || [];

    // contextFields 提交时随请求带上的页面上下文（服务端 composeFabInput 读这三个）。
    //
    // path + query + 标题，仅此三项：它们在地址栏与窗口标题里本来就可见，
    // 注入它们不扩大任何信息面。DOM 内容与页面数据一律不带上 ——
    // 那些可能包含别人的数据（列表页每一行都是），而「AI 顺手读走了整页」
    // 是一次无从察觉的信息外泄。
    function contextFields() {
        return {
            ctxPath: global.location.pathname,
            ctxQuery: global.location.search,
            ctxTitle: (global.document.title || '').split('—')[0].trim()
        };
    }

    function panelOf(root) { return root.querySelector('[data-ai-fab-panel]'); }
    function buttonOf(root) { return root.querySelector('[data-ai-fab-toggle]'); }
    function inputOf(root) { return root.querySelector('[data-ai-fab-input]'); }

    function open(root) {
        var panel = panelOf(root);
        var btn = buttonOf(root);
        if (!panel) { return; }
        panel.hidden = false;
        if (btn) { btn.setAttribute('aria-expanded', 'true'); }
        var input = inputOf(root);
        if (input) { input.focus(); }
    }

    function close(root) {
        var panel = panelOf(root);
        var btn = buttonOf(root);
        if (!panel) { return; }
        panel.hidden = true;
        if (btn) {
            btn.setAttribute('aria-expanded', 'false');
            // 焦点归还到球上：不还的话关掉面板后键盘用户会从页首重新 Tab 一遍。
            btn.focus();
        }
    }

    function submit(input) {
        var form = input && input.form;
        if (form && global.htmx) { global.htmx.trigger(form, 'submit'); }
    }

    WBUI.register(function (scope) {
        var roots = (scope || global.document).querySelectorAll('[data-ai-fab]');
        Array.prototype.forEach.call(roots, function (root) {
            if (root.getAttribute('data-ai-fab-ready') === '1') { return; }
            root.setAttribute('data-ai-fab-ready', '1');

            var btn = buttonOf(root);
            if (btn) {
                btn.addEventListener('click', function () {
                    var panel = panelOf(root);
                    if (panel && panel.hidden) { open(root); } else { close(root); }
                });
            }
            var closer = root.querySelector('[data-ai-fab-close]');
            if (closer) {
                closer.addEventListener('click', function () { close(root); });
            }

            // 提交前注入页面上下文。挂在表单上而不是 document 上：
            // 悬浮球每页只有一个，而挂在 document 上会让**所有** htmx 请求
            // （包括后台各页面的表单）都多带三个字段。
            var form = root.querySelector('[data-ai-fab-form]');
            if (form) {
                form.addEventListener('htmx:configRequest', function (e) {
                    var fields = contextFields();
                    for (var k in fields) {
                        if (Object.prototype.hasOwnProperty.call(fields, k)) {
                            e.detail.parameters[k] = fields[k];
                        }
                    }
                });
            }

            var input = inputOf(root);
            if (input) {
                input.addEventListener('keydown', function (e) {
                    // Enter 发送、Shift+Enter 换行。中文输入法组字期间要放过 ——
                    // 否则用拼音打字时每按一次选词键都会把半截问题发出去。
                    if (e.key !== 'Enter' || e.shiftKey || e.isComposing || e.keyCode === 229) { return; }
                    e.preventDefault();
                    submit(input);
                });
            }

            // Esc 关面板：与后台的抽屉 / 弹窗同一套键盘约定。
            root.addEventListener('keydown', function (e) {
                if (e.key === 'Escape') { close(root); }
            });
        });
    });
})(window);
