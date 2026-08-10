(function (global) {
    'use strict';

    const componentsRegistry = global.BuilderComponents || null;
    const lengthUnits = Object.freeze(['px', '%', 'vw', 'vh', 'rem', 'em']);
    const spacingUnits = Object.freeze(['px', '%', 'rem', 'em']);

    const length = (key, label, defaultUnit = 'px', extra = {}) => ({
        key, label, type: 'length', defaultUnit, units: lengthUnits, target: 'style', ...extra
    });

    const segmented = (key, label, options, extra = {}) => ({
        key, label, type: 'segmented', options, target: 'style', ...extra
    });

    const containerLayout = Object.freeze([
        {
            name: 'container',
            label: '容器',
            fields: [
                {
                    key: 'display', label: '容器布局', type: 'select', target: 'style', default: 'flex',
                    options: [
                        { value: 'flex', label: 'Flexbox（弹性盒）' },
                        { value: 'block', label: '块级布局' }
                    ]
                },
                length('max-width', '宽度', 'px', { placeholder: '1140', min: 0 }),
                length('min-height', '最小高度', 'px', { placeholder: '0', min: 0 })
            ]
        },
        {
            name: 'items',
            label: '项目',
            fields: [
                segmented('flex-direction', '方向', [
                    { value: 'row', label: '→' },
                    { value: 'column', label: '↓' },
                    { value: 'row-reverse', label: '←' },
                    { value: 'column-reverse', label: '↑' }
                ], { default: 'column' }),
                segmented('justify-content', '主轴对齐', [
                    { value: 'flex-start', label: '起始' },
                    { value: 'center', label: '居中' },
                    { value: 'flex-end', label: '末端' },
                    { value: 'space-between', label: '两端' },
                    { value: 'space-around', label: '环绕' },
                    { value: 'space-evenly', label: '均分' }
                ], { default: 'flex-start' }),
                segmented('align-items', '副轴对齐', [
                    { value: 'flex-start', label: '起始' },
                    { value: 'center', label: '居中' },
                    { value: 'flex-end', label: '末端' },
                    { value: 'stretch', label: '拉伸' }
                ], { default: 'stretch' }),
                length('column-gap', '列间距', 'px', { placeholder: '0', min: 0 }),
                length('row-gap', '行间距', 'px', { placeholder: '0', min: 0 }),
                segmented('flex-wrap', '换行', [
                    { value: 'nowrap', label: '不换行' },
                    { value: 'wrap', label: '换行' },
                    { value: 'wrap-reverse', label: '反向' }
                ], { default: 'nowrap' })
            ]
        }
    ]);

    const commonStyle = Object.freeze([
        {
            name: 'background',
            label: '背景',
            fields: [
                { key: 'background-color', label: '颜色', type: 'color', target: 'style', placeholder: '#ffffff' },
                { key: 'background-image', label: '背景图或渐变', type: 'text', target: 'style', placeholder: 'url(...) / linear-gradient(...)' }
            ]
        },
        {
            name: 'border',
            label: '边框',
            fields: [
                {
                    key: 'border-style', label: '类型', type: 'select', target: 'style',
                    options: [
                        { value: '', label: '无' },
                        { value: 'solid', label: '实线' },
                        { value: 'dashed', label: '虚线' },
                        { value: 'dotted', label: '点线' },
                        { value: 'double', label: '双线' }
                    ]
                },
                length('border-width', '宽度', 'px', { placeholder: '1', min: 0 }),
                { key: 'border-color', label: '颜色', type: 'color', target: 'style', placeholder: '#000000' },
                length('border-radius', '圆角', 'px', { placeholder: '0', min: 0 }),
                {
                    key: 'box-shadow', label: '阴影', type: 'select', target: 'style',
                    options: [
                        { value: '', label: '无' },
                        { value: '0 1px 3px rgba(0,0,0,.12)', label: '轻微' },
                        { value: '0 8px 24px rgba(0,0,0,.12)', label: '中等' },
                        { value: '0 16px 48px rgba(0,0,0,.18)', label: '明显' }
                    ]
                }
            ]
        }
    ]);

    const typographyStyle = Object.freeze([
        {
            name: 'typography',
            label: '排版',
            fields: [
                { key: 'color', label: '文字颜色', type: 'color', target: 'style', placeholder: '#0d253d' },
                length('font-size', '字号', 'px', { placeholder: '16', min: 0 }),
                {
                    key: 'font-weight', label: '字重', type: 'select', target: 'style',
                    options: ['300', '400', '500', '600', '700', '800'].map((value) => ({ value, label: value }))
                },
                length('line-height', '行高', 'em', { placeholder: '1.5', min: 0 }),
                segmented('text-align', '对齐', [
                    { value: 'left', label: '左' },
                    { value: 'center', label: '中' },
                    { value: 'right', label: '右' },
                    { value: 'justify', label: '两端' }
                ], { default: 'left' })
            ]
        }
    ]);

    const spacingGroups = Object.freeze([
        {
            name: 'spacing',
            label: '间距与排序',
            fields: [
                {
                    key: 'margin', label: '外距', type: 'dimensions', target: 'style',
                    defaultUnit: 'px', units: spacingUnits, allowNegative: true
                },
                {
                    key: 'padding', label: '内距', type: 'dimensions', target: 'style',
                    defaultUnit: 'px', units: spacingUnits, allowNegative: false
                },
                segmented('align-self', '自对齐', [
                    { value: 'auto', label: '自动' },
                    { value: 'flex-start', label: '起始' },
                    { value: 'center', label: '居中' },
                    { value: 'flex-end', label: '末端' },
                    { value: 'stretch', label: '拉伸' }
                ], { default: 'auto' }),
                { key: 'order', label: '排序', type: 'number', target: 'style', placeholder: '0' }
            ]
        }
    ]);

    const advancedGroups = Object.freeze([
        {
            name: 'position',
            label: '定位',
            fields: [
                {
                    key: 'position', label: '定位方式', type: 'select', target: 'style', default: 'relative',
                    options: [
                        { value: 'relative', label: '默认' },
                        { value: 'absolute', label: '绝对定位' },
                        { value: 'fixed', label: '固定定位' },
                        { value: 'sticky', label: '粘性定位' }
                    ]
                },
                { key: 'z-index', label: '层级', type: 'number', target: 'style', placeholder: '0' },
                length('width', '宽度', '%', { placeholder: '100', min: 0 })
            ]
        },
        {
            name: 'attributes',
            label: '标识',
            fields: [
                { key: 'cssId', label: 'CSS ID', type: 'text', target: 'props', placeholder: 'section-id' },
                { key: 'cssClass', label: 'CSS 类', type: 'text', target: 'props', placeholder: 'class-a class-b' }
            ]
        },
        {
            name: 'motion',
            label: '动效',
            fields: [
                {
                    key: 'animation', label: '进入动画', type: 'select', target: 'style',
                    options: [
                        { value: '', label: '无' },
                        { value: 'fadeIn', label: 'Fade In' },
                        { value: 'fadeInUp', label: 'Fade In Up' },
                        { value: 'fadeInDown', label: 'Fade In Down' },
                        { value: 'slideInLeft', label: 'Slide In Left' },
                        { value: 'slideInRight', label: 'Slide In Right' },
                        { value: 'zoomIn', label: 'Zoom In' }
                    ]
                },
                length('animation-duration', '持续时间', 's', {
                    units: Object.freeze(['s', 'ms']), placeholder: '0.6', min: 0
                }),
                length('animation-delay', '延迟', 's', {
                    units: Object.freeze(['s', 'ms']), placeholder: '0', min: 0
                })
            ]
        }
    ]);

    function componentDefinition(node) {
        const registry = componentsRegistry;
        if (!registry || typeof registry.getDefinition !== 'function') {
            return null;
        }
        return registry.getDefinition(node) || null;
    }

    function contentGroups(node) {
        const fields = componentDefinition(node)?.inspector || [];
        return fields.length ? [{ name: 'content', label: '内容', fields: fields.map((field) => ({ ...field, target: 'props' })) }] : [];
    }

    function designGroups(node) {
        if (isContainer(node)) {
            return [...containerLayout, ...commonStyle, ...spacingGroups];
        }
        const nonTypographic = ['image', 'icon', 'youtube', 'video', 'audio', 'divider', 'spacer', 'alert', 'gallery', 'list', 'accordion', 'toggle', 'modal'];
        return nonTypographic.includes(node.type)
            ? [...commonStyle, ...spacingGroups]
            : [...typographyStyle, ...commonStyle, ...spacingGroups];
    }

    function tabsFor(node) {
        if (!node) { return []; }
        const tabs = [];
        if (!isContainer(node) && contentGroups(node).length) {
            tabs.push({ key: 'content', label: '内容' });
        }
        tabs.push({ key: 'design', label: '设计' });
        tabs.push({ key: 'advanced', label: '高级' });
        return tabs;
    }

    function groupsFor(node, tab) {
        if (!node) { return []; }
        if (tab === 'content') { return contentGroups(node); }
        if (tab === 'design' || tab === 'layout' || tab === 'style') { return designGroups(node); }
        if (tab === 'advanced') { return advancedGroups; }
        return [];
    }

    function defaultValueFor(node, field) {
        if (!node) { return field.default ?? ''; }
        if (node.type !== 'container') { return field.default ?? ''; }
        const variantDefaults = {
            display: node.props.variant === 'div' ? 'block' : 'flex',
            'flex-direction': 'column',
            'justify-content': 'flex-start',
            'align-items': 'stretch',
            'flex-wrap': 'nowrap',
            position: 'relative'
        };
        return variantDefaults[field.key] ?? field.default ?? '';
    }

    function labelFor(node) {
        return componentDefinition(node)?.label || node?.type || '';
    }

    function iconFor(node) {
        return componentDefinition(node)?.icon || '?';
    }

    function isContainer(node) {
        return Boolean(node && node.type === 'container');
    }

    global.BuilderInspector = Object.freeze({
        tabsFor,
        groupsFor,
        defaultValueFor,
        labelFor,
        iconFor,
        isContainer,
        lengthUnits,
        spacingUnits
    });
})(window);