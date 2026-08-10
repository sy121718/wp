(function (global) {
    'use strict';
    const option = (value, label) => ({ value, label });
    const PLACEHOLDER_1 = 'data:image/svg+xml,%3Csvg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 800 600"%3E%3Crect width="800" height="600" fill="%23e2e8f0"/%3E%3Ctext x="50%25" y="50%25" dominant-baseline="middle" text-anchor="middle" font-family="sans-serif" font-size="120" fill="%2364748b"%3E1%3C/text%3E%3C/svg%3E';
    const PLACEHOLDER_2 = 'data:image/svg+xml,%3Csvg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 800 600"%3E%3Crect width="800" height="600" fill="%23f1f5f9"/%3E%3Ctext x="50%25" y="50%25" dominant-baseline="middle" text-anchor="middle" font-family="sans-serif" font-size="120" fill="%2364748b"%3E2%3C/text%3E%3C/svg%3E';
    const PLACEHOLDER_3 = 'data:image/svg+xml,%3Csvg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 800 600"%3E%3Crect width="800" height="600" fill="%23e2e8f0"/%3E%3Ctext x="50%25" y="50%25" dominant-baseline="middle" text-anchor="middle" font-family="sans-serif" font-size="120" fill="%2364748b"%3E3%3C/text%3E%3C/svg%3E';

    global.BuilderComponents.register({
        key: 'gallery',
        type: 'gallery',
        label: '图库',
        category: 'media',
        icon: '⊞',
        defaultProps: {
            images: [
                { src: PLACEHOLDER_1, alt: '图片 1', title: '', href: '' },
                { src: PLACEHOLDER_2, alt: '图片 2', title: '', href: '' },
                { src: PLACEHOLDER_3, alt: '图片 3', title: '', href: '' }
            ],
            mode: 'grid', columns: 3, gap: '16px', autoplay: false, interval: 3000
        },
        fields: [
            {
                key: 'images', label: '图片列表', type: 'repeater', itemLabel: '图片',
                mediaMulti: true,   // 媒体库多选追加模式：点击图片即新增图库项，可重复选择
                fields: [
                    { key: 'src', label: '图片地址', type: 'url', placeholder: 'https://example.com/image.jpg' },
                    { key: 'alt', label: '替代文字', type: 'text', placeholder: '描述图片' },
                    { key: 'title', label: '标题', type: 'text', placeholder: '悬停提示（可选）' },
                    { key: 'href', label: '链接', type: 'url', placeholder: '点击跳转地址（可选）' }
                ]
            },
            { key: 'mode', label: '展示模式', type: 'select', options: [option('grid', '网格'), option('carousel', '轮播')] },
            { key: 'columns', label: '列数（网格模式）', type: 'number', placeholder: '3', min: 1, max: 6 },
            { key: 'gap', label: '间距（网格模式）', type: 'text', placeholder: '16px' },
            { key: 'autoplay', label: '自动播放（轮播模式）', type: 'checkbox' },
            { key: 'interval', label: '切换间隔（毫秒）', type: 'number', placeholder: '3000', min: 1000 }
        ],
        render(node, selectedId, h) {
            const images = Array.isArray(node.props.images) && node.props.images.length
                ? node.props.images
                : [{ src: '', alt: '', title: '', href: '' }];
            const mode = node.props.mode === 'carousel' ? 'carousel' : 'grid';
            const columns = mode === 'grid' ? Math.max(1, Math.min(6, Number(node.props.columns) || 3)) : 1;
            const gap = mode === 'grid' ? h.safeCSSValue(node.props.gap, '16px') : '0';

            const galleryItems = images.map((img, index) => {
                const src = h.safeURL(img.src);
                if (!src) {
                    return `<div class="cmp-gallery-item cmp-gallery-placeholder"><span class="cmp-media-placeholder">图片 ${index + 1}</span></div>`;
                }
                const imgTag = `<img src="${h.escapeHTML(src)}" alt="${h.escapeHTML(img.alt || '')}"${img.title ? ` title="${h.escapeHTML(img.title)}"` : ''} loading="lazy">`;
                const href = h.safeURL(img.href || '');
                return href
                    ? `<a href="${h.escapeHTML(href)}" class="cmp-gallery-item">${imgTag}</a>`
                    : `<div class="cmp-gallery-item">${imgTag}</div>`;
            }).join('');

            const gridStyle = mode === 'grid' ? { 'grid-template-columns': `repeat(${columns}, 1fr)`, gap } : {};
            return h.fill(h.template('gallery'), {
                attrs: h.commonAttributes(node, ['cmp-gallery', `cmp-gallery--${mode}`], selectedId, gridStyle),
                items: galleryItems
            });
        }
    });
})(window);