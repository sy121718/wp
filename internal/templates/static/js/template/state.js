// template/state.js — 模板编辑器的数据模型（纯函数，不碰 DOM）。
//
// ## 与表单页时代的关键差别
//
// 模板正文从「两个 textarea」变成一棵**块树**（TemplateTree，见 service/template_compile.go）。
// 树的所有变更都在这里：加块、删块、移动、改属性。编辑器（index.js）只负责把事件翻成
// 这里的调用，渲染（tree.js / canvas.js）只负责把树画出来 —— 三层各管一件事。
//
// ## 为什么不在这里生成 HTML
//
// **HTML 由 Go 编译**（service.template_compile.go），前端只把树 POST 上去、
// 拿回 body_compiled_html 塞进 iframe srcdoc。前端拼一份 = 预览与发信两套生成器，
// 而那种差异没人会在测试里发现（预览看起来对、发出去不对）。

/** BLOCK_SPEC 12 种块的界面规格：名字、能放什么、有哪些属性。 */
export var BLOCK_SPEC = {
    body: {
        label: '正文',
        accepts: ['wrapper', 'section', 'raw'],
        attrs: []
    },
    wrapper: {
        label: '外层容器',
        accepts: ['section', 'raw'],
        attrs: ['background-color', 'padding']
    },
    section: {
        label: '区块',
        accepts: ['column', 'group', 'raw'],
        attrs: ['background-color', 'padding']
    },
    column: {
        label: '列',
        accepts: ['text', 'button', 'image', 'divider', 'spacer', 'social', 'raw'],
        attrs: ['background-color', 'padding', 'width']
    },
    group: {
        label: '组（并排多列）',
        accepts: ['column'],
        attrs: ['background-color', 'padding']
    },
    text: {
        label: '文本',
        accepts: [],
        attrs: ['color', 'font-size', 'line-height', 'text-align', 'padding'],
        hasText: true
    },
    image: {
        label: '图片',
        accepts: [],
        attrs: ['src', 'alt', 'width', 'padding']
    },
    button: {
        label: '按钮',
        accepts: [],
        attrs: ['href', 'padding'],
        hasText: true
    },
    divider: {
        label: '分隔线',
        accepts: [],
        attrs: ['padding']
    },
    spacer: {
        label: '留白',
        accepts: [],
        attrs: ['height']
    },
    social: {
        label: '社交一行',
        accepts: [],
        attrs: ['padding'],
        hasText: true
    },
    raw: {
        label: '原始片段',
        accepts: [],
        attrs: [],
        hasText: true
    }
};

/** BLOCK_ORDER palette 的顺序（与 BLOCK_SPEC 的键序解耦：界面顺序是设计决定）。 */
export var BLOCK_ORDER = ['section', 'column', 'text', 'button', 'image', 'divider', 'spacer', 'group', 'social', 'raw'];

/** createTree 造一棵空树（body 根）。 */
export function createTree() {
    return {
        schema: 1,
        root: { key: 'root', type: 'body', attrs: {}, children: [], text: '' }
    };
}

/** normalizeTree 把服务端返回的树规整成编辑器形状（缺字段补默认）。 */
export function normalizeTree(raw) {
    if (!raw || typeof raw !== 'object' || !raw.root) { return createTree(); }
    return { schema: raw.schema || 1, root: normalizeNode(raw.root) };
}

function normalizeNode(n) {
    return {
        key: String(n.key || ''),
        type: String(n.type || 'text'),
        attrs: shallow(n.attrs),
        text: typeof n.text === 'string' ? n.text : '',
        children: Array.isArray(n.children) ? n.children.map(normalizeNode) : []
    };
}

function shallow(v) {
    var dst = {};
    if (!v || typeof v !== 'object') { return dst; }
    Object.keys(v).forEach(function (k) { dst[k] = String(v[k]); });
    return dst;
}

/** accepts 某个块类型能放哪些子类型（来自 BLOCK_SPEC；未登记的返回空）。 */
export function accepts(type) {
    var spec = BLOCK_SPEC[type];
    return spec && spec.accepts ? spec.accepts : [];
}

/** clone 深拷贝一棵树（撤销栈用）。 */
export function clone(tree) {
    return JSON.parse(JSON.stringify(tree));
}

