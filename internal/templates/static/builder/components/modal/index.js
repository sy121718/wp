(function (global) {
    'use strict';
    const option = (value, label) => ({ value, label });

    global.BuilderComponents.register({
        key: 'modal',
        type: 'modal',
        label: '弹窗',
        category: 'interactive',
        icon: '⊡',
        canHaveChildren: true,
        defaultProps: { triggerType: 'button', triggerText: '打开弹窗', modalTitle: '弹窗标题', modalWidth: '600px' },
        fields: [
            { key: 'triggerType', label: '触发器类型', type: 'select', options: [option('button', '按钮'), option('text', '文本')] },
            { key: 'triggerText', label: '触发器文字', type: 'text', placeholder: '打开弹窗' },
            { key: 'modalTitle', label: '弹窗标题', type: 'text', placeholder: '弹窗标题' },
            { key: 'modalContent', label: '弹窗内容', type: 'textarea', placeholder: '弹窗内容' },
            { key: 'modalWidth', label: '弹窗宽度', type: 'text', placeholder: '600px' }
        ],
        render(node, selectedId, h) {
            const modalId = `modal-${node.id}`;
            const triggerClass = node.props.triggerType === 'text' ? 'cmp-modal-trigger-text' : 'cmp-modal-trigger-button';
            const width = h.safeCSSValue(node.props.modalWidth, '600px');
            const content = h.renderChildren(node, selectedId);
            const empty = content || selectedId === undefined ? '' : '<div class="cmp-empty">拖放组件到弹窗内容</div>';
            const title = node.props.modalTitle ? `<h3 class="cmp-modal-title">${h.escapeHTML(node.props.modalTitle)}</h3>` : '';
            const contentClass = selectedId === undefined ? 'cmp-modal-content' : 'cmp-modal-content cmp-container';
            const contentAttr = selectedId === undefined ? '' : ` data-node-id="${h.escapeHTML(node.id)}"`;

            return h.fill(h.template('modal'), {
                attrs: h.commonAttributes(node, ['cmp-modal'], selectedId),
                modalId,
                triggerClass,
                triggerText: h.escapeHTML(node.props.triggerText || '打开弹窗'),
                width,
                title,
                contentattrs: `class="${contentClass}"${contentAttr}`,
                children: content,
                empty
            });
        }
    });
})(window);