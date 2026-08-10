(function (global) {
    'use strict';

    global.BuilderComponents.register({
        key: 'toggle',
        type: 'toggle',
        label: '折叠项',
        category: 'interactive',
        icon: '⊟',
        canHaveChildren: true,
        defaultProps: { title: '点击展开', defaultOpen: false },
        fields: [
            { key: 'title', label: '标题', type: 'text', placeholder: '点击展开' },
            { key: 'content', label: '内容', type: 'textarea', placeholder: '折叠内容' },
            { key: 'defaultOpen', label: '默认展开', type: 'checkbox' }
        ],
        render(node, selectedId, h) {
            const open = node.props.defaultOpen === true ? ' open' : '';
            const content = h.renderChildren(node, selectedId);
            const empty = content || selectedId === undefined ? '' : '<div class="cmp-empty">拖放组件到此折叠项</div>';
            const contentClass = selectedId === undefined ? 'cmp-toggle-content' : 'cmp-toggle-content cmp-container';
            const contentAttr = selectedId === undefined ? '' : ` data-node-id="${h.escapeHTML(node.id)}"`;
            return h.fill(h.template('toggle'), {
                attrs: h.commonAttributes(node, ['cmp-toggle'], selectedId),
                open,
                title: h.escapeHTML(node.props.title || '点击展开'),
                contentattrs: `class="${contentClass}"${contentAttr}`,
                children: content,
                empty
            });
        }
    });
})(window);