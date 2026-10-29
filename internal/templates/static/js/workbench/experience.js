// 编辑体验信息属于前端；组件校验与默认属性继续使用生成的契约。
export const libraryCategories = [
    { key: 'basic', title: '基础元素' },
    { key: 'interactive', title: '交互展示' },
    { key: 'commerce', title: '站点与电商' },
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
const siteElements = new Set(['nav', 'languages', 'breadcrumb', 'form', 'social_buttons', 'product', 'productCard', 'productList', 'productSelector', 'addToCart', 'cartIcon', 'orderList', 'searchResults', 'userForms', 'checkoutForm', 'globalref']);
export function categoryOf(type) {
    if (basicElements[type]) return 'basic';
    if (!type.startsWith('core.') || siteElements.has(type.slice(5))) return 'commerce';
    return 'interactive';
}
export function elementAppearance(type) {
    return basicElements[type] || [categoryOf(type) === 'commerce' ? 'blocks' : 'component', ''];
}
// 分组由声明保留；页签只是字段的展示位置，不改变字段路径与存储形状。
export function inspectorTabOf(section, path) {
    path = path || '';
    if (/^(motion|hover|transform)$/.test(section) || /(^|\.)(interaction|hover|animation|textAnim|textAnimDelay)(\.|$)/.test(path)) return 'motion';
    if (/^(background|border)$/.test(section) || /(^|\.)(border|radius)(\.|$)/.test(path)) return 'style';
    if (/^(layout|responsive)$/.test(section) || /(^|\.)(box|spacing|margin|padding|width|height|minWidth|maxWidth|minHeight|maxHeight|position|display|alignSelf|flexGrow|flexShrink|flexBasis|gridColumn|gridRow|order)(\.|$)/.test(path)) return 'layout';
    return section === 'content' ? 'content' : 'style';
}
