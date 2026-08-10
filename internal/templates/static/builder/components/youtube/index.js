(function (global) {
    'use strict';
    const option = (value, label) => ({ value, label });

    global.BuilderComponents.register({
        key: 'youtube',
        type: 'youtube',
        label: 'YouTube',
        category: 'media',
        icon: '▶',
        defaultProps: { url: '', title: 'YouTube 视频', aspectRatio: '16/9', loading: 'lazy' },
        fields: [
            { key: 'url', label: 'YouTube 地址或 ID', type: 'text', placeholder: 'https://youtu.be/...' },
            { key: 'title', label: '视频标题', type: 'text', placeholder: '描述视频内容' },
            { key: 'aspectRatio', label: '宽高比', type: 'select', options: [option('16/9', '16:9'), option('4/3', '4:3'), option('1/1', '1:1'), option('9/16', '9:16')] },
            { key: 'loading', label: '加载方式', type: 'select', options: [option('lazy', '懒加载'), option('eager', '立即加载')] }
        ],
        render(node, selectedId, h) {
            const id = h.youtubeVideoId(node.props.url);
            const ratio = ['16/9', '4/3', '1/1', '9/16'].includes(node.props.aspectRatio) ? node.props.aspectRatio : '16/9';
            const content = id
                ? `<iframe src="https://www.youtube-nocookie.com/embed/${id}" title="${h.escapeHTML(node.props.title || 'YouTube 视频')}" loading="${node.props.loading === 'eager' ? 'eager' : 'lazy'}" allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture; web-share" allowfullscreen></iframe>`
                : '<span class="cmp-media-placeholder">请输入有效的 YouTube 地址或视频 ID</span>';
            return h.fill(h.template('youtube'), {
                attrs: h.commonAttributes(node, ['cmp-embed', 'cmp-youtube'], selectedId, { 'aspect-ratio': ratio }),
                content
            });
        }
    });
})(window);