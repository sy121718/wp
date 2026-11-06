// editor-app/methods/media.js — 媒体库选择器（应用端点）。
// 方法以 `this` 互调，由 index.js 用 Object.assign 合并为同一个 workbench 实例。
// editor-app/methods/media.js — 媒体库选择器（应用端点：分类树 / 列表 / 上传）。
import { meta, csrfHeaders } from '../../editor/core.js';

import { normalizeMediaURL } from '../../editor/media-url.js';

export const mediaMethods = {
            // initMediaPicker 绑定媒体弹层（关闭/遮罩/上传/搜索），由编辑器 init 调用。
            initMediaPicker() {
                var self = this;
                var close = document.getElementById('wb-media-close');
                if (close) close.addEventListener('click', function () { self.closeMediaPicker(); });
                var search = document.getElementById('wb-media-search');
                var timer = null;
                if (search) search.addEventListener('input', function () {
                    clearTimeout(timer);
                    timer = setTimeout(function () {
                        self._mediaSearch = search.value.trim();
                        self.loadMediaList();
                    }, 300);
                });
                var treeSearch = document.getElementById('wb-media-tree-search');
                if (treeSearch) treeSearch.addEventListener('input', function () { self.renderMediaTree(); });
                var mask = document.querySelector('#wb-media-modal .wb-media-mask');
                if (mask) mask.addEventListener('click', function () { self.closeMediaPicker(); });
                var uploadBtn = document.getElementById('wb-media-upload-btn');
                var uploadInput = document.getElementById('wb-media-upload-input');
                if (uploadBtn && uploadInput) {
                    uploadBtn.addEventListener('click', function () { uploadInput.click(); });
                    uploadInput.addEventListener('change', function () {
                        if (uploadInput.files && uploadInput.files[0]) self.uploadMedia(uploadInput.files[0]);
                        uploadInput.value = '';
                    });
                }
            },

            // ---------------- 媒体库选择器 ----------------
            _mediaPickTarget: null,
            _mediaCache: null,
            _mediaCategory: 0,   // 媒体弹窗当前分类筛选（0=全部）
            _mediaSearch: '',    // 媒体弹窗名称搜索
            _mediaTree: [],
            resolveAssetUrl(assetIdOrUrl) {
                if (!assetIdOrUrl) return '';
                var normalized = normalizeMediaURL(assetIdOrUrl, meta.mediaBaseUrl);
                if (normalized && !/^\d+$/.test(String(assetIdOrUrl))) return normalized;
                var list = this._mediaCache || [];
                for (var i = 0; i < list.length; i++) {
                    if (String(list[i].id) === String(assetIdOrUrl)) return normalizeMediaURL(list[i].url, meta.mediaBaseUrl);
                }
                return '';
            },
            openMediaPicker(onPick) {
                this._mediaPickTarget = onPick;
                var modal = document.getElementById('wb-media-modal');
                if (!modal) return;
                modal.hidden = false;
                this.loadMediaTree();
                this.loadMediaList();
            },
            // 媒体弹窗：加载分类树（左栏，复用 MediaLib 渲染；可搜索过滤）。
            loadMediaTree() {
                var self = this;
                fetch('/api/media/category/tree')
                    .then(function (r) { return r.json(); })
                    .then(function (j) {
                        self._mediaTree = (j.data && j.data) || [];
                        self.renderMediaTree();
                    })
                    .catch(function () { /* 树加载失败不阻塞网格 */ });
            },
            renderMediaTree() {
                var self = this;
                var box = document.getElementById('wb-media-tree-list');
                if (!box) return;
                if (!window.MediaLib) return;
                var kw = (document.getElementById('wb-media-tree-search').value || '').trim().toLowerCase();
                var tree = MediaLib.filterTree(this._mediaTree, kw);
                box.innerHTML = '';
                // 「全部」根节点。
                var all = document.createElement('div');
                all.className = 'wb-media-tree-node' + (this._mediaCategory === 0 ? ' is-selected' : '');
                all.textContent = '全部';
                all.addEventListener('click', function () {
                    self._mediaCategory = 0;
                    self.renderMediaTree();
                    self.loadMediaList();
                });
                box.appendChild(all);
                var ul = document.createElement('ul');
                ul.className = 'wb-media-tree-children';
                if (!this._mediaCollapsed) this._mediaCollapsed = {};
                MediaLib.renderTree(ul, tree, {
                    selectedId: this._mediaCategory,
                    collapsed: this._mediaCollapsed,
                    onToggle: function (id, caret) {
                        self._mediaCollapsed[id] = !self._mediaCollapsed[id];
                        caret.classList.toggle('is-collapsed', self._mediaCollapsed[id]);
                        var sub = caret.parentElement.nextElementSibling;
                        if (sub) sub.classList.toggle('is-collapsed', self._mediaCollapsed[id]);
                    },
                    onSelect: function (id) {
                        self._mediaCategory = id;
                        self.renderMediaTree();
                        self.loadMediaList();
                    }
                });
                box.appendChild(ul);
            },
            closeMediaPicker() {
                var modal = document.getElementById('wb-media-modal');
                if (modal) modal.hidden = true;
                this._mediaPickTarget = null;
            },
            loadMediaList() {
                var grid = document.getElementById('wb-media-grid');
                if (!grid) return;
                var self = this;
                if (!grid.firstChild) grid.innerHTML = '<p class="wb-empty">加载中…</p>'; // 已有内容时保留旧 DOM：容器高度不归零，滚动位置不会被钳回顶部
                var seq = (self._mediaReq = (self._mediaReq || 0) + 1);
                var qs = 'page=1&limit=120';
                if (self._mediaCategory > 0) qs += '&category_id=' + self._mediaCategory;
                if (self._mediaSearch) qs += '&search=' + encodeURIComponent(self._mediaSearch);
                fetch('/api/media/list?' + qs)
                    .then(function (r) { return r.json(); })
                    .then(function (j) {
                        if (seq !== self._mediaReq) return;
                        grid.innerHTML = '';
                        var list = (j.data && j.data.list) || [];
                        self._mediaCache = list;
                        if (!list.length) {
                            grid.innerHTML = '<p class="wb-empty">媒体库为空，点右上「上传图片」</p>';
                            return;
                        }
                        list.forEach(function (item) {
                            item.url = normalizeMediaURL(item.url, meta.mediaBaseUrl);
                            if (!item.url) return;
                            var cell = document.createElement('button');
                            cell.type = 'button'; cell.className = 'wb-media-cell'; cell.title = item.file_name || '';
                            // 图片显示缩略图；非图片显示类型角标（视频/文档等）。
                            var isImg = /^image\//.test(item.mime_type || '') || item.file_type === 'image';
                            if (isImg) {
                                var img = document.createElement('img'); img.src = item.url; img.alt = item.file_name || '';
                                cell.appendChild(img);
                            } else {
                                var badge = document.createElement('span'); badge.className = 'wb-media-cell-badge';
                                badge.textContent = { video: '视频', document: '文档', other: '文件' }[item.file_type] || '文件';
                                cell.appendChild(badge);
                            }
                            var cap = document.createElement('span'); cap.textContent = item.file_name || ''; cell.appendChild(cap);
                            cell.addEventListener('click', function () {
                                if (self._mediaPickTarget) self._mediaPickTarget(item.url, String(item.id), item);
                                self.closeMediaPicker();
                            });
                            grid.appendChild(cell);
                        });
                    })
                    .catch(function () { if (seq === self._mediaReq) grid.innerHTML = '<p class="wb-empty">加载失败</p>'; });
            },
            uploadMedia(file) {
                if (!file) return;
                var self = this;
                var form = new FormData();
                form.append('file', file);
                fetch('/api/media/upload', { method: 'POST', headers: csrfHeaders({}), body: form })
                    .then(function (r) { return r.json(); })
                    .then(function (j) {
                        if (j.code && j.code >= 400) { alert(j.message || '上传失败'); return; }
                        var item = (j.data && (j.data.file || j.data)) || {};
                        item.url = normalizeMediaURL(item.url, meta.mediaBaseUrl);
                        if (!item.url) { alert('上传响应缺少有效的完整媒体地址'); return; }
                        // 对齐 WP：上传完成即回填当前字段并关闭弹窗（不再要求用户再点一次）。
                        if (self._mediaPickTarget && item.url) {
                            var pick = self._mediaPickTarget;
                            self._mediaPickTarget = null;
                            self.closeMediaPicker();
                            pick(item.url, String(item.id || ''), item);
                            return;
                        }
                        self.loadMediaList();
                    })
                    .catch(function () { alert('上传失败'); });
            },

};
