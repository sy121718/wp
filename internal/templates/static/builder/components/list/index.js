(function (global) {
    'use strict';
    const option = (value, label) => ({ value, label });
    const CHECK_ICON = '<svg viewBox="0 0 24 24"><path d="M9 16.17L4.83 12l-1.42 1.41L9 19 21 7l-1.41-1.41z" fill="currentColor"/></svg>';

    global.BuilderComponents.register({
        key: 'list',
        type: 'list',
        label: '列表',
        category: 'basic',
        icon: '☰',
        defaultProps: {
            items: [
                { mediaType: 'icon', iconMarkup: CHECK_ICON, imageSrc: '', text: '列表项 1', href: '' },
                { mediaType: 'icon', iconMarkup: CHECK_ICON, imageSrc: '', text: '列表项 2', href: '' }
            ],
            layout: 'vertical', columns: 2
        },
        fields: [
            {
                key: 'items', label: '列表项', type: 'repeater', itemLabel: '列表项',
                fields: [
                    { key: 'mediaType', label: '媒体类型', type: 'select', options: [option('icon', '图标'), option('image', '图片')] },
                    { key: 'iconMarkup', label: 'SVG 源码', type: 'textarea', placeholder: '<svg>...</svg>' },
                    { key: 'imageSrc', label: '图片地址', type: 'url', placeholder: 'https://...' },
                    { key: 'text', label: '文本', type: 'text', placeholder: '列表项内容' },
                    { key: 'href', label: '链接', type: 'url', placeholder: '可选' }
                ]
            },
            { key: 'layout', label: '布局', type: 'select', options: [option('vertical', '垂直列表'), option('grid', '网格')] },
            { key: 'columns', label: '列数（网格模式）', type: 'number', placeholder: '2', min: 1, max: 6 }
        ],
        render(node, selectedId, h) {
            const items = Array.isArray(node.props.items) && node.props.items.length
                ? node.props.items
                : [{ mediaType: 'icon', iconMarkup: '', imageSrc: '', text: '列表项', href: '' }];
            const layout = node.props.layout === 'grid' ? 'grid' : 'vertical';
            const columns = layout === 'grid' ? Math.max(1, Math.min(6, Number(node.props.columns) || 2)) : 1;

            const listItems = items.map((item) => {
                let media = '';
                if (item.mediaType === 'icon' && item.iconMarkup) {
                    const svg = h.sanitizeSVG(item.iconMarkup, '');
                    if (svg) { media = `<span class="cmp-list-icon">${svg}</span>`; }
                } else if (item.mediaType === 'image' && item.imageSrc) {
                    const src = h.safeURL(item.imageSrc);
                    if (src) { media = `<img src="${h.escapeHTML(src)}" alt="" class="cmp-list-image" loading="lazy">`; }
                }
                const text = `<span class="cmp-list-text">${h.escapeHTML(item.text || '')}</span>`;
                const href = h.safeURL(item.href || '');
                const content = `${media}${text}`;
                return href
                    ? `<li class="cmp-list-item"><a href="${h.escapeHTML(href)}" class="cmp-list-link">${content}</a></li>`
                    : `<li class="cmp-list-item">${content}</li>`;
            }).join('');

            return h.fill(h.template('list'), {
                attrs: h.commonAttributes(node, ['cmp-list', `cmp-list--${layout}`], selectedId, layout === 'grid' ? { 'grid-template-columns': `repeat(${columns}, 1fr)` } : {}),
                items: listItems
            });
        }
    });
})(window);