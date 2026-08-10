(function (global) {
    'use strict';
    const option = (value, label) => ({ value, label });

    global.BuilderComponents.register({
        key: 'video',
        type: 'video',
        label: '视频',
        category: 'media',
        icon: '▷',
        defaultProps: { src: '', poster: '', title: '视频', preload: 'metadata', controls: true, autoplay: false, muted: false, loop: false },
        fields: [
            { key: 'src', label: '视频地址', type: 'url', placeholder: 'https://example.com/video.mp4' },
            { key: 'poster', label: '封面地址', type: 'url', placeholder: 'https://example.com/poster.jpg' },
            { key: 'title', label: '视频标题', type: 'text', placeholder: '描述视频内容' },
            { key: 'preload', label: '预加载', type: 'select', options: [option('metadata', '元数据'), option('none', '不预加载'), option('auto', '自动')] },
            { key: 'controls', label: '显示控制栏', type: 'checkbox' },
            { key: 'autoplay', label: '自动播放', type: 'checkbox' },
            { key: 'muted', label: '静音', type: 'checkbox' },
            { key: 'loop', label: '循环播放', type: 'checkbox' }
        ],
        render(node, selectedId, h) {
            const src = h.safeURL(node.props.src);
            if (!src) {
                return `<div ${h.commonAttributes(node, ['cmp-video', 'cmp-video-placeholder'], selectedId)}><span class="cmp-media-placeholder">请输入有效的视频地址</span></div>`;
            }
            const poster = h.safeURL(node.props.poster);
            const posterAttribute = poster ? ` poster="${h.escapeHTML(poster)}"` : '';
            const preload = ['none', 'metadata', 'auto'].includes(node.props.preload) ? node.props.preload : 'metadata';
            return h.fill(h.template('video'), {
                attrs: h.commonAttributes(node, ['cmp-video'], selectedId),
                title: h.escapeHTML(node.props.title || '视频'),
                preload,
                poster: posterAttribute,
                controls: h.booleanAttribute('controls', node.props.controls),
                autoplay: h.booleanAttribute('autoplay', node.props.autoplay),
                muted: h.booleanAttribute('muted', node.props.muted),
                loop: h.booleanAttribute('loop', node.props.loop),
                src: h.escapeHTML(src)
            });
        }
    });
})(window);