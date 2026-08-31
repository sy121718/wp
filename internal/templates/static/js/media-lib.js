/**
 * media-lib.js — 媒体库公共逻辑（/admin/media 与工作台媒体弹窗共用）。
 * 提供：分类树渲染与搜索、附件列表加载（类型/分类/搜索/分页）、详情编辑。
 * 全局命名空间 window.MediaLib。
 */
(function () {
    'use strict';

    var MediaLib = {};

    // ---------- 工具 ----------
    function api(path, opts) {
        opts = opts || {};
        var url = '/api/media/' + path;
        var init = { method: opts.method || 'GET', headers: { 'Content-Type': 'application/json' } };
        if (opts.body) init.body = JSON.stringify(opts.body);
        return fetch(url, init).then(function (r) { return r.json(); }).then(function (j) {
            if (j.code && j.code >= 400) { var err = new Error(j.message || '请求失败'); err.code = j.code; throw err; }
            return j.data;
        });
    }

    // ---------- 分类树 ----------
    // renderTree(container, tree, opts)：递归渲染树节点。
    // opts.onSelect(id, name), opts.onAdd(parentId), opts.onEdit(node), opts.onDelete(node)
    MediaLib.renderTree = function (container, tree, opts) {
        opts = opts || {};
        container.innerHTML = '';
        function build(nodes, ul) {
            nodes.forEach(function (n) {
                var li = document.createElement('li');
                li.className = 'media-tree-item';
                var row = document.createElement('div');
                row.className = 'media-tree-node' + (opts.selectedId === n.id ? ' is-selected' : '');
                row.dataset.id = n.id;
                // 展开/收起箭头（有子分类才显示）。
                var hasKids = !!(n.children && n.children.length);
                var caret = document.createElement('button');
                caret.type = 'button';
                caret.className = 'media-tree-caret' + (opts.collapsed && opts.collapsed[n.id] ? ' is-collapsed' : '');
                caret.title = hasKids ? '展开/收起' : '';
                if (!hasKids) caret.style.visibility = 'hidden';
                caret.addEventListener('click', function (e) {
                    e.stopPropagation();
                    if (opts.onToggle) opts.onToggle(n.id, caret);
                });
                row.appendChild(caret);
                var label = document.createElement('span');
                label.className = 'media-tree-label';
                label.textContent = n.category_name || n.name;
                label.addEventListener('click', function () {
                    if (opts.onSelect) opts.onSelect(n.id, label.textContent);
                });
                row.appendChild(label);
                if (opts.counts && opts.counts[n.id] !== undefined) {
                    var cnt = document.createElement('span');
                    cnt.className = 'media-tree-count';
                    cnt.textContent = opts.counts[n.id];
                    row.appendChild(cnt);
                }
                // 管理操作（后台页才显示）：新建子分类 / 改名 / 删除。
                if (opts.manage) {
                    var act = document.createElement('span');
                    act.className = 'media-tree-actions';
                    var add = document.createElement('button'); add.type = 'button'; add.textContent = '＋'; add.title = '新建子分类';
                    add.addEventListener('click', function (e) { e.stopPropagation(); if (opts.onAdd) opts.onAdd(n.id); });
                    var edit = document.createElement('button'); edit.type = 'button'; edit.textContent = '✎'; edit.title = '改名/移动';
                    edit.addEventListener('click', function (e) { e.stopPropagation(); if (opts.onEdit) opts.onEdit(n); });
                    var del = document.createElement('button'); del.type = 'button'; del.textContent = '✕'; del.title = '删除分类';
                    del.addEventListener('click', function (e) { e.stopPropagation(); if (opts.onDelete) opts.onDelete(n); });
                    act.appendChild(add); act.appendChild(edit); act.appendChild(del);
                    row.appendChild(act);
                }
                li.appendChild(row);
                if (hasKids) {
                    var sub = document.createElement('ul');
                    sub.className = 'media-tree-children';
                    if (opts.collapsed && opts.collapsed[n.id]) sub.classList.add('is-collapsed');
                    build(n.children, sub);
                    li.appendChild(sub);
                }
                ul.appendChild(li);
            });
        }
        build(tree || [], container);
    };

    // filterTree(nodes, keyword)：按名称过滤树（命中或后代命中保留整条链路）。
    MediaLib.filterTree = function (nodes, keyword) {
        if (!keyword) return nodes;
        function hit(n) {
            var self = (n.category_name || n.name || '').toLowerCase().indexOf(keyword) >= 0;
            if (n.children && n.children.length) {
                var kids = n.children.filter(hit);
                if (kids.length) { n = Object.assign({}, n, { children: kids }); return true; }
            }
            return self;
        }
        return nodes.filter(hit);
    };

    // ---------- 附件 ----------
    // list(opts)：加载附件分页。opts: {page, limit, type, categoryId, search}
    MediaLib.list = function (opts) {
        var qs = [];
        qs.push('page=' + (opts.page || 1));
        qs.push('limit=' + (opts.limit || 24));
        if (opts.type && opts.type !== 'all') qs.push('file_type=' + encodeURIComponent(opts.type));
        if (opts.categoryId && opts.categoryId > 0) qs.push('category_id=' + opts.categoryId);
        if (opts.search) qs.push('search=' + encodeURIComponent(opts.search));
        return api('list?' + qs.join('&'));
    };

    // typeLabel(fileType)：类型中文标签。
    MediaLib.typeLabel = function (t) {
        return { image: '图片', video: '视频', document: '文档', other: '其他' }[t] || t || '其他';
    };

    // formatSize(bytes)：人类可读大小。
    MediaLib.formatSize = function (bytes) {
        if (bytes == null) return '';
        if (bytes < 1024) return bytes + ' B';
        if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
        return (bytes / 1024 / 1024).toFixed(1) + ' MB';
    };

    // formatTime(s)：后端时间字符串 → 展示。
    MediaLib.formatTime = function (s) { return (s || '').replace('T', ' ').substring(0, 16); };

    // isImage(mime/fileType)：判断是否图片类。
    MediaLib.isImage = function (mime, type) {
        if (type === 'image') return true;
        return /^image\//.test(mime || '');
    };

    // thumbUrl(item)：缩略图 URL（非图片返回 null）。
    MediaLib.thumbUrl = function (item) {
        if (!item || !item.url) return null;
        if (MediaLib.isImage(item.mime_type, item.file_type)) return item.url;
        return null;
    };

    // api(path, opts)：统一 /api/media/* 请求（返回 data 或抛错）。
    MediaLib.api = api;

    // parseExtra(item)：解析 ExtraInfo JSON（alt/title/description）。
    MediaLib.parseExtra = function (item) {
        var out = { alt: '', title: '', description: '' };
        try {
            var extra = JSON.parse(item.extra_info || '{}');
            out.alt = extra.alt || '';
            out.title = extra.title || '';
            out.description = extra.description || '';
        } catch (e) { /* 忽略 */ }
        return out;
    };

    window.MediaLib = MediaLib;
})();
