(function (global) {
    'use strict';
    const option = (value, label) => ({ value, label });

    global.BuilderComponents.register({
        key: 'audio',
        type: 'audio',
        label: '音频',
        category: 'media',
        icon: '♪',
        defaultProps: { src: '', title: '音频', preload: 'metadata', controls: true, autoplay: false, loop: false },
        fields: [
            { key: 'src', label: '音频地址', type: 'url', placeholder: 'https://example.com/audio.mp3' },
            { key: 'title', label: '音频标题', type: 'text', placeholder: '描述音频内容' },
            { key: 'preload', label: '预加载', type: 'select', options: [option('metadata', '元数据'), option('none', '不预加载'), option('auto', '自动')] },
            { key: 'controls', label: '显示控制栏', type: 'checkbox' },
            { key: 'autoplay', label: '自动播放', type: 'checkbox' },
            { key: 'loop', label: '循环播放', type: 'checkbox' }
        ],
        render(node, selectedId, h) {
            const src = h.safeURL(node.props.src);
            if (!src) {
                return `<div ${h.commonAttributes(node, ['cmp-audio', 'cmp-audio-placeholder'], selectedId)}><span class="cmp-media-placeholder">请输入有效的音频地址</span></div>`;
            }
            const preload = ['none', 'metadata', 'auto'].includes(node.props.preload) ? node.props.preload : 'metadata';
            return h.fill(h.template('audio'), {
                attrs: h.commonAttributes(node, ['cmp-audio'], selectedId),
                title: h.escapeHTML(node.props.title || '音频'),
                preload,
                controls: h.booleanAttribute('controls', node.props.controls),
                autoplay: h.booleanAttribute('autoplay', node.props.autoplay),
                loop: h.booleanAttribute('loop', node.props.loop),
                src: h.escapeHTML(src)
            });
        }
    });
})(window);