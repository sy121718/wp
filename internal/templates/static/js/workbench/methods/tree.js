// workbench/methods/tree.js — 结构树（含 HTMX 路径）（docs/09 §3 拆分）。
// 方法以 `this` 互调，由 index.js 用 Object.assign 合并为同一个 workbench 实例。
import {
    meta, initialDoc, getCSRFToken, csrfHeaders, wbParseColor, wbHslToHsv, wbHsvToRgb, wbRgbToHsv, wbColorPicker, themePrimary, controlLabel, optionLabel, clone, componentSchemas, CONTROL_LABELS, OPTION_LABELS, paletteItems, paletteGroups, MAX_NEST_DEPTH, WB_CP_CHECKER,
} from '../core.js';

export const treeMethods = {
            // ---- 结构树 HTMX 路径（docs/09 §3）----
            // 树 HTML 由服务端渲染（fragments/outline_tree），客户端只做一次事件委托：
            // 选中/右键/双击重命名/caret 折叠/拖拽排序（含组件库拖入）。
            renderTreeHtmx(rootUl) {
                var self = this;
                var body = new URLSearchParams();
                body.set('document', JSON.stringify(this.doc));
                body.set('selectedId', this.selectedId || '');
                body.set('filter', this.filter || '');
                fetch('/workbench/outline', {
                    method: 'POST',
                    credentials: 'same-origin',
                    headers: csrfHeaders({ 'Content-Type': 'application/x-www-form-urlencoded' }),
                    body: body.toString()
                }).then(function (r) { return r.text(); }).then(function (html) {
                    // 刻意保留整块 innerHTML，不切 idiomorph morph（docs/06-C §四评估结论）：
                    // 1) 服务端片段（fragments/outline_tree）不给节点输出 id 属性，idiomorph 只能
                    //    退化为「同 tagName 软匹配」，深层 ul/li 增删时可能错配到相邻节点；
                    // 2) 折叠态 is-collapsed 只存在于客户端（服务端每次渲染都是展开的 ▾），
                    //    morph 会用新片段属性覆盖旧节点，折叠态照样丢——morph 解决不了本树的
                    //    真实痛点；
                    // 3) 拖拽态（is-drop-target / data-dropPlacement）与键盘焦点是渲染间的瞬时
                    //    状态，匹配错误会残留脏标记；
                    // 4) renderThemeStructure 在根下追加的「主题结构」li 不在服务端片段里，整块
                    //    替换天然清理，morph 需要额外增删分支。
                    // 后续若服务端给 <div class="wb-node"> 输出稳定 id、且折叠态改为客户端
                    // data-wb-collapsed 集合在渲染后回放，再评估切 morph。
                    rootUl.innerHTML = html;
                    self.bindTreeHtmx(rootUl);
                    self.renderThemeStructure(rootUl);
                }).catch(function () {
                    rootUl.innerHTML = '<li class="wb-empty">结构树加载失败</li>';
                });
            },
            bindTreeHtmx(rootUl) {
                if (rootUl.dataset.wbBound === '1') {
                    // 已绑定：仅修正未命名节点的中文显示名。
                    this.localizeTree(rootUl);
                    return;
                }
                rootUl.dataset.wbBound = '1';
                var self = this;
                rootUl.addEventListener('click', function (e) {
                    var caret = e.target.closest('.wb-caret');
                    if (caret) {
                        var li = caret.closest('li');
                        if (li) {
                            li.classList.toggle('is-collapsed');
                            caret.textContent = li.classList.contains('is-collapsed') ? '▸' : '▾';
                        }
                        e.stopPropagation();
                        return;
                    }
                    var act = e.target.closest('.wb-node-action');
                    if (act) {
                        e.stopPropagation();
                        var arow = act.closest('.wb-node');
                        var aid = arow && arow.dataset.id;
                        if (!aid) return;
                        var op = act.getAttribute('data-wb-op');
                        if (op === 'up') { self.moveNodeOrder(aid, -1); }
                        else if (op === 'down') { self.moveNodeOrder(aid, 1); }
                        else if (op === 'dup') { self.selectedId = aid; self.duplicate(); }
                        else if (op === 'del') { self.selectedId = aid; self.deleteSelected(); }
                        return;
                    }
                    var row = e.target.closest('.wb-node');
                    if (row && row.dataset.id) self.select(row.dataset.id);
                });
                rootUl.addEventListener('dblclick', function (e) {
                    var row = e.target.closest('.wb-node');
                    if (row && row.dataset.id) self.renameNode(row.dataset.id);
                });
                rootUl.addEventListener('contextmenu', function (e) {
                    var row = e.target.closest('.wb-node');
                    if (!row || !row.dataset.id) return;
                    e.preventDefault();
                    e.stopPropagation();
                    self.select(row.dataset.id);
                    var n = self.findNode(row.dataset.id);
                    if (n) self.showNodeMenu(e.clientX, e.clientY, n);
                });
                rootUl.addEventListener('dragstart', function (e) {
                    var row = e.target.closest('.wb-node');
                    if (!row || !row.dataset.id) return;
                    e.dataTransfer.effectAllowed = 'move';
                    e.dataTransfer.setData('application/x-wb-node', row.dataset.id);
                });
                rootUl.addEventListener('dragover', function (e) {
                    var row = e.target.closest('.wb-node');
                    if (!row || !e.dataTransfer.types.length) return;
                    e.preventDefault();
                    var bounds = row.getBoundingClientRect();
                    var offset = e.clientY - bounds.top;
                    var isContainer = row.getAttribute('data-type') === 'core.container';
                    var placement = isContainer && offset > bounds.height * 0.25 && offset < bounds.height * 0.75
                        ? 'inside' : (offset < bounds.height / 2 ? 'before' : 'after');
                    row.dataset.dropPlacement = placement;
                    row.classList.add('is-drop-target', 'is-drop-' + placement);
                });
                rootUl.addEventListener('dragleave', function (e) {
                    var row = e.target.closest('.wb-node');
                    if (!row) return;
                    row.classList.remove('is-drop-target', 'is-drop-before', 'is-drop-after', 'is-drop-inside');
                    delete row.dataset.dropPlacement;
                });
                rootUl.addEventListener('drop', function (e) {
                    var row = e.target.closest('.wb-node');
                    if (!row || !row.dataset.id) return;
                    e.preventDefault();
                    var isContainer = row.getAttribute('data-type') === 'core.container';
                    var placement = row.dataset.dropPlacement || (isContainer ? 'inside' : 'after');
                    row.classList.remove('is-drop-target', 'is-drop-before', 'is-drop-after', 'is-drop-inside');
                    delete row.dataset.dropPlacement;
                    var type = e.dataTransfer.getData('application/x-wb-component');
                    var nodeID = e.dataTransfer.getData('application/x-wb-node');
                    if (type) {
                        var item = self.resolveDropItem(type, e.dataTransfer);
                        if (item) self.insertComponent(item, row.dataset.id, placement);
                    } else if (nodeID) {
                        self.moveNode(nodeID, row.dataset.id, placement);
                    }
                });
                // 键盘可达（a11y P1 收尾）：树节点 role=treeitem + tabindex=0（服务端渲染），
                // 这里用事件委托补方向键移动焦点、Enter 选中、空格折叠、←→ 展开收起、菜单键开右键菜单。
                rootUl.addEventListener('keydown', function (e) {
                    var row = e.target.closest ? e.target.closest('.wb-node') : null;
                    if (!row || !rootUl.contains(row)) return;
                    var rows = Array.prototype.filter.call(rootUl.querySelectorAll('.wb-node'), function (el) {
                        return el.offsetParent !== null;   // 折叠分组内的节点不参与键盘游走
                    });
                    var idx = rows.indexOf(row);
                    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
                        e.preventDefault();
                        var next = rows[idx + (e.key === 'ArrowDown' ? 1 : -1)];
                        if (next) next.focus();
                        return;
                    }
                    if (e.key === 'Enter') {
                        e.preventDefault();
                        if (row.dataset.id) self.select(row.dataset.id);
                        return;
                    }
                    if (e.key === ' ' || e.key === 'Spacebar') {
                        e.preventDefault();
                        var caret = row.querySelector('.wb-caret');
                        if (caret && caret.textContent) caret.click();
                        return;
                    }
                    if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
                        var li = row.closest('li');
                        var caret2 = row.querySelector('.wb-caret');
                        if (!li || !caret2 || !caret2.textContent) return;
                        e.preventDefault();
                        var collapsed = li.classList.contains('is-collapsed');
                        if (e.key === 'ArrowRight' && collapsed) caret2.click();
                        if (e.key === 'ArrowLeft' && !collapsed) caret2.click();
                        return;
                    }
                    if (e.key === 'ContextMenu' || (e.shiftKey && e.key === 'F10')) {
                        e.preventDefault();
                        if (!row.dataset.id) return;
                        self.select(row.dataset.id);
                        var n = self.findNode(row.dataset.id);
                        if (n) {
                            var rect = row.getBoundingClientRect();
                            self.showNodeMenu(rect.left + 12, rect.bottom, n);
                        }
                    }
                });
                this.localizeTree(rootUl);
            },
            // localizeTree 服务端不知道组件中文名（映射表在前端），按 data-type 覆盖未命名节点显示名。
            localizeTree(rootUl) {
                Array.prototype.forEach.call(rootUl.querySelectorAll('.wb-node'), function (row) {
                    var nameEl = row.querySelector('.wb-node-name');
                    if (!nameEl || nameEl.getAttribute('data-named') === '1') return;
                    var t = row.getAttribute('data-type') || '';
                    var zh = controlLabel(t.replace('core.', ''));
                    if (zh) nameEl.textContent = zh;
                });
            },

            // renderThemeStructure 结构面板底部的「主题结构」分组（页眉/页脚）。
            //
            // 页眉/页脚是主题级结构（settings.structure 的块引用），不属于页面文档节点树，
            // 因此不出现在上面的节点树里。这里单独列出，点击**在新标签页打开块编辑**
            //（对标 Elementor：编辑全局部件不离开当前页面，避免丢失未保存的编辑状态）。
            renderThemeStructure(rootUl) {
                var self = this;
                var struct = (this.doc.settings && this.doc.settings.structure) || {};
                var themeStruct = meta.themeSettings || {};
                var slots = [
                    { label: '页眉', key: 'headerBlockId', kind: 'header' },
                    { label: '页脚', key: 'footerBlockId', kind: 'footer' }
                ];
                var items = [];
                slots.forEach(function (s) {
                    // 页面级覆盖优先，回退主题级绑定。
                    var id = struct[s.key] || themeStruct[s.key] || '';
                    if (!id) return;
                    var blk = (meta.blocks || []).find(function (b) { return b.id === id; });
                    items.push({ label: s.label, id: id, name: blk ? blk.name : id.slice(0, 8) + '…' });
                });
                if (!items.length) return;

                var head = document.createElement('li');
                head.className = 'wb-tree-section';
                head.textContent = '主题结构';
                rootUl.appendChild(head);

                items.forEach(function (it) {
                    var li = document.createElement('li');
                    var row = document.createElement('div');
                    row.className = 'wb-node wb-node-slot';
                    row.title = '在新标签页编辑「' + it.name + '」（' + it.label + '块）';
                    var tag = document.createElement('span');
                    tag.className = 'wb-slot-tag';
                    tag.textContent = it.label;
                    var nm = document.createElement('span');
                    nm.className = 'wb-node-name';
                    nm.textContent = it.name;
                    var ext = document.createElement('span');
                    ext.className = 'wb-slot-ext';
                    ext.textContent = '↗';
                    row.appendChild(tag); row.appendChild(nm); row.appendChild(ext);
                    row.addEventListener('click', function () {
                        window.open('/workbench?block=' + encodeURIComponent(it.id), '_blank');
                    });
                    li.appendChild(row);
                    rootUl.appendChild(li);
                });
            },

            renameNode(id) {
                var node = this.findNode(id);
                if (!node) return;
                var name = prompt('节点显示名', node.name || '');
                if (name === null) return;
                this.snapshot();
                node.name = name.slice(0, 100);
                this.renderTree(); this.saveState = 'dirty';
            },

            toggleHidden(id) {
                var node = this.findNode(id); if (!node) return;
                this.snapshot();
                node.hidden = !node.hidden;
                this.renderTree(); this.refreshCanvas();
            },
            toggleLocked(id) {
                var node = this.findNode(id); if (!node) return;
                this.snapshot();
                node.locked = !node.locked;
                this.renderTree();
            },

};
