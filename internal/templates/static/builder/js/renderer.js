(function (global) {
    'use strict';

    const allowedContainerTags = new Set(['div', 'section', 'main', 'article', 'header', 'footer', 'nav']);
    const inertEmptyContainerStyles = new Set([
        'display', 'flex-direction', 'flex-wrap', 'align-items', 'align-content',
        'justify-content', 'gap', 'row-gap', 'column-gap', 'position', 'z-index'
    ]);
    const frameBindings = new WeakMap();

    function escapeHTML(value) {
        return String(value ?? '')
            .replaceAll('&', '&amp;')
            .replaceAll('<', '&lt;')
            .replaceAll('>', '&gt;')
            .replaceAll('"', '&quot;')
            .replaceAll("'", '&#039;');
    }

    function safeCSSValue(value, fallback) {
        const normalized = String(value ?? '').trim();
        return normalized && !/[<>{};]/.test(normalized) ? normalized : fallback;
    }

    function scriptText(value) {
        return String(value ?? '').replace(/<\/script/gi, '<\\/script');
    }

    function styleText(value) {
        return String(value ?? '').replace(/<\/style/gi, '<\\/style');
    }

    function styleAttribute(style) {
        if (!style || typeof style !== 'object') {
            return '';
        }
        const projected = style.animation
            ? { ...style, 'animation-fill-mode': 'both' }
            : style;
        const value = Object.entries(projected)
            .filter(([key, item]) => /^[-a-zA-Z]+$/.test(key) && typeof item === 'string' && item.trim() !== '')
            .map(([key, item]) => `${key}:${item.replace(/[<>]/g, '')}`)
            .join(';');
        return value ? ` style="${escapeHTML(value)}"` : '';
    }

    function classNames(node, baseClasses, selectedId) {
        const custom = String(node.props.cssClass || '')
            .split(/\s+/)
            .filter((name) => /^[a-zA-Z_][\w-]*$/.test(name));
        if (node.id === selectedId) { custom.push('is-selected'); }
        return [...baseClasses, ...custom].join(' ');
    }

    function commonAttributes(node, baseClasses, selectedId, extraStyle = {}) {
        const id = /^[a-zA-Z_][\w-]*$/.test(node.props.cssId || '')
            ? ` id="${escapeHTML(node.props.cssId)}"`
            : '';
        const style = { ...(node.style || {}), ...extraStyle };
        const editorAttribute = selectedId === undefined ? '' : ` data-node-id="${escapeHTML(node.id)}"`;
        return `class="${escapeHTML(classNames(node, baseClasses, selectedId))}"${id}${editorAttribute}${styleAttribute(style)}`;
    }

    function editableAttributes(node, selectedId) {
        return selectedId === undefined
            ? ''
            : `contenteditable="true" spellcheck="true" data-inline-field="text" data-original-text="${escapeHTML(node.props.text)}"`;
    }

    function safeURL(value, fallback = '') {
        const url = String(value || '').trim();
        if (!url) { return fallback; }
        return /^(https?:\/\/|data:|blob:|\/|#|\.\/|\.\.\/)/i.test(url) ? url : fallback;
    }

    function youtubeVideoId(value) {
        const input = String(value || '').trim();
        if (/^[a-zA-Z0-9_-]{11}$/.test(input)) { return input; }
        try {
            const url = new URL(input);
            const host = url.hostname.replace(/^www\./, '');
            const candidate = host === 'youtu.be'
                ? url.pathname.split('/').filter(Boolean)[0]
                : ['youtube.com', 'm.youtube.com'].includes(host)
                    ? url.searchParams.get('v') || url.pathname.match(/^\/(?:embed|shorts)\/([^/]+)/)?.[1]
                    : '';
            return /^[a-zA-Z0-9_-]{11}$/.test(candidate || '') ? candidate : '';
        } catch (_) {
            return '';
        }
    }

    function sanitizeSVG(markup, title) {
        const parser = new DOMParser();
        const document = parser.parseFromString(String(markup || ''), 'image/svg+xml');
        const root = document.documentElement;
        const allowedTags = new Set(['svg', 'g', 'path', 'circle', 'ellipse', 'rect', 'line', 'polyline', 'polygon', 'defs', 'linearGradient', 'radialGradient', 'stop', 'title']);
        const allowedAttributes = new Set(['viewBox', 'width', 'height', 'fill', 'stroke', 'stroke-width', 'stroke-linecap', 'stroke-linejoin', 'd', 'cx', 'cy', 'r', 'rx', 'ry', 'x', 'y', 'x1', 'x2', 'y1', 'y2', 'points', 'transform', 'opacity', 'offset', 'stop-color', 'stop-opacity', 'gradientUnits', 'gradientTransform', 'id', 'role', 'aria-label', 'focusable']);
        if (root.nodeName.toLowerCase() !== 'svg' || root.querySelector('parsererror')) { return ''; }
        [...root.querySelectorAll('*')].forEach((element) => {
            if (!allowedTags.has(element.nodeName)) {
                element.remove();
                return;
            }
            [...element.attributes].forEach((attribute) => {
                const value = attribute.value.trim();
                if (!allowedAttributes.has(attribute.name) || /^on/i.test(attribute.name) || /(?:javascript:|data:text\/html)/i.test(value)) {
                    element.removeAttribute(attribute.name);
                }
            });
        });
        [...root.attributes].forEach((attribute) => {
            if (!allowedAttributes.has(attribute.name)) { root.removeAttribute(attribute.name); }
        });
        root.setAttribute('role', title ? 'img' : 'presentation');
        root.setAttribute('focusable', 'false');
        if (title) {
            root.setAttribute('aria-label', String(title));
        } else {
            root.removeAttribute('aria-label');
        }
        return new XMLSerializer().serializeToString(root);
    }

    function booleanAttribute(name, enabled) {
        return enabled === true ? ` ${name}` : '';
    }

    function isRedundantEmptyContainer(node, selectedId) {
        if (selectedId !== undefined || node.type !== 'container' || node.children.length !== 0) {
            return false;
        }
        if (String(node.props.cssId || '').trim() || String(node.props.cssClass || '').trim()) {
            return false;
        }
        return Object.entries(node.style || {}).every(([key, value]) =>
            !String(value || '').trim() || inertEmptyContainerStyles.has(key)
        );
    }

    // ==========================================================================
    // 组件渲染：由组件模块（components/*.js）通过 BuilderComponents.register 提供。
    // builder 只负责调度（显示器 + 插槽），组件自带定义、字段和 render。
    // ==========================================================================
    function renderNode(node, selectedId) {
        if (isRedundantEmptyContainer(node, selectedId)) {
            return '';
        }
        // svg 向后兼容：旧的 'svg' type 映射到 icon 组件
        const type = node.type === 'svg' ? 'icon' : node.type;
        const component = global.BuilderComponents?.getByType(type);
        if (component?.render) {
            return component.render(node, selectedId, helpers);
        }
        return '';
    }

    // 组件可用的工具集（render(node, selectedId, helpers) 的第三参）
    const helpers = {
        escapeHTML,
        safeCSSValue,
        scriptText,
        styleText,
        styleAttribute,
        classNames,
        commonAttributes,
        editableAttributes,
        safeURL,
        youtubeVideoId,
        sanitizeSVG,
        booleanAttribute,
        // 容器型组件递归子节点
        renderChild: (child, selectedId) => renderNode(child, selectedId),
        renderChildren: (node, selectedId) => (node.children || []).map((child) => renderNode(child, selectedId)).join(''),
        // 组件模板（components/<key>/template.html，由 loader 注入注册表）
        template: (key) => global.BuilderComponents?.get(key)?.template || '',
        // 模板占位符填充：{{key}} → value（组件 render 内用）
        fill: (template, data) => String(template || '').replace(/\{\{(\w+)\}\}/g, (_, key) => String(data?.[key] ?? ''))
    };

    function previewBridgeScript() {
        return `<script>
(function () {
    'use strict';
    // 使用 location.origin 而不是 '*'，更安全且避免浏览器扩展干扰
    const targetOrigin = window.location.ancestorOrigins?.[0] || window.location.origin;
    const send = (type, detail) => {
        try {
            parent.postMessage({ channel: 'go-wp-builder-preview', type, detail }, targetOrigin);
        } catch (err) {
            // 静默处理 postMessage 错误，避免浏览器扩展干扰
            console.debug('Preview bridge send failed:', err.message);
        }
    };
    const activateTab = (tab) => {
        const tabs = [...tab.closest('[role="tablist"]').querySelectorAll('[role="tab"]')];
        const widget = tab.closest('.cmp-tabs');
        tabs.forEach((item) => {
            const active = item === tab;
            item.setAttribute('aria-selected', String(active));
            item.tabIndex = active ? 0 : -1;
            const panel = widget.querySelector('#' + item.getAttribute('aria-controls'));
            if (panel) { panel.hidden = !active; }
        });
    };
    window.addEventListener('error', (event) => send('codeError', { message: event.message || '自定义代码运行失败' }));
    window.addEventListener('unhandledrejection', (event) => send('codeError', { message: String(event.reason?.message || event.reason || 'Promise 执行失败') }));
    document.addEventListener('click', (event) => {
        event.preventDefault();
        const emptyTrigger = event.target.closest('.cmp-empty, .canvas-empty');
        const tab = event.target.closest('[role="tab"]');
        if (tab) { activateTab(tab); }
        const element = event.target.closest('[data-node-id]');
        const selectedId = element?.dataset.nodeId || document.body?.dataset.rootId;
        send('select', { nodeId: selectedId });
        if (emptyTrigger) { send('openLibrary', { parentId: selectedId }); }
    });
    document.addEventListener('blur', (event) => {
        const editable = event.target.closest?.('[data-inline-field]');
        if (!editable) { return; }
        const value = editable.textContent || '';
        if (value !== editable.dataset.originalText) {
            send('inlineEdit', { nodeId: editable.dataset.nodeId, field: editable.dataset.inlineField, value });
        }
    }, true);
    document.addEventListener('keydown', (event) => {
        const tab = event.target.closest?.('[role="tab"]');
        if (tab && ['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) {
            event.preventDefault();
            const tabs = [...tab.closest('[role="tablist"]').querySelectorAll('[role="tab"]')];
            const current = tabs.indexOf(tab);
            const next = event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1 : (current + (event.key === 'ArrowRight' ? 1 : -1) + tabs.length) % tabs.length;
            activateTab(tabs[next]);
            tabs[next].focus();
            return;
        }
        const editable = event.target.closest?.('[data-inline-field]');
        if (!editable || event.key !== 'Enter' || editable.classList.contains('cmp-text')) { return; }
        event.preventDefault();
        editable.blur();
    });
    document.addEventListener('dragover', (event) => {
        event.preventDefault();
        event.target.closest('.cmp-container')?.classList.add('is-drop-target');
    });
    document.addEventListener('dragleave', (event) => event.target.closest('.cmp-container')?.classList.remove('is-drop-target'));
    document.addEventListener('drop', (event) => {
        event.preventDefault();
        document.querySelectorAll('.is-drop-target').forEach((item) => item.classList.remove('is-drop-target'));
        const componentKey = event.dataTransfer.getData('application/x-builder-component') || event.dataTransfer.getData('text/plain');
        const container = event.target.closest('.cmp-container');
        send('drop', { componentKey, parentId: container?.dataset.nodeId || document.body?.dataset.rootId });
    });
})();
<\/script>`;
    }

    function publishedRuntimeScript() {
        return `<script>
(function () {
    'use strict';
    const activateTab = (tab) => {
        const tabs = [...tab.closest('[role="tablist"]').querySelectorAll('[role="tab"]')];
        const widget = tab.closest('.cmp-tabs');
        tabs.forEach((item) => {
            const active = item === tab;
            item.setAttribute('aria-selected', String(active));
            item.tabIndex = active ? 0 : -1;
            const panel = widget.querySelector('#' + item.getAttribute('aria-controls'));
            if (panel) { panel.hidden = !active; }
        });
    };
    document.addEventListener('click', (event) => {
        const tab = event.target.closest('[role="tab"]');
        if (tab) { activateTab(tab); }
    });
    document.addEventListener('keydown', (event) => {
        const tab = event.target.closest?.('[role="tab"]');
        if (!tab || !['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) { return; }
        event.preventDefault();
        const tabs = [...tab.closest('[role="tablist"]').querySelectorAll('[role="tab"]')];
        const current = tabs.indexOf(tab);
        const next = event.key === 'Home' ? 0 : event.key === 'End' ? tabs.length - 1 : (current + (event.key === 'ArrowRight' ? 1 : -1) + tabs.length) % tabs.length;
        activateTab(tabs[next]);
        tabs[next].focus();
    });
})();
<\/script>`;
    }

    function containsNodeType(node, type) {
        if (node.type === type) {
            return true;
        }
        return Array.isArray(node.children) && node.children.some((child) => containsNodeType(child, type));
    }

    // 收集文档中实际用到的组件 key（发布时按需产出样式）
    function collectUsedComponentKeys(node, keys = new Set()) {
        const definition = global.BuilderComponents?.getDefinition(node);
        if (definition?.key) { keys.add(definition.key); }
        (node.children || []).forEach((child) => collectUsedComponentKeys(child, keys));
        return keys;
    }

    function renderDocument(document, selectedId, options = {}) {
        const editorMode = selectedId !== undefined;
        const settings = global.BuilderEditor.mergeSettings(document.settings);
        const { general, layout, style, advanced } = settings;
        const content = document.root.children.length
            ? document.root.children.map((node) => renderNode(node, selectedId)).join('')
            : editorMode ? '<div class="canvas-empty">点击添加区块或元素</div>' : '';
        const rootTag = allowedContainerTags.has(document.root.props.tag) ? document.root.props.tag : 'main';
        const rootAttributes = commonAttributes(document.root, ['page-shell'], selectedId);
        const alignment = ['left', 'center', 'right'].includes(layout.alignment) ? layout.alignment : 'center';
        const shellMargin = alignment === 'center' ? '0 auto' : alignment === 'right' ? '0 0 0 auto' : '0 auto 0 0';
        // 页面模板：default=内容居中容器 / full-width=全宽 / canvas=无容器
        const template = ['default', 'full-width', 'canvas'].includes(general.template) ? general.template : 'default';
        const shellMaxWidth = template === 'full-width' ? '100%' : safeCSSValue(layout.contentWidth, '1140px');
        const shellBox = template === 'canvas'
            ? '.page-shell{width:100%;min-height:' + safeCSSValue(layout.minHeight, '100vh') + ';}'
            : `.page-shell{width:100%;max-width:${shellMaxWidth};min-height:${safeCSSValue(layout.minHeight, '100vh')};margin:${shellMargin};}`;
        // 四边盒：margin / padding（Elementor Body Style DIMENSIONS）
        const bodyMargin = cssDimensionBox(style, 'margin', '0px');
        const bodyPadding = cssDimensionBox(style, 'padding', '0px');
        // 背景组（Elementor Group_Control_Background 子集：颜色 + 图片 + 位置 + 重复 + 尺寸）
        const backgroundImage = style.backgroundImage
            ? `background-image:url("${escapeHTML(style.backgroundImage)}");background-position:${safeCSSValue(style.backgroundPosition, 'center center')};background-repeat:${safeCSSValue(style.backgroundRepeat, 'no-repeat')};background-size:${safeCSSValue(style.backgroundSize, 'cover')};`
            : '';
        // 隐藏页面标题（Elementor hide_title：注入 --page-title-display）
        const hideTitleRule = general.hideTitle
            ? ':root{--page-title-display:none}'
            : '';
        // 组件间距（Elementor Space Between Widgets，默认 20px）
        const widgetSpacing = layout.spaceBetweenWidgets
            ? `:where(.page-shell,.cmp-container) > :where([class^="cmp-"]):not(.cmp-container):not(.cmp-tabs-panel):not(:last-child){margin-block-end:${safeCSSValue(layout.spaceBetweenWidgets, '20px')}}`
            : '';
        const editLinkStyle = options.editURL
            ? '\n.builder-edit-return{position:fixed;right:20px;bottom:20px;z-index:2147483647;padding:10px 16px;border-radius:9999px;color:#fff;background:#3d444f;box-shadow:0 8px 24px rgba(13,37,61,.25);font:500 14px/1.2 Inter,system-ui,sans-serif;text-decoration:none;}'
            : '';
        const pageStyle = `
html,body{min-height:${safeCSSValue(layout.minHeight, '100vh')};background-color:${safeCSSValue(style.backgroundColor, '#ffffff')};${backgroundImage}}
body{margin:${bodyMargin};padding:${bodyPadding};}
${shellBox}${widgetSpacing}${hideTitleRule}${editLinkStyle}`;
        const favicon = general.favicon
            ? `<link rel="icon" href="${escapeHTML(general.favicon)}">`
            : '';
        const customCodeEnabled = editorMode ? advanced.previewEnabled === true : true;
        const javascript = customCodeEnabled && advanced.javascript
            ? `<script>${scriptText(advanced.javascript)}<\/script>`
            : '';
        const headCode = customCodeEnabled ? String(advanced.head || '') : '';
        const bodyEndCode = customCodeEnabled ? String(advanced.bodyEnd || '') : '';
        const runtimeScript = editorMode
            ? previewBridgeScript()
            : containsNodeType(document.root, 'tabs') ? publishedRuntimeScript() : '';
        // 组件样式：编辑器全量注入（便于预览任何组件），发布仅内联实际用到的组件样式
        const componentStyles = editorMode
            ? global.BuilderComponents?.collectAllStyles() || ''
            : global.BuilderComponents?.collectStyles([...collectUsedComponentKeys(document.root)]) || '';
        const descriptionMeta = general.description
            ? `<meta name="description" content="${escapeHTML(general.description)}">`
            : '';
        const robotsMeta = options.noIndex
            ? '<meta name="robots" content="noindex,nofollow">'
            : '';
        const bodyAttributes = editorMode ? ` data-root-id="${escapeHTML(document.root.id)}"` : '';
        const editLink = !editorMode && options.editURL
            ? `<a class="builder-edit-return" href="${escapeHTML(options.editURL)}">返回编辑</a>`
            : '';

        return `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>${escapeHTML(general.title || general.name)}</title>
${descriptionMeta}
${robotsMeta}
${favicon}
<link rel="stylesheet" href="/static/builder/css/preview.css?v=components-1">
<style data-component-styles>${componentStyles}</style>
<style>${pageStyle}</style>
<style data-page-custom-css>${styleText(advanced.css)}</style>
${runtimeScript}
${headCode}
</head>
<body${bodyAttributes}>
<${rootTag} ${rootAttributes}>${content}</${rootTag}>
${editLink}
${bodyEndCode}
${javascript}
</body>
</html>`;
    }

    // 组装四边盒 CSS：style.marginTop + marginUnit → '10px 5px 10px 5px'
    function cssDimensionBox(style, prefix, fallback) {
        const sides = ['Top', 'Right', 'Bottom', 'Left'];
        const values = sides.map((side) => {
            const raw = style?.[`${prefix}${side}`];
            return raw === '' || raw === undefined || raw === null ? '' : String(raw);
        });
        if (values.every((v) => v === '')) {
            return fallback;
        }
        // 简写优化：四边相同 → 单值
        if (values[0] === values[1] && values[1] === values[2] && values[2] === values[3]) {
            return values[0];
        }
        if (values[0] === values[2] && values[1] === values[3]) {
            return `${values[0]} ${values[1]}`;
        }
        return values.join(' ');
    }

    function renderPublishedDocument(document, editURL) {
        return renderDocument(document, undefined, { editURL, noIndex: Boolean(editURL) });
    }

    function bindFrame(frame, callbacks) {
        const previous = frameBindings.get(frame);
        if (previous) {
            global.removeEventListener('message', previous);
        }
        const listener = (event) => {
            if (event.source !== frame.contentWindow || event.data?.channel !== 'go-wp-builder-preview') {
                return;
            }
            const detail = event.data.detail || {};
            switch (event.data.type) {
            case 'select': callbacks.onSelect(detail.nodeId); break;
            case 'openLibrary': callbacks.onOpenLibrary(detail.parentId); break;
            case 'inlineEdit': callbacks.onInlineEdit(detail.nodeId, detail.field, detail.value); break;
            case 'drop': callbacks.onDrop(detail.componentKey, detail.parentId); break;
            case 'codeError': callbacks.onCodeError(detail.message || '自定义代码运行失败'); break;
            }
        };
        frameBindings.set(frame, listener);
        global.addEventListener('message', listener);
    }

    function renderFrame(frame, document, selectedId, callbacks) {
        bindFrame(frame, callbacks);
        callbacks.onCodeError('');
        frame.srcdoc = renderDocument(document, selectedId);
    }

    global.BuilderRenderer = Object.freeze({ renderNode, renderDocument, renderPublishedDocument, renderFrame });
})(window);