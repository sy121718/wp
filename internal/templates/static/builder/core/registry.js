(function (global) {
    'use strict';

    // ==========================================================================
    // 组件注册表：builder 是「显示器 + 插槽」，组件以独立模块注册进来。
    // 每个组件模块调用 BuilderComponents.register() 提供：定义 + Inspector 字段 + render。
    // ==========================================================================

    const components = [];       // 全部组件（用于组件库面板）
    const byKey = new Map();     // key → 组件定义
    const byType = new Map();    // type → 组件定义（type 用于渲染/节点识别）

    function clone(value) {
        return JSON.parse(JSON.stringify(value));
    }

    /**
     * 注册一个组件模块。
     * @param {Object} definition {
     *   key,            // 组件库 key（唯一，如 'heading'）
     *   type,           // 节点类型（渲染时匹配，如 'heading'；container 变体共享 'container'）
     *   label, category, icon,
     *   defaultProps, defaultStyle,
     *   canHaveChildren,          // 容器型组件（tabs/accordion/toggle/modal）
     *   fields,                   // Inspector 分组字段数组
     *   render(node, selectedId, helpers) => string  // 渲染函数
     * }
     */
    function register(definition) {
        if (!definition || !definition.key || !definition.type) {
            throw new Error('组件注册失败：key 和 type 不能为空');
        }
        if (byKey.has(definition.key)) {
            throw new Error(`组件重复注册: ${definition.key}`);
        }
        components.push(definition);
        byKey.set(definition.key, definition);
        // 同一 type 只保留最后一个注册（如 container 的 div/flexbox 变体）
        byType.set(definition.type, definition);
        return definition;
    }

    function get(key) {
        return byKey.get(key) || null;
    }

    function all() {
        return components.slice();
    }

    // 按节点取组件定义：container 按 variant 映射；svg 向后兼容映射到 icon
    function getDefinition(node) {
        if (!node) { return null; }
        if (node.type === 'container') {
            const variant = node.props.variant === 'flexbox' ? 'flexbox' : 'div';
            return byKey.get(variant) || null;
        }
        if (node.type === 'svg') {
            return byKey.get('icon') || null;
        }
        return byKey.get(node.type) || null;
    }

    // 按 type 取渲染定义（renderNode 用）
    function getByType(type) {
        return byType.get(type) || null;
    }

    function createNode(key) {
        const definition = byKey.get(key);
        if (!definition) { throw new Error(`未知组件: ${key}`); }
        const node = {
            id: crypto.randomUUID(),
            type: definition.type,
            props: clone(definition.defaultProps),
            style: clone(definition.defaultStyle || {})
        };
        // 容器和支持子节点的组件（Tabs/Accordion/Toggle/Modal）需要 children 数组
        if (definition.type === 'container' || definition.canHaveChildren) {
            node.children = [];
        }
        return node;
    }

    global.BuilderComponents = Object.freeze({ register, get, all, getDefinition, getByType, createNode });
})(window);