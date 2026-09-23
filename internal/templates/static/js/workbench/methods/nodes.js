// workbench/methods/nodes.js — 节点查找 / 右键菜单 / 选择联动（docs/09 §3 拆分）。
// 方法以 `this` 互调，由 index.js 用 Object.assign 合并为同一个 workbench 实例。
import {
    meta, initialDoc, getCSRFToken, csrfHeaders, wbParseColor, wbHslToHsv, wbHsvToRgb, wbRgbToHsv, wbColorPicker, themePrimary, controlLabel, optionLabel, clone, componentSchemas, CONTROL_LABELS, OPTION_LABELS, paletteItems, paletteGroups, MAX_NEST_DEPTH, WB_CP_CHECKER,
    alignKeyOf, alignFromChildren,
} from '../core.js';

export const nodesMethods = {
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
            // isInsideTarget 落点「插入到内部」的合法目标：布局容器，以及结构型组件
            // （tabs/accordion —— 拖入其内部即新增一个面板 / 折叠项，props 数组同步补一条）。
            isInsideTarget(node) {
                if (!node) return false;
                return node.type === 'core.container' || !!alignKeyOf(node.type);
            },
            // syncAlignFromChildren 画布方向对齐的落地入口：children 已变更 → props 数组跟随。
            // 走 palette.js 的纯函数 alignFromChildren（浏览器与契约测试同一份实现）：
            // 只有能安全定位（变更前数量一致）时才写 props，脏数据返回 false 保持原样。
            syncAlignFromChildren(parent, action) {
                if (!parent) return false;
                var key = alignKeyOf(parent.type);
                if (!key) return false;
                var props = parent.props || (parent.props = {});
                var next = alignFromChildren(parent.type, props[key], parent.children, action);
                if (!next) return false;
                props[next.key] = next.list;
                return true;
            },
            // removeById 画布删除的唯一入口：先把父级（结构型组件）的内容数组同步删掉
            // 对应条目，再摘除节点 —— 删除页签面板 / 折叠项不再留下「标签数 ≠ 面板数」。
            // 顺序：先删 children 再对齐，alignFromChildren 才能看到「变更后」的子节点数。
            removeById(id) {
                var location = this.findLocation(id);
                if (!location) return null;
                var parent = location.parent, index = location.index;
                location.siblings.splice(location.index, 1);
                if (parent) this.syncAlignFromChildren(parent, { op: 'remove', index: index });
                return location;
            },
            containsNode(node, id) {
                if (!node) return false;
                if (node.id === id) return true;
                var self = this;
                return (node.children || []).some(function (child) { return self.containsNode(child, id); });
            },
            // maxDepth 统计某节点子树最大深度（叶子=1，含自身），用于限制容器嵌套层级。
            maxDepth(node) {
                if (!node || !(node.children && node.children.length)) return 1;
                var self = this, max = 0;
                node.children.forEach(function (c) {
                    var d = self.maxDepth(c);
                    if (d > max) max = d;
                });
                return 1 + max;
            },
            moveNode(sourceID, targetID, placement) {
                if (!sourceID || !targetID || sourceID === targetID) return;
                var source = this.findLocation(sourceID);
                var targetLocation = this.findLocation(targetID);
                if (!source || !targetLocation || this.containsNode(source.node, targetID)) return;
                var insideTarget = placement === 'inside' && this.isInsideTarget(targetLocation.node);
                // 嵌套深度守卫：拖入容器 / 结构型组件前校验，超限拒绝（防止无限嵌套）。
                if (insideTarget && this.maxDepth(targetLocation.node) + this.maxDepth(source.node) > MAX_NEST_DEPTH) {
                    console.warn('嵌套层级超出限制（' + MAX_NEST_DEPTH + ' 层），已取消拖放');
                    return;
                }
                this.snapshot();
                // 源父节点与下标必须在摘除前记录：结构型源父要按这个下标删内容数组条目。
                var sourceParent = source.parent, sourceIndex = source.index;
                source.siblings.splice(source.index, 1);
                if (insideTarget) {
                    var dest = targetLocation.node;
                    dest.children = dest.children || [];
                    var at = dest.children.length; // 追加到末尾：新位置就是 children.length
                    dest.children.push(source.node);
                    if (sourceParent === dest) {
                        // 拖到自己内部 = 移到末尾：children 数量不变，按重排同步。
                        this.syncAlignFromChildren(dest, { op: 'move', index: sourceIndex, to: at });
                    } else {
                        // 跨父：目标父（结构型）多一条，源父（结构型）少一条。
                        this.syncAlignFromChildren(dest, { op: 'insert', index: at });
                        if (sourceParent) this.syncAlignFromChildren(sourceParent, { op: 'remove', index: sourceIndex });
                    }
                } else {
                    // 删除源节点后目标索引可能向前移动，重新定位保证顺序稳定。
                    targetLocation = this.findLocation(targetID);
                    var index = targetLocation.index + (placement === 'before' ? 0 : 1);
                    targetLocation.siblings.splice(index, 0, source.node);
                    if (sourceParent && targetLocation.parent === sourceParent) {
                        // 同父重排：children 已换位，内容数组同步换位（标签顺序即面板顺序）。
                        this.syncAlignFromChildren(sourceParent, { op: 'move', index: sourceIndex, to: index });
                    } else {
                        if (sourceParent) this.syncAlignFromChildren(sourceParent, { op: 'remove', index: sourceIndex });
                        if (targetLocation.parent) this.syncAlignFromChildren(targetLocation.parent, { op: 'insert', index: index });
                    }
                }
                this.selectedId = sourceID;
                this.renderTree();
                this.syncInspector();
                this.refreshCanvas();
                this.renderUI();
            },

            // ---------------- 结构树右键菜单（对标 Elementor） ----------------
            // 复制/粘贴/剪切复用既有剪贴板（copyNode/pasteInto 支持子树深拷贝与 ID 重写）。

            // showNodeMenu 渲染右键菜单（单例浮层，点击别处关闭）。
            showNodeMenu(x, y, node) {
                var self = this;
                var old = document.getElementById('wb-context-menu');
                if (old) old.remove();
                var isContainer = node.type === 'core.container';
                var items = [
                    { label: '编辑', action: function () { self.select(node.id); self.showPanel('edit'); } },
                    { label: '复制', action: function () { self.selectedId = node.id; self.copyNode(); } },
                    { label: '剪切', action: function () { self.selectedId = node.id; self.cutNode(); } },
                    { label: '粘贴到内部', enabled: !!self.clipboard && isContainer, action: function () { self.pasteInto(node.id); } },
                    { label: '粘贴到下方', enabled: !!self.clipboard, action: function () { self.pasteAfter(node.id); } },
                    { label: isContainer ? '在内部插入组件' : '在下方插入组件', action: function () {
                        self.pendingInsertTarget = { id: node.id, placement: isContainer ? 'inside' : 'after' };
                        self.showPanel('library');
                    } },
                    { label: '删除', danger: true, action: function () { self.selectedId = node.id; self.deleteSelected(); } }
                ];
                var menu = document.createElement('div');
                menu.id = 'wb-context-menu';
                menu.className = 'wb-context-menu';
                items.forEach(function (item) {
                    var b = document.createElement('button');
                    b.type = 'button';
                    b.textContent = item.label;
                    if (item.danger) b.classList.add('is-danger');
                    if (item.enabled === false) { b.disabled = true; }
                    else b.addEventListener('click', function () { menu.remove(); item.action(); });
                    menu.appendChild(b);
                });
                document.body.appendChild(menu);
                // 边界收拢：避免超出视口。
                var rect = menu.getBoundingClientRect();
                menu.style.left = Math.min(x, window.innerWidth - rect.width - 8) + 'px';
                menu.style.top = Math.min(y, window.innerHeight - rect.height - 8) + 'px';
                setTimeout(function () {
                    document.addEventListener('click', function closer() {
                        menu.remove();
                        document.removeEventListener('click', closer);
                    });
                    document.addEventListener('contextmenu', function closer2(ev) {
                        if (!menu.contains(ev.target)) { menu.remove(); document.removeEventListener('contextmenu', closer2); }
                    });
                }, 0);
            },

            // 同层上移/下移（dir: -1 上移, 1 下移）。
            moveNodeOrder(id, dir) {
                var loc = this.findLocation(id);
                if (!loc) return;
                var target = loc.index + dir;
                if (target < 0 || target >= loc.siblings.length) return;
                this.snapshot();
                loc.siblings.splice(loc.index, 1);
                loc.siblings.splice(target, 0, loc.node);
                // 同层上移/下移同样要带着内容数组走：tabs/accordion 的标签顺序由数组决定，
                // 只挪 children 会让「第 2 个标签」配到「第 1 个面板」。
                if (loc.parent) this.syncAlignFromChildren(loc.parent, { op: 'move', index: loc.index, to: target });
                this.renderTree();
                this.refreshCanvas();
            },

            // ---------------- 选择与联动 ----------------
            select(id) {
                var node = this.findNode(id);
                if (!node || node.locked) return;
                this.selectedSlot = null; // 选中普通节点即退出槽位态（两者互斥）
                this.selectedId = id;
                this.highlightInCanvas(id);
                this.syncInspector();
                this.markTreeSelection();
                // WP 范式：点选组件即进入该组件的编辑面板。
                this.showEdit();
            },

            // selectSlotFrame 选中结构槽位（页眉 / 页脚）。
            //
            // 槽位是编译期注入的只读边界，页面文档里没有它的节点：所以这里不设 selectedId，
            // 只记录 selectedSlot —— 面板据此渲染「这是站点结构的一部分 + 去编辑那个全局块」，
            // 而不是把页眉当成一个可删除、可拖动的本页元素（那正是「文档里也留一份页眉、
            // 于是出现两个页眉」的入口）。
            selectSlotFrame(frame) {
                if (!frame || !frame.slot) return;
                this.selectedId = '';
                this.selectedSlot = {
                    slot: String(frame.slot),
                    ref: frame.ref || '',
                    refKind: frame.refKind || 'block',
                    nodeId: frame.id || '',
                    // 降级占位（块拿不到 / 空块）没有可编辑的目标，面板只做说明。
                    degradable: !!frame.degrade,
                };
                this.highlightInCanvas(this.selectedSlot.nodeId);
                this.markTreeSelection(); // 结构树里没有槽位节点，此举只为清掉旧高亮
                this.showEdit();
            },
};
