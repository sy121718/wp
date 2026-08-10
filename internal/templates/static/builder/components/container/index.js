(function (global) {
    'use strict';
    const allowedContainerTags = new Set(['div', 'section', 'main', 'article', 'header', 'footer', 'nav']);

    function renderContainer(node, selectedId, h) {
        const tag = allowedContainerTags.has(node.props.tag) ? node.props.tag : 'div';
        const variant = node.props.variant === 'flexbox' ? 'flexbox' : 'div';
        const children = h.renderChildren(node, selectedId);
        const empty = children || selectedId === undefined ? '' : '<div class="cmp-empty">点击添加区块或元素</div>';
        // div/flexbox 变体共享 container/ 目录的模板
        return h.fill(h.template('div'), {
            tag,
            attrs: h.commonAttributes(node, ['cmp-container', `cmp-${variant}`], selectedId),
            children,
            empty
        });
    }

    // DIV 区块（容器变体：div）
    global.BuilderComponents.register({
        key: 'div',
        type: 'container',
        label: 'DIV 区块',
        category: 'layout',
        icon: '□',
        assetDir: 'container',
        defaultProps: { variant: 'div', tag: 'div' },
        fields: [],
        render: renderContainer
    });

    // Flexbox 容器（容器变体：flexbox）
    global.BuilderComponents.register({
        key: 'flexbox',
        type: 'container',
        label: 'Flexbox（弹性盒）',
        category: 'layout',
        icon: '▦',
        assetDir: 'container',
        defaultProps: { variant: 'flexbox', tag: 'div' },
        defaultStyle: { display: 'flex', 'flex-direction': 'column' },
        fields: [],
        render: renderContainer
    });
})(window);