(function (global) {
    'use strict';

    global.BuilderComponents.register({
        key: 'divider',
        type: 'divider',
        label: '分隔线',
        category: 'basic',
        icon: '—',
        defaultProps: { label: '' },
        fields: [
            { key: 'label', label: '辅助说明', type: 'text', placeholder: '可选，仅供屏幕阅读器使用' }
        ],
        render(node, selectedId, h) {
            const label = node.props.label ? ` aria-label="${h.escapeHTML(node.props.label)}"` : '';
            return h.fill(h.template('divider'), {
                attrs: h.commonAttributes(node, ['cmp-divider'], selectedId),
                label
            });
        }
    });
})(window);