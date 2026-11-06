// editor-app/index.js — 应用宿主装配：把应用操作接到通用编辑器上。
//
// 工具（/static/js/editor/）只持通用机制；本文件提供端点、权限、业务面板与
// 保存/发布/冲突确认、媒体库、业务预设、商品选品。完整组件目录（palette /
// alignedRepeaters）由应用侧 generated-contracts.js 的唯一生成物注入。
import { configureHost, configureHostExtras, configureCatalog, configureHostMeta } from '../editor/core.js';
import { configureExperience } from '../editor/experience.js';
import { mount } from '../editor/index.js';
import { normalizeMediaURL } from '../editor/media-url.js';
import { createEditorHost } from './host.js';
import { paletteSpec, alignedRepeaters } from './generated-contracts.js';
import { openBusinessPreset } from './business-presets.js';
import { mediaMethods } from './methods/media.js';
import { historyMethods } from './methods/history.js';
import { appPanelMethods } from './methods/app-panels.js';
import { productBindingPanel } from './methods/controls/product.js';
import { addToCartPanel } from './methods/controls/addtocart.js';

function parseJSON(id, fallback) {
    var el = document.getElementById(id);
    if (!el || !el.textContent) return fallback;
    try { return JSON.parse(el.textContent); } catch (e) { return fallback; }
}

var meta = parseJSON('wb-meta', {});
var doc = parseJSON('wb-bootstrap', { settings: {}, root: [] });
var schemas = parseJSON('wb-schemas', {});

configureHostMeta(meta, doc, schemas);
configureCatalog({ items: paletteSpec.items || {}, alignedRepeaters: alignedRepeaters });
configureExperience({
    siteTypes: ['nav', 'languages', 'breadcrumb', 'form', 'social_buttons', 'articleList', 'searchResults', 'globalref'],
    commerceTypes: ['product', 'productCard', 'productList', 'productSelector', 'addToCart', 'cartIcon', 'orderList', 'userForms', 'checkoutForm']
});

function csrfToken() {
    var m = document.querySelector('meta[name="csrf-token"]');
    if (m && m.getAttribute('content')) return m.getAttribute('content');
    try { return sessionStorage.getItem('csrf_token') || ''; } catch (e) { return ''; }
}
function csrfHeaders(extra) {
    var h = extra || {};
    var t = csrfToken();
    if (t) h['X-CSRF-Token'] = t;
    return h;
}
function request(url, body) {
    return fetch(url, {
        method: 'POST', credentials: 'same-origin',
        headers: csrfHeaders({ 'Content-Type': 'application/json' }),
        body: JSON.stringify(body)
    }).then(function (response) {
        return response.json().catch(function () { throw new Error('服务返回了无法识别的结果，请重试。'); }).then(function (result) {
            if (!response.ok || !result || result.code >= 400) {
                var error = new Error((result && result.message) || '操作失败，请重试。');
                error.status = response.status;
                error.data = result && result.data;
                throw error;
            }
            return result.data || {};
        });
    });
}
function normalizeMedia(value) {
    var url = normalizeMediaURL(value, meta.mediaBaseUrl);
    if (url) return url;
    var cache = mediaMethods._mediaCache || [];
    if (/^\d+$/.test(String(value))) {
        for (var i = 0; i < cache.length; i++) {
            if (String(cache[i].id) === String(value)) return normalizeMediaURL(cache[i].url, meta.mediaBaseUrl);
        }
        return '';
    }
    return '';
}

configureHost(createEditorHost({
    meta: meta, request: request, csrfHeaders: csrfHeaders, csrfToken: csrfToken,
    normalizeMedia: normalizeMedia,
    currentDoc: function () { return (window.__wb && window.__wb.doc) || doc; },
    openBusinessPreset: function (wb, preset) {
        return openBusinessPreset(wb, preset, Object.assign({ businessPresets: businessPresets }, meta));
    }
}));
configureHostExtras({
    inspectorExtras: function (ctx) {
        if (ctx.node.type === 'core.productSelector') productBindingPanel(ctx);
        if (ctx.node.type === 'core.addToCart') addToCartPanel(ctx);
        renderNavExtra(ctx);
    }
});

function renderNavExtra(ctx) {
    ctx.panel.querySelectorAll('[data-wb-host-extra]').forEach(function (slot) {
        var key = slot.getAttribute('data-wb-host-extra');
        var details = document.createElement('details');
        details.className = 'wb-nav-new';
        details.setAttribute('data-wb-nav-new', key);
        var summary = document.createElement('summary');
        summary.className = 'wb-btn wb-btn-sm wb-btn-secondary';
        summary.textContent = '新建菜单项';
        var body = document.createElement('div');
        body.className = 'wb-nav-new-body';
        var title = document.createElement('input');
        title.type = 'text'; title.setAttribute('data-wb-nav-field', 'title'); title.placeholder = '菜单文字，如：关于我们';
        var path = document.createElement('input');
        path.type = 'text'; path.setAttribute('data-wb-nav-field', 'path'); path.placeholder = '链接，如 /about';
        var kind = document.createElement('select');
        kind.setAttribute('data-wb-nav-field', 'kind');
        [['header', '页眉'], ['header_mobile', '移动页眉'], ['footer', '页脚'], ['footer_mobile', '移动页脚']].forEach(function (o) {
            var opt = document.createElement('option'); opt.value = o[0]; opt.textContent = o[1]; kind.appendChild(opt);
        });
        var msg = document.createElement('p'); msg.className = 'wb-hint'; msg.setAttribute('data-wb-nav-msg', ''); msg.setAttribute('role', 'status');
        var create = document.createElement('button');
        create.type = 'button'; create.className = 'wb-btn wb-btn-primary wb-btn-sm'; create.setAttribute('data-wb-nav-create', '');
        create.textContent = '创建并选用';
        body.append(title, path, kind, msg, create);
        details.append(summary, body);
        slot.appendChild(details);
    });
}

// 应用扩展方法：媒体库、修订历史、业务面板。
mount(Object.assign({}, mediaMethods, historyMethods, appPanelMethods));
