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
        container: '容器', heading: '标题', text2: '文本', image: '图片', gallery: '图集',
        button: '按钮', divider: '分隔线', spacer: '间隔', globalref: '全局块',
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

    /** 枚举值 → 面板显示名(分段按钮用);缺省回退原值。 */
    var OPTION_LABELS = {
        h1: 'H1', h2: 'H2', h3: 'H3', h4: 'H4', h5: 'H5', h6: 'H6',
        div: '容器', section: '区块', article: '文章', header: '页头', footer: '页脚', main: '主体',
        solid: '实线', dashed: '虚线', dotted: '点线', double: '双线',
        column: '纵向', row: '横向',
        'flex-start': '起始', center: '居中', 'flex-end': '末端',
        'space-between': '两端', 'space-around': '环绕', stretch: '拉伸',
        internal: '站内', external: '外链', anchor: '锚点', native: '电话邮件', modal: '弹窗', link: '链接',
        self: '当前页', blank: '新窗口', none: '无', nofollow: 'nofollow',
        grid: '网格', carousel: '轮播', original: '原始', cover: '填充', contain: '包含', fill: '拉伸满',
        lightbox: '灯箱', sm: '小', md: '中', lg: '大', xl: '特大', xs: '特小',
        solid2: '', underline: '下划线', 'line-through': '删除线',
        uppercase: '大写', lowercase: '小写', capitalize: '首字母',
        richtext: '富文本', plaintext: '纯文本',
        'fade-in': '淡入', 'slide-up': '上滑'
    };
    function optionLabel(v) { return OPTION_LABELS[v] || v; }

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
        { type: 'core.spacer', label: '间隔', hint: '留白空间', props: { height: { desktop: '32px' } } },
        { type: 'core.slider', label: '轮播', hint: '多屏滑动（可嵌套）', props: { perView: { desktop: 1 }, autoplay: 0, showArrows: true, showDots: true, gap: '16px' } },
        { type: 'core.list', label: '列表', hint: '图标/序号/圆点列表', props: { style: 'icon', items: [{ icon: 'check', text: '列表项内容' }] } },
        { type: 'core.infobox', label: '信息框', hint: '图标+标题+文本', props: { icon: 'shield', title: '信息框标题', text: '一句话描述你的服务或卖点。', align: 'center' } },
        { type: 'core.social_buttons', label: '社交图标', hint: '社交平台图标组', props: { color: 'brand', size: '40px', shape: 'circle', items: [{ platform: 'facebook', url: 'https://facebook.com' }, { platform: 'x', url: 'https://x.com' }, { platform: 'instagram', url: 'https://instagram.com' }] } },
        { type: 'core.video', label: '视频', hint: '外链嵌入/本地 MP4', props: { url: 'https://www.youtube.com/watch?v=dQw4w9WgXcQ', controls: true, ratio: '16:9' } },
        { type: 'core.tabs', label: '页签', hint: '多面板切换', props: { tabs: [{ label: '页签一' }, { label: '页签二' }] } },
        { type: 'core.accordion', label: '手风琴', hint: '折叠展开', props: { items: [{ title: '折叠项一', open: true }, { title: '折叠项二' }] } },
        { type: 'core.marquee', label: '跑马灯', hint: '无缝滚动内容', props: { speed: 12, direction: 'left', gap: '24px' } },
        { type: 'core.counter', label: '计数器', hint: '数字统计', props: { start: 0, end: 100, suffix: '+' } }
    ];

    /**
     * 工作台状态机：AST 为唯一事实源；画布 iframe 与大纲树均为其投影。
     * Undo 栈保存全量快照（页面规模可控，简单可靠）。
     */
    /** 组件库分组：基础组件大分类平铺（细分类留给进阶组件，当前无进阶内容）；
     *  「区块」概念归全局块（页眉/页脚/区块）。 */
    var paletteGroups = [
        { key: 'basic', title: '基础组件', types: ['core.container', 'core.heading', 'core.text', 'core.button', 'core.image', 'core.gallery', 'core.divider', 'core.spacer', 'core.slider', 'core.list', 'core.infobox', 'core.social_buttons', 'core.video', 'core.tabs', 'core.accordion', 'core.marquee', 'core.counter'] }
    ];

    /** 区块预设（预组合的全局 section，一键插入整个容器）。
     *  结构与 Page Document 一致：JSON AST 片段。 */
    function workbench() {
        return {
            pageId: meta.pageId,
            draftVersion: meta.version,
            doc: initialDoc,

            // 视图状态
            device: 'desktop',
            tab: 'layout',
            view: 'edit',           // 左侧面板视图：edit / library / settings / global / history
            paletteOpen: { basic: true },  // 组件库手风琴展开状态（默认展开基础组件）
            libraryFilter: '',
            pendingInsertTarget: null, // 画布「+ 插入组件」浮标记住的插入位置
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
                this.renderPaletteComponents();
                this.renderPaletteBlocks();
            },
            // 组件页签：按类型手风琴分组（布局/文本/媒体/元素/区块），点击标题展开收起。
            renderPaletteComponents() {
                var root = document.getElementById('wb-palette');
                if (!root) return;
                root.innerHTML = '';
                var self = this;
                var f = (this.libraryFilter || '').toLowerCase();
                function match(text) { return !f || text.toLowerCase().indexOf(f) >= 0; }
                function makeGroup(title, key, fill) {
                    var hits = fill();
                    if (!hits.length) return;
                    var open = !!f || !!self.paletteOpen[key]; // 搜索时全部展开
                    var head = document.createElement('button');
                    head.type = 'button';
                    head.className = 'wb-palette-group' + (open ? ' is-open' : '');
                    head.innerHTML = '<span class="wb-palette-caret"></span>';
                    head.appendChild(document.createTextNode(title));
                    var body = document.createElement('div');
                    body.className = 'wb-palette-group-body';
                    if (!open) body.style.display = 'none';
                    head.addEventListener('click', function () {
                        self.paletteOpen[key] = !self.paletteOpen[key];
                        head.classList.toggle('is-open', self.paletteOpen[key]);
                        body.style.display = self.paletteOpen[key] ? '' : 'none';
                    });
                    hits.forEach(function (el) { body.appendChild(el); });
                    root.appendChild(head);
                    root.appendChild(body);
                }
                function makeItem(item) {
                    var button = document.createElement('button');
                    button.type = 'button';
                    button.className = 'wb-palette-item';
                    button.draggable = true;
                    button.dataset.type = item.type;
                    button.innerHTML = '<strong></strong><span></span>';
                    button.querySelector('strong').textContent = item.label;
                    button.querySelector('span').textContent = item.hint;
                    button.addEventListener('click', function () {
                        var pending = self.pendingInsertTarget;
                        if (pending && self.findNode(pending.id)) {
                            self.insertComponent(item, pending.id, pending.placement);
                            self.pendingInsertTarget = null;
                        } else {
                            self.insertComponent(item);
                        }
                        self.showEdit();
                    });
                    button.addEventListener('dragstart', function (event) {
                        event.dataTransfer.effectAllowed = 'copy';
                        event.dataTransfer.setData('application/x-wb-component', item.type);
                    });
                    return button;
                }
                var any = false;
                paletteGroups.forEach(function (group) {
                    makeGroup(group.title, group.key, function () {
                        return group.types.map(function (type) {
                            return paletteItems.filter(function (e) { return e.type === type; })[0];
                        }).filter(function (item) { return item && match(item.label + item.hint + item.type); })
                          .map(makeItem);
                    });
                    any = any || root.lastChild !== null;
                });
                if (root.children.length === 0) {
                    root.innerHTML = '<p class="wb-empty">没有匹配的组件</p>';
                }
            },
            // 全局块页签：页眉/页脚/区块分组（拖拽或点击插入 globalref 引用）。
            renderPaletteBlocks() {
                var root = document.getElementById('wb-palette-blocks');
                if (!root) return;
                root.innerHTML = '';
                var self = this;
                var f = (this.libraryFilter || '').toLowerCase();
                function match(text) { return !f || text.toLowerCase().indexOf(f) >= 0; }
                function makeBlockButton(b) {
                    var button = document.createElement('button');
                    button.type = 'button';
                    button.className = 'wb-palette-item';
                    button.draggable = true;
                    button.dataset.type = 'core.globalref';
                    button.innerHTML = '<strong></strong><span></span>';
                    button.querySelector('strong').textContent = b.name;
                    button.querySelector('span').textContent = '全局块 · ' + (b.kind === 'header' ? '页眉' : b.kind === 'footer' ? '页脚' : '区块');
                    button.addEventListener('click', function () {
                        self.insertComponent(self.globalRefItem(b));
                        self.showEdit();
                    });
                    button.addEventListener('dragstart', function (event) {
                        event.dataTransfer.effectAllowed = 'copy';
                        event.dataTransfer.setData('application/x-wb-component', 'core.globalref');
                        event.dataTransfer.setData('application/x-wb-block', b.id);
                    });
                    return button;
                }
                var any = false;
                [['header', '页眉'], ['footer', '页脚'], ['block', '区块']].forEach(function (g) {
                    var hits = (meta.blocks || []).filter(function (b) { return b.kind === g[0] && match(b.name); });
                    if (!hits.length) return;
                    any = true;
                    var h = document.createElement('div');
                    h.className = 'wb-palette-group';
                    h.textContent = g[1];
                    root.appendChild(h);
                    hits.forEach(function (b) { root.appendChild(makeBlockButton(b)); });
                });
                if (!any) root.innerHTML = '<p class="wb-empty">还没有全局块。到后台「全局块」创建页眉/页脚/区块后，这里可一键引用。</p>';
            },
            // globalRefItem 全局块的插入描述（引用节点只带 blockId）。
            globalRefItem(b) {
                return { type: 'core.globalref', label: b.name, hint: '全局块', props: { blockId: b.id } };
            },
            // resolveDropItem 拖入落点解析：全局块按 DataTransfer 里的块 ID 匹配。
            resolveDropItem(type, dataTransfer) {
                var self = this;
                if (type === 'core.globalref') {
                    var bid = dataTransfer.getData('application/x-wb-block');
                    return (meta.blocks || []).filter(function (b) { return b.id === bid; })
                        .map(function (b) { return self.globalRefItem(b); })[0];
                }
                return paletteItems.filter(function (entry) { return entry.type === type; })[0];
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
            moveNodeOrder(id, dir) {                var loc = this.findLocation(id);
                if (!loc) return;
                var target = loc.index + dir;
                if (target < 0 || target >= loc.siblings.length) return;
                this.snapshot();
                loc.siblings.splice(loc.index, 1);
                loc.siblings.splice(target, 0, loc.node);
                this.renderTree();
                this.refreshCanvas();
            },

            // ---------------- 选择与联动 ----------------
            select(id) {
                var node = this.findNode(id);
                if (!node || node.locked) return;
                this.selectedId = id;
                this.highlightInCanvas(id);
                this.syncInspector();
                this.markTreeSelection();
                // WP 范式：点选组件即进入该组件的编辑面板。
                this.showEdit();
            },
            // ---- 左侧面板双视图切换 ----
            // ---- 左侧面板统一切换（library/edit/settings/global/history） ----
            showPanel(view) {
                var views = ['library', 'edit', 'settings', 'global', 'history'];
                views.forEach(function (v) {
                    var el = document.getElementById('wb-panel-' + v);
                    if (el) el.hidden = (v !== view);
                });
                // 图标栏高亮：编辑视图归入「组件」入口。
                var railView = (view === 'edit') ? 'library' : view;
                document.querySelectorAll('.wb-rail-btn').forEach(function (b) {
                    b.classList.toggle('is-active', b.id === 'wb-rail-' + railView);
                });
                if (view === 'library') this.showLibraryPanel();
                if (view === 'edit') this.showEditPanel();
                if (view === 'settings') this.renderSettingsPanel();
                if (view === 'global') this.renderGlobalPanel();
                if (view === 'history') this.loadHistory();
            },
            showLibraryPanel() {
                var hint = document.getElementById('wb-library-hint');
                if (hint) {
                    var pending = this.pendingInsertTarget;
                    if (pending) {
                        var node = this.findNode(pending.id);
                        hint.textContent = '点击组件，插入到「' + (node ? (node.name || node.id) : pending.id) + '」' + (pending.placement === 'inside' ? '内部' : '之后');
                        hint.style.display = 'block';
                    } else {
                        hint.style.display = 'none';
                    }
                }
                var search = document.getElementById('wb-library-search');
                if (search) search.focus();
            },
            showEditPanel() {
                var node = this.findNode(this.selectedId);
                var title = document.getElementById('wb-edit-title');
                if (title) title.textContent = node ? (controlLabel(String(node.type).replace('core.', '')) + ' · ' + (node.name || node.id)) : '组件';
                this.syncInspector();
            },
            showLibrary() { this.showPanel('library'); },
            showEdit() { this.showPanel('edit'); },

            // ---- 页面设置面板：SEO / 版心 / body 类名（改动写入草稿） ----
            renderSettingsPanel() {
                var body = document.getElementById('wb-settings-body');
                if (!body) return;
                var self = this;
                var doc = this.doc;
                if (!doc.settings) doc.settings = {};
                if (!doc.settings.seo) doc.settings.seo = {};
                if (!doc.settings.layout) doc.settings.layout = {};
                body.innerHTML = '';

                function setByPath(path, value) {
                    var parts = path.split('.'); var target = doc;
                    for (var i = 0; i < parts.length - 1; i++) {
                        if (!target[parts[i]]) target[parts[i]] = {};
                        target = target[parts[i]];
                    }
                    target[parts[parts.length - 1]] = value;
                    self.saveState = 'dirty';
                    self.refreshCanvas();
                }
                function bind(labelText, path, kind, placeholder) {
                    var wrap = document.createElement('div'); wrap.className = 'wb-field';
                    var label = document.createElement('label'); label.textContent = labelText; wrap.appendChild(label);
                    var input = document.createElement(kind === 'textarea' ? 'textarea' : 'input');
                    if (kind === 'textarea') input.rows = 3;
                    input.placeholder = placeholder || '';
                    var parts = path.split('.'); var cur = doc;
                    for (var i = 0; i < parts.length; i++) { cur = cur == null ? undefined : cur[parts[i]]; }
                    input.value = cur == null ? '' : String(cur);
                    input.addEventListener('change', function () {
                        self.snapshot();
                        setByPath(path, input.value);
                    });
                    wrap.appendChild(input);
                    body.appendChild(wrap);
                }
                // 版心模式。
                var modeWrap = document.createElement('div'); modeWrap.className = 'wb-field wb-field-seg';
                var modeLabel = document.createElement('label'); modeLabel.textContent = '版心模式'; modeWrap.appendChild(modeLabel);
                var seg = document.createElement('div'); seg.className = 'wb-seg';
                [['full', '全宽'], ['boxed', '定宽']].forEach(function (pair) {
                    var b = document.createElement('button'); b.type = 'button'; b.className = 'wb-seg-btn';
                    b.textContent = pair[1];
                    if ((doc.settings.layout.mode || 'full') === pair[0]) b.classList.add('is-active');
                    b.addEventListener('click', function () {
                        self.snapshot();
                        doc.settings.layout.mode = pair[0];
                        seg.querySelectorAll('.wb-seg-btn').forEach(function (x) { x.classList.toggle('is-active', x === b); });
                        self.saveState = 'dirty';
                        self.refreshCanvas();
                    });
                    seg.appendChild(b);
                });
                modeWrap.appendChild(seg); body.appendChild(modeWrap);

                bind('SEO 标题', 'settings.seo.title', 'input', '浏览器标签页与搜索结果标题');
                bind('SEO 描述', 'settings.seo.description', 'textarea', '搜索结果里的摘要文案');
                // Body 附加类名（数组存储，输入以空格分隔）。
                var bcWrap = document.createElement('div'); bcWrap.className = 'wb-field';
                var bcLabel = document.createElement('label'); bcLabel.textContent = 'Body 附加类名'; bcWrap.appendChild(bcLabel);
                var bcInput = document.createElement('input'); bcInput.type = 'text';
                bcInput.placeholder = '空格分隔，如 hero dark';
                bcInput.value = (doc.settings.bodyClasses || []).join(' ');
                bcInput.addEventListener('change', function () {
                    self.snapshot();
                    setByPath('settings.bodyClasses', bcInput.value.split(/\s+/).filter(Boolean));
                });
                bcWrap.appendChild(bcInput); body.appendChild(bcWrap);

                var tip = document.createElement('p'); tip.className = 'wb-empty';
                tip.textContent = '改动已写入草稿，点右上「存草稿」保存；发布后生效。';
                body.appendChild(tip);
            },

            // ---- 全局设置面板：站点主题颜色/字体（保存后合入全部页面） ----
            renderGlobalPanel() {
                var body = document.getElementById('wb-global-body');
                if (!body) return;
                var self = this;
                body.innerHTML = '';
                if (!meta.themeId) {
                    body.innerHTML = '<p class="wb-empty">当前页面未挂接主题。<br>到后台「主题管理」创建并激活主题后，这里可就地调整。</p>';
                    return;
                }
                var t = meta.themeSettings || {};
                var colors = t.colors || {};
                var colorDefs = [['primary', '主色'], ['text', '文本色'], ['background', '页面背景'], ['surface', '卡片底色'], ['border', '边框色']];
                var inputs = {};
                colorDefs.forEach(function (def) {
                    var wrap = document.createElement('div'); wrap.className = 'wb-field wb-color-row';
                    var label = document.createElement('label'); label.textContent = def[1];
                    label.style.flex = '0 0 72px';
                    wrap.appendChild(label);
                    var text = document.createElement('input'); text.type = 'text'; text.className = 'wb-color-text';
                    text.value = colors[def[0]] || '';
                    var swatch = document.createElement('input'); swatch.type = 'color'; swatch.className = 'wb-color-swatch';
                    swatch.value = /^#[0-9a-fA-F]{3,8}$/.test(text.value) ? text.value : '#2563eb';
                    swatch.addEventListener('input', function () { text.value = swatch.value; });
                    wrap.appendChild(text); wrap.appendChild(swatch);
                    body.appendChild(wrap);
                    inputs[def[0]] = text;
                });
                var fontWrap = document.createElement('div'); fontWrap.className = 'wb-field';
                var fontLabel = document.createElement('label'); fontLabel.textContent = '正文字体栈'; fontWrap.appendChild(fontLabel);
                var fontInput = document.createElement('input'); fontInput.type = 'text';
                fontInput.placeholder = '如：system-ui, sans-serif';
                fontInput.value = t.fontFamily || '';
                fontWrap.appendChild(fontInput); body.appendChild(fontWrap);

                var saveBtn = document.createElement('button'); saveBtn.type = 'button';
                saveBtn.className = 'btn btn-primary'; saveBtn.textContent = '保存并应用到全部页面';
                saveBtn.addEventListener('click', function () {
                    var form = new URLSearchParams();
                    form.append('id', meta.themeId);
                    colorDefs.forEach(function (def) { form.append(def[0], inputs[def[0]].value.trim()); });
                    form.append('fontFamily', fontInput.value.trim());
                    // 透传页眉页脚块绑定（本面板不改，避免被覆盖清空）。
                    form.append('headerBlockId', t.headerBlockId || '');
                    form.append('footerBlockId', t.footerBlockId || '');
                    saveBtn.disabled = true; saveBtn.textContent = '保存中…';
                    fetch('/admin/themes/settings/save', { method: 'POST', body: form })
                        .then(function (r) {
                            saveBtn.disabled = false; saveBtn.textContent = '保存并应用到全部页面';
                            if (!r.ok) { alert('保存失败，请重试'); return; }
                            meta.themeSettings = {
                                colors: (function () { var c = {}; colorDefs.forEach(function (d) { c[d[0]] = inputs[d[0]].value.trim(); }); return c; })(),
                                fontFamily: fontInput.value.trim(),
                                headerBlockId: t.headerBlockId || '',
                                footerBlockId: t.footerBlockId || ''
                            };
                            alert('已保存，主题将合入全部页面；页面需重新构建后生效。');
                        })
                        .catch(function () { saveBtn.disabled = false; saveBtn.textContent = '保存并应用到全部页面'; alert('保存失败，请重试'); });
                });
                body.appendChild(saveBtn);
                var tip = document.createElement('p'); tip.className = 'wb-empty';
                tip.textContent = '保存后主题设置批量合入全部页面文档，已发布页面需重新构建才会带新主题。';
                body.appendChild(tip);
            },

            // ---- 修订历史面板：草稿快照列表 + 一键恢复 ----
            loadHistory() {
                var body = document.getElementById('wb-history-body');
                if (!body) return;
                var self = this;
                body.innerHTML = '<p class="wb-empty">加载中…</p>';
                fetch('/api/page/revision/list?pageId=' + encodeURIComponent(meta.pageId))
                    .then(function (r) { return r.json(); })
                    .then(function (j) {
                        if (j.code && j.code >= 400) {
                            body.innerHTML = '<p class="wb-empty">' + (j.message || '加载失败') + '</p>';
                            return;
                        }
                        var list = j.data || [];
                        body.innerHTML = '';
                        if (!list.length) {
                            body.innerHTML = '<p class="wb-empty">还没有修订历史。每次保存草稿都会生成一条快照。</p>';
                            return;
                        }
                        list.forEach(function (rev) {
                            var row = document.createElement('div'); row.className = 'wb-history-row';
                            var head = document.createElement('div'); head.className = 'wb-history-head';
                            var v = document.createElement('strong'); v.textContent = 'v' + rev.version; head.appendChild(v);
                            var info = document.createElement('span'); info.className = 'wb-history-info';
                            info.textContent = (rev.draftPath || '') + ' · ' + new Date(rev.createdAt).toLocaleString();
                            head.appendChild(info);
                            var restore = document.createElement('button');
                            restore.type = 'button'; restore.className = 'btn btn-secondary btn-sm'; restore.textContent = '恢复';
                            restore.addEventListener('click', function () {
                                if (!confirm('恢复到 v' + rev.version + '？当前草稿将覆盖保存为新修订。')) return;
                                self.api('draft/save', {
                                    id: meta.pageId,
                                    expectedVersion: self.draftVersion,
                                    draftPath: rev.draftPath || meta.draftPath,
                                    draftDocument: rev.draftDocument
                                }, function (data) {
                                    self.draftVersion = data.draftVersion || (self.draftVersion + 1);
                                    self.doc = rev.draftDocument;
                                    self.ensureRootContainer();
                                    self.selectedId = null;
                                    self.renderTree();
                                    self.refreshCanvas();
                                    self.renderUI();
                                    self.loadHistory();
                                });
                            });
                            head.appendChild(restore);
                            row.appendChild(head);
                            body.appendChild(row);
                        });
                    })
                    .catch(function () { body.innerHTML = '<p class="wb-empty">加载失败</p>'; });
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
                    // 结构树只显示一层名称：用户命名 > 组件中文名 > 节点 ID。
                    return n.name || controlLabel(String(n.type || '').replace('core.', '')) || n.id;
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
                                    var item = self.resolveDropItem(type, event.dataTransfer);
                                    if (item) self.insertComponent(item, n.id, placement);
                                } else if (nodeID) {
                                    self.moveNode(nodeID, n.id, placement);
                                }
                            });

                            var caret = document.createElement('button');
                            caret.className = 'wb-caret';
                            caret.textContent = (n.children && n.children.length) ? '▾' : '';
                            caret.title = '展开/收起';
                            if (n.children && n.children.length) {
                                caret.addEventListener('click', function (e) {
                                    e.stopPropagation();
                                    li.classList.toggle('is-collapsed');
                                    caret.textContent = li.classList.contains('is-collapsed') ? '▸' : '▾';
                                });
                            }
                            row.appendChild(caret);

                            var nameSpan = document.createElement('span');
                            nameSpan.className = 'wb-node-name';
                            nameSpan.textContent = label(n);
                            row.appendChild(nameSpan);

                            if (n.hidden) { var eh = document.createElement('span'); eh.textContent = '隐'; eh.className = 'wb-node-flag'; eh.title = '编辑期隐藏'; row.appendChild(eh); }
                            if (n.locked) { var el = document.createElement('span'); el.textContent = '锁'; el.className = 'wb-node-flag'; eh.title = '已锁定'; row.appendChild(el); }

                            // 右键菜单（对标 Elementor）：编辑/复制/粘贴/复制到此下/下方插入/删除。
                            row.addEventListener('contextmenu', function (event) {
                                event.preventDefault();
                                event.stopPropagation();
                                self.select(n.id);
                                self.showNodeMenu(event.clientX, event.clientY, n);
                            });

                            // 悬浮操作:上移/下移/复制/删除(触屏与鼠标通用,不依赖拖拽)。
                            var actions = document.createElement('span');
                            actions.className = 'wb-node-actions';
                            [['↑', '上移', function () { self.moveNodeOrder(n.id, -1); }],
                             ['↓', '下移', function () { self.moveNodeOrder(n.id, 1); }],
                             ['⧉', '复制', function () { self.selectedId = n.id; self.duplicate(); }],
                             ['✕', '删除', function () { self.selectedId = n.id; self.deleteSelected(); }]].forEach(function (pair) {
                                var ab = document.createElement('button');
                                ab.type = 'button'; ab.className = 'wb-node-action'; ab.textContent = pair[0]; ab.title = pair[1];
                                ab.addEventListener('click', function (e) { e.stopPropagation(); pair[2](); });
                                actions.appendChild(ab);
                            });
                            row.appendChild(actions);

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
                function clearDropMarks(scope) {
                    (scope || doc).querySelectorAll('.wb-drop-before,.wb-drop-after,.wb-drop-inside').forEach(function (el) {
                        el.classList.remove('wb-drop-before', 'wb-drop-after', 'wb-drop-inside');
                    });
                }
                doc.addEventListener('dragover', function (event) {
                    event.preventDefault();
                    var target = event.target.closest && event.target.closest('[data-wp-id]');
                    clearDropMarks();
                    if (!target) return;
                    var bounds = target.getBoundingClientRect();
                    var offset = event.clientY - bounds.top;
                    var isContainer = (target.getAttribute('data-wp-id') && (self.findNode(target.getAttribute('data-wp-id')) || {}).type === 'core.container');
                    var placement = isContainer && offset > bounds.height * .25 && offset < bounds.height * .75 ? 'inside' : (offset < bounds.height / 2 ? 'before' : 'after');
                    target.classList.add('wb-drop-' + placement);
                });
                doc.addEventListener('dragleave', function (event) {
                    if (!event.relatedTarget) clearDropMarks();
                });
                doc.addEventListener('drop', function (event) {
                    event.preventDefault();
                    clearDropMarks();
                    var target = event.target.closest && event.target.closest('[data-wp-id]');
                    var targetID = target && target.getAttribute('data-wp-id');
                    var type = event.dataTransfer.getData('application/x-wb-component');
                    // 树/画布内元素拖动(x-wb-node)由 iframe 桥接统一处理,此处只接组件库拖入。
                    if (!type) return;
                    var item = self.resolveDropItem(type, event.dataTransfer);
                    if (!item) return;
                    if (targetID) {
                        var targetNode = self.findNode(targetID);
                        var isContainer = targetNode && targetNode.type === 'core.container';
                        var bounds = target.getBoundingClientRect();
                        var offset = event.clientY - bounds.top;
                        var placement = isContainer && offset > bounds.height * .25 && offset < bounds.height * .75 ? 'inside' : (offset < bounds.height / 2 ? 'before' : 'after');
                        self.insertComponent(item, targetID, placement);
                    } else {
                        self.insertComponent(item);
                    }
                });
            },
            highlightInCanvas(id) {
                var win = document.getElementById('wb-canvas') && document.getElementById('wb-canvas').contentWindow;
                if (!win || !win.document) return;
                var el = win.document.querySelector('[data-wp-id="' + id + '"]');
                if (el) el.scrollIntoView({ behavior: 'smooth', block: 'center' });
                // 通知 iframe 桥接层更新选中描边与「+ 插入组件」浮标位置。
                win.postMessage({ type: 'wb-mark-selected', id: id }, window.location.origin);
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
            // pasteAfter 把剪贴板内容粘贴为目标节点之后的兄弟（对标 Elementor 粘贴语义）。
            pasteAfter(nodeId) {
                if (!this.clipboard) return;
                this.snapshot();
                var node = this.deepCopyStripMeta(this.clipboard.node);
                var self = this;
                (function assign(list) { (list || []).forEach(function (c) { c.id = self.newId(c.id || 'node'); assign(c.children); }); })([node]);
                var loc = this.findLocation(nodeId);
                if (!loc) return;
                loc.siblings.splice(loc.index + 1, 0, node);
                if (this.clipboard.mode === 'cut') { this.removeById(this.clipboard.node.id); this.clipboard = null; }
                this.selectedId = node.id;
                this.renderTree(); this.syncInspector(); this.refreshCanvas(); this.renderUI();
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
                // 面板重建前销毁富文本编辑器实例，避免残留 DOM 与事件。
                if (window.tinymce) { try { window.tinymce.remove(); } catch (e) {} }
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
                    if (kind === 'segment') {
                        // 分段按钮组:choices = [[value, text]],当前值高亮。
                        var seg = document.createElement('div'); seg.className = 'wb-seg';
                        var current = get(path) == null ? '' : String(get(path));
                        var segBtns = [];
                        choices.forEach(function (ch) {
                            var b = document.createElement('button');
                            b.type = 'button'; b.className = 'wb-seg-btn'; b.textContent = ch[1]; b.title = ch[0];
                            if (ch[0] === current || (!current && ch[2])) b.classList.add('is-active');
                            b.addEventListener('click', function () {
                                commit(path, ch[0], after);
                                segBtns.forEach(function (x) { x.classList.toggle('is-active', x === b); });
                            });
                            segBtns.push(b); seg.appendChild(b);
                        });
                        var cap = document.createElement('label'); cap.textContent = label;
                        wrap.appendChild(cap); wrap.appendChild(seg); panel.appendChild(wrap);
                        return;
                    }
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
                    wrap.appendChild(input);
                    // 颜色类字段：色板 + 文本组合（支持 var(--token) 主题变量）。
                    var colorKeyMatch = path.match(/\.(color|bgColor|borderColor|titleColor|border)$/i) || /^props\.(color|bgColor|background)$/.test(path);
                    if (kind === 'input' && colorKeyMatch) {
                        var cWrap = document.createElement('div'); cWrap.className = 'wb-color-row';
                        var swatch = document.createElement('input'); swatch.type = 'color'; swatch.className = 'wb-color-swatch';
                        var raw = get(path) == null ? '' : String(get(path)).trim();
                        swatch.value = /^#[0-9a-fA-F]{3,8}$/.test(raw) ? raw : '#2563eb';
                        input.classList.add('wb-color-text');
                        swatch.addEventListener('input', function () {
                            input.value = swatch.value;
                            input.dispatchEvent(new Event('change'));
                        });
                        cWrap.appendChild(input); cWrap.appendChild(swatch);
                        wrap.appendChild(cWrap);
                        panel.appendChild(wrap);
                        return;
                    }
                    // 媒体类字段：缩略图预览 + 媒体库选择 + 清除（对齐 Elementor 图片控件）。
                    // assetId 尾缀：媒体库回填 ID（构建期解析变体），并提供「外部地址」次级输入（写 src 互斥清空）。
                    // src/bgImage 尾缀：直接回填 URL（画布/产物直出）。
                    if (kind === 'input' && /\.(assetId|src|bgImage)$/.test(path)) {
                        var isAssetID = /\.assetId$/.test(path);
                        var srcPath = path.replace(/\.assetId$/, '.src');
                        var setPreview = function (url) {
                            previewImg.src = url || '';
                            previewImg.classList.toggle('is-empty', !previewImg.src);
                            previewTip.textContent = previewImg.src ? '' : '点击选择图片';
                        };
                        var applyPick = function (url, assetId) {
                            if (isAssetID) {
                                // 媒体库选择与外部地址互斥：选库清外部地址，反之亦然。
                                commit(path, assetId);
                                commit(srcPath, '');
                                input.value = assetId;
                                setPreview(url);
                            } else {
                                commit(path, url);
                                input.value = url;
                                setPreview(url);
                            }
                        };
                        var previewBox = document.createElement('div');
                        previewBox.className = 'wb-media-field' + (input.value ? ' has-image' : '');
                        var previewImg = document.createElement('img');
                        previewImg.alt = '';
                        var initialURL = self.resolveAssetUrl(input.value);
                        if (initialURL) previewImg.src = initialURL; else previewImg.classList.add('is-empty');
                        var pickFromPreview = function () {
                            self.openMediaPicker(applyPick);
                        };
                        previewBox.appendChild(previewImg);
                        var previewTip = document.createElement('span');
                        previewTip.className = 'wb-media-tip';
                        previewTip.textContent = initialURL ? '' : '点击选择图片';
                        previewBox.appendChild(previewTip);
                        previewBox.addEventListener('click', pickFromPreview);
                        var pickBtn = document.createElement('button');
                        pickBtn.type = 'button'; pickBtn.className = 'wb-btn wb-btn-secondary wb-btn-sm';
                        pickBtn.textContent = '媒体库';
                        pickBtn.addEventListener('click', pickFromPreview);
                        var clearBtn = document.createElement('button');
                        clearBtn.type = 'button'; clearBtn.className = 'wb-btn wb-btn-ghost wb-btn-sm';
                        clearBtn.textContent = '清除';
                        clearBtn.addEventListener('click', function () {
                            input.value = '';
                            commit(path, '');
                            previewImg.src = '';
                            previewImg.classList.add('is-empty');
                            previewTip.textContent = '点击选择图片';
                        });
                        var row = document.createElement('div'); row.className = 'wb-media-row';
                        row.appendChild(pickBtn); row.appendChild(clearBtn);
                        wrap.removeChild(input);
                        wrap.appendChild(previewBox);
                        wrap.appendChild(row);
                        if (isAssetID) {
                            // 外部图片地址（次级输入）：填写写 src 并清 assetId，与媒体库选择互斥。
                            var extInput = document.createElement('input');
                            extInput.type = 'text';
                            extInput.placeholder = '或输入外部图片地址（https://…）';
                            extInput.value = String(get(srcPath) || '');
                            extInput.addEventListener('change', function () {
                                var v = extInput.value.trim();
                                if (v) {
                                    commit(srcPath, v);
                                    commit(path, '');
                                    input.value = '';
                                    setPreview(v);
                                } else {
                                    commit(srcPath, '');
                                }
                            });
                            wrap.appendChild(extInput);
                        }
                        panel.appendChild(wrap);
                        return;
                    }
                    panel.appendChild(wrap);
                }
                function checkbox(label, path) {
                    var wrap = document.createElement('label'); wrap.className = 'wb-check-field';
                    var input = document.createElement('input'); input.type = 'checkbox'; input.checked = !!get(path);
                    input.addEventListener('change', function () { commit(path, input.checked); });
                    wrap.appendChild(input); wrap.appendChild(document.createTextNode(label)); panel.appendChild(wrap);
                }
                // 分段按钮组(WP 式):短枚举平铺,点选即提交,当前值高亮。
                function segmentedField(label, path, ctl, after) {
                    var wrap = document.createElement('div'); wrap.className = 'wb-field wb-field-seg';
                    var caption = document.createElement('label'); caption.textContent = label; wrap.appendChild(caption);
                    var seg = document.createElement('div'); seg.className = 'wb-seg';
                    var current = get(path) == null ? '' : String(get(path));
                    var buttons = [];
                    // 选项支持 {value,label} 对象（ct tag 声明中文标签）或纯字符串。
                    (ctl.options || []).forEach(function (o) {
                        var value = (o && typeof o === 'object') ? o.value : o;
                        var text = (o && typeof o === 'object' && o.label) ? o.label : optionLabel(value);
                        var b = document.createElement('button');
                        b.type = 'button'; b.className = 'wb-seg-btn';
                        b.textContent = text;
                        b.title = value;
                        if (value === current || (!current && ctl.default && value === ctl.default)) b.classList.add('is-active');
                        b.addEventListener('click', function () {
                            commit(path, value, after);
                            buttons.forEach(function (x) { x.classList.toggle('is-active', x === b); });
                        });
                        buttons.push(b);
                        seg.appendChild(b);
                    });
                    wrap.appendChild(seg); panel.appendChild(wrap);
                }
                // 单位值输入：解析 "16px" -> 值+单位；紧凑一行。
                function unitInput(label, path, units) {
                    units = units || ['px', '%', 'em', 'rem', 'vw'];
                    var wrap = document.createElement('div'); wrap.className = 'wb-field wb-field-unit';
                    var caption = document.createElement('label'); caption.textContent = label; wrap.appendChild(caption);
                    var row = document.createElement('div'); row.className = 'wb-unit-row';
                    var input = document.createElement('input'); input.type = 'text'; input.className = 'wb-unit-value';
                    var unitSel = document.createElement('select'); unitSel.className = 'wb-unit-select';
                    units.forEach(function (u) {
                        var o = document.createElement('option'); o.value = u; o.textContent = u; unitSel.appendChild(o);
                    });
                    var raw = get(path) == null ? '' : String(get(path)).trim();
                    var m = raw.match(/^(-?[0-9.]+)\s*([a-z%]+)$/i);
                    input.value = m ? m[1] : (raw && !m ? raw : '');
                    if (m) unitSel.value = m[2].toLowerCase(); else unitSel.value = units[0];
                    function push() {
                        var v = input.value.trim();
                        if (v === '') { commit(path, ''); return; }
                        // 已带单位(或 clamp()/字母开头)则原样存;纯数字才追加单位。
                        commit(path, /^-?[0-9.]+$/.test(v) ? v + unitSel.value : v);
                    }
                    input.addEventListener('change', push);
                    unitSel.addEventListener('change', function () { if (input.value.trim() !== '') push(); });
                    row.appendChild(input); row.appendChild(unitSel); wrap.appendChild(row); panel.appendChild(wrap);
                }
                // 三端响应式单位输入(同类型合并):一个控件 + 设备图标切换,绑 Responsive{desktop,tablet,mobile}。
                function responsiveUnitField(label, objPath, units) {
                    units = units || ['px', '%', 'em', 'rem', 'vw'];
                    var wrap = document.createElement('div'); wrap.className = 'wb-field wb-field-unit';
                    var caption = document.createElement('label'); caption.textContent = label; wrap.appendChild(caption);
                    var row = document.createElement('div'); row.className = 'wb-unit-row';
                    var input = document.createElement('input'); input.type = 'text'; input.className = 'wb-unit-value';
                    var unitSel = document.createElement('select'); unitSel.className = 'wb-unit-select';
                    units.forEach(function (u) {
                        var o = document.createElement('option'); o.value = u; o.textContent = u; unitSel.appendChild(o);
                    });
                    var device = 'desktop';
                    var devices = [['desktop', '🖥'], ['tablet', '▭'], ['mobile', '📱']];
                    var devRow = document.createElement('div'); devRow.className = 'wb-device-row';
                    function readVal(dev) {
                        var v = get(objPath + '.' + dev);
                        return v == null ? '' : String(v).trim();
                    }
                    function load(dev) {
                        device = dev;
                        var raw = readVal(dev);
                        var m = raw.match(/^(-?[0-9.]+)\s*([a-z%]+)$/i);
                        input.value = m ? m[1] : (raw && !m ? raw : '');
                        unitSel.value = m ? m[2].toLowerCase() : units[0];
                        devBtns.forEach(function (b) { b.classList.toggle('is-active', b.dataset.dev === device); b.classList.toggle('has-val', !!readVal(b.dataset.dev)); });
                    }
                    function push() {
                        var v = input.value.trim();
                        var val = v === '' ? '' : (/^-?[0-9.]+$/.test(v) ? v + unitSel.value : v);
                        commit(objPath + '.' + device, val);
                        devBtns.forEach(function (b) { b.classList.toggle('has-val', !!readVal(b.dataset.dev)); });
                    }
                    input.addEventListener('change', push);
                    unitSel.addEventListener('change', function () { if (input.value.trim() !== '') push(); });
                    var devBtns = [];
                    devices.forEach(function (d) {
                        var b = document.createElement('button');
                        b.type = 'button'; b.className = 'wb-dev-btn'; b.textContent = d[1]; b.title = d[0]; b.dataset.dev = d[0];
                        b.addEventListener('click', function () { load(d[0]); });
                        devBtns.push(b); devRow.appendChild(b);
                    });
                    row.appendChild(input); row.appendChild(unitSel);
                    wrap.appendChild(row);
                    var row2 = document.createElement('div'); row2.className = 'wb-unit-row';
                    row2.appendChild(devRow);
                    wrap.appendChild(row2); panel.appendChild(wrap);
                    load(device);
                }
                // 四向维度输入(WP 式):上右下左 + 链接联动 + 三端切换。
                // 值形态为 CSS 简写字符串(单值/两值/四值),编译端 padding/margin 直接输出,
                // 后端零改动。继承语义:某端未设置则回退桌面值。
                function dimensionsField(label, objPath, units) {
                    units = units || ['px', '%', 'em', 'rem', 'vw'];
                    var wrap = document.createElement('div'); wrap.className = 'wb-field wb-field-dims';
                    var caption = document.createElement('label'); caption.textContent = label; wrap.appendChild(caption);
                    var device = 'desktop';
                    var linked = true;
                    var inputs = [];  // [top, right, bottom, left]
                    var unitSel = document.createElement('select'); unitSel.className = 'wb-unit-select';
                    units.forEach(function (u) {
                        var o = document.createElement('option'); o.value = u; o.textContent = u; unitSel.appendChild(o);
                    });
                    var linkBtn = document.createElement('button');
                    linkBtn.type = 'button'; linkBtn.className = 'wb-dims-link is-linked'; linkBtn.textContent = '🔗'; linkBtn.title = '四边联动';
                    linkBtn.addEventListener('click', function () {
                        linked = !linked;
                        linkBtn.classList.toggle('is-linked', linked);
                        linkBtn.textContent = linked ? '🔗' : '⛓️‍💥';
                    });
                    function parseShorthand(raw) {
                        var parts = String(raw || '').trim().split(/\s+/).filter(Boolean);
                        if (!parts.length) return ['', '', '', ''];
                        function strip(v) { var m = v.match(/^(-?[0-9.]+)\s*([a-z%]+)$/i); return m ? m[1] : v; }
                        var unitSeen = null;
                        parts.forEach(function (v) { var m = v.match(/^(-?[0-9.]+)\s*([a-z%]+)$/i); if (m) { unitSeen = m[2].toLowerCase(); } });
                        if (unitSeen) unitSel.value = unitSeen;
                        var t = strip(parts[0]);
                        var r2 = parts.length > 1 ? strip(parts[1]) : t;
                        var b = parts.length > 2 ? strip(parts[2]) : t;
                        var l = parts.length > 3 ? strip(parts[3]) : r2;
                        return [t, r2, b, l];
                    }
                    function readRaw(dev) {
                        var v = get(objPath + '.' + dev);
                        return v == null ? '' : String(v).trim();
                    }
                    var devBtns = [];
                    var grid = document.createElement('div'); grid.className = 'wb-dims-grid';
                    function buildInputs() {
                        grid.innerHTML = '';
                        inputs = [];
                        var sides = ['上', '右', '下', '左'];
                        for (var i = 0; i < 4; i++) {
                            var inp = document.createElement('input');
                            inp.type = 'text'; inp.className = 'wb-dims-input'; inp.placeholder = sides[i];
                            inp.dataset.side = String(i);
                            inp.addEventListener('change', function (ev) { onInput(ev.target.dataset.side); });
                            inputs.push(inp); grid.appendChild(inp);
                        }
                        grid.appendChild(linkBtn);
                        grid.appendChild(unitSel);
                    }
                    function syncSides() {
                        // 联动:任一输入变化同步其余(仅链接开时)。
                        if (linked) {
                            var v = inputs[0].value;
                            for (var i = 1; i < 4; i++) inputs[i].value = v;
                        }
                    }
                    function onInput(sideIdx) {
                        if (linked) syncSides();
                        write();
                    }
                    function write() {
                        var t = inputs[0].value.trim(), r2 = inputs[1].value.trim(), b = inputs[2].value.trim(), l = inputs[3].value.trim();
                        var u = unitSel.value;
                        function fmt(v) { if (v === '') return ''; return isNaN(parseFloat(v)) ? v : v + u; }
                        var ft = fmt(t), fr = fmt(r2), fb = fmt(b), fl = fmt(l);
                        var out;
                        if (linked || (t === b && r2 === l)) out = ft || (fr || fb || fl);
                        else if (t === b) out = ft + ' ' + fr;
                        else out = [ft, fr, fb, fl].join(' ');
                        if (out === undefined) out = '';
                        commit(objPath + '.' + device, out.trim());
                        devBtns.forEach(function (btn) { btn.classList.toggle('has-val', !!readRaw(btn.dataset.dev)); });
                    }
                    function load(dev) {
                        device = dev;
                        buildInputs();
                        var four = parseShorthand(readRaw(dev));
                        for (var i = 0; i < 4; i++) inputs[i].value = four[i];
                        devBtns.forEach(function (btn) { btn.classList.toggle('is-active', btn.dataset.dev === device); btn.classList.toggle('has-val', !!readRaw(btn.dataset.dev)); });
                    }
                    var devRow = document.createElement('div'); devRow.className = 'wb-device-row';
                    [['desktop', '🖥'], ['tablet', '▭'], ['mobile', '📱']].forEach(function (d) {
                        var b = document.createElement('button');
                        b.type = 'button'; b.className = 'wb-dev-btn'; b.textContent = d[1]; b.title = d[0]; b.dataset.dev = d[0];
                        b.addEventListener('click', function () { load(d[0]); });
                        devBtns.push(b); devRow.appendChild(b);
                    });
                    var row = document.createElement('div'); row.className = 'wb-unit-row';
                    row.appendChild(grid);
                    wrap.appendChild(row);
                    var row2 = document.createElement('div'); row2.className = 'wb-unit-row';
                    row2.appendChild(devRow);
                    wrap.appendChild(row2);
                    panel.appendChild(wrap);
                    load(device);
                }
                // 排版组：组级设备切换，字号/行高/对齐绑 TextStyle 三端
                // (fontSize 校验支持 clamp() 流式字号，输入以字母开头时不追加单位)。
                // image 样式：固定高度三端（结构字段 schema 管不到）。
                function imageStylePanel() {
                    heading('尺寸');
                    [['desktop', '桌面'], ['tablet', '平板'], ['mobile', '手机']].forEach(function (d) {
                        unitInput(d[1] + '高度', 'props.height.' + d[0], ['px', '%', 'vh']);
                    });
                }
                // heading 样式：对齐/宽度三端。
                function headingStylePanel() {
                    heading('对齐与宽度');
                    [['desktop', '桌面'], ['tablet', '平板'], ['mobile', '手机']].forEach(function (d) {
                        field(d[1] + '对齐', 'props.align.' + d[0], 'select', [['', '默认'], ['left', '左对齐'], ['center', '居中'], ['right', '右对齐']]);
                        unitInput(d[1] + '宽度', 'props.width.' + d[0], ['px', '%', 'vw']);
                    });
                }

                function typographyPanel(typoPath) {
                    var device = 'desktop';
                    var devices = [['desktop', '🖥'], ['tablet', '▭'], ['mobile', '📱']];
                    var devRow = document.createElement('div'); devRow.className = 'wb-device-row';
                    var devBtns = [];
                    devices.forEach(function (d) {
                        var b = document.createElement('button');
                        b.type = 'button'; b.className = 'wb-dev-btn'; b.textContent = d[1]; b.title = d[0]; b.dataset.dev = d[0];
                        b.addEventListener('click', function () {
                            device = d[0];
                            devBtns.forEach(function (x) { x.classList.toggle('is-active', x.dataset.dev === device); });
                            rebuild();
                        });
                        devBtns.push(b); devRow.appendChild(b);
                    });
                    var host = document.createElement('div');
                    var wrapAll = document.createElement('div'); wrapAll.className = 'wb-field wb-typo-group';
                    var head = document.createElement('div'); head.className = 'wb-typo-head';
                    var cap = document.createElement('label'); cap.textContent = '排版';
                    head.appendChild(cap); head.appendChild(devRow);
                    wrapAll.appendChild(head); wrapAll.appendChild(host);
                    panel.appendChild(wrapAll);

                    function base() { return typoPath + '.' + device; }
                    function rebuild() {
                        host.innerHTML = '';
                        devBtns.forEach(function (x) { x.classList.toggle('is-active', x.dataset.dev === device); });
                        var save = panel;
                        panel = host;
                        unitInput('字号', base() + '.fontSize', ['px', 'rem', 'em', 'vw']);
                        unitInput('行高', base() + '.lineHeight', ['', 'px', 'rem']);
                        var alignSeg = document.createElement('div'); alignSeg.className = 'wb-seg';
                        var aligns = [['left', '左'], ['center', '中'], ['right', '右'], ['justify', '两端']];
                        var cur = get(base() + '.textAlign') || '';
                        var btns = [];
                        aligns.forEach(function (a) {
                            var b = document.createElement('button');
                            b.type = 'button'; b.className = 'wb-seg-btn'; b.textContent = a[1];
                            if (cur === a[0]) b.classList.add('is-active');
                            b.addEventListener('click', function () {
                                commit(base() + '.textAlign', a[0]);
                                btns.forEach(function (x) { x.classList.toggle('is-active', x === b); });
                            });
                            btns.push(b); alignSeg.appendChild(b);
                        });
                        var aw = document.createElement('div'); aw.className = 'wb-field wb-field-seg';
                        var ac = document.createElement('label'); ac.textContent = '对齐';
                        aw.appendChild(ac); aw.appendChild(alignSeg); host.appendChild(aw);
                        panel = save;
                    }
                    rebuild();
                }
                // 富文本编辑器（TinyMCE）：工具条与构建期白名单对齐
                // （加粗/斜体/下划线/删除线/代码/列表/引用/链接），
                // 提交 HTML 在发布构建时经 sanitizeRichHTML 白名单清洗。
                function richTextField(label, path) {
                    var wrap = document.createElement('div'); wrap.className = 'wb-field wb-field-richtext';
                    var caption = document.createElement('label'); caption.textContent = label + '（富文本）'; wrap.appendChild(caption);
                    var ta = document.createElement('textarea'); ta.rows = 8;
                    ta.value = get(path) == null ? '' : String(get(path));
                    wrap.appendChild(ta); panel.appendChild(wrap);
                    if (!window.tinymce) {
                        ta.addEventListener('change', function () { commit(path, ta.value); });
                        return;
                    }
                    window.tinymce.init({
                        target: ta,
                        height: 220,
                        menubar: false,
                        branding: false,
                        plugins: 'lists link autolink code',
                        toolbar: 'undo redo | bold italic underline strikethrough | bullist numlist blockquote | link code',
                        link_default_protocol: 'https',
                        setup: function (editor) {
                            editor.on('init', function () { editor.setContent(ta.value || ''); });
                            editor.on('change input', function () {
                                // 输入即写回 AST（节流由 TinyMCE change 触发频率保证）。
                                self.snapshot();
                                set(path, editor.getContent());
                                self.renderTree(); self.refreshCanvas(); self.renderUI();
                            });
                        }
                    });
                }
                // schema 控件 → 表单字段（key 即 props 顶层键，与后端 JSON 序列化一致）。
                function schemaField(ctl) {
                    // hidden 控件不渲染（如 image 的外部地址字段，由媒体控件内嵌输入承担）。
                    if (ctl.hidden) return;
                    // 显示名优先用 ct tag 声明的中文标签，其次字段名映射表。
                    var label = ctl.label || controlLabel(ctl.key);
                    var path = 'props.' + ctl.key;
                    // text 组件富文本模式：正文内容用 TinyMCE 渲染。
                    if (node.type === 'core.text' && ctl.key === 'text' && (get('props.mode') || 'richtext') !== 'plaintext') {
                        richTextField(label, path);
                        return;
                    }
                    if (ctl.kind === 'select') {
                        // 选项归一为 [value, label]：ct tag 已声明中文标签时优先。
                        var opts = (ctl.options || []).map(function (o) {
                            var value = (o && typeof o === 'object') ? o.value : o;
                            var text = (o && typeof o === 'object' && o.label) ? o.label : optionLabel(value);
                            return [value, text || (value === '' ? '（无）' : value)];
                        });
                        // 短枚举(≤6 项)用分段按钮组(WP 式节省空间),长列表保留下拉。
                        if (opts.length <= 6) {
                            segmentedField(label, path, ctl, (node.type === 'core.text' && ctl.key === 'mode') ? modeAfter : null);
                            return;
                        }
                        var choices = opts;
                        if (ctl.default) choices.unshift(['', '（默认）']);
                        // text.mode 切换后重建面板，切换富文本/纯文本编辑形态。
                        var afterSel = (node.type === 'core.text' && ctl.key === 'mode') ? modeAfter : null;
                        field(label, path, 'select', choices, afterSel);
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
                // mode 切换(richtext/plaintext)后重建检查器以切换编辑形态。
                function modeAfter() {
                    self.syncInspector();
                }
                // container 布局面板（嵌套结构未走 ct 声明，字段路径与编译端手写对齐）。
                function containerLayout() {
                    field('语义标签', 'props.tag', 'segment', [['div', '容器'], ['section', '区块'], ['article', '文章'], ['header', '页头'], ['footer', '页脚'], ['main', '主体']]);
                    heading('Flex 布局');
                    field('排列方向', 'props.layout.flex.direction', 'segment', [['column', '纵向'], ['row', '横向']], function () { set('props.layout.engine', 'flex'); });
                    field('主轴分布', 'props.layout.flex.justify', 'segment', [['flex-start', '起始'], ['center', '居中'], ['flex-end', '末端'], ['space-between', '两端'], ['space-around', '环绕'], ['space-evenly', '均分']]);
                    field('交叉对齐', 'props.layout.flex.align', 'segment', [['stretch', '拉伸'], ['center', '居中'], ['flex-start', '起始'], ['flex-end', '末端']]);
                    checkbox('允许换行', 'props.layout.flex.wrap');
                    unitInput('组件间距', 'props.layout.flex.gap');
                    heading('尺寸与留白');
                    dimensionsField('内边距（三端）', 'props.box.padding');
                    dimensionsField('外边距（三端）', 'props.box.margin');
                    unitInput('最小高度', 'props.box.minHeight');
                }
                // container 视觉面板。
                function containerVisual() {
                    heading('背景');
                    field('背景颜色', 'props.visual.bgColor', 'input');
                    field('背景渐变', 'props.visual.bgGradient', 'input');
                    field('背景图片', 'props.visual.bgImage', 'input');
                    // 背景显示控制（对齐 Elementor：定位/附着/重复/尺寸）。
                    field('背景定位', 'props.visual.bgPosition', 'select', [['default', '（默认）'], ['center', '居中'], ['center top', '中上'], ['center bottom', '中下'], ['left top', '左上'], ['left center', '左中'], ['left bottom', '左下'], ['right top', '右上'], ['right center', '右中'], ['right bottom', '右下'], ['custom', '自定义']]);
                    if (get('props.visual.bgPosition') === 'custom') {
                        field('定位值', 'props.visual.bgPositionXY', 'input');
                    }
                    field('背景附着方式', 'props.visual.bgAttachment', 'select', [['default', '（默认）'], ['scroll', '随页面滚动'], ['fixed', '固定（视差）'], ['local', '随内容滚动']]);
                    field('背景重复', 'props.visual.bgRepeat', 'select', [['default', '（默认）'], ['no-repeat', '不重复'], ['repeat', '平铺'], ['repeat-x', '横向平铺'], ['repeat-y', '纵向平铺']]);
                    field('显示尺寸', 'props.visual.bgSize', 'select', [['default', '（默认）'], ['auto', '原始'], ['contain', '完整包含'], ['cover', '铺满裁剪'], ['custom', '自定义']]);
                    if (get('props.visual.bgSize') === 'custom') {
                        field('尺寸值', 'props.visual.bgSizeValue', 'input');
                    }
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
                    field('嵌入位置', 'props.inset.position', 'segment', [['center', '居中'], ['left', '靠左'], ['right', '靠右']]);
                    unitInput('两侧留白', 'props.inset.spacing');
                }
                // divider 嵌入文本样式。
                function dividerInsetStyle() {
                    heading('嵌入文本样式');
                    field('字号', 'props.inset.fontSize', 'input');
                    field('字重', 'props.inset.fontWeight', 'select', [['', '（默认）'], ['400', '常规'], ['500', '中等'], ['600', '半粗'], ['700', '粗体']]);
                    field('颜色', 'props.inset.color', 'input');
                }
                // slider 轮播面板：每屏显示数（三端）+ 开关组。
                function sliderPanel() {
                    heading('每屏显示数');
                    [['desktop', '桌面'], ['tablet', '平板'], ['mobile', '手机']].forEach(function (d) {
                        var seg = document.createElement('div'); seg.className = 'wb-field wb-field-seg';
                        var cap = document.createElement('label'); cap.textContent = d[1]; seg.appendChild(cap);
                        var inner = document.createElement('div'); inner.className = 'wb-seg';
                        var cur = get('props.perView.' + d[0]) || 1;
                        [1, 2, 3, 4].forEach(function (n) {
                            var b = document.createElement('button'); b.type = 'button'; b.className = 'wb-seg-btn';
                            b.textContent = n;
                            if (cur === n) b.classList.add('is-active');
                            b.addEventListener('click', function () {
                                commit('props.perView.' + d[0], n);
                                inner.querySelectorAll('.wb-seg-btn').forEach(function (x) { x.classList.toggle('is-active', x === b); });
                            });
                            inner.appendChild(b);
                        });
                        seg.appendChild(inner); panel.appendChild(seg);
                    });
                    var autoWrap = document.createElement('div'); autoWrap.className = 'wb-field';
                    var autoLabel = document.createElement('label'); autoLabel.textContent = '自动播放间隔（秒，0=关）'; autoWrap.appendChild(autoLabel);
                    var autoInput = document.createElement('input'); autoInput.type = 'number'; autoInput.min = 0; autoInput.step = 0.5;
                    autoInput.value = get('props.autoplay') || 0;
                    autoInput.addEventListener('change', function () { commit('props.autoplay', Number(autoInput.value) || 0); });
                    autoWrap.appendChild(autoInput); panel.appendChild(autoWrap);
                    checkbox('显示箭头', 'props.showArrows');
                    checkbox('显示圆点', 'props.showDots');
                    unitInput('slide 间距', 'props.gap', ['px']);
                }
                // list 列表面板：样式 + 项 repeater。
                function listPanel() {
                    heading('列表项');
                    var st = get('props.style') || 'icon';
                    [['icon', '图标'], ['number', '序号'], ['dot', '圆点']].forEach(function (pair) {
                        var row = document.createElement('button'); row.type = 'button';
                        row.className = 'wb-btn wb-btn-sm' + (st === pair[0] ? ' wb-btn-primary' : '');
                        row.textContent = pair[1];
                        row.addEventListener('click', function () { commit('props.style', pair[0]); self.syncInspector(); });
                        panel.appendChild(row);
                    });
                    if (!Array.isArray(get('props.items'))) set('props.items', []);
                    var items = get('props.items');
                    function save() { commit('props.items', items); }
                    items.forEach(function (item, idx) {
                        var row = document.createElement('div'); row.className = 'wb-repeater-row';
                        var mid = document.createElement('div'); mid.className = 'wb-repeater-mid';
                        if (get('props.style') === 'icon') {
                            var iconSel = document.createElement('select');
                            [['', '默认对勾'], ['check', '对勾'], ['star', '星形'], ['arrow', '箭头'], ['shield', '盾牌'], ['truck', '卡车'], ['cross', '叉形']].forEach(function (o) {
                                var opt = document.createElement('option'); opt.value = o[0]; opt.textContent = o[1];
                                iconSel.appendChild(opt);
                            });
                            iconSel.value = item.icon || '';
                            iconSel.addEventListener('change', function () { item.icon = iconSel.value; save(); });
                            mid.appendChild(iconSel);
                        }
                        var text = document.createElement('input'); text.type = 'text'; text.placeholder = '列表项文本'; text.value = item.text || '';
                        text.addEventListener('change', function () { item.text = text.value; save(); });
                        mid.appendChild(text);
                        var link = document.createElement('input'); link.type = 'text'; link.placeholder = '链接（可选）'; link.value = item.link || '';
                        link.addEventListener('change', function () { item.link = link.value; save(); });
                        mid.appendChild(link);
                        var del = document.createElement('button'); del.type = 'button'; del.className = 'wb-icon-btn'; del.textContent = '✕';
                        del.addEventListener('click', function () { items.splice(idx, 1); save(); self.syncInspector(); });
                        row.appendChild(mid); row.appendChild(del);
                        panel.appendChild(row);
                    });
                    var add = document.createElement('button'); add.type = 'button'; add.className = 'wb-btn wb-btn-secondary wb-btn-sm wb-repeater-add';
                    add.textContent = '+ 添加列表项';
                    add.addEventListener('click', function () { items.push({ icon: 'check', text: '新列表项', link: '' }); save(); self.syncInspector(); });
                    panel.appendChild(add);
                }
                // infobox 信息框面板：图标/图片 + 标题/文本 + 链接。
                function infoboxPanel() {
                    heading('图标 / 图片');
                    var cur = get('props.icon') || '';
                    [['', '无'], ['check', '对勾'], ['star', '星形'], ['arrow', '箭头'], ['shield', '盾牌'], ['truck', '卡车'], ['cross', '叉形']].forEach(function (o) {
                        var b = document.createElement('button'); b.type = 'button'; b.className = 'wb-btn wb-btn-sm' + (cur === o[0] ? ' wb-btn-primary' : '');
                        b.textContent = o[1];
                        b.addEventListener('click', function () { commit('props.icon', o[0]); self.syncInspector(); });
                        panel.appendChild(b);
                    });
                    field('图片（选填，优先于图标）', 'props.mediaImage', 'input');
                    heading('内容');
                    field('副标题', 'props.subtitle', 'input');
                    field('标题', 'props.title', 'input');
                    field('描述', 'props.text', 'textarea');
                    field('链接', 'props.link', 'input');
                    field('按钮文字（填写后链接显示为按钮）', 'props.btnText', 'input');
                }
                // social_buttons 面板：平台 repeater。
                function socialPanel() {
                    if (!Array.isArray(get('props.items'))) set('props.items', []);
                    var items = get('props.items');
                    function save() { commit('props.items', items); }
                    var platforms = ['facebook', 'x', 'instagram', 'youtube', 'tiktok', 'telegram', 'whatsapp', 'pinterest', 'linkedin'];
                    var labels = { facebook: 'Facebook', x: 'X', instagram: 'Instagram', youtube: 'YouTube', tiktok: 'TikTok', telegram: 'Telegram', whatsapp: 'WhatsApp', pinterest: 'Pinterest', linkedin: 'LinkedIn' };
                    items.forEach(function (item, idx) {
                        var row = document.createElement('div'); row.className = 'wb-repeater-row';
                        var mid = document.createElement('div'); mid.className = 'wb-repeater-mid';
                        var sel = document.createElement('select');
                        platforms.forEach(function (pl) {
                            var opt = document.createElement('option'); opt.value = pl; opt.textContent = labels[pl];
                            sel.appendChild(opt);
                        });
                        sel.value = item.platform || 'facebook';
                        sel.addEventListener('change', function () { item.platform = sel.value; save(); });
                        var link = document.createElement('input'); link.type = 'text'; link.placeholder = '链接地址'; link.value = item.url || '';
                        link.addEventListener('change', function () { item.url = link.value; save(); });
                        mid.appendChild(sel); mid.appendChild(link);
                        var del = document.createElement('button'); del.type = 'button'; del.className = 'wb-icon-btn'; del.textContent = '✕';
                        del.addEventListener('click', function () { items.splice(idx, 1); save(); self.syncInspector(); });
                        row.appendChild(mid); row.appendChild(del);
                        panel.appendChild(row);
                    });
                    var add = document.createElement('button'); add.type = 'button'; add.className = 'wb-btn wb-btn-secondary wb-btn-sm wb-repeater-add';
                    add.textContent = '+ 添加平台';
                    add.addEventListener('click', function () { items.push({ platform: 'facebook', url: '' }); save(); self.syncInspector(); });
                    panel.appendChild(add);
                }
                // video 视频面板：媒体库选文件（assetId 自动触发媒体控件）+ 外链 + 封面 + 播放选项。
                function videoPanel() {
                    field('视频文件', 'props.assetId', 'input');
                    field('外链地址（YouTube/本地 URL）', 'props.url', 'input');
                    field('封面图', 'props.poster', 'input');
                    checkbox('自动播放', 'props.autoplay');
                    checkbox('循环播放', 'props.loop');
                    checkbox('静音', 'props.muted');
                    checkbox('播放控件', 'props.controls');
                }

                // tabs 页签面板：标签列表 repeater（与 children 面板一一对应）。
                function tabsPanel() {
                    if (!Array.isArray(get('props.tabs'))) set('props.tabs', []);
                    var tabs = get('props.tabs');
                    var kids = node.children || [];
                    function save() { commit('props.tabs', tabs); }
                    tabs.forEach(function (t, idx) {
                        var row = document.createElement('div'); row.className = 'wb-repeater-row';
                        var mid = document.createElement('div'); mid.className = 'wb-repeater-mid';
                        var input = document.createElement('input'); input.type = 'text'; input.placeholder = '页签' + (idx + 1) + ' 标签';
                        input.value = t.label || '';
                        input.addEventListener('change', function () { t.label = input.value; save(); });
                        mid.appendChild(input);
                        var del = document.createElement('button'); del.type = 'button'; del.className = 'wb-icon-btn'; del.textContent = '✕';
                        del.addEventListener('click', function () { tabs.splice(idx, 1); save(); self.syncInspector(); });
                        row.appendChild(mid); row.appendChild(del);
                        panel.appendChild(row);
                    });
                    var add = document.createElement('button'); add.type = 'button'; add.className = 'wb-btn wb-btn-secondary wb-btn-sm wb-repeater-add';
                    add.textContent = '+ 添加页签（同时拖入一个面板）';
                    add.addEventListener('click', function () { tabs.push({ label: '新页签' }); save(); self.syncInspector(); });
                    panel.appendChild(add);
                    var tip = document.createElement('p'); tip.className = 'wb-empty';
                    tip.textContent = '面板数需与页签数一致：在画布中向 tabs 内部拖入组件即新增一个面板。';
                    panel.appendChild(tip);
                }
                // accordion 面板：折叠项标题 repeater。
                function accordionPanel() {
                    if (!Array.isArray(get('props.items'))) set('props.items', []);
                    var items = get('props.items');
                    function save() { commit('props.items', items); }
                    items.forEach(function (it, idx) {
                        var row = document.createElement('div'); row.className = 'wb-repeater-row';
                        var mid = document.createElement('div'); mid.className = 'wb-repeater-mid';
                        var input = document.createElement('input'); input.type = 'text'; input.placeholder = '折叠项' + (idx + 1) + ' 标题';
                        input.value = it.title || '';
                        input.addEventListener('change', function () { it.title = input.value; save(); });
                        mid.appendChild(input);
                        var openCk = document.createElement('label'); openCk.className = 'wb-check-field';
                        var ck = document.createElement('input'); ck.type = 'checkbox'; ck.checked = !!it.open;
                        ck.addEventListener('change', function () { it.open = ck.checked; save(); });
                        openCk.appendChild(ck); openCk.appendChild(document.createTextNode('默认展开'));
                        mid.appendChild(openCk);
                        var del = document.createElement('button'); del.type = 'button'; del.className = 'wb-icon-btn'; del.textContent = '✕';
                        del.addEventListener('click', function () { items.splice(idx, 1); save(); self.syncInspector(); });
                        row.appendChild(mid); row.appendChild(del);
                        panel.appendChild(row);
                    });
                    var add = document.createElement('button'); add.type = 'button'; add.className = 'wb-btn wb-btn-secondary wb-btn-sm wb-repeater-add';
                    add.textContent = '+ 添加折叠项（同时拖入一个内容）';
                    add.addEventListener('click', function () { items.push({ title: '新折叠项', open: false }); save(); self.syncInspector(); });
                    panel.appendChild(add);
                    checkbox('同时只开一个', 'props.oneOpen');
                    checkbox('无边框', 'props.borderless');
                }
                // marquee 面板。
                function marqueePanel() {
                    heading('滚动设置');
                    var spWrap = document.createElement('div'); spWrap.className = 'wb-field';
                    var spLabel = document.createElement('label'); spLabel.textContent = '滚动速度（秒）'; spWrap.appendChild(spLabel);
                    var spInput = document.createElement('input'); spInput.type = 'number'; spInput.min = 1; spInput.step = 1;
                    spInput.value = get('props.speed') || 12;
                    spInput.addEventListener('change', function () { commit('props.speed', Number(spInput.value) || 12); });
                    spWrap.appendChild(spInput); panel.appendChild(spWrap);
                    field('滚动方向', 'props.direction', 'select', [['left', '向左'], ['right', '向右']]);
                    unitInput('内容间距', 'props.gap', ['px']);
                    checkbox('悬停暂停', 'props.pauseOnHover');
                    var tip = document.createElement('p'); tip.className = 'wb-empty';
                    tip.textContent = '向跑马灯内部拖入任意组件（文本/Logo/卡片），内容将无缝滚动。';
                    panel.appendChild(tip);
                }
                // counter 面板。
                function counterPanel() {
                    heading('数值');
                    field('起始值', 'props.start', 'input');
                    field('结束值', 'props.end', 'input');
                    field('小数位', 'props.decimals', 'input');
                    field('前缀', 'props.prefix', 'input');
                    field('后缀', 'props.suffix', 'input');
                    var durWrap = document.createElement('div'); durWrap.className = 'wb-field';
                    var durLabel = document.createElement('label'); durLabel.textContent = '动画时长（秒）'; durWrap.appendChild(durLabel);
                    var durInput = document.createElement('input'); durInput.type = 'number'; durInput.min = 0.5; durInput.step = 0.5;
                    durInput.value = get('props.duration') || 2;
                    durInput.addEventListener('change', function () { commit('props.duration', Number(durInput.value) || 2); });
                    durWrap.appendChild(durInput); panel.appendChild(durWrap);
                }

                // spacer 高度面板（Responsive 嵌套结构）。
                function spacerPanel() {
                    heading('留白高度');
                    field('桌面端高度', 'props.height.desktop', 'input');
                    field('平板端高度', 'props.height.tablet', 'input');
                    field('手机端高度', 'props.height.mobile', 'input');
                }

                // gallery 图片列表 repeater:逐项媒体库选图/alt/删除,底部添加。
                function galleryItemsPanel(itemsPath) {
                    if (!Array.isArray(get(itemsPath))) set(itemsPath, []);
                    var items = get(itemsPath);
                    function save() {
                        commit(itemsPath, items);
                    }
                    items.forEach(function (item, idx) {
                        var row = document.createElement('div'); row.className = 'wb-repeater-row';
                        var thumb = document.createElement('img');
                        thumb.className = 'wb-repeater-thumb';
                        var res = self.resolveAssetUrl(item.assetId);
                        if (res) thumb.src = res;
                        row.appendChild(thumb);
                        var mid = document.createElement('div'); mid.className = 'wb-repeater-mid';
                        var pick = document.createElement('button');
                        pick.type = 'button'; pick.className = 'wb-btn wb-btn-secondary wb-btn-sm';
                        pick.textContent = '选图';
                        pick.addEventListener('click', function () {
                            self.openMediaPicker(function (url, assetId) {
                                item.assetId = assetId || String(url);
                                save();
                            });
                        });
                        mid.appendChild(pick);
                        var alt = document.createElement('input');
                        alt.type = 'text'; alt.placeholder = '替代文字'; alt.value = item.alt || '';
                        alt.addEventListener('change', function () { item.alt = alt.value; save(); });
                        mid.appendChild(alt);
                        row.appendChild(mid);
                        var del = document.createElement('button');
                        del.type = 'button'; del.className = 'wb-icon-btn'; del.textContent = '✕'; del.title = '删除此项';
                        del.addEventListener('click', function () {
                            items.splice(idx, 1);
                            save();
                            self.syncInspector();
                        });
                        row.appendChild(del);
                        panel.appendChild(row);
                    });
                    var add = document.createElement('button');
                    add.type = 'button'; add.className = 'wb-btn wb-btn-secondary wb-btn-sm wb-repeater-add';
                    add.textContent = '+ 添加图片';
                    add.addEventListener('click', function () {
                        items.push({ assetId: '', alt: '', caption: '', link: '' });
                        save();
                        self.syncInspector();
                    });
                    panel.appendChild(add);
                }

                var schema = componentSchemas[node.type] || [];
                var groups = { content: [], style: [], advanced: [] };
                schema.forEach(function (ctl) { (groups[ctl.section] || groups.content).push(ctl); });

                var nodeTitle = node.name || String(node.type).replace('core.', '');
                // 两页签：内容 / 样式（对齐用户设计：样式类属性全部归样式，不单设「高级」）。
                if (this.tab === 'style') {
                    heading(nodeTitle + ' · 样式');
                    if (node.type === 'core.container') containerVisual();
                    if (node.type === 'core.divider') dividerInsetStyle();
                    if (node.type === 'core.heading' || node.type === 'core.text') {
                        typographyPanel('props.typography');
                    }
                    if (node.type === 'core.image') imageStylePanel();
                    if (node.type === 'core.heading') headingStylePanel();
                    groups.style.forEach(schemaField);
                    if (groups.advanced.length) {
                        heading('高级');
                        groups.advanced.forEach(schemaField);
                    }
                    var actions = document.createElement('div'); actions.className = 'wb-inspector-actions';
                    [['复制组件', self.copyNode.bind(self)], ['粘贴样式', self.pasteStyle.bind(self)]].forEach(function (pair) {
                        var button = document.createElement('button'); button.type = 'button'; button.className = 'wb-btn wb-btn-secondary wb-btn-sm'; button.textContent = pair[0];
                        button.addEventListener('click', pair[1]); actions.appendChild(button);
                    });
                    panel.appendChild(actions);
                    if (!groups.style.length && !groups.advanced.length && node.type !== 'core.container' && node.type !== 'core.divider' && node.type !== 'core.list' && node.type !== 'core.infobox' && node.type !== 'core.social_buttons' && node.type !== 'core.video' && node.type !== 'core.slider') {
                        var none1 = document.createElement('p'); none1.className = 'wb-empty'; none1.textContent = '该组件暂无独立样式项'; panel.appendChild(none1);
                    }
                } else {
                    heading(nodeTitle + ' · 内容');
                    field('显示名称', 'name', 'input');
                    if (node.type === 'core.container') containerLayout();
                    if (node.type === 'core.divider') dividerInset();
                    if (node.type === 'core.spacer') spacerPanel();
                    if (node.type === 'core.slider') sliderPanel();
                    if (node.type === 'core.list') listPanel();
                    if (node.type === 'core.infobox') infoboxPanel();
                    if (node.type === 'core.social_buttons') socialPanel();
                    if (node.type === 'core.video') videoPanel();
                    if (node.type === 'core.tabs') tabsPanel();
                    if (node.type === 'core.accordion') accordionPanel();
                    if (node.type === 'core.marquee') marqueePanel();
                    if (node.type === 'core.counter') counterPanel();
                    if (node.type === 'core.gallery') {
                        heading('图片列表');
                        galleryItemsPanel('props.items');
                        heading('其余设置');
                    }
                    groups.content.forEach(schemaField);
                    if (!groups.content.length && node.type !== 'core.container' && node.type !== 'core.divider' && node.type !== 'core.spacer' && node.type !== 'core.slider' && node.type !== 'core.list' && node.type !== 'core.infobox' && node.type !== 'core.social_buttons' && node.type !== 'core.video' && node.type !== 'core.gallery' && node.type !== 'core.tabs' && node.type !== 'core.accordion' && node.type !== 'core.marquee' && node.type !== 'core.counter') {
                        var none2 = document.createElement('p'); none2.className = 'wb-empty'; none2.textContent = '该组件暂无内容项'; panel.appendChild(none2);
                    }
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
                if (/^https?:|^\//.test(assetIdOrUrl)) return assetIdOrUrl;
                var list = this._mediaCache || [];
                for (var i = 0; i < list.length; i++) {
                    if (String(list[i].id) === String(assetIdOrUrl)) return list[i].url;
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
                grid.innerHTML = '<p class="wb-empty">加载中…</p>';
                var qs = 'page=1&limit=120';
                if (self._mediaCategory > 0) qs += '&category_id=' + self._mediaCategory;
                if (self._mediaSearch) qs += '&search=' + encodeURIComponent(self._mediaSearch);
                fetch('/api/media/list?' + qs)
                    .then(function (r) { return r.json(); })
                    .then(function (j) {
                        grid.innerHTML = '';
                        var list = (j.data && j.data.list) || [];
                        self._mediaCache = list;
                        if (!list.length) {
                            grid.innerHTML = '<p class="wb-empty">媒体库为空，点右上「上传图片」</p>';
                            return;
                        }
                        list.forEach(function (item) {
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
                    .catch(function () { grid.innerHTML = '<p class="wb-empty">加载失败</p>'; });
            },
            uploadMedia(file) {
                if (!file) return;
                var self = this;
                var form = new FormData();
                form.append('file', file);
                fetch('/api/media/upload', { method: 'POST', body: form })
                    .then(function (r) { return r.json(); })
                    .then(function (j) {
                        if (j.code && j.code >= 400) { alert(j.message || '上传失败'); return; }
                        self.loadMediaList();
                    })
                    .catch(function () { alert('上传失败'); });
            },

            // ---------------- API 接线（草稿保存 / 构建 / 发布） ----------------
            api(path, body, onDone) {
                var self = this;
                self.busy = true;
                self.renderUI();
                // saveBase='block'（全局块编辑）时走 /api/block/ 前缀，默认页面 /api/page/。
                fetch('/api/' + (meta.saveBase || 'page') + '/' + path, {
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
                // 全局块编辑：保存到 dashboard 编排端点（保存后自动传播 stale）。
                if (meta.saveBase === 'block') {
                    self.busy = true; self.renderUI();
                    fetch('/admin/blocks/save-content', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ id: meta.pageId, name: meta.blockName, document: this.doc })
                    }).then(function (r) { return r.json(); })
                      .then(function (j) {
                          self.busy = false;
                          if (j.code && j.code >= 400) {
                              self.saveState = 'error'; self.renderUI();
                              alert(j.message || '保存失败');
                              return;
                          }
                          self.saveState = 'saved';
                          self.refreshCanvas();
                      })
                      .catch(function () { self.busy = false; self.saveState = 'error'; self.renderUI(); });
                    return;
                }
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
                if (meta.saveBase === 'block') { this.saveDraft(); return; }
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
                if (meta.saveBase === 'block') { this.saveDraft(); return; }
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
                // 预载媒体库列表：检查器 assetId 字段的缩略图解析依赖 _mediaCache。
                this.loadMediaList();
                document.addEventListener('keydown', function (e) { self.onKeydown(e); });
                // iframe 内点击经 postMessage 上报选中（editor=1 注入桥接脚本）。
                window.addEventListener('message', function (ev) {
                    if (ev.origin !== window.location.origin || !ev.data) return;
                    if (ev.data.type === 'wb-select' && ev.data.id) { self.select(ev.data.id); return; }
                    if (ev.data.type === 'wb-insert-here' && ev.data.id) {
                        // 画布「+ 插入组件」浮标：目标是容器则插入其内部，否则插到其后。
                        var node = self.findNode(ev.data.id);
                        self.pendingInsertTarget = {
                            id: ev.data.id,
                            placement: node && node.type === 'core.container' ? 'inside' : 'after'
                        };
                        self.showLibrary();
                        return;
                    }
                    if (ev.data.type === 'wb-canvas-drop' && ev.data.nodeID) {
                        // 画布内元素拖动重排：桥接已算好落点，容器中带按内部插入。
                        var t = ev.data.targetID ? self.findNode(ev.data.targetID) : null;
                        var placement = ev.data.placement;
                        if (t && t.type === 'core.container' && ev.data.inMiddle) placement = 'inside';
                        if (!t) return;
                        self.moveNode(ev.data.nodeID, ev.data.targetID, placement);
                    }
                });
                var search = document.querySelector('#wb-navigator .wb-search');
                if (search) search.addEventListener('input', function () {
                    self.filter = search.value;
                    self.renderTree();
                });
                // 检查器三个页签：布局(内容) / 样式 / 扩展——切换后重渲染面板。
                // 检查器两个页签：内容 / 样式（样式类属性全部归样式页签）。
                var tabKeys = ['content', 'style'];
                var tabButtons = document.querySelectorAll('.wb-tabs button');
                tabButtons.forEach(function (button, index) {
                    button.addEventListener('click', function () {
                        self.tab = tabKeys[index] || 'content';
                        tabButtons.forEach(function (b, i) { b.classList.toggle('is-active', i === index); });
                        self.syncInspector();
                    });
                });
                // 面板头部显隐/锁定快捷开关。
                var editHideBtn = document.getElementById('wb-edit-hide');
                if (editHideBtn) editHideBtn.addEventListener('click', function () {
                    var n = self.findNode(self.selectedId);
                    if (!n) return;
                    self.toggleHidden(n.id);
                    editHideBtn.textContent = n.hidden ? '🚫' : '👁';
                });
                var editLockBtn = document.getElementById('wb-edit-lock');
                if (editLockBtn) editLockBtn.addEventListener('click', function () {
                    var n = self.findNode(self.selectedId);
                    if (!n) return;
                    self.toggleLocked(n.id);
                    editLockBtn.textContent = n.locked ? '🔒' : '🔓';
                });
                // 左侧双视图：+ 打开组件库；× 收起面板；🗑 删除选中组件。
                var back = document.getElementById('wb-back-library');
                if (back) back.addEventListener('click', function () { self.showLibrary(); });
                var addBtn = document.getElementById('wb-add-component');
                if (addBtn) addBtn.addEventListener('click', function () {
                    var inspector = document.querySelector('.wb-inspector');
                    if (inspector) inspector.classList.add('is-open');
                    self.showLibrary();
                });
                var libClose = document.getElementById('wb-library-close');
                if (libClose) libClose.addEventListener('click', function () {
                    document.getElementById('wb-panel-library').hidden = true;
                    if (!self.selectedId) self.showEdit();
                });
                var editDelete = document.getElementById('wb-edit-delete');
                if (editDelete) editDelete.addEventListener('click', function () { self.deleteSelected(); });
                var libSearch = document.getElementById('wb-library-search');
                if (libSearch) libSearch.addEventListener('input', function () {
                    self.libraryFilter = libSearch.value;
                    self.renderPalette();
                });
                // 左侧图标栏：面板切换（编辑视图归入「组件」入口）。
                [['wb-rail-library', 'library'], ['wb-rail-settings', 'settings'],
                 ['wb-rail-global', 'global'], ['wb-rail-history', 'history']].forEach(function (pair) {
                    var b = document.getElementById(pair[0]);
                    if (b) b.addEventListener('click', function () { self.showPanel(pair[1]); });
                });
                var railNav = document.getElementById('wb-rail-navigator');
                if (railNav) railNav.addEventListener('click', function () {
                    self.navigatorOpen = !self.navigatorOpen;
                    document.getElementById('wb-navigator').classList.toggle('is-hidden', !self.navigatorOpen);
                });
                // 组件库页签：组件 | 全局块 | SEO。
                document.querySelectorAll('.wb-lib-tabs button').forEach(function (btn) {
                    btn.addEventListener('click', function () {
                        document.querySelectorAll('.wb-lib-tabs button').forEach(function (b) { b.classList.toggle('is-active', b === btn); });
                        var tab = btn.dataset.libtab;
                        var p1 = document.getElementById('wb-palette');
                        var p2 = document.getElementById('wb-palette-blocks');
                        var p3 = document.getElementById('wb-palette-seo');
                        if (p1) p1.hidden = tab !== 'components';
                        if (p2) p2.hidden = tab !== 'blocks';
                        if (p3) p3.hidden = tab !== 'seo';
                    });
                });
                // 媒体库弹层：关闭/遮罩/上传。
                var mediaClose = document.getElementById('wb-media-close');
                if (mediaClose) mediaClose.addEventListener('click', function () { self.closeMediaPicker(); });
                // 媒体弹窗：名称搜索（防抖）与分类树搜索。
                var mediaSearch = document.getElementById('wb-media-search');
                var mediaSearchTimer = null;
                if (mediaSearch) mediaSearch.addEventListener('input', function () {
                    clearTimeout(mediaSearchTimer);
                    mediaSearchTimer = setTimeout(function () {
                        self._mediaSearch = mediaSearch.value.trim();
                        self.loadMediaList();
                    }, 300);
                });
                var mediaTreeSearch = document.getElementById('wb-media-tree-search');
                if (mediaTreeSearch) mediaTreeSearch.addEventListener('input', function () {
                    self.renderMediaTree();
                });
                var mediaMask = document.querySelector('#wb-media-modal .wb-media-mask');
                if (mediaMask) mediaMask.addEventListener('click', function () { self.closeMediaPicker(); });
                var mediaUpBtn = document.getElementById('wb-media-upload-btn');
                var mediaUpInput = document.getElementById('wb-media-upload-input');
                if (mediaUpBtn && mediaUpInput) {
                    mediaUpBtn.addEventListener('click', function () { mediaUpInput.click(); });
                    mediaUpInput.addEventListener('change', function () {
                        if (mediaUpInput.files && mediaUpInput.files[0]) self.uploadMedia(mediaUpInput.files[0]);
                        mediaUpInput.value = '';
                    });
                }
                var controls = {
                    'wb-device-desktop': function () { self.setDevice('desktop'); },
                    'wb-device-tablet': function () { self.setDevice('tablet'); },
                    'wb-device-mobile': function () { self.setDevice('mobile'); },
                    'wb-save-draft': function () { self.saveDraft(); },
                    'wb-publish': function () { self.buildAndPublish(); },
                    'wb-page-settings': function () { self.showPanel('settings'); },
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