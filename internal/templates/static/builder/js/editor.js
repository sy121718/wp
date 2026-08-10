(function (global) {
    'use strict';

    const clone = (value) => JSON.parse(JSON.stringify(value));
    const maxContainerDepth = 3;

    function createDefaultSettings() {
        return {
            // 基础信息（对齐 Elementor Document 层：post_title / hide_title / template）
            general: {
                name: '未命名页面',
                title: '未命名页面',
                description: '',
                favicon: '',
                // 页面模板：default=主题默认容器 / full-width=全宽 / canvas=无容器
                template: 'default',
                // 隐藏页面标题（Elementor Hide Title，发布时注入 --page-title-display）
                hideTitle: false
            },
            // 布局（对齐 Elementor Kit Settings-Layout）
            layout: {
                // 内容区宽度，Elementor 默认 1140px
                contentWidth: '1140px',
                alignment: 'center',
                minHeight: '100vh',
                // 组件间距（Elementor Space Between Widgets，默认 20px）
                spaceBetweenWidgets: '20px'
            },
            // 页面样式（对齐 Elementor PageBase Body Style：四边盒 + Background 组）
            style: {
                marginTop: '', marginRight: '', marginBottom: '', marginLeft: '',
                marginUnit: 'px',
                paddingTop: '', paddingRight: '', paddingBottom: '', paddingLeft: '',
                paddingUnit: 'px',
                backgroundColor: '#ffffff',
                backgroundImage: '',
                backgroundPosition: 'center center',
                backgroundRepeat: 'no-repeat',
                backgroundSize: 'cover'
            },
            // 高级（对齐 Elementor Custom CSS + 额外代码）
            advanced: {
                css: '',
                head: '',
                bodyEnd: '',
                javascript: '',
                previewEnabled: false
            }
        };
    }

    function mergeSettings(settings = {}) {
        const defaults = createDefaultSettings();
        const migrated = migrateLegacySettings(settings);
        return Object.keys(defaults).reduce((result, section) => {
            const value = migrated?.[section];
            result[section] = value && typeof value === 'object' && !Array.isArray(value)
                ? { ...defaults[section], ...clone(value) }
                : defaults[section];
            return result;
        }, {});
    }

    // 旧版本页面设置迁移：styles.css → advanced.css；customCode.* → advanced.*
    function migrateLegacySettings(settings) {
        if (!settings || typeof settings !== 'object') {
            return settings;
        }
        const migrated = { ...settings };
        const styles = migrated.styles;
        const customCode = migrated.customCode;
        if ((styles && typeof styles === 'object') || (customCode && typeof customCode === 'object')) {
            migrated.advanced = {
                ...(migrated.advanced && typeof migrated.advanced === 'object' ? migrated.advanced : {}),
                css: styles?.css ?? migrated.advanced?.css ?? '',
                head: customCode?.head ?? migrated.advanced?.head ?? '',
                bodyEnd: customCode?.bodyEnd ?? migrated.advanced?.bodyEnd ?? '',
                javascript: customCode?.javascript ?? migrated.advanced?.javascript ?? '',
                previewEnabled: customCode?.previewEnabled ?? migrated.advanced?.previewEnabled ?? false
            };
            delete migrated.styles;
            delete migrated.customCode;
        }
        return migrated;
    }

    function normalizeDocument(document) {
        const normalized = clone(document);
        normalized.settings = mergeSettings(normalized.settings);
        return normalized;
    }

    function createDocument() {
        return {
            schemaVersion: 2,
            id: crypto.randomUUID(),
            settings: createDefaultSettings(),
            root: {
                id: crypto.randomUUID(),
                type: 'container',
                props: { variant: 'root', tag: 'main' },
                style: {},
                children: []
            }
        };
    }

    function validateDocument(document) {
        if (!document || document.schemaVersion !== 2 || !document.root || document.root.type !== 'container') {
            throw new Error('Page Document 格式无效');
        }
        const ids = new Set();
        const visit = (node) => {
            if (!node || typeof node.id !== 'string' || ids.has(node.id)) {
                throw new Error('节点 ID 缺失或重复');
            }
            if (!['container', 'tabs', 'accordion', 'toggle', 'modal', 'heading', 'text', 'image', 'svg', 'icon', 'button', 'youtube', 'divider', 'video', 'audio', 'spacer', 'alert', 'card', 'list', 'gallery'].includes(node.type)) {
                throw new Error(`不支持的节点类型: ${node.type}`);
            }
            if (!node.props || typeof node.props !== 'object' || Array.isArray(node.props)) {
                throw new Error('节点 props 格式无效');
            }
            if (!node.style || typeof node.style !== 'object' || Array.isArray(node.style)) {
                throw new Error('节点 style 格式无效');
            }
            if (node.type === 'tabs') {
                if (!Array.isArray(node.props.items) || node.props.items.length === 0 || node.props.items.some((item) =>
                    !item || typeof item.title !== 'string' || typeof item.content !== 'string'
                )) {
                    throw new Error('Tab 节点 items 格式无效');
                }
            }
            if (node.type === 'container' && node !== document.root && !['div', 'flexbox'].includes(node.props.variant)) {
                throw new Error('容器节点 variant 格式无效');
            }
            ids.add(node.id);
            if (node.type === 'container' || ['tabs', 'accordion', 'toggle', 'modal'].includes(node.type)) {
                if (!Array.isArray(node.children)) {
                    throw new Error(`${node.type} 节点缺少 children`);
                }
                node.children.forEach(visit);
            } else if ('children' in node) {
                throw new Error(`只有容器和交互组件允许 children`);
            }
        };
        visit(document.root);
    }

    class Editor extends EventTarget {
        constructor(document = createDocument()) {
            super();
            const normalized = normalizeDocument(document);
            validateDocument(normalized);
            this.document = normalized;
            this.selectedId = null;
            this.history = [];
            this.future = [];
            this.nodeIndex = new Map();
            this.rebuildIndex();
        }

        rebuildIndex() {
            this.nodeIndex.clear();
            const visit = (node, parent) => {
                this.nodeIndex.set(node.id, { node, parent });
                // 容器和支持子节点的交互组件都需要遍历 children
                if (node.type === 'container' || ['tabs', 'accordion', 'toggle', 'modal'].includes(node.type)) {
                    node.children.forEach((child) => visit(child, node));
                }
            };
            visit(this.document.root, null);
        }

        emitChange() {
            this.dispatchEvent(new CustomEvent('documentChanged', {
                detail: { document: this.document, selectedId: this.selectedId }
            }));
        }

        emitSelection() {
            this.dispatchEvent(new CustomEvent('selectionChanged', {
                detail: { selectedId: this.selectedId, node: this.getSelected() }
            }));
        }

        execute(mutator) {
            const before = clone(this.document);
            mutator();
            validateDocument(this.document);
            this.history.push(before);
            if (this.history.length > 100) {
                this.history.shift();
            }
            this.future = [];
            this.rebuildIndex();
            this.emitChange();
        }

        select(nodeId) {
            const nextId = nodeId && this.nodeIndex.has(nodeId) ? nodeId : null;
            if (this.selectedId === nextId) {
                return;
            }
            this.selectedId = nextId;
            this.emitSelection();
        }

        addNode(parentId, node, index) {
            const target = this.nodeIndex.get(parentId);
            if (!target) {
                throw new Error('父节点不存在');
            }
            // 允许向容器和支持子节点的交互组件添加子元素
            const canAcceptChildren = target.node.type === 'container' || 
                ['tabs', 'accordion', 'toggle', 'modal'].includes(target.node.type);
            if (!canAcceptChildren) {
                throw new Error('只能向容器或交互组件添加子元素');
            }
            const nextNode = clone(node);
            this.execute(() => {
                const children = target.node.children;
                const position = Number.isInteger(index) ? Math.max(0, Math.min(index, children.length)) : children.length;
                children.splice(position, 0, nextNode);
            });
            this.select(nextNode.id);
            return nextNode.id;
        }

        deleteNode(nodeId) {
            const entry = this.nodeIndex.get(nodeId);
            if (!entry || !entry.parent) {
                return false;
            }
            this.execute(() => {
                const index = entry.parent.children.findIndex((node) => node.id === nodeId);
                entry.parent.children.splice(index, 1);
                if (this.selectedId === nodeId) {
                    this.selectedId = null;
                }
            });
            this.emitSelection();
            return true;
        }

        moveNode(nodeId, newParentId, index) {
            const source = this.nodeIndex.get(nodeId);
            const target = this.nodeIndex.get(newParentId);
            if (!source || !source.parent || !target) {
                throw new Error('移动节点参数无效');
            }
            // 允许移动到容器或支持子节点的交互组件
            const canAcceptChildren = target.node.type === 'container' || 
                ['tabs', 'accordion', 'toggle', 'modal'].includes(target.node.type);
            if (!canAcceptChildren) {
                throw new Error('只能移动到容器或交互组件中');
            }
            if (nodeId === newParentId || this.isDescendant(newParentId, nodeId)) {
                throw new Error('不能把节点移动到自身或其后代中');
            }
            this.execute(() => {
                const sourceIndex = source.parent.children.findIndex((node) => node.id === nodeId);
                const [node] = source.parent.children.splice(sourceIndex, 1);
                const position = Number.isInteger(index) ? Math.max(0, Math.min(index, target.node.children.length)) : target.node.children.length;
                target.node.children.splice(position, 0, node);
            });
        }

        isDescendant(nodeId, ancestorId) {
            let current = this.nodeIndex.get(nodeId);
            while (current && current.parent) {
                if (current.parent.id === ancestorId) {
                    return true;
                }
                current = this.nodeIndex.get(current.parent.id);
            }
            return false;
        }

        updateProps(nodeId, patch) {
            const entry = this.nodeIndex.get(nodeId);
            if (!entry) {
                return false;
            }
            this.execute(() => {
                entry.node.props = { ...entry.node.props, ...clone(patch) };
            });
            return true;
        }

        updateArrayProp(nodeId, key, updater) {
            const entry = this.nodeIndex.get(nodeId);
            const current = entry?.node.props?.[key];
            if (!entry || !Array.isArray(current) || typeof updater !== 'function') {
                return false;
            }
            this.execute(() => {
                const next = updater(clone(current));
                if (!Array.isArray(next)) {
                    throw new Error('数组属性更新结果无效');
                }
                entry.node.props = { ...entry.node.props, [key]: next };
            });
            return true;
        }

        updateStyle(nodeId, patch) {
            const entry = this.nodeIndex.get(nodeId);
            if (!entry) {
                return false;
            }
            this.execute(() => {
                entry.node.style = { ...(entry.node.style || {}), ...clone(patch) };
            });
            return true;
        }

        updateSettings(section, patch) {
            if (!Object.hasOwn(createDefaultSettings(), section) || !patch || typeof patch !== 'object') {
                return false;
            }
            this.execute(() => {
                this.document.settings[section] = {
                    ...this.document.settings[section],
                    ...clone(patch)
                };
            });
            return true;
        }

        duplicateNode(nodeId) {
            const entry = this.nodeIndex.get(nodeId);
            if (!entry || !entry.parent) {
                return null;
            }
            const copy = clone(entry.node);
            const renewIds = (node) => {
                node.id = crypto.randomUUID();
                if (node.type === 'container') {
                    node.children.forEach(renewIds);
                }
            };
            renewIds(copy);
            const index = entry.parent.children.findIndex((node) => node.id === nodeId);
            return this.addNode(entry.parent.id, copy, index + 1);
        }

        getSelected() {
            return this.selectedId ? this.nodeIndex.get(this.selectedId)?.node || null : null;
        }

        getChildren(nodeId) {
            const node = this.nodeIndex.get(nodeId)?.node;
            return node && node.type === 'container' ? node.children : [];
        }

        undo() {
            if (!this.history.length) {
                return false;
            }
            this.future.push(clone(this.document));
            this.document = this.history.pop();
            this.rebuildIndex();
            if (this.selectedId && !this.nodeIndex.has(this.selectedId)) {
                this.selectedId = null;
            }
            this.emitChange();
            this.emitSelection();
            return true;
        }

        redo() {
            if (!this.future.length) {
                return false;
            }
            this.history.push(clone(this.document));
            this.document = this.future.pop();
            this.rebuildIndex();
            if (this.selectedId && !this.nodeIndex.has(this.selectedId)) {
                this.selectedId = null;
            }
            this.emitChange();
            this.emitSelection();
            return true;
        }

        toJSON() {
            return JSON.stringify(this.document, null, 2);
        }

        loadJSON(json) {
            const parsed = typeof json === 'string' ? JSON.parse(json) : clone(json);
            const document = normalizeDocument(parsed);
            validateDocument(document);
            this.document = document;
            this.selectedId = null;
            this.history = [];
            this.future = [];
            this.rebuildIndex();
            this.emitChange();
            this.emitSelection();
        }

        clear() {
            if (!this.document.root.children.length) {
                return false;
            }
            this.execute(() => {
                this.document.root.children = [];
                this.selectedId = null;
            });
            this.emitSelection();
            return true;
        }
    }

    global.BuilderEditor = Object.freeze({
        Editor,
        createDocument,
        createDefaultSettings,
        mergeSettings,
        validateDocument
    });
})(window);