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
 *
 * 选择面板的版式（2026-09-30 改）：**左分类树 + 中网格 + 右详情**，与媒体库列表页同构。
 * 原来的弹窗只有「搜索 + 网格」两件套 —— 417 张图翻 9 页找一张、看不到分类，
 * 点一下就选中并关窗、连图有多大都不知道。现在的行为：
 *   · 点网格里的图 → 选中它并在**右侧显示详情**（大图 + 文件名 + 尺寸 + 体积），不关窗；
 *   · 详情区的按钮确认（单选）或加入多选（多选模式）；
 *   · 左侧分类树按 category/tree 过滤，顶部搜索按文件名过滤 —— 两条收窄路径都要有，
 *     417 张的库只靠搜索等于没有导航。
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

    // renderPreview 按当前值刷新图像框：有图显示缩略图、无图显示提示；
    // 地址行与「移除」只在有图时出现（空框上挂一个「移除」没有意义）。
    function renderPreview(root) {
        var input = root.querySelector('[data-media-input]');
        var img = root.querySelector('[data-media-img]');
        var tip = root.querySelector('[data-media-tip]');
        var urlText = root.querySelector('[data-media-url]');
        var clear = root.querySelector('[data-media-clear]');
        var value = input ? String(input.value || '').trim() : '';
        var src = srcOf(value);
        if (img) {
            if (src) {
                img.src = src;
                img.hidden = false;
            } else {
                img.removeAttribute('src');
                img.hidden = true;
            }
        }
        if (tip) {
            tip.hidden = !!src;
        }
        if (urlText) {
            urlText.textContent = value;
            urlText.hidden = !value;
        }
        if (clear) {
            clear.hidden = !value;
        }
        root.classList.toggle('is-filled', !!src);
    }

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
    // 分类树数据（loadTree 写、renderTreeBox 读）。**必须声明**：它是闭包内共享状态，
    // 漏了声明时严格模式下直接 ReferenceError —— 而它被 catch 吞掉，表现成
    // 「分类加载失败」，看上去像接口或权限问题（实际接口 200、数据也在）。
    var tree = null;
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
            '  <div class="media-pick-body" data-pick-body>',
            '    <aside class="media-pick-tree" data-pick-tree-panel>',
            '      <input type="search" class="media-pick-tree-search" data-pick-tree-search>',
            '      <div class="media-pick-tree-box" data-pick-tree></div>',
            '    </aside>',
            '    <div class="media-pick-main">',
            '      <div class="media-pick-grid" data-pick-grid></div>',
            '      <footer class="media-pick-foot">',
            '        <span data-pick-info></span>',
            '        <span class="media-pick-spacer"></span>',
            '        <button type="button" class="btn btn-sm" data-pick-prev>‹</button>',
            '        <button type="button" class="btn btn-sm" data-pick-next>›</button>',
            '        <span class="media-pick-multi" data-pick-multi hidden>',
            '          <span class="media-pick-count">已选 <b data-pick-count>0</b> 张</span>',
            '          <button type="button" class="btn btn-sm btn-primary" data-pick-confirm disabled>确定</button>',
            '        </span>',
            '      </footer>',
            '    </div>',
            '    <aside class="media-pick-detail" data-pick-detail hidden>',
            '      <div data-pick-detail-body></div>',
            '    </aside>',
            '  </div>',
            '</div>'
        ].join('');
        document.body.appendChild(wrap);
        var treeSearch = wrap.querySelector('[data-pick-tree-search]');
        if (treeSearch) {
            treeSearch.addEventListener('input', renderTreeBox);
        }
        var confirm = wrap.querySelector('[data-pick-confirm]');
        if (confirm) {
            confirm.addEventListener('click', function () {
                if (!state.multi || !state.picked.length) { return; }
                var picked = state.picked.slice();
                // **先取出 handler 再关窗**：closeModal 会把 pickHandler 置空，
                // 反过来的话回调永远不会执行 —— 表现是「选了 7 张、点了确定，什么都没发生」。
                var handler = pickHandler;
                closeModal();
                if (handler) { handler(picked); }
            });
        }

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

    // openModal(onPick, opts)：opts.multi = true 进入多选 —— 点图累积选中、不关窗，
    // 按「确定」一次性把 url 数组回调出去。多图字段（商品图集）用这个模式；
    // 单选（封面图 / 富文本插图）行为不变。
    function openModal(onPick, opts) {
        if (!modal) { modal = buildModal(); }
        pickHandler = onPick;
        state.multi = !!(opts && opts.multi);
        // 文件类型过滤：默认只看图片（绝大多数字段要的就是图），
        // 富文本的附件要能挑任意文件（PDF/压缩包…），由调用方给 'all'。
        state.fileType = (opts && opts.fileType) || 'image';
        state.picked = [];
        state.categoryId = state.categoryId || 0;
        modal.hidden = false;
        state.page = 1;
        var search = modal.querySelector('[data-pick-search]');
        if (search) { search.value = ''; state.search = ''; }
        syncMultiBar();
        renderDetail(null);
        loadTree();
        loadGrid();
    }

    // syncMultiBar 刷新多选状态条（单选模式下整条隐藏）。
    function syncMultiBar() {
        if (!modal) { return; }
        var bar = modal.querySelector('[data-pick-multi]');
        if (!bar) { return; }
        bar.hidden = !state.multi;
        if (!state.multi) { return; }
        var count = bar.querySelector('[data-pick-count]');
        if (count) { count.textContent = String(state.picked.length); }
        var confirm = bar.querySelector('[data-pick-confirm]');
        if (confirm) { confirm.disabled = state.picked.length === 0; }
    }

    function closeModal() {
        if (modal) { modal.hidden = true; }
        renderDetail(null);
        pickHandler = null;
    }

    function loadGrid() {
        if (!modal) { return; }
        var grid = modal.querySelector('[data-pick-grid]');
        var info = modal.querySelector('[data-pick-info]');
        grid.innerHTML = '<p class="media-pick-empty">…</p>';
        var qs = 'page=' + state.page + '&limit=' + state.limit;
        // 'all' 表示「不加类型过滤」而不是「类型等于 all」（与列表页 MediaLib.list 同一口径）——
        // 传字面量 all 会被后端当成一种不存在的类型，返回 0 条，看起来像「媒体库是空的」。
        if (state.fileType && state.fileType !== 'all') {
            qs += '&file_type=' + encodeURIComponent(state.fileType);
        }
        if (state.search) { qs += '&search=' + encodeURIComponent(state.search); }
        // 分类过滤：417 张的库只靠搜索等于没有导航（与列表页同一个 category_id 参数）。
        if (state.categoryId) { qs += '&category_id=' + state.categoryId; }
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

                    // 文件名行：搜索之后要能一眼核对「这张是不是我要的」，只靠 hover 提示不够
                    //（鼠标常驻在图上的时候看不到别的图叫什么）。长名单行截断 + title 给全称。
                    var name = document.createElement('span');
                    name.className = 'media-pick-name';
                    name.textContent = item.file_name || '';
                    name.title = item.file_name || '';
                    cell.appendChild(name);
                    if (state.picked.indexOf(item.url) >= 0) {
                        cell.classList.add('is-picked');
                    }
                    cell.setAttribute('data-pick-url', item.url);
                    cell.addEventListener('click', function () {
                        // 点图 = 选中它 + 右侧显示详情（大图与尺寸），**不关窗**。
                        // 要不要用这张，由详情区的按钮决定 —— 原来的「点一下就选中并关闭」
                        // 让人来不及看图有多大、是不是要找的那张。
                        var at = state.picked.indexOf(item.url);
                        if (state.multi) {
                            if (at >= 0) {
                                state.picked.splice(at, 1);
                            } else {
                                state.picked.push(item.url);
                            }
                        } else {
                            state.picked = at >= 0 ? [] : [item.url];
                        }
                        syncPickedCells();
                        syncMultiBar();
                        renderDetail(at >= 0 && !state.multi ? null : item);
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


    // ---------- 分类树（左栏）----------
    // 自己渲染而不是复用 media-lib.js 的 renderTree：那一份要在页面里先加载 media-lib.js，
    // 而本控件的约定是「不依赖 MediaLib」（见文件头）—— 一个选图框不该绑定别的脚本的加载顺序。
    // loadTree 加载分类树。
    //
    // 权限：/api/media/category/tree 要 media:category_tree —— 而本面板的调用方是
    // **任意后台编辑页**（商品 / 文章 / 图集），编辑者不一定有这个权限。
    // 所以拿不到时**整栏隐去**（退回两栏），而不是把 403 显示成「分类加载失败」：
    // 对他而言那不是错误，只是「没有分类可筛」。报错会让人以为面板坏了。
    function loadTree() {
        if (!modal) { return; }
        var box = modal.querySelector('[data-pick-tree]');
        if (!box) { return; }
        fetch('/api/media/category/tree', { credentials: 'same-origin' })
            .then(function (r) {
                if (!r.ok) { throw new Error('tree ' + r.status); }
                return r.json();
            })
            .then(function (j) {
                // 列表页的 api() 直接返回 data，这里自己打 fetch 所以取 .data；
                // 兼容两种形状（有的端点 data 就是数组本身）。
                tree = (j && j.data) ? j.data : (j || []);
                if (!tree.length) {
                    hideTreePanel();
                    return;
                }
                try {
                    renderTreeBox();
                } catch (err) {
                    // 「渲染出错」与「没有分类」是两件事：混在一个 catch 里会让前者静默变成后者 ——
                    // 分类明明有（接口 200），面板却当自己没权限。日志分开是为了下次能一眼分清。
                    console.error('[media-pick] 分类树渲染失败', err);
                    hideTreePanel();
                }
            })
            .catch(function (err) {
                console.error('[media-pick] 分类树加载失败', err);
                hideTreePanel();
            });
    }

    // hideTreePanel 隐去左栏（三栏变两栏）。
    function hideTreePanel() {
        if (!modal) { return; }
        var panel = modal.querySelector('[data-pick-tree-panel]');
        if (panel) {
            panel.hidden = true;
        }
        var body = modal.querySelector('[data-pick-body]');
        if (body) {
            body.classList.add('is-no-tree');
        }
    }

    function renderTreeBox() {
        if (!modal) { return; }
        var box = modal.querySelector('[data-pick-tree]');
        if (!box) { return; }
        var search = modal.querySelector('[data-pick-tree-search]');
        var kw = (search && search.value ? search.value : '').trim().toLowerCase();
        box.innerHTML = '';

        var all = document.createElement('button');
        all.type = 'button';
        all.className = 'media-pick-tree-node' + (state.categoryId ? '' : ' is-selected');
        all.textContent = '全部';
        all.addEventListener('click', function () { selectCategory(0); });
        box.appendChild(all);

        (tree || []).forEach(function (node) {
            var name = node.category_name || node.name || '';
            if (kw && name.toLowerCase().indexOf(kw) < 0) { return; }
            var btn = document.createElement('button');
            btn.type = 'button';
            btn.className = 'media-pick-tree-node' + (state.categoryId === node.id ? ' is-selected' : '');
            btn.textContent = name;
            btn.addEventListener('click', function () { selectCategory(node.id); });
            box.appendChild(btn);
        });
    }

    function selectCategory(id) {
        state.categoryId = id || 0;
        state.page = 1;
        renderTreeBox();
        loadGrid();
    }

    // ---------- 详情（右栏）----------
    function fmtSize(bytes) {
        var n = Number(bytes || 0);
        if (!n) { return '—'; }
        if (n < 1024) { return n + ' B'; }
        if (n < 1024 * 1024) { return (n / 1024).toFixed(1) + ' KB'; }
        return (n / 1024 / 1024).toFixed(1) + ' MB';
    }

    function renderDetail(item) {
        if (!modal) { return; }
        var panel = modal.querySelector('[data-pick-detail]');
        var body = modal.querySelector('[data-pick-detail-body]');
        if (!panel || !body) { return; }
        if (!item) {
            panel.hidden = true;
            body.innerHTML = '';
            return;
        }
        panel.hidden = false;
        body.innerHTML = '';

        var img = document.createElement('img');
        img.className = 'media-pick-detail-img';
        img.src = item.url;
        img.alt = item.file_name || '';
        body.appendChild(img);

        // 尺寸不取接口字段：列表接口不返回 width/height（详情接口才返回），
        // 显示成「0 × 0」比不显示更误导。改从图片自身读（load 后就有了），
        // 读不到就留「—」——它标的是「这张图的像素尺寸」，前端本来就知道得更准。
        var kv = [
            ['文件名', item.file_name || ''],
            ['类型', item.mime_type || ''],
            ['尺寸', '—'],
            ['体积', fmtSize(item.file_size)]
        ];
        var dl = document.createElement('dl');
        dl.className = 'media-pick-detail-kv';
        var sizeDD = null;
        kv.forEach(function (pair) {
            var dt = document.createElement('dt');
            dt.textContent = pair[0];
            var dd = document.createElement('dd');
            dd.textContent = pair[1] || '—';
            if (pair[0] === '尺寸') { sizeDD = dd; }
            dl.appendChild(dt);
            dl.appendChild(dd);
        });
        body.appendChild(dl);
        img.addEventListener('load', function () {
            if (sizeDD && img.naturalWidth) {
                sizeDD.textContent = img.naturalWidth + ' × ' + img.naturalHeight;
            }
        });

        var act = document.createElement('button');
        act.type = 'button';
        act.className = 'btn btn-primary media-pick-detail-act';
        act.textContent = state.multi ? '加入选择' : '选用这张';
        act.addEventListener('click', function () {
            if (state.multi) {
                if (state.picked.indexOf(item.url) < 0) {
                    state.picked.push(item.url);
                }
                syncPickedCells();
                syncMultiBar();
                return;
            }
            // 回调带完整 item：附件行要拿文件名当链接文字，光有 url 不够。
            if (pickHandler) { pickHandler(item.url, item); }
            closeModal();
        });
        body.appendChild(act);
    }

    // syncPickedCells 按 state.picked 刷新网格里的选中态。
    function syncPickedCells() {
        if (!modal) { return; }
        var cells = modal.querySelectorAll('[data-pick-url]');
        Array.prototype.forEach.call(cells, function (cell) {
            var url = cell.getAttribute('data-pick-url');
            if (state.picked.indexOf(url) >= 0) {
                cell.classList.add('is-picked');
            } else {
                cell.classList.remove('is-picked');
            }
        });
    }

    // ---------- 对外接口 ----------
    // 打开媒体选择器（onPick(相对URL)）。两个调用方：本文件的媒体字段控件、富文本插图的
    // rich-editor/image.js —— 图片选择器只该有一份 UI，两边各写一套会立刻分叉
    //（一边能搜能翻页、另一边只能粘贴地址）。
    // opts.fileType：'image'（默认）| 'all' | 'video' | 'document'。
    // 回调第二个参数是完整 item（文件名/类型/体积），附件插入要用它。
    WBUI.openMediaPicker = function (onPick, opts) {
        openModal(onPick, opts || {});
    };
    // 多选入口（onPick(urls)）。图集用它。
    WBUI.openMediaPickerMulti = function (onPick, opts) {
        var merged = opts || {};
        merged.multi = true;
        openModal(onPick, merged);
    };

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
