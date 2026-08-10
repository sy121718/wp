(function (global) {
    'use strict';
    const option = (value, label) => ({ value, label });

    global.BuilderComponents.register({
        key: 'card',
        type: 'card',
        label: '卡片',
        category: 'basic',
        icon: '▭',
        defaultProps: {
            mediaType: 'icon',
            iconMarkup: '<svg viewBox="0 0 24 24"><path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z" fill="currentColor"/></svg>',
            imageSrc: '', imageAlt: '', title: '卡片标题', titleTag: 'h3',
            description: '这里是卡片描述内容。', buttonText: '', buttonHref: '#',
            layout: 'vertical', textAlign: 'left'
        },
        fields: [
            { key: 'mediaType', label: '媒体类型', type: 'select', options: [option('none', '无'), option('icon', '图标'), option('image', '图片')] },
            { key: 'iconMarkup', label: 'SVG 源码', type: 'textarea', placeholder: '<svg viewBox="0 0 24 24">...</svg>', condition: { field: 'mediaType', value: 'icon' } },
            { key: 'imageSrc', label: '图片地址', type: 'url', placeholder: 'https://example.com/image.jpg', condition: { field: 'mediaType', value: 'image' } },
            { key: 'imageAlt', label: '图片替代文字', type: 'text', placeholder: '描述图片', condition: { field: 'mediaType', value: 'image' } },
            { key: 'title', label: '标题', type: 'text', placeholder: '卡片标题' },
            { key: 'titleTag', label: '标题标签', type: 'select', options: ['h2', 'h3', 'h4', 'h5', 'h6'].map((value) => option(value, value.toUpperCase())) },
            { key: 'description', label: '描述内容', type: 'textarea', placeholder: '卡片描述' },
            { key: 'buttonText', label: '按钮文字', type: 'text', placeholder: '了解更多（留空不显示按钮）' },
            { key: 'buttonHref', label: '按钮链接', type: 'url', placeholder: '#' },
            { key: 'layout', label: '布局', type: 'select', options: [option('vertical', '垂直'), option('horizontal', '水平')] },
            { key: 'textAlign', label: '文字对齐', type: 'select', options: [option('left', '左对齐'), option('center', '居中'), option('right', '右对齐')] }
        ],
        render(node, selectedId, h) {
            const titleTag = ['h2', 'h3', 'h4', 'h5', 'h6'].includes(node.props.titleTag) ? node.props.titleTag : 'h3';
            const layout = node.props.layout === 'horizontal' ? 'horizontal' : 'vertical';
            const textAlign = ['left', 'center', 'right'].includes(node.props.textAlign) ? node.props.textAlign : 'left';

            let media = '';
            if (node.props.mediaType === 'icon' && node.props.iconMarkup) {
                const svg = h.sanitizeSVG(node.props.iconMarkup, '');
                if (svg) { media = `<div class="cmp-card-icon">${svg}</div>`; }
            } else if (node.props.mediaType === 'image' && node.props.imageSrc) {
                const src = h.safeURL(node.props.imageSrc);
                if (src) { media = `<img src="${h.escapeHTML(src)}" alt="${h.escapeHTML(node.props.imageAlt || '')}" class="cmp-card-image" loading="lazy">`; }
            }

            const title = node.props.title ? `<${titleTag} class="cmp-card-title">${h.escapeHTML(node.props.title)}</${titleTag}>` : '';
            const description = node.props.description ? `<div class="cmp-card-description">${h.escapeHTML(node.props.description)}</div>` : '';
            const button = node.props.buttonText ? `<a href="${h.escapeHTML(h.safeURL(node.props.buttonHref, '#'))}" class="cmp-card-button">${h.escapeHTML(node.props.buttonText)}</a>` : '';

            return h.fill(h.template('card'), {
                attrs: h.commonAttributes(node, ['cmp-card', `cmp-card--${layout}`, `cmp-card--${textAlign}`], selectedId),
                media,
                title,
                description,
                button
            });
        }
    });
})(window);