/** findNode 按 key 找节点（返回 {node, parent, index}；找不到返回 null）。 */
export function findNode(tree, key) {
    var found = null;
    (function walk(node, parent, index) {
        if (found) { return; }
        if (node.key === key) {
            found = { node: node, parent: parent, index: index };
            return;
        }
        for (var i = 0; i < node.children.length; i++) {
            walk(node.children[i], node, i);
            if (found) { return; }
        }
    })(tree.root, null, -1);
    return found;
}

/** nextKey 生成一个稳定 key（块的身份：重排不换）。 */
var seq = 0;
export function nextKey(type) {
    seq++;
    return type + '_' + Date.now().toString(36) + '_' + seq;
}

/** addChild 在 parentKey 下插入一个块（position 省略 = 追加）。 */
export function addChild(tree, parentKey, type, position) {
    var next = clone(tree);
    var hit = findNode(next, parentKey);
    if (!hit) { return tree; }
    if (accepts(hit.node.type).indexOf(type) < 0) { return tree; }
    var node = { key: nextKey(type), type: type, attrs: defaultAttrs(type), text: defaultText(type), children: [] };
    if (typeof position === 'number' && position >= 0 && position <= hit.node.children.length) {
        hit.node.children.splice(position, 0, node);
    } else {
        hit.node.children.push(node);
    }
    return next;
}

/** defaultAttrs 新块的属性初值（够过校验的最小合法值）。 */
function defaultAttrs(type) {
    if (type === 'text') { return { 'font-size': '15px', 'line-height': '1.7' }; }
    if (type === 'section' || type === 'column') { return { padding: '16px' }; }
    if (type === 'button') { return { href: 'https://' }; }
    if (type === 'image') { return { src: '', alt: '', width: '100%' }; }
    if (type === 'spacer') { return { height: '24' }; }
    return {};
}

function defaultText(type) {
    if (type === 'text') { return '在这里写正文，支持 {{.name}} 这样的变量。'; }
    if (type === 'button') { return '点这里'; }
    if (type === 'social') { return '关注我们'; }
    if (type === 'raw') { return ''; }
    return '';
}

/** removeNode 删除一个块（连同它的子树）。根节点不可删。 */
export function removeNode(tree, key) {
    var next = clone(tree);
    var hit = findNode(next, key);
    if (!hit || !hit.parent) { return tree; }
    hit.parent.children.splice(hit.index, 1);
    return next;
}

/** moveNode 在**同一父节点内**上移 / 下移（方向 -1 / +1）。 */
export function moveNode(tree, key, direction) {
    var next = clone(tree);
    var hit = findNode(next, key);
    if (!hit || !hit.parent) { return tree; }
    var target = hit.index + direction;
    if (target < 0 || target >= hit.parent.children.length) { return tree; }
    var list = hit.parent.children;
    var tmp = list[target];
    list[target] = list[hit.index];
    list[hit.index] = tmp;
    return next;
}

/** setAttr 改一个属性（空串 = 用默认，从 attrs 里删掉）。 */
export function setAttr(tree, key, attr, value) {
    var next = clone(tree);
    var hit = findNode(next, key);
    if (!hit) { return tree; }
    if (String(value || '').trim() === '') {
        delete hit.node.attrs[attr];
    } else {
        hit.node.attrs[attr] = String(value);
    }
    return next;
}

/** setText 改块的文本（只有 hasText 的类型用）。 */
export function setText(tree, key, text) {
    var next = clone(tree);
    var hit = findNode(next, key);
    if (!hit) { return tree; }
    hit.node.text = String(text || '');
    return next;
}

/** toJSON 导出提交给服务端的树（只保留契约字段）。 */
export function toJSON(tree) {
    return { schema: 1, root: exportNode(tree.root) };
}

function exportNode(n) {
    var out = { key: n.key, type: n.type };
    if (n.attrs && Object.keys(n.attrs).length) { out.attrs = n.attrs; }
    if (n.text) { out.text = n.text; }
    if (n.children && n.children.length) { out.children = n.children.map(exportNode); }
    return out;
}

/** walk 深度优先遍历（渲染 / 结构树共用）。 */
export function walk(node, fn, depth) {
    fn(node, depth || 0);
    for (var i = 0; i < node.children.length; i++) {
        walk(node.children[i], fn, (depth || 0) + 1);
    }
}
