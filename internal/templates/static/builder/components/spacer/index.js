(function (global) {
    'use strict';

    global.BuilderComponents.register({
        key: 'spacer',
        type: 'spacer',
        label: '空白间隔',
        category: 'basic',
        icon: '⬍',
        defaultProps: { height: '20px' },
        fields: [
            { key: 'height', label: '高度', type: 'text', placeholder: '例如 20px 或 2rem' }
        ],
        render(node, selectedId, h) {
            return h.fill(h.template('spacer'), {
                attrs: h.commonAttributes(node, ['cmp-spacer'], selectedId, { height: h.safeCSSValue(node.props.height, '20px') })
            });
        }
    });
})(window);