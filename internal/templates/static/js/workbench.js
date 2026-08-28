// workbench.js — 可视化编辑器前端状态机（docs/03-A §4/§5）。
//
// 职责边界（静态编译优先）：
//   - 编辑器运行时 UI 交互（大纲树/检查器/快捷键/剪贴板/Undo 栈）；
//   - 产物零脚本：本文件不进入发布 Artifact，仅编辑器外壳使用。
// 数据契约：Page Document JSON（settings + root[]，node: id/type/props/children/name/hidden/locked）。
(function () {
    'use strict';

    /** 元数据与初始 AST。 */
    var meta = JSON.parse(document.getElementById('wb-meta').textContent);
    var initialDoc = null;
    try { initialDoc = JSON.parse(document.getElementById('wb-bootstrap').textContent || '{}'); } catch (e) { /* 空文档兜底 */ }
    if (!initialDoc) initialDoc = { settings: {}, root: [] };
    if (!Array.isArray(initialDoc.root)) initialDoc.root = [];
    if (!initialDoc.settings) initialDoc.settings = {};

    /** 组件 Inspector 面板 schema（docs/02-C3 声明式 Controls，后端 ComponentSchemas 注入）。 */
    var componentSchemas = {};
    try { componentSchemas = JSON.parse(document.getElementById('wb-schemas').textContent || '{}'); } catch (e) { componentSchemas = {}; }

    /** schema key → 面板中文标签（缺省回退 key 本身）。 */
    var CONTROL_LABELS = {
        text: '文本内容', tag: '语义标签', color: '颜色', weight: '字重',
        letterSpacing: '字间距', transform: '大小写转换', lineClamp: '多行截断',
        textShadow: '文字阴影', fontSize: '字号', fontWeight: '字重',
        background: '背景色', border: '边框颜色', shadow: '阴影级别',
        variant: '外观风格', radius: '圆角', hoverLift: '悬停上浮', spacing: '间距',
        hoverShift: '悬停位移', action: '点击动作', value: '跳转地址', target: '打开方式',
        rel: '链接关系', source: '图标来源', name: '名称', assetId: '媒体资源 ID',
        position: '位置', kind: '类型', iconName: '图标样式', align: '对齐',
        style: '样式', mode: '展示模式', aspectRatio: '宽高比', aspectRatioValue: '自定义宽高比',
        objectFit: '填充方式', borderWidth: '边框宽度', borderColor: '边框颜色',
        clickAction: '点击行为', defaultLink: '默认链接', captionMode: '说明方式',
        caption: '说明文字', fallback: '兜底文本', title: '标题', sizes: '响应式尺寸',
        src: '图片地址', alt: '替代文字', width: '宽度', height: '高度',
        inlineSvg: '内联 SVG', sticky: '滚动吸顶', stickyTop: '吸顶偏移',
        entrance: '入场动画', bgGradient: '背景渐变', bgImage: '背景图',
        borderStyle: '边框样式', overlay: '遮罩强度', columns: '栅格列数'
    };
    function controlLabel(key) { return CONTROL_LABELS[key] || key; }

    /** 深拷贝（结构化克隆不可用时的兜底）。 */
    function clone(v) { return v === undefined ? undefined : JSON.parse(JSON.stringify(v)); }

    /** 组件库：仅提供可直接通过 AST 校验的默认节点。 */
    var paletteItems = [
        { type: 'core.container', label: '容器', hint: '布局容器', props: { tag: 'section', layout: { engine: 'flex', flex: { direction: 'column', gap: '16px' } }, box: { padding: { desktop: '32px' } } } },
        { type: 'core.heading', label: '标题', hint: '文字标题', props: { text: '新标题', tag: 'h2' } },
        { type: 'core.text', label: '文本', hint: '正文段落', props: { mode: 'plaintext', plainTag: 'p', text: '在这里输入正文内容。' } },
        { type: 'core.button', label: '按钮', hint: '行动按钮', props: { text: '了解更多', action: 'internal', value: '/' } },
        { type: 'core.image', label: '图片', hint: '外部图片', props: { src: 'https://placehold.co/1200x800/png', alt: '图片占位符', objectFit: 'cover', width: '100%' } },
        { type: 'core.divider', label: '分隔线', hint: '内容分隔', props: { style: 'solid', weight: '1px' } },
        { type: 'core.spacer', label: '间隔', hint: '留白空间', props: { height: { desktop: '32px' } } }
    ];

    /**
     * 工作台状态机：AST 为唯一事实源；画布 iframe 与大纲树均为其投影。
     * Undo 栈保存全量快照（页面规模可控，简单可靠）。
     */
    function workbench() {
        return {
            pageId: meta.pageId,
            draftVersion: meta.version,
            doc: initialDoc,

            // 视图状态
            device: 'desktop',
            tab: 'layout',
            immersive: false,
            navigatorOpen: true,
            filter: '',
            busy: false,
            saveState: '',

            // 选择与剪贴板
            selectedId: '',
            clipboard: null,      // { mode: 'copy'|'cut', node }
            styleClipboard: null, // 仅样式

            // Undo / Redo
            undoStack: [],
            redoStack: [],

            // ------------------------------------------------------------------
            get canUndo() { return this.undoStack.length > 0; },
            get canRedo() { return this.redoStack.length > 0; },
            statusText() {
                if (this.busy) return '处理中…';
                if (this.saveState === 'saved') return '已保存';
                if (this.saveState === 'dirty') return '有未保存修改';
                if (this.saveState === 'error') return '操作失败';
                return '就绪';
            },

            // ---------------- 视图动作 ----------------
            setDevice(d) {
                this.device = d;
                var frame = document.getElementById('wb-canvas-frame');
                if (frame) frame.className = 'wb-canvas-frame is-' + d;
                ['desktop', 'tablet', 'mobile'].forEach(function (name) {
                    var button = document.getElementById('wb-device-' + name);
                    if (button) button.classList.toggle('is-active', name === d);
                });
            },
            renderUI() {
                var status = document.getElementById('wb-status');
                if (status) status.textContent = this.statusText();
                var undo = document.getElementById('wb-undo');
                if (undo) undo.disabled = !this.canUndo;
                var redo = document.getElementById('wb-redo');
                if (redo) redo.disabled = !this.canRedo;
                ['wb-save-draft', 'wb-publish'].forEach(function (id) {
                    var button = document.getElementById(id);
                    if (button) button.disabled = window.__wb && window.__wb.busy;
                });
            },

            // ---------------- 组件库与插入 ----------------
            renderPalette() {
                var root = document.getElementById('wb-palette');
                if (!root) return;
                root.innerHTML = '';
                var self = this;
                paletteItems.forEach(function (item) {
                    var button = document.createElement('button');
                    button.type = 'button';
                    button.className = 'wb-palette-item';
                    button.draggable = true;
                    button.dataset.type = item.type;
                    button.innerHTML = '<strong></strong><span></span>';
                    button.querySelector('strong').textContent = item.label;
                    button.querySelector('span').textContent = item.hint;
                    button.addEventListener('click', function () { self.insertComponent(item); });
                    button.addEventListener('dragstart', function (event) {
                        event.dataTransfer.effectAllowed = 'copy';
                        event.dataTransfer.setData('application/x-wb-component', item.type);
                    });
                    root.appendChild(button);
                });
            },
            insertComponent(item, targetID, placement) {
                if (!item) return;
                this.snapshot();
                var node = {
                    id: this.newId(item.type.replace('core.', '')),
                    type: item.type,
                    name: item.label,
                    props: clone(item.props)
                };
                var targetLocation = this.findLocation(targetID || this.selectedId);
                var target = targetLocation && targetLocation.node;
                if (target && target.type === 'core.container' && placement !== 'before' && placement !== 'after') {
                    target.children = target.children || [];
                    target.children.push(node);
                } else if (targetLocation) {
                    targetLocation.siblings.splice(targetLocation.index + (placement === 'before' ? 0 : 1), 0, node);
                } else {
                    this.ensureRootContainer().children.push(node);
                }
                this.selectedId = node.id;
                this.renderTree();
                this.syncInspector();
                this.refreshCanvas();
                this.renderUI();
            },

            // ---------------- 快照与提交 ----------------
            snapshot() {
                this.undoStack.push(JSON.stringify(this.doc));
                if (this.undoStack.length > 100) this.undoStack.shift();
                this.redoStack.length = 0;
                this.saveState = 'dirty';
            },
            undo() {
                var prev = this.undoStack.pop();
                if (!prev) return;
                this.redoStack.push(JSON.stringify(this.doc));
                this.doc = JSON.parse(prev);
                this.renderTree(); this.refreshCanvas(); this.syncInspector();
                this.saveState = 'dirty';
            },
            redo() {
                var next = this.redoStack.pop();
                if (!next) return;
                this.undoStack.push(JSON.stringify(this.doc));
                this.doc = JSON.parse(next);
                this.renderTree(); this.refreshCanvas(); this.syncInspector();
                this.saveState = 'dirty';
            },

            // ---------------- 节点查找 ----------------
            walk(list, fn, parent) {
                for (var i = 0; i < list.length; i++) {
                    if (fn(list[i], parent, list, i) === false) return false;
                    if (list[i].children && !this.walk(list[i].children, fn, list[i])) return false;
                }
                return true;
            },
            findNode(id) {
                var found = null;
                this.walk(this.doc.root, function (n) { if (n.id === id) { found = n; return false; } });
                return found;
            },
            findParent(id) {
                var found = null;
                this.walk(this.doc.root, function (n, p) {
                    if ((n.children || []).some(function (c) { return c.id === id; })) { found = n; return false; }
                });
                return found;
            },
            findLocation(id) {
                var location = null;
                this.walk(this.doc.root, function (n, p, siblings, idx) {
                    if (n.id === id) { location = { node: n, parent: p, siblings: siblings, index: idx }; return false; }
                });
                return location;
            },
            removeById(id) {
                var location = this.findLocation(id);
                if (location) location.siblings.splice(location.index, 1);
                return location;
            },
            containsNode(node, id) {
                if (!node) return false;
                if (node.id === id) return true;
                var self = this;
                return (node.children || []).some(function (child) { return self.containsNode(child, id); });
            },
            ensureRootContainer() {
                var roots = this.doc.root || [];
                if (roots.length === 1 && roots[0].type === 'core.container') {
                    roots[0].children = roots[0].children || [];
                    return roots[0];
                }
                var container = {
                    id: this.newId('section'),
                    type: 'core.container',
                    name: '页面主体',
                    props: { tag: 'main', layout: { engine: 'flex', flex: { direction: 'column', gap: '16px' } }, box: {} },
                    children: roots.splice(0)
                };
                this.doc.root = [container];
                return container;
            },
            moveNode(sourceID, targetID, placement) {
                if (!sourceID || !targetID || sourceID === targetID) return;
                var source = this.findLocation(sourceID);
                var targetLocation = this.findLocation(targetID);
                if (!source || !targetLocation || this.containsNode(source.node, targetID)) return;
                this.snapshot();
                source.siblings.splice(source.index, 1);
                if (placement === 'inside' && targetLocation.node.type === 'core.container') {
                    targetLocation.node.children = targetLocation.node.children || [];
                    targetLocation.node.children.push(source.node);
                } else {
                    // 删除源节点后目标索引可能向前移动，重新定位保证顺序稳定。
                    targetLocation = this.findLocation(targetID);
                    var index = targetLocation.index + (placement === 'before' ? 0 : 1);
                    targetLocation.siblings.splice(index, 0, source.node);
                }
                this.selectedId = sourceID;
                this.renderTree();
                this.syncInspector();
                this.refreshCanvas();
                this.renderUI();
            },

            // ---------------- 选择与联动 ----------------
            select(id) {
                var node = this.findNode(id);
                if (!node || node.locked) return;
                this.selectedId = id;
                this.highlightInCanvas(id);
                this.syncInspector();
                this.markTreeSelection();
            },
            markTreeSelection() {
                document.querySelectorAll('#wb-tree .wb-node').forEach(function (el) {
                    el.classList.toggle('is-selected', el.dataset.id === window.__wb.selectedId);
                });
            },

            // 大纲树渲染（可被 filter 过滤）
            renderTree() {
                var self = this;
                var rootUl = document.getElementById('wb-tree');
                if (!rootUl) return;
                rootUl.innerHTML = '';
                var f = (this.filter || '').toLowerCase();

                function label(n) {
                    var title = n.name || (n.props && (n.props.text || n.props.alt)) || n.id;
                    return '[' + String(n.type || '').replace('core.', '') + '] ' + title;
                }

                function build(nodes, ul, visibleParent) {
                    nodes.forEach(function (n) {
                        var text = label(n);
                        // 过滤：节点或任一后代命中即显示整条链路。
                        var subtreeHit = !f ||
                            text.toLowerCase().indexOf(f) >= 0 ||
                            JSON.stringify(n.children || []).toLowerCase().indexOf(f) >= 0;
                        if (visibleParent || subtreeHit) {
                            var li = document.createElement('li');
                            var row = document.createElement('div');
                            row.className = 'wb-node' + (self.selectedId === n.id ? ' is-selected' : '');
                            row.dataset.id = n.id;
                            row.draggable = true;
                            row.addEventListener('dragstart', function (event) {
                                event.dataTransfer.effectAllowed = 'move';
                                event.dataTransfer.setData('application/x-wb-node', n.id);
                            });
                            row.addEventListener('dragover', function (event) {
                                if (event.dataTransfer.types.length) {
                                    event.preventDefault();
                                    var bounds = row.getBoundingClientRect();
                                    var offset = event.clientY - bounds.top;
                                    var placement = n.type === 'core.container' && offset > bounds.height * .25 && offset < bounds.height * .75 ? 'inside' : (offset < bounds.height / 2 ? 'before' : 'after');
                                    row.dataset.dropPlacement = placement;
                                    row.classList.add('is-drop-target', 'is-drop-' + placement);
                                }
                            });
                            row.addEventListener('dragleave', function () {
                                row.classList.remove('is-drop-target', 'is-drop-before', 'is-drop-after', 'is-drop-inside');
                                delete row.dataset.dropPlacement;
                            });
                            row.addEventListener('drop', function (event) {
                                event.preventDefault();
                                var placement = row.dataset.dropPlacement || (n.type === 'core.container' ? 'inside' : 'after');
                                row.classList.remove('is-drop-target', 'is-drop-before', 'is-drop-after', 'is-drop-inside');
                                delete row.dataset.dropPlacement;
                                var type = event.dataTransfer.getData('application/x-wb-component');
                                var nodeID = event.dataTransfer.getData('application/x-wb-node');
                                if (type) {
                                    var item = paletteItems.filter(function (entry) { return entry.type === type; })[0];
                                    self.insertComponent(item, n.id, placement);
                                } else if (nodeID) {
                                    self.moveNode(nodeID, n.id, placement);
                                }
                            });

                            var caret = document.createElement('button');
                            caret.className = 'wb-caret';
                            caret.textContent = (n.children && n.children.length) ? '展开' : '';
                            if (n.children && n.children.length) {
                                caret.addEventListener('click', function () {
                                    li.classList.toggle('is-collapsed');
                                    caret.textContent = li.classList.contains('is-collapsed') ? '展开' : '收起';
                                });
                            }
                            row.appendChild(caret);

                            var nameSpan = document.createElement('span');
                            nameSpan.className = 'wb-node-name';
                            nameSpan.textContent = label(n).replace(/^\[[^]]+\] /, '');
                            row.appendChild(nameSpan);

                            var typeSpan = document.createElement('span');
                            typeSpan.className = 'wb-node-type';
                            typeSpan.textContent = String(n.type || '').replace('core.', '');
                            row.appendChild(typeSpan);

                            if (n.hidden) { var eh = document.createElement('span'); eh.textContent = '隐藏'; eh.title = '编辑期隐藏'; row.appendChild(eh); }
                            if (n.locked) { var el = document.createElement('span'); el.textContent = '锁定'; el.title = '已锁定'; row.appendChild(el); }

                            row.addEventListener('click', function () { self.select(n.id); });
                            row.addEventListener('dblclick', function () { self.renameNode(n.id); });

                            li.appendChild(row);
                            if (n.children && n.children.length) {
                                var sub = document.createElement('ul');
                                li.appendChild(sub);
                                build(n.children, sub, visibleParent || (f && subtreeHit));
                            }
                            ul.appendChild(li);
                        }
                    });
                }
                build(self.doc.root || [], rootUl, !!f);
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

            // ---------------- 画布联动 ----------------
            refreshCanvas() {
                var frame = document.getElementById('wb-canvas');
                if (!frame) return;
                var form = document.getElementById('wb-preview-form');
                if (!form) {
                    form = document.createElement('form');
                    form.id = 'wb-preview-form';
                    form.method = 'POST';
                    form.action = '/workbench/preview';
                    form.target = frame.name || 'wb-canvas';
                    form.style.display = 'none';
                    var id = document.createElement('input'); id.name = 'id'; form.appendChild(id);
                    var version = document.createElement('input'); version.name = 'expectedVersion'; form.appendChild(version);
                    var draft = document.createElement('input'); draft.name = 'draftDocument'; form.appendChild(draft);
                    document.body.appendChild(form);
                }
                form.elements.id.value = meta.pageId;
                form.elements.expectedVersion.value = this.draftVersion;
                form.elements.draftDocument.value = JSON.stringify(this.doc);
                form.submit();
            },
            bindCanvasDrop() {
                var frame = document.getElementById('wb-canvas');
                var doc = frame && frame.contentDocument;
                if (!doc || doc.__wbDropBound) return;
                doc.__wbDropBound = true;
                var self = this;
                doc.addEventListener('dragover', function (event) { event.preventDefault(); });
                doc.addEventListener('drop', function (event) {
                    event.preventDefault();
                    var target = event.target.closest && event.target.closest('[data-wp-id]');
                    var targetID = target && target.getAttribute('data-wp-id');
                    var type = event.dataTransfer.getData('application/x-wb-component');
                    var nodeID = event.dataTransfer.getData('application/x-wb-node');
                    if (type) {
                        var item = paletteItems.filter(function (entry) { return entry.type === type; })[0];
                        self.insertComponent(item, targetID, targetID ? undefined : 'inside');
                    } else if (nodeID && targetID) {
                        var targetNode = self.findNode(targetID);
                        self.moveNode(nodeID, targetID, targetNode && targetNode.type === 'core.container' ? 'inside' : 'after');
                    }
                });
            },
            highlightInCanvas(id) {
                var win = document.getElementById('wb-canvas') && document.getElementById('wb-canvas').contentWindow;
                if (!win || !win.document) return;
                var el = win.document.querySelector('[data-wp-id="' + id + '"]');
                if (el) el.scrollIntoView({ behavior: 'smooth', block: 'center' });
            },

            // ---------------- 剪贴板 ----------------
            newId(base) {
                var prefix = (base.split('-')[0] || 'node');
                var n = 1; while (this.findNode(prefix + '-' + n)) n++;
                return prefix + '-' + n;
            },
            deepCopyStripMeta(node) {
                var copy = clone(node);
                copy.id = '';
                copy.locked = false; copy.hidden = false;
                (function strip(list) { (list || []).forEach(function (c) { c.id = ''; c.locked = false; strip(c.children); }); })(copy.children);
                return copy;
            },
            copyNode() {
                var node = this.findNode(this.selectedId); if (!node) return;
                this.clipboard = { mode: 'copy', node: clone(node) };
                this.styleClipboard = clone(node.props || {});
            },
            cutNode() {
                var node = this.findNode(this.selectedId); if (!node) return;
                this.copyNode(); this.clipboard.mode = 'cut';
            },
            pasteInto(parentId) {
                if (!this.clipboard) return;
                this.snapshot();
                var node = this.deepCopyStripMeta(this.clipboard.node);
                var self = this;
                (function assign(list) { (list || []).forEach(function (c) { c.id = self.newId(c.id || 'node'); assign(c.children); }); })([node]);
                if (parentId) {
                    var parent = this.findNode(parentId);
                    parent.children = parent.children || [];
                    parent.children.push(node);
                } else { this.doc.root.push(node); }
                if (this.clipboard.mode === 'cut') { this.removeById(this.clipboard.node.id); this.clipboard = null; }
                this.renderTree(); this.refreshCanvas();
            },
            pasteStyle() {
                var node = this.findNode(this.selectedId);
                if (!node || !this.styleClipboard) return;
                this.snapshot();
                node.props = clone(this.styleClipboard);
                this.refreshCanvas(); this.syncInspector();
            },
            duplicate() {
                var node = this.findNode(this.selectedId); if (!node) return;
                var backupClip = this.clipboard;
                this.clipboard = { mode: 'copy', node: clone(node) };
                var parent = this.findParent(this.selectedId);
                this.pasteInto(parent ? parent.id : '');
                this.clipboard = backupClip;
            },
            deleteSelected() {
                if (!this.selectedId) return;
                this.snapshot();
                this.removeById(this.selectedId);
                this.selectedId = '';
                this.renderTree(); this.refreshCanvas(); this.syncInspector();
            },

            // ---------------- 可视化属性检查器（docs/02-C3 schema 驱动） ----------------
            syncInspector() {
                var panel = document.getElementById('inspector-panel');
                if (!panel) return;
                var node = this.findNode(this.selectedId);
                panel.innerHTML = '';
                var self = this;
                if (!node) {
                    var empty = document.createElement('p');
                    empty.className = 'wb-empty';
                    empty.textContent = '在画布或大纲树中选择组件';
                    panel.appendChild(empty);
                    return;
                }
                node.props = node.props || {};

                function get(path) {
                    return path.split('.').reduce(function (value, key) { return value == null ? undefined : value[key]; }, node);
                }
                function set(path, value) {
                    var parts = path.split('.'); var target = node;
                    for (var i = 0; i < parts.length - 1; i++) {
                        if (!target[parts[i]]) target[parts[i]] = {};
                        target = target[parts[i]];
                    }
                    target[parts[parts.length - 1]] = value;
                }
                function commit(path, value, after) {
                    self.snapshot();
                    set(path, value);
                    if (after) after(value);
                    self.renderTree(); self.refreshCanvas(); self.renderUI();
                }
                function heading(text) {
                    var h = document.createElement('h3'); h.className = 'wb-inspector-section'; h.textContent = text; panel.appendChild(h);
                }
                function field(label, path, kind, choices, after, extra) {
                    var wrap = document.createElement('div'); wrap.className = 'wb-field';
                    var caption = document.createElement('label'); caption.textContent = label; wrap.appendChild(caption);
                    var input = document.createElement(kind === 'textarea' ? 'textarea' : kind === 'select' ? 'select' : 'input');
                    if (kind === 'textarea') input.rows = 4;
                    if (kind === 'number') { input.type = 'number'; }
                    if (extra) { if (extra.min !== undefined) input.min = extra.min; if (extra.max !== undefined) input.max = extra.max; if (extra.step !== undefined) input.step = extra.step; }
                    if (kind === 'select') {
                        choices.forEach(function (choice) {
                            var option = document.createElement('option'); option.value = choice[0]; option.textContent = choice[1]; input.appendChild(option);
                        });
                    }
                    input.value = get(path) == null ? '' : String(get(path));
                    input.addEventListener('change', function () {
                        var value = input.value;
                        if (kind === 'number') value = input.value === '' ? '' : Number(input.value);
                        commit(path, value, after);
                    });
                    wrap.appendChild(input); panel.appendChild(wrap);
                }
                function checkbox(label, path) {
                    var wrap = document.createElement('label'); wrap.className = 'wb-check-field';
                    var input = document.createElement('input'); input.type = 'checkbox'; input.checked = !!get(path);
                    input.addEventListener('change', function () { commit(path, input.checked); });
                    wrap.appendChild(input); wrap.appendChild(document.createTextNode(label)); panel.appendChild(wrap);
                }
                // schema 控件 → 表单字段（key 即 props 顶层键，与后端 JSON 序列化一致）。
                function schemaField(ctl) {
                    var label = controlLabel(ctl.key);
                    var path = 'props.' + ctl.key;
                    if (ctl.kind === 'select') {
                        var choices = (ctl.options || []).map(function (o) { return [o, o === '' ? '（无）' : o]; });
                        if (ctl.default) choices.unshift(['', ctl.default + '（默认）']);
                        field(label, path, 'select', choices);
                    } else if (ctl.kind === 'text') {
                        field(label, path, 'textarea');
                    } else if (ctl.kind === 'int' || ctl.kind === 'slider') {
                        field(label, path, 'number', null, null, { min: ctl.min, max: ctl.max, step: ctl.step || 1 });
                    } else if (ctl.kind === 'bool') {
                        checkbox(label, path);
                    } else {
                        // string / safe / url / regex：文本输入。
                        field(label, path, 'input');
                    }
                }
                // container 布局面板（嵌套结构未走 ct 声明，字段路径与编译端手写对齐）。
                function containerLayout() {
                    field('语义标签', 'props.tag', 'select', [['div', '容器'], ['section', '区块'], ['article', '文章'], ['header', '页头'], ['footer', '页脚'], ['main', '主体']]);
                    heading('Flex 布局');
                    field('排列方向', 'props.layout.flex.direction', 'select', [['column', '纵向'], ['row', '横向']], function () { set('props.layout.engine', 'flex'); });
                    field('主轴分布', 'props.layout.flex.justify', 'select', [['', '（默认）'], ['flex-start', '起始对齐'], ['center', '居中'], ['flex-end', '末端对齐'], ['space-between', '两端分布'], ['space-around', '环绕分布']]);
                    field('交叉对齐', 'props.layout.flex.align', 'select', [['', '（默认）'], ['stretch', '拉伸'], ['center', '居中'], ['flex-start', '起始'], ['flex-end', '末端']]);
                    checkbox('允许换行', 'props.layout.flex.wrap');
                    field('组件间距', 'props.layout.flex.gap', 'input');
                    heading('尺寸与留白');
                    field('内边距（桌面）', 'props.box.padding.desktop', 'input');
                    field('外边距（桌面）', 'props.box.margin.desktop', 'input');
                    field('最小高度', 'props.box.minHeight', 'input');
                }
                // container 视觉面板。
                function containerVisual() {
                    heading('背景');
                    field('背景颜色', 'props.visual.bgColor', 'input');
                    field('背景渐变', 'props.visual.bgGradient', 'input');
                    field('背景图片', 'props.visual.bgImage', 'input');
                    heading('边框与圆角');
                    field('边框宽度', 'props.visual.borderWidth', 'input');
                    field('边框样式', 'props.visual.borderStyle', 'select', [['', '（默认）'], ['solid', '实线'], ['dashed', '虚线'], ['dotted', '点线']]);
                    field('边框颜色', 'props.visual.borderColor', 'input');
                    field('圆角', 'props.visual.radius', 'input');
                    field('阴影', 'props.visual.shadow', 'select', [['', '无'], ['sm', '小'], ['md', '中'], ['lg', '大'], ['xl', '特大']]);
                    heading('交互');
                    checkbox('滚动吸顶', 'props.interaction.sticky');
                    field('吸顶偏移', 'props.interaction.stickyTop', 'input');
                    checkbox('悬停上浮', 'props.interaction.hoverLift');
                    field('入场动画', 'props.interaction.entrance', 'select', [['', '无'], ['fade-in', '淡入'], ['slide-up', '上滑进入']]);
                }

                // divider 嵌入元素面板（Inset 嵌套结构未走 ct 顶层声明）。
                function dividerInset() {
                    heading('嵌入元素');
                    field('嵌入类型', 'props.inset.kind', 'select', [['none', '无'], ['text', '文本'], ['icon', '图标']]);
                    field('嵌入文本', 'props.inset.text', 'input');
                    field('图标样式', 'props.inset.iconName', 'select', [['star', '星形'], ['diamond', '菱形'], ['dot', '圆点']]);
                    field('嵌入位置', 'props.inset.position', 'select', [['center', '居中'], ['left', '靠左'], ['right', '靠右']]);
                    field('两侧留白', 'props.inset.spacing', 'input');
                }
                // divider 嵌入文本样式。
                function dividerInsetStyle() {
                    heading('嵌入文本样式');
                    field('字号', 'props.inset.fontSize', 'input');
                    field('字重', 'props.inset.fontWeight', 'select', [['', '（默认）'], ['400', '常规'], ['500', '中等'], ['600', '半粗'], ['700', '粗体']]);
                    field('颜色', 'props.inset.color', 'input');
                }
                // spacer 高度面板（Responsive 嵌套结构）。
                function spacerPanel() {
                    heading('留白高度');
                    field('桌面端高度', 'props.height.desktop', 'input');
                    field('平板高度', 'props.height.tablet', 'input');
                    field('手机端高度', 'props.height.mobile', 'input');
                }

                var schema = componentSchemas[node.type] || [];
                var groups = { content: [], style: [], advanced: [] };
                schema.forEach(function (ctl) { (groups[ctl.section] || groups.content).push(ctl); });

                var nodeTitle = node.name || String(node.type).replace('core.', '');
                if (this.tab === 'style') {
                    heading(nodeTitle + ' · 样式');
                    if (node.type === 'core.container') containerVisual();
                    if (node.type === 'core.divider') dividerInsetStyle();
                    groups.style.forEach(schemaField);
                    if (!groups.style.length && node.type !== 'core.container' && node.type !== 'core.divider') {
                        var none1 = document.createElement('p'); none1.className = 'wb-empty'; none1.textContent = '该组件暂无独立样式项'; panel.appendChild(none1);
                    }
                } else if (this.tab === 'extend') {
                    heading('编辑元数据');
                    field('显示名称', 'name', 'input');
                    checkbox('在编辑器中隐藏', 'hidden');
                    checkbox('锁定，禁止误选', 'locked');
                    if (groups.advanced.length) {
                        heading('高级属性');
                        groups.advanced.forEach(schemaField);
                    }
                    var actions = document.createElement('div'); actions.className = 'wb-inspector-actions';
                    [['复制组件', self.copyNode.bind(self)], ['粘贴样式', self.pasteStyle.bind(self)]].forEach(function (pair) {
                        var button = document.createElement('button'); button.type = 'button'; button.className = 'wb-btn wb-btn-secondary wb-btn-sm'; button.textContent = pair[0];
                        button.addEventListener('click', pair[1]); actions.appendChild(button);
                    });
                    panel.appendChild(actions);
                } else {
                    heading(nodeTitle + ' · 布局与内容');
                    if (node.type === 'core.container') containerLayout();
                    if (node.type === 'core.divider') dividerInset();
                    if (node.type === 'core.spacer') spacerPanel();
                    groups.content.forEach(schemaField);
                    if (!groups.content.length && node.type !== 'core.container' && node.type !== 'core.divider' && node.type !== 'core.spacer') {
                        var none2 = document.createElement('p'); none2.className = 'wb-empty'; none2.textContent = '该组件暂无内容项'; panel.appendChild(none2);
                    }
                }
            },

            // ---------------- API 接线（草稿保存 / 构建 / 发布） ----------------
            api(path, body, onDone) {
                var self = this;
                self.busy = true;
                self.renderUI();
                fetch('/api/page/' + path, {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(body)
                }).then(function (r) { return r.json().then(function (j) { return { ok: r.ok, j: j }; }); })
                  .then(function (res) {
                      self.busy = false;
                      self.renderUI();
                      if (!res.ok || (res.j.code && res.j.code >= 400)) {
                          self.saveState = 'error';
                          self.renderUI();
                          alert((res.j && res.j.message) || '请求失败');
                          return;
                      }
                      if (onDone) onDone(res.j.data || {});
                      self.renderUI();
                  })
                  .catch(function () { self.busy = false; self.saveState = 'error'; self.renderUI(); });
            },
            saveDraft() {
                var self = this;
                this.api('draft/save', {
                    id: meta.pageId,
                    expectedVersion: this.draftVersion,
                    draftPath: meta.draftPath,
                    draftDocument: this.doc
                }, function (data) {
                    self.draftVersion = data.draftVersion || (self.draftVersion + 1);
                    self.saveState = 'saved';
                    self.refreshCanvas();
                });
            },
            publishFlow() {
                var self = this;
                // 先保存草稿 → Build（预期版本为服务端新版本）→ Publish。
                this.api('draft/save', {
                    id: meta.pageId,
                    expectedVersion: this.draftVersion,
                    draftPath: meta.draftPath,
                    draftDocument: this.doc
                }, function (data) {
                    self.draftVersion = data.draftVersion || (self.draftVersion + 1);
                    self.api('build', { id: meta.pageId, expectedVersion: self.draftVersion }, function () {
                        self.api('publish', { id: meta.pageId }, function () {
                            self.saveState = 'saved';
                        });
                    });
                });
            },
            buildAndPublish() {
                if (confirm('将保存当前草稿并构建发布到线上，确认？')) this.publishFlow();
            },

            // ---------------- 快捷键体系（§5.2） ----------------
            onKeydown(e) {
                var mod = e.ctrlKey || e.metaKey;
                if (!mod) {
                    if (e.key === 'Delete' || e.key === 'Backspace') { this.deleteSelected(); e.preventDefault(); }
                    if (e.key === 'Escape') {
                        var parent = this.findParent(this.selectedId);
                        if (parent) this.select(parent.id);
                    }
                    return;
                }
                switch (e.key.toLowerCase()) {
                    case 's': this.saveDraft(); e.preventDefault(); break;                 // 存草稿
                    case 'p': this.immersive = !this.immersive; e.preventDefault(); break;  // 沉浸折叠
                    case 'z': e.shiftKey ? this.redo() : this.undo(); e.preventDefault(); break;
                    case 'y': this.redo(); e.preventDefault(); break;
                    case 'd': this.duplicate(); e.preventDefault(); break;
                    case 'c': this.copyNode(); break;
                    case 'x': this.cutNode(); break;
                    case 'v':
                        if (e.shiftKey) this.pasteStyle(); else this.pasteInto('');
                        break;
                }
            },

            init() {
                var self = this;
                var hadSingleRootContainer = this.doc.root.length === 1 && this.doc.root[0].type === 'core.container';
                this.ensureRootContainer();
                if (!hadSingleRootContainer) this.saveState = 'dirty';
                window.__wb = this;
                this.setDevice(this.device);
                this.renderPalette();
                this.renderTree();
                this.renderUI();
                document.addEventListener('keydown', function (e) { self.onKeydown(e); });
                // iframe 内点击经 postMessage 上报选中（editor=1 注入桥接脚本）。
                window.addEventListener('message', function (ev) {
                    if (ev.origin !== window.location.origin || !ev.data || ev.data.type !== 'wb-select' || !ev.data.id) return;
                    self.select(ev.data.id);
                });
                var search = document.querySelector('#wb-navigator .wb-search');
                if (search) search.addEventListener('input', function () {
                    self.filter = search.value;
                    self.renderTree();
                });
                // 检查器三个页签：布局(内容) / 样式 / 扩展——切换后重渲染面板。
                var tabKeys = ['layout', 'style', 'extend'];
                var tabButtons = document.querySelectorAll('.wb-tabs button');
                tabButtons.forEach(function (button, index) {
                    button.addEventListener('click', function () {
                        self.tab = tabKeys[index] || 'layout';
                        tabButtons.forEach(function (b, i) { b.classList.toggle('is-active', i === index); });
                        self.syncInspector();
                    });
                });
                var controls = {
                    'wb-device-desktop': function () { self.setDevice('desktop'); },
                    'wb-device-tablet': function () { self.setDevice('tablet'); },
                    'wb-device-mobile': function () { self.setDevice('mobile'); },
                    'wb-save-draft': function () { self.saveDraft(); },
                    'wb-publish': function () { self.buildAndPublish(); },
                    'wb-page-settings': function () { self.saveDraft(); },
                    'wb-navigator-toggle': function () {
                        self.navigatorOpen = !self.navigatorOpen;
                        document.getElementById('wb-navigator').classList.toggle('is-hidden', !self.navigatorOpen);
                    },
                    'wb-navigator-close': function () {
                        self.navigatorOpen = false;
                        document.getElementById('wb-navigator').classList.add('is-hidden');
                    },
                    'wb-undo': function () { self.undo(); self.renderUI(); },
                    'wb-redo': function () { self.redo(); self.renderUI(); },
                    'wb-immersive': function () {
                        self.immersive = !self.immersive;
                        document.getElementById('wb-main').classList.toggle('is-immersive', self.immersive);
                        this.textContent = self.immersive ? '退出沉浸模式' : '沉浸模式 (Ctrl+P)';
                    }
                };
                Object.keys(controls).forEach(function (id) {
                    var button = document.getElementById(id);
                    if (button) button.addEventListener('click', controls[id]);
                });
                // 画布加载后重新绑定拖放，并同步大纲树高亮。
                var frame = document.getElementById('wb-canvas');
                if (frame) frame.addEventListener('load', function () {
                    self.bindCanvasDrop();
                    self.markTreeSelection();
                });
            }
        };
    }

    // 直接装配原生事件，避免宿主 Webview 的 Alpine 表达式缓存影响工作台操作。
    function boot() {
        if (window.__wb) return;
        workbench().init();
    }
    if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', boot);
    else boot();

    window.workbench = workbench;
})();