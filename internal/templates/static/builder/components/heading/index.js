(function (global) {
    'use strict';
    const option = (value, label) => ({ value, label });

    global.BuilderComponents.register({
        key: 'heading',
        type: 'heading',
        label: '标题',
        category: 'basic',
        icon: 'H',
        defaultProps: { text: '这是一个标题', level: 'h2' },
        fields: [
            { key: 'text', label: '标题内容', type: 'text', placeholder: '输入标题' },
            { key: 'level', label: '标题级别', type: 'select', options: ['h1', 'h2', 'h3', 'h4', 'h5', 'h6'].map((value) => option(value, value.toUpperCase())) }
        ],
        render(node, selectedId, h) {
            const level = ['h1', 'h2', 'h3', 'h4', 'h5', 'h6'].includes(node.props.level) ? node.props.level : 'h2';
            return h.fill(h.template('heading'), {
                level,
                attrs: h.commonAttributes(node, ['cmp-heading'], selectedId),
                content: h.escapeHTML(node.props.text)
            });
        }
    });
})(window);