// 组合区块只生成基础节点，插入后没有额外组件边界，结构最多四层。
var serial = 0;
function node(type, name, props, children) {
    return { id: 'studio-' + (++serial), type: 'core.' + type, name: name, props: props, ...(children ? { children: children } : {}) };
}
function text(name, value, size, color) {
    return node('text', name, { advanced: { margin: { desktop: { top: '0px', right: '0px', bottom: '0px', left: '0px' } } }, mode: 'plaintext', plainTag: 'p', text: value, color: color || '#68696e', typography: { desktop: { fontSize: size || '15px', lineHeight: '1.8' } } });
}
function heading(name, value, size, color, tag) {
    return node('heading', name, { advanced: { margin: { desktop: { top: '0px', right: '0px', bottom: '0px', left: '0px' } } }, tag: tag || 'h2', text: value, color: color || '#202124', weight: '600', letterSpacing: '-1px', typography: { desktop: { fontSize: size, lineHeight: '1.12' }, mobile: { fontSize: size === '68px' ? '42px' : size, lineHeight: '1.2' } } });
}
function container(name, props, children) {
    return node('container', name, { tag: 'section', layout: { engine: 'flex', flex: { direction: 'column', gap: '20px' } }, ...props }, children);
}
function button(label, dark) {
    return node('button', label, { text: label, action: 'anchor', value: 'studio-create', target: 'self', size: 'lg', bg: dark ? '#202124' : '#ffffff', textColor: dark ? '#ffffff' : '#202124', hoverBg: dark ? '#42454a' : '#e9ede8', fontSize: '14px', fontWeight: '500', radiusTL: '999px', radiusTR: '999px', radiusBL: '999px', radiusBR: '999px', icon: { source: 'builtin', name: 'arrow-right', position: 'suffix', spacing: '20px' }, advanced: { alignSelf: 'start', interaction: { hoverEffect: 'lift' } } });
}
var nav = container('导航', { tag: 'header', layout: { engine: 'flex', flex: { direction: 'row', justify: 'space-between', align: 'center' } }, box: { maxWidth: '1200px', center: true, padding: { desktop: '28px 48px', mobile: '20px' } } }, [heading('品牌', 'form & field', '23px'), text('导航说明', '独立设计 · 自由表达', '12px')]);
var hero = container('创作首屏', { layout: { engine: 'grid', grid: { columns: { desktop: 2, tablet: 1, mobile: 1 }, columnGap: '56px', rowGap: '32px' } }, box: { maxWidth: '1200px', center: true, padding: { desktop: '64px 48px 72px', tablet: '40px 32px', mobile: '32px 20px' } } }, [
    container('首屏内容', { tag: 'div', layout: { engine: 'flex', flex: { direction: 'column', gap: '28px', justify: 'center' } } }, [
        text('引导语', 'A SMALL STUDIO. A BIG OPEN CANVAS.', '11px', '#697666'),
        heading('主标题', '自由创作，即刻开始。', '68px', '#222820', 'h1'),
        text('首屏描述', '从一个简单的元素开始，让文字、色彩与空间，慢慢长成属于你的作品。', '16px'),
        button('开始创造', true),
        text('首屏注释', '为每一种灵感，留一点自由。', '12px', '#8a8d86')
    ]),
    container('流动色彩', { tag: 'div', box: { minHeight: '440px', padding: { desktop: '36px', mobile: '28px' }, overflow: 'hidden' }, layout: { engine: 'flex', flex: { direction: 'column', justify: 'space-between', align: 'center', gap: '24px' } }, visual: { bgGradient: 'linear-gradient(135deg, #e3eadf, #c3d1b6, #e7d7ba, #b7c9c0)', bgGradientAnimated: true, radius: '32px' }, interaction: { entrance: 'fade-in-up', entranceDuration: 'slow' } }, [
        text('作品标记', 'EXPLORATIONS / 001', '11px', '#53604b'),
        node('heading', '漂浮符号', { tag: 'div', text: '✳', color: '#43563c', weight: '400', typography: { desktop: { fontSize: '208px', lineHeight: '1' }, mobile: { fontSize: '170px' } }, advanced: { interaction: { loopEffect: 'float' } } }),
        text('作品说明', 'Less structure. More possibility.', '13px', '#53604b')
    ])
]);
var intro = container('内容引导', { box: { maxWidth: '1200px', center: true, padding: { desktop: '48px 48px 24px', mobile: '28px 20px' } } }, [text('章节标记', '01 / MADE OF SIMPLE THINGS', '11px'), heading('章节标题', '简单的元素，无限的可能。', '32px'), text('章节描述', '没有复杂的层级。每一处细节，都在你的掌控之中。')]);
intro.children[1].props.advanced.customId = 'studio-create';
var features = container('内容网格', { layout: { engine: 'grid', grid: { columns: { desktop: 3, tablet: 3, mobile: 1 }, columnGap: '16px', rowGap: '16px' } }, box: { maxWidth: '1200px', center: true, padding: { desktop: '16px 48px 72px', tablet: '16px 32px 48px', mobile: '12px 20px 40px' } } }, [
    ['01', '从基础出发', '标题、段落、图片与按钮。熟悉的元素，就能表达不一样的想法。', '#f2f4ef'],
    ['02', '让细节发生', '调整留白与节奏，给元素一点动效，让页面在呼吸间鲜活起来。', '#f4f0e9'],
    ['03', '自由，属于每个人', '所有工具都向你敞开。无需解锁，专注于创造本身。', '#eef1f3']
].map(function (item) {
    return container(item[1], { tag: 'article', box: { padding: { desktop: '30px', mobile: '26px' }, minHeight: '240px' }, visual: { bgColor: item[3], radius: '20px' }, interaction: { hoverEffect: 'lift', entrance: 'fade-in-up', scrollReveal: 'reveal' } }, [text('卡片序号', item[0], '12px'), heading('卡片标题', item[1], '22px', '', 'h3'), text('卡片描述', item[2], '14px')]);
}));
var footer = container('页尾', { tag: 'footer', box: { maxWidth: '1200px', center: true, padding: { desktop: '28px 48px 40px', mobile: '24px 20px' } }, layout: { engine: 'flex', flex: { direction: 'row', justify: 'space-between', wrap: true, gap: '16px' } } }, [text('页尾品牌', 'form & field', '14px', '#343a30'), text('页尾说明', 'Built with curiosity. Open by design.', '12px')]);
export const starterPresets = [
    { id: 'studio-page', label: '自由画布 · 完整示例', category: '浅层结构 · 流动渐变与悬浮动画', document: [nav, hero, intro, features, footer] },
    { id: 'studio-hero', label: '双列品牌首屏', category: '标题 + 段落 + 按钮 + 容器', document: [hero, intro] },
    { id: 'studio-grid', label: '三列内容卡片', category: 'Grid 响应式 · 手机自动单列', document: [intro, features] }
];
