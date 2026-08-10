(function (global) {
    'use strict';
    const option = (value, label) => ({ value, label });

    global.BuilderComponents.register({
        key: 'accordion',
        type: 'accordion',
        label: '手风琴',
        category: 'interactive',
        icon: '≣',
        canHaveChildren: true,
        defaultProps: { items: [{ title: '折叠项 1' }, { title: '折叠项 2' }], allowMultiple: false },
        fields: [
            {
                key: 'items', label: '折叠项', type: 'repeater', itemLabel: '折叠项',
                fields: [
                    { key: 'title', label: '标题', type: 'text', placeholder: '折叠项标题' }
                ]
            },
            { key: 'allowMultiple', label: '允许多个同时展开', type: 'checkbox' }
        ],
        render(node, selectedId, h) {
            const items = Array.isArray(node.props.items) && node.props.items.length
                ? node.props.items
                : [{ title: '折叠项' }];
            const childrenPerItem = Math.ceil((node.children?.length || 0) / items.length);

            const accordionItems = items.map((item, index) => {
                const open = index === 0 ? ' open' : '';
                const start = index * childrenPerItem;
                const end = start + childrenPerItem;
                const itemChildren = (node.children || []).slice(start, end);
                const content = itemChildren.map((child) => h.renderChild(child, selectedId)).join('');
                const empty = content || selectedId === undefined ? '' : '<div class="cmp-empty">拖放组件到此折叠项</div>';
                const contentClass = selectedId === undefined ? 'cmp-accordion-content' : 'cmp-accordion-content cmp-container';
                const contentAttr = selectedId === undefined ? '' : ` data-node-id="${h.escapeHTML(node.id)}"`;
                return `<details class="cmp-accordion-item"${open}><summary class="cmp-accordion-title">${h.escapeHTML(item.title || `项目 ${index + 1}`)}</summary><div class="${contentClass}"${contentAttr}>${content}${empty}</div></details>`;
            }).join('');

            return h.fill(h.template('accordion'), {
                attrs: h.commonAttributes(node, ['cmp-accordion'], selectedId),
                items: accordionItems
            });
        }
    });
})(window);