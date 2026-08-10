(function (global) {
    'use strict';

    global.BuilderComponents.register({
        key: 'text',
        type: 'text',
        label: '段落',
        category: 'basic',
        icon: '≡',
        defaultProps: { text: '在这里输入段落内容。', allowHtml: true },
        fields: [
            { key: 'text', label: '段落内容', type: 'wysiwyg', placeholder: '输入正文' }
        ],
        render(node, selectedId, h) {
            const content = node.props.allowHtml === true
                ? String(node.props.text || '')
                : h.escapeHTML(node.props.text);
            return h.fill(h.template('text'), {
                attrs: h.commonAttributes(node, ['cmp-text'], selectedId),
                editattrs: h.editableAttributes(node, selectedId),
                content
            });
        }
    });
})(window);