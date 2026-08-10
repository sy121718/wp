(function (global) {
    'use strict';
    const option = (value, label) => ({ value, label });

    global.BuilderComponents.register({
        key: 'button',
        type: 'button',
        label: '按钮',
        category: 'basic',
        icon: '▰',
        defaultProps: { text: '开始使用', href: '#', target: '_self', variant: 'primary' },
        fields: [
            { key: 'text', label: '按钮文字', type: 'text', placeholder: '输入按钮文字' },
            { key: 'href', label: '链接', type: 'url', placeholder: 'https://example.com' },
            { key: 'target', label: '打开方式', type: 'select', options: [option('_self', '当前窗口'), option('_blank', '新窗口')] },
            { key: 'variant', label: '样式', type: 'select', options: [option('primary', '主要'), option('secondary', '次要'), option('ghost', '幽灵')] }
        ],
        render(node, selectedId, h) {
            const variant = ['primary', 'secondary', 'ghost'].includes(node.props.variant) ? node.props.variant : 'primary';
            const target = node.props.target === '_blank' ? '_blank' : '_self';
            const rel = target === '_blank' ? ' rel="noopener noreferrer"' : '';
            return h.fill(h.template('button'), {
                attrs: h.commonAttributes(node, ['cmp-button', `cmp-button--${variant}`], selectedId),
                href: h.escapeHTML(h.safeURL(node.props.href, '#')),
                target,
                rel,
                editattrs: h.editableAttributes(node, selectedId),
                text: h.escapeHTML(node.props.text)
            });
        }
    });
})(window);