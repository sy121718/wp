(function (global) {
    'use strict';

    global.BuilderComponents.register({
        key: 'icon',
        type: 'icon',
        label: '图标',
        category: 'basic',
        icon: '◆',
        defaultProps: {
            markup: '<svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="9" fill="none" stroke="currentColor" stroke-width="2"/></svg>',
            title: '装饰图标', size: '24px'
        },
        fields: [
            { key: 'markup', label: 'SVG 源码', type: 'textarea', placeholder: '<svg viewBox="0 0 24 24">...</svg>' },
            { key: 'title', label: '无障碍标题', type: 'text', placeholder: '描述图标含义' },
            { key: 'size', label: '尺寸', type: 'text', placeholder: '例如 24px 或 2rem' }
        ],
        render(node, selectedId, h) {
            const svg = h.sanitizeSVG(node.props.markup, node.props.title);
            const content = svg || '<span class="cmp-media-placeholder">SVG 源码无效</span>';
            const size = h.safeCSSValue(node.props.size, '24px');
            return h.fill(h.template('icon'), {
                attrs: h.commonAttributes(node, ['cmp-icon'], selectedId, { width: size, height: size }),
                content
            });
        }
    });
})(window);