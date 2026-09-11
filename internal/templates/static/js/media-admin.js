/**
 * media-admin.js — 后台媒体库页面逻辑（左树右库）。
 * 依赖 media-lib.js（MediaLib）。
 */
(function () {
    'use strict';
    var M = window.MediaLib;
    if (!M) return;

    var state = {
        page: 1,
        limit: 24,
        type: 'all',
        categoryId: 0,
        categoryName: '全部',
        search: '',
        view: 'grid',
        tree: [],
        collapsed: {},   // 分类折叠状态（id -> true）
        selected: null,   // 详情侧栏附件
        checked: {},      // 批量下载勾选集合（id -> true）
        counts: {}        // 分类 → 附件数（后续可按需扩展）
    };

    // ---------- 系统对话框收敛 ----------
    // 页面上只剩 confirm()/alert() 还是「系统原生」的样子：样式不可控、会冻结整页、
    // 在嵌入 / 自动化环境里直接被吞掉（用户点删除看到的就是它）。统一走基座控件
    // （js/ui/confirm.js 的同一个 <dialog>）；基座缺席时退回原生，功能不丢。
    function notify(message, opts) {
        if (window.WBUI && WBUI.alert) { WBUI.alert(message, opts); return; }
        window.alert(message);
    }
    function ask(message, onOk, opts) {
        if (window.WBUI && WBUI.confirm) { WBUI.confirm(message, onOk, opts); return; }
        if (window.confirm(message)) { onOk(); }
    }

    // ---------- 分类树 ----------
    function loadTree() {
        M.api('category/tree').then(function (tree) {
            state.tree = tree || [];
            renderTree();
            renderCatOptions();
        }).catch(function (err) { console.error(err); });
    }

    function renderTree() {
        var box = document.getElementById('ml-tree');
        if (!box) return;
        var kw = (document.getElementById('ml-tree-search') || {}).value || '';
        var tree = M.filterTree(state.tree, kw.trim().toLowerCase());
        // 「全部」根节点。
        var allRow = document.createElement('div');
        allRow.className = 'media-tree-node is-root' + (state.categoryId === 0 ? ' is-selected' : '');
        var allLabel = document.createElement('span');
        allLabel.className = 'media-tree-label';
        allLabel.textContent = '全部';
        allLabel.addEventListener('click', function () { selectCategory(0, '全部'); });
        allRow.appendChild(allLabel);
        box.innerHTML = '';
        box.appendChild(allRow);
        // 树（管理模式）。
        var ul = document.createElement('ul');
        ul.className = 'media-tree-children';
        M.renderTree(ul, tree, {
            selectedId: state.categoryId,
            collapsed: state.collapsed,
            manage: true,
            onToggle: function (id, caret) {
                state.collapsed[id] = !state.collapsed[id];
                caret.classList.toggle('is-collapsed', state.collapsed[id]);
                var sub = caret.parentElement.nextElementSibling;
                if (sub) sub.classList.toggle('is-collapsed', state.collapsed[id]);
            },
            onSelect: selectCategory,
            onAdd: function (parentId) { openCatModal(null, parentId); },
            onEdit: function (node) { openCatModal(node); },
            onDelete: function (node) {
                ask('删除分类「' + (node.category_name || node.name) + '」？该分类下的附件将移入未分类。', function () {
                    M.api('category/delete', { method: 'POST', body: { id: node.id } }).then(function () {
                        if (state.categoryId === node.id) selectCategory(0, '全部');
                        loadTree();
                    }).catch(function (err) { notify(err.message); });
                }, { title: '删除分类', ok: '删除', danger: true });
            }
        });
        box.appendChild(ul);
    }

    function selectCategory(id, name) {
        state.categoryId = id;
        state.categoryName = name || '全部';
        state.page = 1;
        renderTree();
        loadList();
    }

    // ---------- 列表 ----------
    function loadList() {
        var grid = document.getElementById('ml-grid');
        var tableBody = document.getElementById('ml-table-body');
        var empty = document.getElementById('ml-empty');
        if (grid) grid.innerHTML = '<p class="media-empty">加载中…</p>';
        M.list({
            page: state.page, limit: state.limit,
            type: state.type, categoryId: state.categoryId, search: state.search
        }).then(function (res) {
            var list = res.list || [];
            var total = res.total || 0;
            renderGrid(list);
            renderTable(list);
            var has = list.length > 0;
            if (grid) grid.hidden = !has || state.view !== 'grid';
            if (tableBody) tableBody.closest('.media-table-wrap').hidden = !has || state.view !== 'list';
            if (empty) empty.hidden = has;
            renderPager(total);
        }).catch(function (err) {
            if (grid) grid.innerHTML = '<p class="media-empty">加载失败：' + err.message + '</p>';
        });
    }

    function renderGrid(list) {
        var grid = document.getElementById('ml-grid');
        if (!grid) return;
        grid.innerHTML = '';
        list.forEach(function (item) {
            var card = document.createElement('div');
            card.className = 'media-card';
            // 批量下载勾选框（左上角，阻止冒泡避免打开详情）。
            var check = document.createElement('input');
            check.type = 'checkbox';
            check.className = 'media-card-check';
            check.title = '勾选后可批量下载';
            check.checked = !!state.checked[item.id];
            check.addEventListener('click', function (e) { e.stopPropagation(); });
            check.addEventListener('change', function () {
                if (check.checked) state.checked[item.id] = true;
                else delete state.checked[item.id];
                updateBatchBtn();
            });
            card.appendChild(check);
            var thumb = document.createElement('div');
            thumb.className = 'media-card-thumb';
            var imgUrl = M.thumbUrl(item);
            if (imgUrl) {
                var img = document.createElement('img');
                img.src = imgUrl; img.alt = item.file_name || '';
                thumb.appendChild(img);
            } else {
                thumb.innerHTML = '<span class="media-card-type">' + M.typeLabel(item.file_type) + '</span>';
            }
            var name = document.createElement('div');
            name.className = 'media-card-name';
            name.textContent = item.file_name || '';
            card.appendChild(thumb); card.appendChild(name);
            card.addEventListener('click', function () { openDetail(item); });
            grid.appendChild(card);
        });
    }

    function renderTable(list) {
        var body = document.getElementById('ml-table-body');
        if (!body) return;
        body.innerHTML = '';
        list.forEach(function (item) {
            var tr = document.createElement('tr');
            var tdCheck = document.createElement('td');
            var check = document.createElement('input');
            check.type = 'checkbox';
            check.checked = !!state.checked[item.id];
            check.addEventListener('change', function () {
                if (check.checked) state.checked[item.id] = true;
                else delete state.checked[item.id];
                updateBatchBtn();
            });
            tdCheck.appendChild(check);
            tr.appendChild(tdCheck);
            var tdFile = document.createElement('td');
            tdFile.textContent = item.file_name || '';
            var tdCat = document.createElement('td');
            tdCat.textContent = catName(item.category_id);
            var tdType = document.createElement('td'); tdType.textContent = M.typeLabel(item.file_type);
            var tdSize = document.createElement('td'); tdSize.textContent = M.formatSize(item.file_size);
            var tdTime = document.createElement('td'); tdTime.textContent = M.formatTime(item.create_time);
            var tdOps = document.createElement('td');
            var btn = document.createElement('button'); btn.type = 'button'; btn.className = 'btn btn-sm'; btn.textContent = '详情';
            btn.addEventListener('click', function () { openDetail(item); });
            tdOps.appendChild(btn);
            tr.appendChild(tdFile); tr.appendChild(tdCat); tr.appendChild(tdType); tr.appendChild(tdSize); tr.appendChild(tdTime); tr.appendChild(tdOps);
            body.appendChild(tr);
        });
    }

    function catName(id) {
        function find(nodes) {
            for (var i = 0; i < nodes.length; i++) {
                if (nodes[i].id === id) return nodes[i].category_name || nodes[i].name;
                var hit = find(nodes[i].children || []);
                if (hit) return hit;
            }
            return null;
        }
        return find(state.tree) || '未分类';
    }

    function renderPager(total) {
        var pager = document.getElementById('ml-pager');
        if (!pager) return;
        var pages = Math.max(1, Math.ceil(total / state.limit));
        pager.hidden = pages <= 1;
        document.getElementById('ml-page-info').textContent = '第 ' + state.page + ' / ' + pages + ' 页 · 共 ' + total + ' 项';
        document.getElementById('ml-prev').disabled = state.page <= 1;
        document.getElementById('ml-next').disabled = state.page >= pages;
    }

    // ---------- 详情侧栏 ----------
    function openDetail(item) {
        state.selected = item;
        var panel = document.getElementById('ml-detail');
        var body = document.getElementById('ml-detail-body');
        if (!panel || !body) return;
        panel.hidden = false;
        var extra = M.parseExtra(item);
        var imgUrl = M.thumbUrl(item);
        body.innerHTML = '';
        if (imgUrl) {
            var img = document.createElement('img');
            img.className = 'media-detail-img';
            img.src = imgUrl; img.alt = item.file_name || '';
            body.appendChild(img);
        }
        // 字段表单。
        var fields = [
            { key: 'file_name', label: '文件名', type: 'text', value: item.file_name || '' },
            { key: 'category_id', label: '分类', type: 'category', value: item.category_id || 0 },
            { key: 'alt', label: '替代文本 (alt)', type: 'text', value: extra.alt },
            { key: 'title', label: '标题', type: 'text', value: extra.title },
            { key: 'description', label: '说明', type: 'textarea', value: extra.description }
        ];
        var form = document.createElement('div');
        form.className = 'media-detail-form';
        var inputs = {};
        fields.forEach(function (f) {
            var wrap = document.createElement('div'); wrap.className = 'media-detail-field';
            var label = document.createElement('label'); label.textContent = f.label; wrap.appendChild(label);
            var input;
            if (f.type === 'textarea') {
                input = document.createElement('textarea'); input.rows = 3;
            } else if (f.type === 'category') {
                input = document.createElement('select');
                var optAll = document.createElement('option'); optAll.value = '0'; optAll.textContent = '未分类';
                input.appendChild(optAll);
                (function fill(nodes, depth) {
                    nodes.forEach(function (n) {
                        var o = document.createElement('option');
                        o.value = n.id;
                        o.textContent = (depth ? '　'.repeat(depth) : '') + (n.category_name || n.name);
                        input.appendChild(o);
                        fill(n.children || [], depth + 1);
                    });
                })(state.tree, 0);
                input.value = String(f.value || 0);
            } else {
                input = document.createElement('input'); input.type = 'text';
            }
            input.value = f.value == null ? '' : String(f.value);
            inputs[f.key] = input;
            wrap.appendChild(input);
            form.appendChild(wrap);
        });
        body.appendChild(form);
        // 元数据与操作。
        var meta = document.createElement('div');
        meta.className = 'media-detail-meta';
        // mime_type 来自上传方提交的 multipart Content-Type（上传方可完全控制），
        // 属于不可信输入：必须用 textContent 逐格填充，绝不拼进 innerHTML。
        var metaType = document.createElement('div');
        metaType.textContent = '类型：' + M.typeLabel(item.file_type) + (item.mime_type ? '（' + item.mime_type + '）' : '');
        var metaSize = document.createElement('div');
        metaSize.textContent = '大小：' + M.formatSize(item.file_size);
        var metaTime = document.createElement('div');
        metaTime.textContent = '上传时间：' + M.formatTime(item.create_time);
        meta.appendChild(metaType);
        meta.appendChild(metaSize);
        meta.appendChild(metaTime);
        // 变体状态徽标行（thumb/medium/webp），数据来自 detail 接口的 variants。
        var variantRow = document.createElement('div');
        variantRow.className = 'media-variant-badges';
        meta.appendChild(variantRow);
        function renderVariantBadges(variants) {
            variantRow.innerHTML = '';
            var labels = { thumb: '缩略图', medium: 'medium', webp: 'webp' };
            var texts = { pending: '排队中', processing: '生成中', ready: '就绪', failed: '失败' };
            (variants || []).forEach(function (v) {
                var b = document.createElement('span');
                b.className = 'media-variant-badge is-' + (v.status || 'pending');
                b.title = (v.width && v.height ? v.width + '×' + v.height + ' ' : '') + M.formatSize(v.file_size);
                b.textContent = (labels[v.variant_type] || v.variant_type) + '·' + (texts[v.status] || v.status);
                variantRow.appendChild(b);
            });
        }
        // 拉详情拿变体状态（列表项不含 variants）。
        M.api('detail?id=' + item.id).then(function (fresh) {
            item.variants = (fresh && fresh.variants) || [];
            renderVariantBadges(item.variants);
        }).catch(function () { /* 变体状态拉取失败不打断详情 */ });
        var urlRow = document.createElement('div');
        urlRow.className = 'media-detail-url';
        var urlInput = document.createElement('input'); urlInput.type = 'text'; urlInput.readOnly = true; urlInput.value = item.url || '';
        var copyBtn = document.createElement('button'); copyBtn.type = 'button'; copyBtn.className = 'btn btn-sm'; copyBtn.textContent = '复制';
        copyBtn.addEventListener('click', function () { urlInput.select(); document.execCommand('copy'); notify('已复制 URL'); });
        urlRow.appendChild(urlInput); urlRow.appendChild(copyBtn);
        body.appendChild(meta); body.appendChild(urlRow);
        // 保存 / 删除。
        var actions = document.createElement('div');
        actions.className = 'media-detail-actions';
        var saveBtn = document.createElement('button'); saveBtn.type = 'button'; saveBtn.className = 'btn btn-primary'; saveBtn.textContent = '保存';
        saveBtn.addEventListener('click', function () {
            var body2 = {
                id: item.id,
                file_name: inputs.file_name.value.trim(),
                category_id: Number(inputs.category_id.value || 0),
                alt: inputs.alt.value.trim(),
                title: inputs.title.value.trim(),
                description: inputs.description.value.trim()
            };
            M.api('update', { method: 'POST', body: body2 }).then(function () {
                notify('已保存');
                state.selected = null;
                panel.hidden = true;
                loadList();
            }).catch(function (err) { notify(err.message); });
        });
        var delBtn = document.createElement('button'); delBtn.type = 'button'; delBtn.className = 'btn btn-danger'; delBtn.textContent = '删除';
        delBtn.addEventListener('click', function () {
            ask('确定删除「' + (item.file_name || '') + '」？', function () {
                M.api('delete', { method: 'POST', body: { id: item.id } }).then(function () {
                    state.selected = null;
                    panel.hidden = true;
                    loadList();
                }).catch(function (err) { notify(err.message); });
            }, { title: '删除附件', ok: '删除', danger: true });
        });
        actions.appendChild(saveBtn); actions.appendChild(delBtn);
        body.appendChild(actions);
        // 变体与资源包操作（048 改造）。
        var varActions = document.createElement('div');
        varActions.className = 'media-detail-actions';
        var dlBtn = document.createElement('button');
        dlBtn.type = 'button'; dlBtn.className = 'btn btn-secondary';
        dlBtn.textContent = '下载资源包';
        dlBtn.title = '原图 + webp + 缩略图 + medium 打包 zip 下载';
        dlBtn.addEventListener('click', function () {
            window.open('/api/media/download?id=' + item.id, '_blank');
        });
        var regenBtn = document.createElement('button');
        regenBtn.type = 'button'; regenBtn.className = 'btn';
        regenBtn.textContent = '重新生成变体';
        regenBtn.addEventListener('click', function () {
            regenBtn.disabled = true; regenBtn.textContent = '生成中…';
            M.api('variants/generate', { method: 'POST', body: { id: item.id } })
                .then(function (variants) {
                    notify('变体生成完成');
                    renderVariantBadges(variants || []);
                    loadList();
                })
                .catch(function (err) { notify(err.message); })
                .finally(function () { regenBtn.disabled = false; regenBtn.textContent = '重新生成变体'; });
        });
        varActions.appendChild(dlBtn); varActions.appendChild(regenBtn);
        body.appendChild(varActions);
    }

    // ---------- 上传 ----------
    function uploadFiles(files, categoryID) {
        if (!files || !files.length) return;
        var form = new FormData();
        for (var i = 0; i < files.length; i++) form.append('file', files[i]);
        if (categoryID && categoryID > 0) form.append('category_id', categoryID);
        var list = document.getElementById('ml-upload-list');
        var row = document.createElement('div'); row.textContent = '上传中 ' + files.length + ' 个文件…';
        if (list) list.appendChild(row);
        // FormData 提交不设 Content-Type（浏览器自动带 boundary）；写请求必须带 CSRF token。
        fetch('/api/media/upload', { method: 'POST', headers: M.apiHeaders({}), body: form })
            .then(function (r) { return r.json().then(function (j) { return { ok: r.ok, body: j }; }); })
            .then(function (res) {
                // 必须校验 HTTP 状态与业务 code：被拒（无 upload 权限 403 / 类型不符 400）
                // 时后端同样返回 JSON，原实现一律显示「上传完成」，用户以为成功了。
                if (!res.ok || (res.body && res.body.code !== 0)) {
                    row.textContent = '上传失败：' + ((res.body && res.body.message) || '未知错误');
                    return;
                }
                row.textContent = '上传完成';
                setTimeout(function () {
                    if (list) list.innerHTML = '';
                    closeUpload();
                    loadTree(); loadList();
                }, 400);
            })
            .catch(function () { row.textContent = '上传失败'; });
    }

    // 弹窗开合交给基座控件（js/ui/modal.js）：焦点陷阱、Esc、遮罩点击、关闭后焦点归还
    // 都由它负责。这里只留「打开前先把选项渲染好」这类业务动作 —— 顺序不能反：
    // 先渲染选项再打开，打开时的扫描才能把新渲染的 select 一并增强。
    function openUpload() {
        renderCatOptions(document.getElementById('ml-upload-category'));
        openModal('ml-upload-modal');
    }
    function closeUpload() {
        closeModal('ml-upload-modal');
    }

    // ---------- 分类弹窗 ----------
    function openCatModal(node, defaultParent) {
        if (!document.getElementById('ml-cat-modal')) return;
        document.getElementById('ml-cat-title').textContent = node ? '编辑分类' : '新建分类';
        document.getElementById('ml-cat-id').value = node ? node.id : '';
        renderCatOptions(document.getElementById('ml-cat-parent'), node ? node.parent_id : (defaultParent || 0), node ? node.id : 0);
        document.getElementById('ml-cat-name').value = node ? (node.category_name || node.name) : '';
        openModal('ml-cat-modal');
    }
    function closeCatModal() {
        closeModal('ml-cat-modal');
    }

    // openModal/closeModal 薄封装：基座控件缺席时退回原生 <dialog> 语义 ——
    // 「少引一个脚本」不该变成「弹窗根本打不开」。
    function openModal(id) {
        var dlg = document.getElementById(id);
        if (!dlg) return;
        if (window.WBUI && WBUI.modal) { WBUI.modal.open(dlg); return; }
        if (dlg.showModal) { dlg.showModal(); } else { dlg.setAttribute('open', ''); }
    }
    function closeModal(id) {
        var dlg = document.getElementById(id);
        if (!dlg) return;
        if (window.WBUI && WBUI.modal) { WBUI.modal.close(dlg); return; }
        if (dlg.close) { dlg.close(); } else { dlg.removeAttribute('open'); }
    }

    function renderCatOptions(select, selected, excludeId) {
        if (!select) return;
        var keep = select === document.getElementById('ml-upload-category');
        select.innerHTML = '';
        var optRoot = document.createElement('option');
        optRoot.value = keep ? '0' : '0';
        optRoot.textContent = keep ? '未分类' : '（顶级分类）';
        select.appendChild(optRoot);
        (function fill(nodes, depth) {
            nodes.forEach(function (n) {
                if (excludeId && n.id === excludeId) return;
                var o = document.createElement('option');
                o.value = n.id;
                o.textContent = (depth ? '　'.repeat(depth) : '') + (n.category_name || n.name);
                select.appendChild(o);
                fill(n.children || [], depth + 1);
            });
        })(state.tree, 0);
        select.value = String(selected || 0);
    }

    function saveCategory() {
        var id = Number(document.getElementById('ml-cat-id').value || 0);
        var name = document.getElementById('ml-cat-name').value.trim();
        var parentId = Number(document.getElementById('ml-cat-parent').value || 0);
        if (!name) { notify('请输入分类名称'); return; }
        var req = id
            ? M.api('category/update', { method: 'POST', body: { id: id, category_name: name, parent_id: parentId } })
            : M.api('category/create', { method: 'POST', body: { parent_id: parentId, category_name: name } });
        req.then(function () {
            closeCatModal();
            loadTree();
        }).catch(function (err) { notify(err.message); });
    }

    // ---------- 事件绑定 ----------
    function bind() {
        // 顶栏：名称搜索（分类维度由左侧树承担，不再设类型下拉）。
        var searchInput = document.getElementById('ml-search');
        var searchTimer = null;
        searchInput.addEventListener('input', function () {
            clearTimeout(searchTimer);
            searchTimer = setTimeout(function () {
                state.search = searchInput.value.trim();
                state.page = 1;
                loadList();
            }, 300);
        });
        // 树搜索（过滤节点，不请求）。
        document.getElementById('ml-tree-search').addEventListener('input', function () { renderTree(); });
        // 分页。
        document.getElementById('ml-prev').addEventListener('click', function () { if (state.page > 1) { state.page--; loadList(); } });
        document.getElementById('ml-next').addEventListener('click', function () { state.page++; loadList(); });
        // 视图切换。
        document.getElementById('ml-view-grid').addEventListener('click', function () { state.view = 'grid'; refreshView(); });
        document.getElementById('ml-view-list').addEventListener('click', function () { state.view = 'list'; refreshView(); });
        // 批量下载（勾选 id 集合 → GET /api/media/download/batch?ids=1,2,3）。
        document.getElementById('ml-batch-download').addEventListener('click', function () {
            var ids = Object.keys(state.checked);
            if (!ids.length) { notify('请先勾选要下载的图片'); return; }
            window.open('/api/media/download/batch?ids=' + ids.join(','), '_blank');
        });
        // 上传。
        document.getElementById('ml-upload-btn').addEventListener('click', openUpload);
        // 关闭按钮 / 遮罩 / Esc 都归基座（data-modal-close + <dialog> 原生行为），这里不再逐个绑。
        var fileInput = document.getElementById('ml-file-input');
        var drop = document.getElementById('ml-drop');
        drop.addEventListener('click', function () { fileInput.click(); });
        // drop 区此前是纯 div：键盘用户够不到「选择文件」，补 Enter/Space 与 role=button 对齐。
        drop.addEventListener('keydown', function (e) {
            if (e.key === 'Enter' || e.key === ' ' || e.key === 'Spacebar') {
                e.preventDefault();
                fileInput.click();
            }
        });
        fileInput.addEventListener('change', function () {
            uploadFiles(fileInput.files, Number(document.getElementById('ml-upload-category').value || 0));
            fileInput.value = '';
        });
        document.getElementById('ml-drop').addEventListener('dragover', function (e) { e.preventDefault(); });
        document.getElementById('ml-drop').addEventListener('drop', function (e) {
            e.preventDefault();
            uploadFiles(e.dataTransfer.files, Number(document.getElementById('ml-upload-category').value || 0));
        });
        // 分类管理。
        document.getElementById('ml-category-add').addEventListener('click', function () { openCatModal(null, 0); });
        document.getElementById('ml-cat-save').addEventListener('click', saveCategory);
        // 详情。
        document.getElementById('ml-detail-close').addEventListener('click', function () {
            document.getElementById('ml-detail').hidden = true;
            state.selected = null;
        });
    }

    function refreshView() {
        document.getElementById('ml-view-grid').classList.toggle('is-active', state.view === 'grid');
        document.getElementById('ml-view-list').classList.toggle('is-active', state.view === 'list');
        loadList();
    }

    // updateBatchBtn 批量下载按钮随勾选数变化提示文案。
    function updateBatchBtn() {
        var btn = document.getElementById('ml-batch-download');
        if (!btn) return;
        var n = Object.keys(state.checked).length;
        btn.textContent = n > 0 ? ('批量下载(' + n + ')') : '批量下载';
    }

    // ---------- 启动 ----------
    function init() {
        bind();
        loadTree();
        loadList();
    }

    function safeInit() {
        try { init(); }
        catch (e) { window.__mediaInitError = String(e && e.stack || e); }
    }
    if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', safeInit);
    else safeInit();
})();
