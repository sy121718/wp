// 编辑体验信息属于前端；组件校验与默认属性继续使用生成的契约。
export const libraryCategories = [
    { key: 'basic', title: '基础元素' },
    { key: 'interactive', title: '交互展示' },
    { key: 'site', title: '内容与站点' },
    { key: 'commerce', title: '电商与账号' },
    { key: 'plugins', title: '插件组件' },
    { key: 'presets', title: '组合区块' },
    { key: 'all', title: '全部' }
];
const basicElements = {
    'core.container': ['square-dashed', 'div / section'],
    'core.heading': ['heading', 'h1 – h6'],
    'core.text': ['pilcrow', 'p / span'],
    'core.button': ['mouse-pointer-2', 'a / button'],
    'core.image': ['image', 'img'],
    'core.video': ['video', 'video'],
    'core.list': ['list', 'ul / ol'],
    'core.quote': ['quote', 'blockquote'],
    'core.divider': ['minus', 'hr'],
    'core.spacer': ['space', '间距'],
    'core.icon': ['shapes', '图标'],
    'core.table': ['table-2', 'table']
};
const siteElements = new Set(['nav', 'languages', 'breadcrumb', 'form', 'social_buttons', 'articleList', 'searchResults', 'globalref']);
const commerceElements = new Set(['product', 'productCard', 'productList', 'productSelector', 'addToCart', 'cartIcon', 'orderList', 'userForms', 'checkoutForm']);
export function categoryOf(type) {
    if (basicElements[type]) return 'basic';
    if (!type.startsWith('core.')) return 'plugins';
    if (siteElements.has(type.slice(5))) return 'site';
    if (commerceElements.has(type.slice(5))) return 'commerce';
    return 'interactive';
}
export function initialInspectorTabOf(type) {
    if (type === 'core.container' || type === 'core.spacer') return 'layout';
    return type === 'core.divider' ? 'style' : 'content';
}
export function elementAppearance(type) {
    return basicElements[type] || [categoryOf(type) === 'commerce' ? 'blocks' : 'component', ''];
}
// 分组由声明保留；页签只是字段的展示位置，不改变字段路径与存储形状。
export function inspectorTabOf(section, path) {
    path = path || '';
    if (section === 'collection') return 'content';
    if (/^(motion|hover|transform)$/.test(section) || /(^|\.)(interaction|hover|animation|textAnim|textAnimDelay)(\.|$)/.test(path)) return 'motion';
    if (/^(background|border)$/.test(section) || /(^|\.)(border|radius)(\.|$)/.test(path)) return 'style';
    if (/^(layout|responsive)$/.test(section) || /(^|\.)(layout|columns|box|spacing|margin|padding|width|height|minWidth|maxWidth|minHeight|maxHeight|position|display|alignSelf|flexGrow|flexShrink|flexBasis|gridColumn|gridRow|order)(\.|$)/.test(path)) return 'layout';
    return section === 'content' ? 'content' : 'style';
}
