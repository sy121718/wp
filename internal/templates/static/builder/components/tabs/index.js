(function (global) {
    'use strict';
    const option = (value, label) => ({ value, label });

    global.BuilderComponents.register({
        key: 'tabs',
        type: 'tabs',
        label: 'Tab 选项卡',
        category: 'interactive',
        icon: '▤',
        canHaveChildren: true,
        defaultProps: { items: [{ title: '选项卡 1' }, { title: '选项卡 2' }], activeIndex: 0 },
        fields: [
            {
                key: 'items', label: '选项卡', type: 'repeater', itemLabel: '选项卡',
                fields: [
                    { key: 'title', label: '标题', type: 'text', placeholder: '选项卡标题' }
                ]
            },
            { key: 'activeIndex', label: '默认激活项（从 0 开始）', type: 'number', placeholder: '0' }
        ],
        render(node, selectedId, h) {
            const items = Array.isArray(node.props.items) && node.props.items.length
                ? node.props.items
                : [{ title: '选项卡' }];
            const active = Math.max(0, Math.min(items.length - 1, Number(node.props.activeIndex || 0)));
            const childrenPerTab = Math.ceil((node.children?.length || 0) / items.length);

            const tabs = items.map((item, index) =>
                `<button type="button" role="tab" id="tab-${h.escapeHTML(node.id)}-${index}" aria-controls="panel-${h.escapeHTML(node.id)}-${index}" aria-selected="${index === active}" tabindex="${index === active ? '0' : '-1'}" data-tab-index="${index}">${h.escapeHTML(item.title || `选项卡 ${index + 1}`)}</button>`
            ).join('');

            const panels = items.map((item, index) => {
                const start = index * childrenPerTab;
                const end = start + childrenPerTab;
                const tabChildren = (node.children || []).slice(start, end);
                const content = tabChildren.map((child) => h.renderChild(child, selectedId)).join('');
                const empty = content || selectedId === undefined ? '' : '<div class="cmp-empty">拖放组件到此选项卡</div>';
                const panelClass = selectedId === undefined ? 'cmp-tabs-panel' : 'cmp-tabs-panel cmp-container';
                const panelAttr = selectedId === undefined ? '' : ` data-node-id="${h.escapeHTML(node.id)}"`;
                return `<div class="${panelClass}" role="tabpanel" id="panel-${h.escapeHTML(node.id)}-${index}" aria-labelledby="tab-${h.escapeHTML(node.id)}-${index}"${panelAttr}${index === active ? '' : ' hidden'}>${content}${empty}</div>`;
            }).join('');

            return h.fill(h.template('tabs'), {
                attrs: h.commonAttributes(node, ['cmp-tabs'], selectedId),
                tabs,
                panels
            });
        }
    });
})(window);