(function (global) {
    'use strict';
    const option = (value, label) => ({ value, label });

    global.BuilderComponents.register({
        key: 'image',
        type: 'image',
        label: '图片',
        category: 'media',
        icon: '▧',
        defaultProps: {
            src: 'data:image/svg+xml,%3Csvg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1200 675"%3E%3Crect width="1200" height="675" fill="%23f1f5f9"/%3E%3Ctext x="50%25" y="50%25" dominant-baseline="middle" text-anchor="middle" font-family="sans-serif" font-size="48" fill="%2364748b"%3EImage%3C/text%3E%3C/svg%3E',
            alt: '占位图片', title: '', width: '100%', loading: 'lazy'
        },
        fields: [
            { key: 'src', label: '图片地址', type: 'url', placeholder: 'https://example.com/image.jpg' },
            { key: 'alt', label: '替代文字', type: 'text', placeholder: '描述图片内容' },
            { key: 'title', label: '图片标题', type: 'text', placeholder: '可选' },
            { key: 'width', label: '宽度', type: 'text', placeholder: '例如 100% 或 640px' },
            { key: 'loading', label: '加载方式', type: 'select', options: [option('lazy', '懒加载'), option('eager', '立即加载')] }
        ],
        render(node, selectedId, h) {
            const src = h.safeURL(node.props.src);
            if (!src) {
                return `<div ${h.commonAttributes(node, ['cmp-image-placeholder'], selectedId)}><span class="cmp-media-placeholder">请输入有效的图片地址</span></div>`;
            }
            const loading = node.props.loading === 'eager' ? 'eager' : 'lazy';
            const title = node.props.title ? ` title="${h.escapeHTML(node.props.title)}"` : '';
            return h.fill(h.template('image'), {
                attrs: h.commonAttributes(node, ['cmp-image'], selectedId, { width: h.safeCSSValue(node.props.width, '100%') }),
                src: h.escapeHTML(src),
                alt: h.escapeHTML(node.props.alt),
                title,
                loading
            });
        }
    });
})(window);