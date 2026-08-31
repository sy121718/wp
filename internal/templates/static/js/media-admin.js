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
        counts: {}        // 分类 → 附件数（后续可按需扩展）
    };

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
                if (!confirm('删除分类「' + (node.category_name || node.name) + '」？该分类下的附件将移入未分类。')) return;
                M.api('category/delete', { method: 'POST', body: { id: node.id } }).then(function () {
                    if (state.categoryId === node.id) selectCategory(0, '全部');
                    loadTree();
                }).catch(function (err) { alert(err.message); });
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
        meta.innerHTML =
            '<div>类型：' + M.typeLabel(item.file_type) + (item.mime_type ? '（' + item.mime_type + '）' : '') + '</div>' +
            '<div>大小：' + M.formatSize(item.file_size) + '</div>' +
            '<div>上传时间：' + M.formatTime(item.create_time) + '</div>';
        var urlRow = document.createElement('div');
        urlRow.className = 'media-detail-url';
        var urlInput = document.createElement('input'); urlInput.type = 'text'; urlInput.readOnly = true; urlInput.value = item.url || '';
        var copyBtn = document.createElement('button'); copyBtn.type = 'button'; copyBtn.className = 'btn btn-sm'; copyBtn.textContent = '复制';
        copyBtn.addEventListener('click', function () { urlInput.select(); document.execCommand('copy'); alert('已复制 URL'); });
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
                alert('已保存');
                state.selected = null;
                panel.hidden = true;
                loadList();
            }).catch(function (err) { alert(err.message); });
        });
        var delBtn = document.createElement('button'); delBtn.type = 'button'; delBtn.className = 'btn btn-danger'; delBtn.textContent = '删除';
        delBtn.addEventListener('click', function () {
            if (!confirm('确定删除「' + (item.file_name || '') + '」？')) return;
            M.api('delete', { method: 'POST', body: { id: item.id } }).then(function () {
                state.selected = null;
                panel.hidden = true;
                loadList();
            }).catch(function (err) { alert(err.message); });
        });
        actions.appendChild(saveBtn); actions.appendChild(delBtn);
        body.appendChild(actions);
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
        fetch('/api/media/upload', { method: 'POST', body: form })
            .then(function (r) { return r.json(); })
            .then(function (j) {
                row.textContent = '上传完成';
                setTimeout(function () {
                    if (list) list.innerHTML = '';
                    closeUpload();
                    loadTree(); loadList();
                }, 400);
            })
            .catch(function () { row.textContent = '上传失败'; });
    }

    function openUpload() {
        document.getElementById('ml-upload-modal').hidden = false;
        document.getElementById('ml-upload-mask').hidden = false;
        renderCatOptions(document.getElementById('ml-upload-category'));
    }
    function closeUpload() {
        document.getElementById('ml-upload-modal').hidden = true;
        document.getElementById('ml-upload-mask').hidden = true;
    }

    // ---------- 分类弹窗 ----------
    function openCatModal(node, defaultParent) {
        var modal = document.getElementById('ml-cat-modal');
        var mask = document.getElementById('ml-cat-mask');
        if (!modal) return;
        modal.hidden = false; mask.hidden = false;
        document.getElementById('ml-cat-title').textContent = node ? '编辑分类' : '新建分类';
        document.getElementById('ml-cat-id').value = node ? node.id : '';
        renderCatOptions(document.getElementById('ml-cat-parent'), node ? node.parent_id : (defaultParent || 0), node ? node.id : 0);
        document.getElementById('ml-cat-name').value = node ? (node.category_name || node.name) : '';
    }
    function closeCatModal() {
        document.getElementById('ml-cat-modal').hidden = true;
        document.getElementById('ml-cat-mask').hidden = true;
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
        if (!name) { alert('请输入分类名称'); return; }
        var req = id
            ? M.api('category/update', { method: 'POST', body: { id: id, category_name: name, parent_id: parentId } })
            : M.api('category/create', { method: 'POST', body: { parent_id: parentId, category_name: name } });
        req.then(function () {
            closeCatModal();
            loadTree();
        }).catch(function (err) { alert(err.message); });
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
        // 上传。
        document.getElementById('ml-upload-btn').addEventListener('click', openUpload);
        document.getElementById('ml-upload-close').addEventListener('click', closeUpload);
        document.getElementById('ml-upload-mask').addEventListener('click', closeUpload);
        var fileInput = document.getElementById('ml-file-input');
        document.getElementById('ml-drop').addEventListener('click', function () { fileInput.click(); });
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
        document.getElementById('ml-cat-close').addEventListener('click', closeCatModal);
        document.getElementById('ml-cat-mask').addEventListener('click', closeCatModal);
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
