(function (global) {
    'use strict';
    const option = (value, label) => ({ value, label });

    global.BuilderComponents.register({
        key: 'alert',
        type: 'alert',
        label: '提示框',
        category: 'basic',
        icon: '⚠',
        defaultProps: { type: 'info', title: '提示', content: '这是一条提示信息', dismissible: false },
        fields: [
            { key: 'type', label: '类型', type: 'select', options: [option('info', '信息'), option('success', '成功'), option('warning', '警告'), option('danger', '危险')] },
            { key: 'title', label: '标题', type: 'text', placeholder: '提示标题' },
            { key: 'content', label: '内容', type: 'textarea', placeholder: '提示内容' },
            { key: 'dismissible', label: '可关闭', type: 'checkbox' }
        ],
        render(node, selectedId, h) {
            const type = ['info', 'success', 'warning', 'danger'].includes(node.props.type) ? node.props.type : 'info';
            const dismiss = node.props.dismissible === true
                ? '<button type="button" class="cmp-alert-close" aria-label="关闭">×</button>'
                : '';
            const title = node.props.title ? `<strong class="cmp-alert-title">${h.escapeHTML(node.props.title)}</strong>` : '';
            return h.fill(h.template('alert'), {
                attrs: h.commonAttributes(node, ['cmp-alert', `cmp-alert--${type}`], selectedId),
                dismiss,
                title,
                content: `<div class="cmp-alert-content">${h.escapeHTML(node.props.content)}</div>`
            });
        }
    });
})(window);