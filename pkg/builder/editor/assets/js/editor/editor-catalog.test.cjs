const test = require('node:test');
const assert = require('node:assert/strict');

// 工具目录只消费显式注入的完整注册结果；不内置应用组件清单。
async function catalogModule() {
    return import('./catalog.js');
}

const contracts = {
    items: {
        'core.container': { displayName: '容器', category: 'basic', defaultProps: {} },
        'core.tabs': { displayName: '页签', category: 'basic', defaultProps: { tabs: [{ label: '页签1' }] } },
        'core.product': { displayName: '商品详情', category: 'basic', defaultProps: {}, requiredEntityTypes: ['product'] }
    },
    alignedRepeaters: {
        'core.tabs': { alignKey: 'tabs', field: 'label', noun: '页签', type: 'core.tabs', label: '' }
    }
};

function alloc() {
    var seq = {};
    return function (base) { seq[base] = (seq[base] || 0) + 1; return base + '-' + seq[base]; };
}

test('未注入目录时不产生任何组件条目（工具不内置应用语义）', async () => {
    const m = await catalogModule();
    assert.deepEqual(m.paletteItems, []);
    assert.deepEqual(m.paletteSpecItems(), {});
});

test('注入目录后组件库按分组与排序派生，业务组件只在注入后出现', async () => {
    const m = await catalogModule();
    m.configureCatalog(contracts);
    assert.deepEqual(m.paletteItems.map(i => i.type), ['core.container', 'core.tabs', 'core.product']);
    assert.deepEqual(m.paletteGroups.map(g => g.key), ['basic']);
    assert.equal(m.defaultPropsOf('core.tabs').tabs[0].label, '页签1');
});

test('结构型插入按注入默认内容补齐子节点与数组，非结构型不造子节点', async () => {
    const m = await catalogModule();
    m.configureCatalog(contracts);
    const tabs = m.buildInsertNode(m.paletteItems.find(i => i.type === 'core.tabs'), alloc());
    assert.equal(tabs.children.length, 1);
    assert.equal(tabs.props.tabs.length, 1);
    const container = m.buildInsertNode(m.paletteItems.find(i => i.type === 'core.container'), alloc());
    assert.equal(container.children, undefined);
});

test('对齐规则来自注入声明：新增/删除/移动保持数组与子节点一致', async () => {
    const m = await catalogModule();
    m.configureCatalog(contracts);
    let node = m.buildInsertNode(m.paletteItems.find(i => i.type === 'core.tabs'), alloc());
    const add = m.alignMutation('core.tabs', node.props.tabs, node.children, { op: 'add' }, alloc());
    assert.equal(add.list.length, 2);
    assert.equal(add.children.length, 2);
    const add2 = m.alignMutation('core.tabs', add.list, add.children, { op: 'add' }, alloc());
    const moved = m.alignMutation('core.tabs', add2.list, add2.children, { op: 'move', index: 0, to: 2 });
    assert.equal(moved.list.length, 3);
    assert.equal(moved.children.length, 3);
    const fromKids = m.alignFromChildren('core.tabs', add2.list.slice(0, 2), add2.children, { op: 'insert', index: 2 });
    assert.equal(fromKids.list.length, 3);
});

test('业务实体限制只读组件声明，未知类型返回空规则', async () => {
    const m = await catalogModule();
    m.configureCatalog(contracts);
    assert.match(m.paletteUnavailableReason(m.paletteItems.find(i => i.type === 'core.product'), { target: { type: 'page' } }), /实体上下文/);
    assert.equal(m.alignKeyOf('core.container'), '');
    assert.equal(m.buildDefaultChild('core.container', 0, alloc()), null);
});
