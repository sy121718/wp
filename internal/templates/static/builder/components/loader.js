(function (global) {
    'use strict';

    // ==========================================================================
    // 组件加载器：组件以「目录三件套」存在（<key>/index.js + template.html + style.css）。
    // 组件 index.js 先同步注册到 BuilderComponents，loader 再同步拉取每个组件的
    // 模板与样式，注入注册表；样式同时写入页面（编辑器预览）与注册表（发布按需收集）。
    // ==========================================================================

    function loadText(url) {
        const request = new XMLHttpRequest();
        request.open('GET', url, false); // 同步：保证 Alpine 初始化前模板/样式就绪
        request.send(null);
        if (request.status >= 200 && request.status < 300) {
            return request.responseText;
        }
        return '';
    }

    function componentBasePath(key) {
        return `/static/builder/components/${key}/`;
    }

    function loadComponentAssets(key) {
        // 资源目录：默认取组件 key；container 的 div/flexbox 变体共享 container/ 目录
        const definition = global.BuilderComponents.get(key);
        if (!definition) { return; }
        const base = componentBasePath(definition.assetDir || key);
        // 模板（结构）与样式（组件专属 CSS）各自独立文件
        const template = loadText(`${base}template.html`);
        const style = loadText(`${base}style.css`);
        definition.template = template;
        definition.styleContent = style;
        if (style) {
            const styleTag = document.createElement('style');
            styleTag.setAttribute('data-component-css', key);
            styleTag.textContent = style;
            document.head.appendChild(styleTag);
        }
    }

    function loadAllComponents() {
        global.BuilderComponents.all().forEach((definition) => {
            if (definition.key) {
                loadComponentAssets(definition.key);
            }
        });
    }

    // 收集一组组件类型的样式（发布/预览用），按注册顺序去重
    function collectStyles(keys) {
        const seen = new Set();
        let result = '';
        global.BuilderComponents.all().forEach((definition) => {
            if (keys.includes(definition.key) && definition.styleContent && !seen.has(definition.key)) {
                seen.add(definition.key);
                result += `\n${definition.styleContent}`;
            }
        });
        return result;
    }

    // 收集全部组件样式（编辑器预览用）
    function collectAllStyles() {
        return global.BuilderComponents.all()
            .map((definition) => definition.styleContent || '')
            .join('\n');
    }

    global.BuilderComponents = Object.freeze({
        ...global.BuilderComponents,
        loadAllComponents,
        collectStyles,
        collectAllStyles
    });

    // 页面加载完成前同步执行（builder.html 中本文件位于全部组件 index.js 之后）
    loadAllComponents();
})(window);