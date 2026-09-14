// workbench/methods/canvas.js — 画布直改 / 画布联动 / 剪贴板（docs/09 §3 拆分）。
// 方法以 `this` 互调，由 index.js 用 Object.assign 合并为同一个 workbench 实例。
import {
    meta, initialDoc, getCSRFToken, csrfHeaders, wbParseColor, wbHslToHsv, wbHsvToRgb, wbRgbToHsv, wbColorPicker, themePrimary, controlLabel, optionLabel, clone, componentSchemas, CONTROL_LABELS, OPTION_LABELS, paletteItems, paletteGroups, MAX_NEST_DEPTH, WB_CP_CHECKER, buildInsertNode,
} from '../core.js';

export const canvasMethods = {
            // ---------------- 画布直改支持 ----------------
            // textFieldOf 组件类型 → 就地编辑的文本 prop 键（双击编辑回写目标）。
            textFieldOf(type) {
                var map = {
                    'core.heading': 'text', 'core.text': 'text', 'core.button': 'text',
                    'core.card': 'title', 'core.quote': 'text', 'core.badge': 'text',
                    'core.infobox': 'title', 'core.counter': 'suffix', 'core.progress': 'label'
                };
                return map[type] || null;
            },
            // handleCanvasCtx 画布右键/快捷条操作分发（复用既有方法）。
            handleCanvasCtx(id, op, value) {
                this.selectedId = id;
                switch (op) {
                    case 'copy': this.copyNode(); break;
                    case 'cut': this.cutNode(); break;
                    case 'paste-inside': this.pasteInto(id); break;
                    case 'delete': this.deleteSelected(); break;
                    case 'move-up': this.moveNodeOrder(id, -1); break;
                    case 'move-down': this.moveNodeOrder(id, 1); break;
                    case 'entrance':
                        // 入场动画快捷项：写 props.advanced.interaction.entrance。
                        var n = this.findNode(id);
                        if (n) {
                            this.snapshot();
                            n.props = n.props || {};
                            n.props.advanced = n.props.advanced || {};
                            n.props.advanced.interaction = n.props.advanced.interaction || {};
                            // 切换：同值再点取消。
                            n.props.advanced.interaction.entrance =
                                n.props.advanced.interaction.entrance === value ? '' : value;
                            this.saveState = 'dirty';
                            this.renderTree(); this.refreshCanvas(); this.renderUI();
                        }
                        break;
                    case 'hover':
                        var n2 = this.findNode(id);
                        if (n2) {
                            this.snapshot();
                            n2.props = n2.props || {};
                            n2.props.advanced = n2.props.advanced || {};
                            n2.props.advanced.interaction = n2.props.advanced.interaction || {};
                            n2.props.advanced.interaction.hoverEffect =
                                n2.props.advanced.interaction.hoverEffect === value ? '' : value;
                            this.saveState = 'dirty';
                            this.renderTree(); this.refreshCanvas(); this.renderUI();
                        }
                        break;
                }
            },
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
                // 区块预设项：整段 AST 片段（对比 makeItem 的单组件 type+props）。
                function makePresetItem(p) {
                    var button = document.createElement('button');
                    button.type = 'button';
                    button.className = 'wb-palette-item wb-preset-item';
                    button.draggable = true;
                    // 有缩略图显示图，无则仅文字（label 恒显示）。
                    if (p.thumbnail) {
                        var img = document.createElement('img');
                        img.className = 'wb-preset-thumb';
                        img.src = p.thumbnail;
                        img.alt = p.label || p.id;
                        // 内联尺寸约束（不改 CSS 文件）：缩略图固定高度封面，文字紧随其后。
                        img.style.cssText = 'width:100%;height:52px;object-fit:cover;border-radius:4px;margin-bottom:6px;display:block;background:#f1f5f9;';
                        // 缩略图路径失效（如包内相对路径无静态服务）→ 移除退化为纯文字。
                        img.addEventListener('error', function () { img.remove(); });
                        button.appendChild(img);
                    }
                    var strong = document.createElement('strong');
                    strong.textContent = p.label || p.id;
                    button.appendChild(strong);
                    var span = document.createElement('span');
                    span.textContent = p.category || '区块预设';
                    button.appendChild(span);
                    button.addEventListener('click', function () {
                        self.insertPreset(p);
                        self.showEdit();
                    });
                    button.addEventListener('dragstart', function (event) {
                        // 预设拖拽：复用 globalref 的 id 传值机制（document 已在
                        // meta.presets 内存中，无需把整段 AST 序列化进 DataTransfer）。
                        event.dataTransfer.effectAllowed = 'copy';
                        event.dataTransfer.setData('application/x-wb-preset', p.id);
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
                // 区块预设分组：插件 manifest 声明的预组合 AST 片段（docs/06 §5.2），
                // 作为独立分组挂在组件页签末尾，一键插入整段结构。
                var presets = (meta.presets || []);
                if (presets.length) {
                    makeGroup('区块预设', 'presets', function () {
                        return presets.filter(function (p) {
                            return p && match((p.label || '') + ' ' + (p.category || ''));
                        }).map(makePresetItem);
                    });
                }
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
                var kindLabels = { header: '页眉', footer: '页脚', block: '区块',
                    announcement: '公告栏', sidebar: '侧边栏', breadcrumb: '面包屑', drawer: '抽屉导航', search: '搜索框',
                    cta: 'CTA 段', trust: '信任徽章', brands: '品牌墙', contact: '联系方式', about: '关于我们',
                    banner: '横幅', grid: '多栏布局', snippet: '片段模板' };
                // isTemplate：一次性复制语义（docs/02-D §5）——点击即调 /api/block/clone
                // 复制独立 AST 插入；不允许以引用方式插入（构建期会拒绝 template 引用）。
                function isTemplate(b) { return b.reuseMode === 'template'; }
                function makeBlockButton(b) {
                    var button = document.createElement('button');
                    button.type = 'button';
                    button.className = 'wb-palette-item';
                    var tpl = isTemplate(b);
                    button.draggable = !tpl;
                    button.dataset.type = 'core.globalref';
                    button.innerHTML = '<strong></strong><span></span>';
                    button.querySelector('strong').textContent = b.name;
                    button.querySelector('span').textContent = (kindLabels[b.kind] || '区块') +
                        ' · ' + (tpl ? '复制（点击插入独立副本）' : '引用（点击/拖拽；Shift+点击改为复制）');
                    button.addEventListener('click', function (ev) {
                        if (tpl) { self.insertBlockClone(b); return; }
                        // Shift+点击＝强制复制插入（global 块也可一次性复制，docs/02-D §5.3 显式选择）。
                        if (ev.shiftKey) { self.insertBlockClone(b); return; }
                        self.insertComponent(self.globalRefItem(b));
                        self.showEdit();
                    });
                    if (!tpl) button.addEventListener('dragstart', function (event) {
                        event.dataTransfer.effectAllowed = 'copy';
                        event.dataTransfer.setData('application/x-wb-component', 'core.globalref');
                        event.dataTransfer.setData('application/x-wb-block', b.id);
                    });
                    return button;
                }
                var any = false;
                // 分组策略：页眉/页脚按 kind 分组；区块（block）按 category 细分
                // （对接后续商品区块/文章区块/营销区块等自定义分类）。
                var categoryLabels = { general: '通用区块', product: '商品区块', article: '文章区块', marketing: '营销区块' };
                function appendGroup(title, items) {
                    if (!items.length) return;
                    any = true;
                    var h = document.createElement('div');
                    h.className = 'wb-palette-group';
                    h.textContent = title;
                    root.appendChild(h);
                    items.forEach(function (b) { root.appendChild(makeBlockButton(b)); });
                }
                [['header', '页眉'], ['footer', '页脚']].forEach(function (g) {
                    appendGroup(g[1], (meta.blocks || []).filter(function (b) { return b.kind === g[0] && match(b.name); }));
                });
                // 其余全部类型（block/骨架/内容段/布局/snippet，docs/02-D §4）：按 category 分组。
                var blockHits = (meta.blocks || []).filter(function (b) {
                    return b.kind !== 'header' && b.kind !== 'footer' && match(b.name);
                });
                var byCat = {};
                blockHits.forEach(function (b) {
                    var cat = b.category || 'general';
                    (byCat[cat] = byCat[cat] || []).push(b);
                });
                Object.keys(byCat).sort().forEach(function (cat) {
                    appendGroup(categoryLabels[cat] || (cat + ' 区块'), byCat[cat]);
                });
                if (!any) root.innerHTML = '<p class="wb-empty">还没有全局块。到后台「全局块」创建页眉/页脚/区块后，这里可一键引用。</p>';
            },
            // globalRefItem 全局块的插入描述（引用节点只带 blockId）。
            globalRefItem(b) {
                return { type: 'core.globalref', label: b.name, hint: '全局块', props: { blockId: b.id } };
            },
            // insertBlockClone 「插入-复制」动作（docs/02-D §5.2）：调 /api/block/clone
            // 取独立 AST（全部节点已重生成 ID），复用 insertPreset 顶级平铺插入。
            // 之后与源块互不影响；源块修改不传播（template 语义）。
            insertBlockClone(b) {
                var self = this;
                fetch('/api/block/clone', {
                    method: 'POST',
                    headers: Object.assign({ 'Content-Type': 'application/json' }, csrfHeaders({})),
                    body: JSON.stringify({ id: b.id })
                }).then(function (r) { return r.json(); }).then(function (res) {
                    if (!res || res.code !== 200 || !res.data || !res.data.document) {
                        console.warn('复制块失败', res);
                        return;
                    }
                    var nodes = res.data.document.root;
                    if (!Array.isArray(nodes) || !nodes.length) return;
                    self.insertPreset({ document: nodes });
                    self.showEdit();
                }).catch(function (err) { console.warn('复制块请求失败', err); });
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
            // makeIdAllocator 生成一批插入节点的 ID 分配器：本批内不重复，
            // 也不与当前文档已有 ID 冲突。newId 只查文档，批量生成（父节点 +
            // 默认子节点）时同 base 会撞车，故由分配器统一记账。
            makeIdAllocator() {
                var used = {};
                var self = this;
                return function (base) {
                    var prefix = String(base || 'node').split('-')[0] || 'node';
                    var n = 1;
                    while (used[prefix + '-' + n] || self.findNode(prefix + '-' + n)) n++;
                    used[prefix + '-' + n] = true;
                    return prefix + '-' + n;
                };
            },
            // insertComponent 插入单个组件库条目。
            // 节点构造统一走 palette.js 的 buildInsertNode（插入时不留空壳）：
            //   A 类结构型（tabs/accordion/slider/marquee）自动带默认子节点；
            //   B 类数组型（faq/form/gallery）自动补一条默认项；
            //   C 类必需标量（标题文本 / 图片地址 / 容器语义标签…）缺失时兜底。
            // 于是「从组件库拖入或点击插入」的节点天然满足 Go 侧最小校验。
            insertComponent(item, targetID, placement) {
                if (!item) return;
                var node = buildInsertNode(item, this.makeIdAllocator());
                if (!node) return;
                var targetLocation = this.findLocation(targetID || this.selectedId);
                var target = targetLocation && targetLocation.node;
                // 落点是否为「插入到内部」：容器与结构型组件（tabs/accordion）都算。
                var insideTarget = placement !== 'before' && placement !== 'after' && this.isInsideTarget(target);
                // 嵌套深度守卫：按「目标深度 + 新节点子树深度」判断（与 moveNode 一致）。
                // 新节点可能自带默认子节点（如页签面板），不能再按叶子估算。
                if (insideTarget && this.maxDepth(target) + this.maxDepth(node) > MAX_NEST_DEPTH) {
                    console.warn('嵌套层级超出限制（' + MAX_NEST_DEPTH + ' 层），已取消插入');
                    return;
                }
                this.snapshot();
                if (insideTarget) {
                    target.children = target.children || [];
                    var insideAt = target.children.length; // 追加到末尾：新位置即 children.length
                    target.children.push(node);
                    // 结构型组件（tabs/accordion）：拖入内部即新增面板 / 折叠项，内容数组同步补一条。
                    this.syncAlignFromChildren(target, { op: 'insert', index: insideAt });
                } else if (targetLocation) {
                    var sibAt = targetLocation.index + (placement === 'before' ? 0 : 1);
                    targetLocation.siblings.splice(sibAt, 0, node);
                    // 目标父节点是结构型组件时，插入到某个面板前后 = 新增一个页签 / 折叠项，
                    // 内容数组必须在同一下标补条目，否则「标签数 ≠ 面板数」编译失败。
                    if (targetLocation.parent) this.syncAlignFromChildren(targetLocation.parent, { op: 'insert', index: sibAt });
                } else {
                    // 无插入目标：顶级平铺（root 是森林，Section 直接作为
                    // 顶级节点；容器是按需添加的可选项，无任何包裹兜底）。
                    this.doc.root.push(node);
                }
                this.selectedId = node.id;
                this.renderTree();
                this.syncInspector();
                this.refreshCanvas();
                this.renderUI();
            },
            // insertPreset 一键插入区块预设（docs/06 §5.2）：
            // 预设是整段 AST 数组（非单组件 type+props），深拷贝后递归重写所有
            // 节点 ID（保留 props/结构，仅重写 ID 保证唯一），再顶级平铺进 root。
            // 与 insertComponent 互补：insertComponent 插入单个 paletteItem，本方法插入整段。
            insertPreset(preset) {
                if (!preset) return;
                var nodes = preset.document;
                if (!Array.isArray(nodes) || !nodes.length) return;
                this.snapshot();
                // 深拷贝预设 AST，避免污染 meta.presets 源数据（后续插入可重复用）。
                nodes = clone(nodes);
                var self = this;
                // 递归重写 ID：与 pasteInto 同源，但保留 document 里的 props/结构。
                // 必须用批内记账的分配器：newId 只查「已入档」节点，而本批节点要等
                // 全部重写完成后才 push 进 root，用 newId 会让同批同前缀节点撞成同一个 ID
                // （删除命中错误节点 + 构建期 ValidateNodeID 拒绝发布）。
                var allocId = this.makeIdAllocator();
                (function assign(list) {
                    (list || []).forEach(function (n) {
                        n.id = allocId(n.id || 'node');
                        assign(n.children);
                    });
                })(nodes);
                // 顶级平铺：本项目无强制根容器，预组合区块（Section）顶级平铺是一等形态。
                for (var i = 0; i < nodes.length; i++) this.doc.root.push(nodes[i]);
                // 选中首个节点，便于立即编辑。
                this.selectedId = (nodes[0] && nodes[0].id) || '';
                this.renderTree();
                this.syncInspector();
                this.refreshCanvas();
                this.renderUI();
            },

            // ---------------- 画布联动 ----------------
            // refreshCanvas 调度版：250ms 防抖合并。连续 AST 变更（拖拽/滑块/连续
            // 字段提交）只触发一次 iframe 提交，最后一次以最新 doc 为准；
            // 结构性/需要立即反馈的场景（保存成功、历史回放）用 flushCanvas 强制提交。
            refreshCanvas() {
                var self = this;
                if (!document.getElementById('wb-canvas')) return;
                if (this._canvasTimer) return; // 已排程：等待合并，提交时使用最新 doc
                this._canvasTimer = setTimeout(function () {
                    self._canvasTimer = null;
                    self.submitCanvas();
                }, 250);
            },
            // flushCanvas 立即提交当前画布（清掉未决的防抖定时器）。
            flushCanvas() {
                if (this._canvasTimer) {
                    clearTimeout(this._canvasTimer);
                    this._canvasTimer = null;
                }
                this.submitCanvas();
            },
            // submitCanvas 实际提交：整文档 JSON → /workbench/preview 重载 iframe。
            submitCanvas() {
                var frame = document.getElementById('wb-canvas');
                if (!frame) return;
                if (meta.saveBase === 'template') {
                    this.submitTemplateCanvas();
                    return;
                }
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
                    // 原生表单提交无法携带自定义头，CSRF token 走隐藏域（中间件支持表单字段校验）。
                    var csrf = document.createElement('input'); csrf.type = 'hidden'; csrf.name = 'csrf_token'; form.appendChild(csrf);
                    document.body.appendChild(form);
                }
                form.elements.id.value = meta.pageId;
                form.elements.expectedVersion.value = this.draftVersion;
                form.elements.draftDocument.value = JSON.stringify(this.doc);
                form.elements.csrf_token.value = getCSRFToken();
                form.submit();
            },
            submitTemplateCanvas() {
                var frame = document.getElementById('wb-canvas');
                if (!frame) return;
                var form = document.getElementById('wb-template-preview-form');
                if (!form) {
                    form = document.createElement('form');
                    form.id = 'wb-template-preview-form';
                    form.method = 'POST';
                    form.action = '/workbench/template/preview';
                    form.target = frame.name || 'wb-canvas';
                    form.style.display = 'none';
                    ['id', 'entityType', 'entityId', 'projectId', 'draftDocument', 'csrf_token'].forEach(function (name) {
                        var el = document.createElement('input');
                        el.name = name;
                        form.appendChild(el);
                    });
                    document.body.appendChild(form);
                }
                form.elements.id.value = meta.pageId;
                form.elements.entityType.value = meta.entityType || '';
                form.elements.entityId.value = meta.entityId || '';
                form.elements.projectId.value = meta.projectId || '';
                form.elements.draftDocument.value = JSON.stringify(this.doc);
                form.elements.csrf_token.value = getCSRFToken();
                form.submit();
            },
            // fetchCanvasHTML 拉取整页预览 HTML（不重载 iframe），供局部刷新提取节点片段。
            fetchCanvasHTML() {
                var body = new URLSearchParams();
                if (meta.saveBase === 'template') {
                    body.set('id', meta.pageId);
                    body.set('entityType', meta.entityType || '');
                    body.set('entityId', meta.entityId || '');
                    body.set('projectId', meta.projectId || '');
                    body.set('draftDocument', JSON.stringify(this.doc));
                    body.set('csrf_token', getCSRFToken());
                    return fetch('/workbench/template/preview', {
                        method: 'POST',
                        headers: csrfHeaders({ 'Content-Type': 'application/x-www-form-urlencoded' }),
                        body: body.toString(),
                    }).then(function (res) {
                        if (!res.ok) return Promise.reject(new Error('preview ' + res.status));
                        return res.text();
                    });
                }
                body.set('id', meta.pageId);
                body.set('expectedVersion', this.draftVersion);
                body.set('draftDocument', JSON.stringify(this.doc));
                body.set('csrf_token', getCSRFToken());
                return fetch('/workbench/preview', {
                    method: 'POST',
                    headers: csrfHeaders({ 'Content-Type': 'application/x-www-form-urlencoded' }),
                    body: body.toString(),
                }).then(function (res) {
                    if (!res.ok) return Promise.reject(new Error('preview ' + res.status));
                    return res.text();
                });
            },
            // patchCanvas 局部刷新：把目标节点的新 DOM 与整页 CSS 送进 iframe 替换，
            // 不重载 iframe（滚动位置/输入焦点保持）；失败或无节点上下文回退整页刷新。
            patchCanvas(nodeId) {
                var self = this;
                var frame = document.getElementById('wb-canvas');
                if (!nodeId || !frame || !frame.contentWindow) { this.refreshCanvas(); return; }
                // 连续改动会产生并发请求：只接受最后一次的响应，避免旧 HTML 覆盖新状态。
                var seq = (this._patchSeq = (this._patchSeq || 0) + 1);
                this.fetchCanvasHTML().then(function (html) {
                    if (self._patchSeq !== seq) return;
                    var doc = new DOMParser().parseFromString(html, 'text/html');
                    var fresh = doc.querySelector('.sky-c-' + nodeId);
                    if (!fresh) { self.refreshCanvas(); return; }
                    var styleEl = doc.querySelector('style');
                    frame.contentWindow.postMessage({
                        type: 'wb-patch',
                        id: nodeId,
                        html: fresh.outerHTML,
                        css: styleEl ? styleEl.textContent : null,
                    }, location.origin);
                }).catch(function () { self.refreshCanvas(); });
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
                    var target = event.target.closest && event.target.closest('[data-sky-id]');
                    clearDropMarks();
                    if (!target) return;
                    var bounds = target.getBoundingClientRect();
                    var offset = event.clientY - bounds.top;
                    var isContainer = (target.getAttribute('data-sky-id') && (self.findNode(target.getAttribute('data-sky-id')) || {}).type === 'core.container');
                    var placement = isContainer && offset > bounds.height * .25 && offset < bounds.height * .75 ? 'inside' : (offset < bounds.height / 2 ? 'before' : 'after');
                    target.classList.add('wb-drop-' + placement);
                });
                doc.addEventListener('dragleave', function (event) {
                    if (!event.relatedTarget) clearDropMarks();
                });
                doc.addEventListener('drop', function (event) {
                    event.preventDefault();
                    clearDropMarks();
                    var target = event.target.closest && event.target.closest('[data-sky-id]');
                    var targetID = target && target.getAttribute('data-sky-id');
                    // 预设拖入：按 id 找回预设并整段插入（与点击一致的顶级平铺）。
                    var presetID = event.dataTransfer.getData('application/x-wb-preset');
                    if (presetID) {
                        var preset = (meta.presets || []).filter(function (x) { return x.id === presetID; })[0];
                        if (preset) self.insertPreset(preset);
                        return;
                    }
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
                var el = win.document.querySelector('[data-sky-id="' + id + '"]');
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
                // 整棵粘贴子树共用一个 ID 分配器：newId 只查文档，批量重写时同前缀会全部
                // 落成 node-1（子树内 ID 重复 → 校验直接拒绝）。
                var alloc = this.makeIdAllocator();
                (function assign(list) { (list || []).forEach(function (c) { c.id = alloc(c.id || 'node'); assign(c.children); }); })([node]);
                if (parentId) {
                    var parent = this.findNode(parentId);
                    if (!parent) { this.doc.root.push(node); }
                    else {
                        parent.children = parent.children || [];
                        var pasteAt = parent.children.length; // 追加到末尾
                        parent.children.push(node);
                        // 父节点是结构型组件（tabs/accordion）：粘贴 / 复制面板即新增一条标签 / 标题。
                        this.syncAlignFromChildren(parent, { op: 'insert', index: pasteAt });
                    }
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
                // 同 pasteInto：整棵子树共用一个分配器，避免批量重写撞出重复 ID。
                var alloc = this.makeIdAllocator();
                (function assign(list) { (list || []).forEach(function (c) { c.id = alloc(c.id || 'node'); assign(c.children); }); })([node]);
                var loc = this.findLocation(nodeId);
                if (!loc) return;
                var afterAt = loc.index + 1;
                loc.siblings.splice(afterAt, 0, node);
                // 目标父节点是结构型组件：粘贴到面板之后 = 新增一个页签 / 折叠项。
                if (loc.parent) this.syncAlignFromChildren(loc.parent, { op: 'insert', index: afterAt });
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

};
