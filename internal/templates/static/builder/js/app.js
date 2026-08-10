(function (global) {
    'use strict';

    const storageKey = 'go_wp.builder.document.v2';

    global.builderApp = function builderApp() {
        const editor = new global.BuilderEditor.Editor();
        let renderScheduled = false;

        return {
            components: global.BuilderComponents.all(),
            categories: [
                { name: 'layout', label: '布局容器' },
                { name: 'basic', label: '基础内容' },
                { name: 'media', label: '媒体' },
                { name: 'interactive', label: '交互' }
            ],
            selectedId: null,
            revision: 0,
            notice: '',
            noticeTimer: null,
            panelLeftVisible: true,
            panelRightVisible: true,
            panelMode: 'add',        // 'add' | 'page' | 'edit'
            pageSettings: global.BuilderEditor.createDefaultSettings(),
            pageCodeError: '',
            isGenerating: false,
            editorTab: 'content',
            previewDevice: 'desktop',
            previewDevices: [
                { key: 'desktop', label: '桌面', width: '100%' },
                { key: 'tablet', label: '平板', width: '768px' },
                { key: 'mobile', label: '手机', width: '390px' }
            ],
            expandedItems: {},        // { id: true } for tree expand
            structureItems: [],       // AST 的结构树投影
            treeDrag: null,            // 当前结构树拖拽上下文
            treePointerStartX: null,   // 用于判断左右层级变化
            insertionParentId: null,   // 从空容器打开组件库时的插入目标
            unitDrafts: {},            // 空值控件当前选择的单位
            dimensionLinkDrafts: {},   // 四边控件是否联动
            dimensionSides: [
                { key: 'top', label: '上' },
                { key: 'right', label: '右' },
                { key: 'bottom', label: '下' },
                { key: 'left', label: '左' }
            ],
            // 媒体库选择器状态
            mediaPickerOpen: false,
            mediaPickerTarget: null,   // { field, itemIndex?, subField?, multi? } 回填目标
            mediaPickerAdded: 0,       // 多选模式下本次已追加的数量
            mediaItems: [],
            mediaTotal: 0,
            mediaPage: 1,
            mediaLimit: 12,
            mediaUploading: false,

            get inspectorTabs() {
                return global.BuilderInspector.tabsFor(this.selectedNode);
            },

            get inspectorGroups() {
                return global.BuilderInspector.groupsFor(this.selectedNode, this.editorTab);
            },

            get selectedNode() {
                this.revision;
                return editor.getSelected();
            },

            get isContainerSelected() {
                return global.BuilderInspector.isContainer(this.selectedNode);
            },

            get canUndo() {
                this.revision;
                return editor.history.length > 0;
            },

            get canRedo() {
                this.revision;
                return editor.future.length > 0;
            },

            /* ---------- 结构树 ---------- */
            refreshStructureItems() {
                const items = [];
                const visit = (node, depth) => {
                    if (!node) { return; }
                    const definition = global.BuilderComponents.getDefinition(node);
                    // 容器和支持子节点的交互组件
                    const canHaveChildren = node.type === 'container' || 
                        ['tabs', 'accordion', 'toggle', 'modal'].includes(node.type);
                    items.push({
                        id: node.id,
                        depth,
                        label: definition?.label || node.type,
                        icon: definition?.icon || '?',
                        isContainer: canHaveChildren,
                        hasChildren: canHaveChildren && node.children?.length > 0
                    });
                    if (canHaveChildren && this.expandedItems[node.id] !== false) {
                        node.children.forEach((child) => visit(child, depth + 1));
                    }
                };

                const root = editor.document.root;
                items.push({
                    id: root.id,
                    depth: 0,
                    label: '页面',
                    icon: '◎',
                    isContainer: true,
                    hasChildren: root.children.length > 0
                });
                if (this.expandedItems[root.id] !== false) {
                    root.children.forEach((child) => visit(child, 1));
                }
                this.structureItems = items;
            },

            toggleExpand(nodeId) {
                if (this.expandedItems[nodeId] === false) {
                    this.expandedItems[nodeId] = true;
                } else {
                    this.expandedItems[nodeId] = false;
                }
                this.refreshStructureItems();
                this.revision += 1;
            },

            selectStructureNode(nodeId) {
                // 根节点（页面）点击 → 打开页面设置面板
                if (nodeId === editor.document.root.id) {
                    editor.select(null);
                    this.switchPanelMode('page');
                    return;
                }
                editor.select(nodeId);
                this.panelMode = 'edit';
            },

            /* ---------- 面板模式切换 ---------- */
            switchPanelMode(mode) {
                if (!['add', 'page', 'edit'].includes(mode)) { return; }
                this.panelMode = mode;
                this.insertionParentId = null;
                if (mode === 'page') {
                    this.pageCodeError = '';
                    this.refreshPageSettings();
                }
                if (mode === 'add') {
                    this.$nextTick(() => this.initSortable());
                }
            },

            refreshPageSettings() {
                this.pageSettings = global.BuilderEditor.mergeSettings(editor.document.settings);
            },

            pageSettingValue(section, key) {
                this.revision;
                return this.pageSettings?.[section]?.[key] ?? '';
            },

            updatePageSetting(section, key, value) {
                editor.updateSettings(section, { [key]: value });
            },

            /* 页面设置：四边盒值读写（如 style.marginTop + style.marginUnit） */
            pageDimensionValue(section, prefix, side) {
                this.revision;
                const value = this.pageSettings?.[section]?.[`${prefix}${side}`];
                return value === '' || value === undefined ? '' : String(value);
            },

            updatePageDimension(section, prefix, side, value) {
                const unit = this.pageSettings?.[section]?.[`${prefix}Unit`] || 'px';
                const normalized = value === '' ? '' : String(Number(value));
                if (normalized !== '' && !Number.isFinite(Number(normalized))) { return; }
                editor.updateSettings(section, { [`${prefix}${side}`]: normalized === '' ? '' : `${normalized}${unit}` });
            },

            /* 页面设置：背景图走媒体库选择（单选回填） */
            openPageMediaPicker(key) {
                this.mediaPickerTarget = {
                    field: { key, target: 'props' },
                    itemIndex: null,
                    subField: null,
                    multi: false,
                    pageSetting: { section: 'style', key }
                };
                this.mediaPickerAdded = 0;
                this.mediaPickerOpen = true;
                this.mediaPage = 1;
                this.loadMediaPage(1);
            },

            inspectorLabel(node) {
                return global.BuilderInspector.labelFor(node);
            },

            inspectorIcon(node) {
                return global.BuilderInspector.iconFor(node);
            },

            defaultEditorTab(node) {
                return global.BuilderInspector.tabsFor(node)[0]?.key || 'content';
            },

            /* ---------- 组件面板 ---------- */
            componentsFor(category) {
                return this.components.filter((component) => component.category === category);
            },

            init() {
                global.builderEditor = editor;
                editor.addEventListener('documentChanged', () => {
                    this.refreshPageSettings();
                    this.refreshStructureItems();
                    this.revision += 1;
                    this.scheduleRender();
                });
                editor.addEventListener('selectionChanged', (event) => {
                    this.selectedId = editor.selectedId;
                    this.revision += 1;
                    this.scheduleRender();
                    if (editor.selectedId) {
                        this.panelMode = 'edit';
                        this.editorTab = this.defaultEditorTab(event.detail.node);
                    }
                });

                this.restoreSavedDocument();
                this.restoreRequestedDocument();
                this.refreshPageSettings();
                this.refreshStructureItems();
                this.$nextTick(() => {
                    this.initSortable();
                    this.initTreeSortable();
                    this.render();
                });

                global.addEventListener('keydown', (event) => this.onKeydown(event));
            },

            scheduleRender() {
                if (renderScheduled) { return; }
                renderScheduled = true;
                requestAnimationFrame(() => {
                    renderScheduled = false;
                    this.render();
                });
            },

            render() {
                if (!this.$refs.preview) { return; }
                global.BuilderRenderer.renderFrame(
                    this.$refs.preview,
                    editor.document,
                    editor.selectedId,
                    {
                        onSelect: (nodeId) => this.selectNode(nodeId),
                        onDrop: (componentKey, parentId) => this.addComponent(componentKey, parentId),
                        onOpenLibrary: (parentId) => this.openComponentLibrary(parentId),
                        onInlineEdit: (nodeId, field, value) => this.updateInlineField(nodeId, field, value),
                        onCodeError: (message) => {
                            this.pageCodeError = message;
                            if (message) { this.showNotice('页面自定义代码运行错误'); }
                        }
                    }
                );
            },

            /* ---------- SortableJS ---------- */
            initSortable() {
                if (!global.Sortable) { return; }
                this.$root.querySelectorAll('.component-list').forEach((list) => {
                    global.Sortable.create(list, {
                        group: { name: 'builder-components', pull: 'clone', put: false },
                        sort: false,
                        animation: 120,
                        fallbackOnBody: true
                    });
                });
            },

            initTreeSortable() {
                const tree = this.$refs.structureTree;
                if (!tree || !global.Sortable || tree._builderSortable) { return; }
                tree.addEventListener('pointerdown', (event) => {
                    this.treePointerStartX = event.clientX;
                });
                tree._builderSortable = global.Sortable.create(tree, {
                    animation: 150,
                    fallbackOnBody: true,
                    draggable: '.structure-item:not(.is-root)',
                    ghostClass: 'structure-item-ghost',
                    chosenClass: 'structure-item-chosen',
                    onStart: (event) => {
                        const pointerX = Number.isFinite(this.treePointerStartX)
                            ? this.treePointerStartX
                            : event.item.getBoundingClientRect().left;
                        this.treeDrag = {
                            nodeId: event.item.dataset.nodeId,
                            startDepth: Number(event.item.dataset.depth || 1),
                            startX: pointerX,
                            lastX: pointerX
                        };
                    },
                    onMove: (event) => {
                        const pointerX = event.originalEvent?.clientX;
                        if (this.treeDrag && Number.isFinite(pointerX)) {
                            this.treeDrag.lastX = pointerX;
                        }
                        return event.related.classList.contains('is-root') ? 1 : true;
                    },
                    onEnd: (event) => {
                        const drag = this.treeDrag;
                        this.treeDrag = null;
                        this.treePointerStartX = null;
                        if (!drag?.nodeId) { return; }
                        try {
                            const depthDelta = Math.round((drag.lastX - drag.startX) / 20);
                            const desiredDepth = Math.max(1, drag.startDepth + depthDelta);
                            const destination = this._resolveTreeDestination(event.item, drag.nodeId, desiredDepth);
                            if (destination) {
                                editor.moveNode(drag.nodeId, destination.parentId, destination.index);
                                this.expandedItems[destination.parentId] = true;
                            }
                        } catch (error) {
                            this.showNotice(error.message);
                            this.revision += 1;
                        }
                    }
                });
            },

            _resolveTreeDestination(draggedElement, nodeId, desiredDepth) {
                let parentId = editor.document.root.id;
                let actualDepth = 1;
                let cursor = draggedElement.previousElementSibling;

                while (cursor && desiredDepth > 1) {
                    const candidateId = cursor.dataset.nodeId;
                    const candidateDepth = Number(cursor.dataset.depth || 0);
                    const candidate = editor.nodeIndex.get(candidateId)?.node;
                    if (candidate?.type === 'container' && candidateDepth < desiredDepth &&
                        candidateId !== nodeId && !editor.isDescendant(candidateId, nodeId)) {
                        parentId = candidateId;
                        actualDepth = candidateDepth + 1;
                        break;
                    }
                    cursor = cursor.previousElementSibling;
                }

                if (actualDepth === 1 && desiredDepth > 1) {
                    const sourceParent = editor.nodeIndex.get(nodeId)?.parent;
                    if (sourceParent && sourceParent.id !== editor.document.root.id) {
                        parentId = sourceParent.id;
                    }
                }

                let index = 0;
                cursor = draggedElement.previousElementSibling;
                while (cursor) {
                    const siblingId = cursor.dataset.nodeId;
                    const siblingEntry = editor.nodeIndex.get(siblingId);
                    if (siblingId !== nodeId && siblingEntry?.parent?.id === parentId) {
                        index += 1;
                    }
                    cursor = cursor.previousElementSibling;
                }
                return { parentId, index };
            },

            /* ---------- 拖拽 ---------- */
            onDragStart(event, componentKey) {
                event.dataTransfer.effectAllowed = 'copy';
                event.dataTransfer.setData('application/x-builder-component', componentKey);
                event.dataTransfer.setData('text/plain', componentKey);
            },

            onCanvasDrop(event) {
                const key = event.dataTransfer.getData('application/x-builder-component') || event.dataTransfer.getData('text/plain');
                this.addComponent(key, editor.document.root.id);
            },

            onCanvasClick(event) {
                // Click on empty canvas area (not on a node) → switch to component library
                if (!event.target.closest('[data-node-id]')) {
                    this.switchPanelMode('add');
                    editor.select(null);
                }
            },

            openComponentLibrary(parentId) {
                const target = editor.nodeIndex.get(parentId)?.node;
                this.insertionParentId = target?.type === 'container'
                    ? parentId
                    : editor.document.root.id;
                this.panelLeftVisible = true;
                this.panelMode = 'add';
                this.$nextTick(() => this.initSortable());
            },

            addComponentDirectly(componentKey) {
                let parentId = this.insertionParentId;
                const insertionTarget = parentId ? editor.nodeIndex.get(parentId)?.node : null;
                if (!insertionTarget || insertionTarget.type !== 'container') {
                    parentId = editor.document.root.id;
                    const selected = editor.getSelected();
                    if (selected) {
                        const entry = editor.nodeIndex.get(selected.id);
                        if (entry && entry.parent) {
                            parentId = entry.parent.id;
                        }
                    }
                }
                this.addComponent(componentKey, parentId);
            },

            addComponent(componentKey, parentId) {
                if (!componentKey) { return; }
                try {
                    const node = global.BuilderComponents.createNode(componentKey);
                    editor.addNode(parentId || editor.document.root.id, node);
                    this.expandedItems[node.id] = true;
                } catch (error) {
                    this.showNotice(error.message);
                }
            },

            selectNode(nodeId) {
                editor.select(nodeId);
                if (nodeId) {
                    this.panelMode = 'edit';
                }
            },

            updateInlineField(nodeId, field, value) {
                const entry = editor.nodeIndex.get(nodeId);
                if (!entry || !Object.hasOwn(entry.node.props, field)) { return; }
                editor.updateProps(nodeId, { [field]: value });
            },

            /* ---------- Inspector 控件协议 ---------- */
            controlValue(field) {
                const source = field.target === 'props'
                    ? this.selectedNode?.props
                    : this.selectedNode?.style;
                return source?.[field.key] ?? global.BuilderInspector.defaultValueFor(this.selectedNode, field);
            },

            controlColorValue(field) {
                const value = String(this.controlValue(field));
                return /^#[0-9a-f]{6}$/i.test(value) ? value : '#000000';
            },

            updateControl(field, value) {
                if (!editor.selectedId) { return; }
                if (field.target === 'props') {
                    editor.updateProps(editor.selectedId, { [field.key]: value });
                    return;
                }
                editor.updateStyle(editor.selectedId, { [field.key]: String(value) });
            },

            /* ---------- 富文本（WYSIWYG，对齐 Elementor text-editor） ---------- */
            wysiwygSync(field, el) {
                const value = this.selectedNode?.props?.[field.key] ?? '';
                if (el.innerHTML !== value) {
                    el.innerHTML = value;
                }
            },

            wysiwygInput(field, event) {
                if (!editor.selectedId) { return; }
                editor.updateProps(editor.selectedId, { [field.key]: event.target.innerHTML });
            },

            wysiwygExec(field, command) {
                if (!editor.selectedId) { return; }
                const el = this.$root?.querySelector(`[data-field-key="${field.key}"]`);
                if (!el) { return; }
                el.focus();
                let value;
                if (command === 'createLink') {
                    value = global.prompt('输入链接地址', 'https://');
                    if (!value) { return; }
                }
                global.document.execCommand(command, false, value);
                editor.updateProps(editor.selectedId, { [field.key]: el.innerHTML });
                this.revision += 1;
            },

            repeaterItems(field) {
                const value = this.selectedNode?.props?.[field.key];
                return Array.isArray(value) ? value : [];
            },

            updateRepeaterItem(field, index, key, value) {
                if (!editor.selectedId) { return; }
                editor.updateArrayProp(editor.selectedId, field.key, (items) => items.map((item, itemIndex) =>
                    itemIndex === index ? { ...item, [key]: value } : item
                ));
            },

            updateRepeaterItemField(field, index, key, value) {
                if (!editor.selectedId) { return; }
                editor.updateArrayProp(editor.selectedId, field.key, (items) => items.map((item, itemIndex) =>
                    itemIndex === index ? { ...item, [key]: value } : item
                ));
            },

            addRepeaterItem(field) {
                if (!editor.selectedId) { return; }
                const defaultItem = {};
                if (field.fields && Array.isArray(field.fields)) {
                    field.fields.forEach(subField => {
                        if (subField.type === 'checkbox') {
                            defaultItem[subField.key] = false;
                        } else if (subField.type === 'number') {
                            defaultItem[subField.key] = subField.placeholder || 0;
                        } else {
                            defaultItem[subField.key] = subField.placeholder || '';
                        }
                    });
                } else {
                    // 向后兼容：Tab 的旧格式
                    const items = this.repeaterItems(field);
                    defaultItem.title = `选项卡 ${items.length + 1}`;
                    defaultItem.content = `选项卡 ${items.length + 1} 的内容`;
                }
                editor.updateArrayProp(editor.selectedId, field.key, (items) => [...items, defaultItem]);
            },

            removeRepeaterItem(field, index) {
                if (!editor.selectedId || this.repeaterItems(field).length <= 1) { return; }
                editor.updateArrayProp(editor.selectedId, field.key, (items) => items.filter((_, itemIndex) => itemIndex !== index));
            },

            moveRepeaterItem(field, index, offset) {
                const target = index + offset;
                if (!editor.selectedId || target < 0 || target >= this.repeaterItems(field).length) { return; }
                editor.updateArrayProp(editor.selectedId, field.key, (items) => {
                    const [item] = items.splice(index, 1);
                    items.splice(target, 0, item);
                    return items;
                });
            },

            /* ---------- 媒体库选择器 ---------- */
            get mediaTotalPages() {
                return Math.max(1, Math.ceil(this.mediaTotal / this.mediaLimit));
            },

            mediaTypeBadge(fileType) {
                const badges = { image: '图片', video: '视频', audio: '音频', document: '文档' };
                return badges[fileType] || '文件';
            },

            openMediaPicker(field, itemIndex, subField) {
                // mediaMulti=true 的 repeater 字段进入多选追加模式
                const multi = !!(field.mediaMulti);
                this.mediaPickerTarget = {
                    field,
                    itemIndex: itemIndex ?? null,
                    subField: subField ?? null,
                    multi
                };
                this.mediaPickerAdded = 0;
                this.mediaPickerOpen = true;
                this.mediaPage = 1;
                this.loadMediaPage(1);
            },

            async loadMediaPage(page) {
                if (page < 1) { return; }
                try {
                    const response = await fetch(`/api/media/list?page=${page}&limit=${this.mediaLimit}`);
                    const result = await response.json();
                    if (!response.ok || result.code !== 200) {
                        throw new Error(result.message || '加载媒体列表失败');
                    }
                    this.mediaItems = result.data?.list || [];
                    this.mediaTotal = result.data?.total || 0;
                    this.mediaPage = result.data?.page || page;
                } catch (error) {
                    this.showNotice(error.message || '加载媒体列表失败');
                }
            },

            async uploadMedia(event) {
                const file = event.target.files?.[0];
                if (!file) { return; }
                this.mediaUploading = true;
                try {
                    const form = new FormData();
                    form.append('file', file);
                    const response = await fetch('/api/media/upload', { method: 'POST', body: form });
                    const result = await response.json();
                    if (!response.ok || result.code !== 200) {
                        throw new Error(result.message || '上传失败');
                    }
                    this.selectMedia(result.data);
                    this.loadMediaPage(this.mediaPage);
                } catch (error) {
                    this.showNotice(error.message || '上传失败');
                } finally {
                    this.mediaUploading = false;
                    event.target.value = '';
                }
            },

            selectMedia(item) {
                const target = this.mediaPickerTarget;
                if (!target || !item?.url) { return; }

                // 多选追加模式：新增一个图库项，弹窗保持打开，可继续点选（含重复图片）
                if (target.multi && target.field && editor.selectedId) {
                    editor.updateArrayProp(editor.selectedId, target.field.key, (items) => {
                        const newItem = {};
                        (target.field.fields || []).forEach((subField) => {
                            if (subField.key === 'src' || subField.key === 'imageSrc') {
                                newItem[subField.key] = item.url;
                            } else if (subField.type === 'checkbox') {
                                newItem[subField.key] = false;
                            } else if (subField.type === 'number') {
                                newItem[subField.key] = subField.placeholder || 0;
                            } else {
                                newItem[subField.key] = subField.placeholder || '';
                            }
                        });
                        return [...items, newItem];
                    });
                    this.mediaPickerAdded += 1;
                    return;
                }

                // 单选回填模式：页面设置（如背景图）优先于组件字段
                if (target.pageSetting) {
                    this.updatePageSetting(target.pageSetting.section, target.pageSetting.key, item.url);
                } else if (target.subField && target.itemIndex !== null) {
                    this.updateRepeaterItemField(target.field, target.itemIndex, target.subField.key, item.url);
                } else {
                    this.updateControl(target.field, item.url);
                }
                this.mediaPickerOpen = false;
            },

            async deleteMedia(id) {
                if (!global.confirm('确定删除该媒体吗？此操作不可恢复。')) { return; }
                try {
                    const response = await fetch('/api/media/delete', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ id })
                    });
                    const result = await response.json();
                    if (!response.ok || result.code !== 200) {
                        throw new Error(result.message || '删除失败');
                    }
                    this.showNotice('媒体已删除');
                    if (this.mediaItems.length === 1 && this.mediaPage > 1) {
                        this.loadMediaPage(this.mediaPage - 1);
                    } else {
                        this.loadMediaPage(this.mediaPage);
                    }
                } catch (error) {
                    this.showNotice(error.message || '删除失败');
                }
            },

            styleValue(key) {
                return this.selectedNode?.style?.[key] ?? '';
            },

            parseCSSLength(value, defaultUnit) {
                const match = String(value ?? '').trim().match(/^(-?(?:\d+\.?\d*|\.\d+))([a-z%]+)?$/i);
                return match ? { number: match[1], unit: match[2] || defaultUnit } : null;
            },

            lengthDraftKey(field) {
                return `${this.selectedId}:${field.key}`;
            },

            parseLengthValue(field) {
                return this.parseCSSLength(this.controlValue(field), field.defaultUnit);
            },

            lengthNumber(field) {
                return this.parseLengthValue(field)?.number || '';
            },

            lengthUnit(field) {
                return this.parseLengthValue(field)?.unit || this.unitDrafts[this.lengthDraftKey(field)] || field.defaultUnit;
            },

            updateLengthNumber(field, value) {
                if (!editor.selectedId) { return; }
                if (value === '') {
                    this.updateControl(field, '');
                    return;
                }
                const number = Number(value);
                if (!Number.isFinite(number)) { return; }
                const normalized = field.min === 0 ? Math.max(0, number) : number;
                this.updateControl(field, `${normalized}${this.lengthUnit(field)}`);
            },

            updateLengthUnit(field, unit) {
                if (!editor.selectedId || !field.units.includes(unit)) { return; }
                this.unitDrafts[this.lengthDraftKey(field)] = unit;
                const number = this.lengthNumber(field);
                if (number !== '') {
                    this.updateControl(field, `${number}${unit}`);
                }
            },

            dimensionDraftKey(field) {
                return `${this.selectedId}:${field.key}`;
            },

            dimensionValues(field) {
                const style = this.selectedNode?.style || {};
                const shorthand = String(style[field.key] || '').trim().split(/\s+/).filter(Boolean);
                const expanded = shorthand.length === 1
                    ? [shorthand[0], shorthand[0], shorthand[0], shorthand[0]]
                    : shorthand.length === 2
                        ? [shorthand[0], shorthand[1], shorthand[0], shorthand[1]]
                        : shorthand.length === 3
                            ? [shorthand[0], shorthand[1], shorthand[2], shorthand[1]]
                            : shorthand.slice(0, 4);
                return this.dimensionSides.reduce((values, side, index) => {
                    values[side.key] = style[`${field.key}-${side.key}`] || expanded[index] || '';
                    return values;
                }, {});
            },

            dimensionNumber(field, side) {
                return this.parseCSSLength(this.dimensionValues(field)[side], field.defaultUnit)?.number || '';
            },

            dimensionUnit(field) {
                const values = this.dimensionValues(field);
                const used = this.dimensionSides
                    .map((side) => this.parseCSSLength(values[side.key], field.defaultUnit)?.unit)
                    .find(Boolean);
                return used || this.unitDrafts[this.dimensionDraftKey(field)] || field.defaultUnit;
            },

            dimensionLinked(field) {
                return this.dimensionLinkDrafts[this.dimensionDraftKey(field)] !== false;
            },

            toggleDimensionLink(field) {
                const key = this.dimensionDraftKey(field);
                this.dimensionLinkDrafts[key] = !this.dimensionLinked(field);
                this.revision += 1;
            },

            updateDimensionNumber(field, side, value) {
                if (!editor.selectedId) { return; }
                const unit = this.dimensionUnit(field);
                const normalized = value === ''
                    ? ''
                    : String(field.allowNegative ? Number(value) : Math.max(0, Number(value)));
                if (normalized !== '' && !Number.isFinite(Number(normalized))) { return; }
                const sides = this.dimensionLinked(field)
                    ? this.dimensionSides.map((item) => item.key)
                    : [side];
                const patch = { [field.key]: '' };
                sides.forEach((key) => { patch[`${field.key}-${key}`] = normalized === '' ? '' : `${normalized}${unit}`; });
                editor.updateStyle(editor.selectedId, patch);
            },

            updateDimensionUnit(field, unit) {
                if (!editor.selectedId || !field.units.includes(unit)) { return; }
                this.unitDrafts[this.dimensionDraftKey(field)] = unit;
                const values = this.dimensionValues(field);
                const patch = { [field.key]: '' };
                this.dimensionSides.forEach((side) => {
                    const number = this.parseCSSLength(values[side.key], field.defaultUnit)?.number;
                    if (number !== undefined) {
                        patch[`${field.key}-${side.key}`] = `${number}${unit}`;
                    }
                });
                if (Object.keys(patch).length > 1) {
                    editor.updateStyle(editor.selectedId, patch);
                } else {
                    this.revision += 1;
                }
            },

            setPreviewDevice(device) {
                if (this.previewDevices.some((item) => item.key === device)) {
                    this.previewDevice = device;
                }
            },

            togglePanelLeft() {
                this.panelLeftVisible = !this.panelLeftVisible;
            },

            togglePanelRight() {
                this.panelRightVisible = !this.panelRightVisible;
            },

            deleteSelected() {
                if (editor.selectedId) {
                    editor.deleteNode(editor.selectedId);
                }
            },

            duplicateSelected() {
                if (editor.selectedId) {
                    editor.duplicateNode(editor.selectedId);
                }
            },

            /* ---------- 历史 ---------- */
            undo() { editor.undo(); },
            redo() { editor.redo(); },

            /* ---------- 持久化与测试构建 ---------- */
            async generateAndVisit() {
                if (this.isGenerating) { return; }
                this.isGenerating = true;
                try {
                    const pageID = editor.document.id;
                    const editURL = `/builder?document=${encodeURIComponent(pageID)}`;
                    const html = global.BuilderRenderer.renderPublishedDocument(editor.document, editURL);
                    localStorage.setItem(storageKey, editor.toJSON());
                    const response = await fetch('/builder/generate', {
                        method: 'POST',
                        headers: { 'Content-Type': 'application/json' },
                        body: JSON.stringify({ pageId: pageID, html, document: editor.document })
                    });
                    const result = await response.json();
                    if (!response.ok || result.code !== 0 || !result.data?.url) {
                        throw new Error(result.message || '生成测试 HTML 失败');
                    }
                    global.location.assign(result.data.url);
                } catch (error) {
                    this.showNotice(error.message || '生成测试 HTML 失败');
                    this.isGenerating = false;
                }
            },

            async restoreRequestedDocument() {
                const pageID = new URLSearchParams(global.location.search).get('document');
                if (!pageID) { return; }
                try {
                    const response = await fetch(`/builder/document?id=${encodeURIComponent(pageID)}`, {
                        headers: { 'Accept': 'application/json' }
                    });
                    if (!response.ok) {
                        const result = await response.json();
                        throw new Error(result.message || '测试页面源码不存在');
                    }
                    const document = await response.json();
                    editor.loadJSON(document);
                    localStorage.setItem(storageKey, editor.toJSON());
                    global.history.replaceState(null, '', '/builder');
                    this.showNotice('已恢复测试页面，可以继续编辑');
                } catch (error) {
                    this.showNotice(error.message || '恢复测试页面失败');
                }
            },

            save() {
                localStorage.setItem(storageKey, editor.toJSON());
                this.showNotice('页面文档已保存');
            },

            load() {
                const json = localStorage.getItem(storageKey);
                if (!json) { this.showNotice('没有可加载的页面文档'); return; }
                try {
                    editor.loadJSON(json);
                    this.showNotice('页面文档已加载');
                } catch (error) {
                    this.showNotice('加载失败：' + error.message);
                }
            },

            restoreSavedDocument() {
                const json = localStorage.getItem(storageKey);
                if (!json) { return; }
                try {
                    editor.loadJSON(json);
                } catch (error) {
                    localStorage.removeItem(storageKey);
                    this.showNotice('已忽略无效草稿：' + error.message);
                }
            },

            clearAll() {
                if (!global.confirm('确定清空当前页面吗？此操作可通过撤销恢复。')) { return; }
                if (editor.clear()) { this.showNotice('页面已清空'); }
            },

            exportJSON() {
                const blob = new Blob([editor.toJSON()], { type: 'application/json;charset=utf-8' });
                const url = URL.createObjectURL(blob);
                const anchor = document.createElement('a');
                anchor.href = url;
                anchor.download = 'page-' + editor.document.id + '.json';
                anchor.click();
                URL.revokeObjectURL(url);
                this.showNotice('Page Document 已导出');
            },

            toggleTheme() {
                const current = document.documentElement.getAttribute('data-theme') || 'light';
                const next = current === 'light' ? 'dark' : 'light';
                document.documentElement.setAttribute('data-theme', next);
                localStorage.setItem('theme', next);
            },

            onKeydown(event) {
                const modifier = event.ctrlKey || event.metaKey;
                if (!modifier || event.key.toLowerCase() !== 'z') { return; }
                event.preventDefault();
                if (event.shiftKey) { this.redo(); } else { this.undo(); }
            },

            showNotice(message) {
                this.notice = message;
                global.clearTimeout(this.noticeTimer);
                this.noticeTimer = global.setTimeout(() => { this.notice = ''; }, 2400);
            }
        };
    };

    document.addEventListener('alpine:init', () => {
        global.Alpine.data('builderApp', global.builderApp);
    });
})(window);