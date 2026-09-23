/* ui/mediafield.js — 原始控件：媒体字段（缩略图预览 + 媒体库选择 + 外链粘贴）。
 *
 * 用法（admin/partials/media_field.html 产出这套标记）：
 *   <div data-media-field data-media-pick-label="媒体库" data-media-clear-label="清除">
 *     <div class="media-field-preview" data-media-preview>
 *       <img alt="" data-media-img hidden>
 *       <span class="media-field-tip" data-media-tip></span>
 *     </div>
 *     <div class="media-field-row">
 *       <input class="form-input" type="text" data-media-input name="...">
 *       <button type="button" data-media-pick></button>
 *       <button type="button" data-media-clear></button>
 *     </div>
 *   </div>
 *
 * 四条约定：
 *   · **input 是唯一真值来源**：媒体库选中只做「写回 input + 派发 change」，
 *     不另存一份状态 —— 页面既有的表单收集逻辑（按 name 取值、监听 change）
 *     一行都不用改；
 *   · 预览只认「能直接当 src 的值」：以 / http(s): // data: 开头的当地址，其余一律
 *     显示未选择 —— 猜错会把「路径写错」渲染成一张 404 图，比空着更难排查；
 *   · 弹窗**按需创建一次**并挂在 body 上，htmx 局部替换后不重复建
 *     （后台大量片段刷新，重复 append 会一层层叠出多个遮罩）；
 *   · MediaLib 不是依赖：本控件直接打 /api/media/list（只读 GET，不需要 CSRF），
 *     免得为了一个选图框把 media-lib.js 的加载顺序绑进公共入口。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});
    global.WBUI = WBUI;
    WBUI.controls = WBUI.controls || [];

    // srcOf 值 → 预览地址；判不出来返回空串（不猜）。
    function srcOf(value) {
        var v = String(value == null ? '' : value).trim();
        if (!v) { return ''; }
        if (/^https?:\/\//i.test(v) || v.charAt(0) === '/' || /^data:/i.test(v)) { return v; }
        return '';
    }

    // renderPreview 按当前 input 值刷新缩略图；空值时显示占位文案。
    function renderPreview(root) {
        var input = root.querySelector('[data-media-input]');
        var img = root.querySelector('[data-media-img]');
        var tip = root.querySelector('[data-media-tip]');
        if (!input || !img || !tip) { return; }
        var src = srcOf(input.value);
        if (src) {
            img.src = src;
            img.hidden = false;
            tip.hidden = true;
            root.classList.add('has-image');
        } else {
            img.removeAttribute('src');
            img.hidden = true;
            tip.hidden = false;
            root.classList.remove('has-image');
        }
    }

    // applyValue 写回 input 并广播 change —— 唯一真值仍在那只输入框里。
    function applyValue(root, value) {
        var input = root.querySelector('[data-media-input]');
        if (!input) { return; }
        input.value = value;
        renderPreview(root);
        input.dispatchEvent(new Event('input', { bubbles: true }));
        input.dispatchEvent(new Event('change', { bubbles: true }));
    }

    // ---------- 媒体库弹窗（全局单例） ----------

    var modal = null;
    var pickHandler = null;
    var state = { page: 1, search: '', total: 0, limit: 48 };

    function buildModal() {
        var wrap = document.createElement('div');
        wrap.className = 'media-pick-mask';
        wrap.hidden = true;
        wrap.setAttribute('role', 'dialog');
        wrap.setAttribute('aria-modal', 'true');
        wrap.innerHTML = [
            '<div class="media-pick-panel">',
            '  <header class="media-pick-head">',
            '    <input type="search" class="media-pick-search" data-pick-search>',
            '    <button type="button" class="btn btn-sm" data-pick-close>×</button>',
            '  </header>',
            '  <div class="media-pick-grid" data-pick-grid></div>',
            '  <footer class="media-pick-foot">',
            '    <span data-pick-info></span>',
            '    <span class="media-pick-spacer"></span>',
            '    <button type="button" class="btn btn-sm" data-pick-prev>‹</button>',
            '    <button type="button" class="btn btn-sm" data-pick-next>›</button>',
            '  </footer>',
            '</div>'
        ].join('');
        document.body.appendChild(wrap);

        wrap.addEventListener('click', function (e) { if (e.target === wrap) { closeModal(); } });
        wrap.querySelector('[data-pick-close]').addEventListener('click', closeModal);
        wrap.querySelector('[data-pick-prev]').addEventListener('click', function () {
            if (state.page > 1) { state.page -= 1; loadGrid(); }
        });
        wrap.querySelector('[data-pick-next]').addEventListener('click', function () {
            if (state.page * state.limit < state.total) { state.page += 1; loadGrid(); }
        });
        var search = wrap.querySelector('[data-pick-search]');
        var timer = null;
        search.addEventListener('input', function () {
            clearTimeout(timer);
            timer = setTimeout(function () {
                state.search = search.value.trim();
                state.page = 1;
                loadGrid();
            }, 250);
        });
        document.addEventListener('keydown', function (e) {
            if (e.key === 'Escape' && modal && !modal.hidden) { closeModal(); }
        });
        return wrap;
    }

    function openModal(onPick) {
        if (!modal) { modal = buildModal(); }
        pickHandler = onPick;
        modal.hidden = false;
        state.page = 1;
        var search = modal.querySelector('[data-pick-search]');
        if (search) { search.value = ''; state.search = ''; }
        loadGrid();
    }

    function closeModal() {
        if (modal) { modal.hidden = true; }
        pickHandler = null;
    }

    function loadGrid() {
        if (!modal) { return; }
        var grid = modal.querySelector('[data-pick-grid]');
        var info = modal.querySelector('[data-pick-info]');
        grid.innerHTML = '<p class="media-pick-empty">…</p>';
        var qs = 'page=' + state.page + '&limit=' + state.limit + '&file_type=image';
        if (state.search) { qs += '&search=' + encodeURIComponent(state.search); }
        fetch('/api/media/list?' + qs, { credentials: 'same-origin' })
            .then(function (r) { return r.json(); })
            .then(function (j) {
                var data = (j && j.data) || {};
                var list = data.list || [];
                state.total = data.total || 0;
                grid.innerHTML = '';
                if (!list.length) {
                    grid.innerHTML = '<p class="media-pick-empty">没有匹配的图片</p>';
                }
                list.forEach(function (item) {
                    var cell = document.createElement('button');
                    cell.type = 'button';
                    cell.className = 'media-pick-cell';
                    cell.title = item.file_name || '';
                    var img = document.createElement('img');
                    img.alt = item.file_name || '';
                    img.loading = 'lazy';
                    img.src = item.url;
                    cell.appendChild(img);
                    cell.addEventListener('click', function () {
                        if (pickHandler) { pickHandler(item.url); }
                        closeModal();
                    });
                    grid.appendChild(cell);
                });
                var pages = Math.max(1, Math.ceil(state.total / state.limit));
                info.textContent = state.total + ' 张 · 第 ' + state.page + '/' + pages + ' 页';
            })
            .catch(function () {
                grid.innerHTML = '<p class="media-pick-empty">媒体库加载失败</p>';
            });
    }

    // ---------- 控件注册 ----------

    WBUI.register(function (scope) {
        WBUI.$$('[data-media-field]', scope).forEach(function (root) {
            if (!WBUI.markOnce(root, 'MediaField')) { return; }
            var pick = root.querySelector('[data-media-pick]');
            var clear = root.querySelector('[data-media-clear]');
            var input = root.querySelector('[data-media-input]');
            if (pick) {
                pick.addEventListener('click', function () {
                    openModal(function (url) { applyValue(root, url); });
                });
            }
            if (clear) {
                clear.addEventListener('click', function () { applyValue(root, ''); });
            }
            if (input) {
                // 手敲/粘贴地址后立即出预览，不用等保存。
                input.addEventListener('change', function () { renderPreview(root); });
                input.addEventListener('blur', function () { renderPreview(root); });
            }
            renderPreview(root);
        });
    });
})(window);
