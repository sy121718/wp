#!/usr/bin/env node
/* 写基座的对象提供器（data-write-builder）顶层键对账。
 *
 * 为什么需要：这些端点提交的是 JS 组装出来的 JSON 对象，静态 HTML 里看不到字段名。
 * 字段门禁只能报「未检查」——这是诚实的，但会长期挂在红灯里。
 *
 * 做法：**真实执行** provider 函数（不是文本解析），喂最小 DOM/FormData stub，
 * 取返回对象的顶层键，与契约 JSON 的 dto 字段表比对。
 *
 * 为什么不猜：
 *   · 每个 provider 的 DOM 依赖不同，stub 必须逐个别写；驱动不起来就报
 *     「不可静态驱动」并退出非零，绝不跳过或推断。
 *   · stub 只覆盖「让 provider 跑起来」所需的最小子集；provider 里任何读不到的值
 *     取默认，键是否存在不因此改变（键来自字面量或固定分支）。
 *
 * 每个 provider 必须绑定到**真实端点**与契约 JSON 里该端点的 DTO 字段表：
 *   · 端点必须在契约里存在、协议为 json、DTO 非空、字段表非空；
 *   · provider 产出的顶层键必须 ⊆ 该 DTO 的字段表；
 *   · 契约缺失 / 空契约 / DTO 无字段一律失败 —— 不能只「调用 provider 成功」就报对账通过。
 *
 * 用法：node scripts/write-object-keys.js <contracts.json> [ledger.json]
 *   退出 0 = 全部驱动成功且键 ⊆ DTO；1 = 有不可驱动、空契约或键不匹配；2 = 参数错误。
 *   给了 ledger 路径时，把「本次真实执行并核对通过」的端点写进台账，
 *   供字段门禁把该端点从「JSON 对象提供器未检查」里摘出来。台账只含实际跑过的端点。
 */
'use strict';

const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const ROOT = path.resolve(__dirname, '..');
const crypto = require('node:crypto');

// hashFile 生成 provider 源码指纹：源码一改，旧台账就不再引用它。
// hashShape 生成字段契约指纹（名字 + required）：required 变化必须让旧台账失效。
function hashShape(fields) {
    const payload = fields.map(f => [f.name, !!f.required]).sort((a, b) => (a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : 0));
    return crypto.createHash('sha256').update(JSON.stringify(payload)).digest('hex').slice(0, 16);
}

// hashCases 生成场景形状指纹（场景名 + 产出的键集合）：台账自身一致性校验。
//
// 注意它只能证明「台账里的 cases 与台账记的指纹一致」，证明不了 cases 就是**当前驱动**产出的。
// 驱动改了（改场景名、预期值、嵌套断言）而台账没重跑时，这条指纹照样自洽 ——
// 因此台账还必须记录 DRIVER_HASH，由字段门禁对照真实驱动脚本文件校验。
function hashCases(cases) {
    const payload = cases.map(c => [c.name || '', [...(c.keys || [])].sort()])
        .sort((a, b) => (JSON.stringify(a) < JSON.stringify(b) ? -1 : 1));
    return crypto.createHash('sha256').update(JSON.stringify(payload)).digest('hex').slice(0, 16);
}

// DRIVER_SOURCE / DRIVER_HASH：本文件的路径与内容指纹。
// 台账登记它之后，任何驱动定义改动（场景名、预期值、嵌套断言、断言逻辑）都会让旧台账
// 与当前驱动对不上，字段门禁据此判「证据过期」，逼着重跑对账而不是拿旧台账继续摘未知项。
const DRIVER_SOURCE = 'scripts/write-object-keys.js';
const DRIVER_HASH = (() => {
    try {
        return crypto.createHash('sha256').update(fs.readFileSync(__filename)).digest('hex').slice(0, 16);
    } catch {
        return '';
    }
})();

function hashFile(rel) {
    try {
        return crypto.createHash('sha256').update(fs.readFileSync(path.join(ROOT, rel))).digest('hex').slice(0, 16);
    } catch {
        return '';
    }
}

// 每个 provider 的最小驱动：fields 是 FormData 的键值，dom 决定 querySelector 返回什么。
// 只列「provider 真的读到的」那些，多写会让 stub 变成假文档。
//
// endpoint 是该 provider 真正提交到的写端点（METHOD PATH），必须与模板的 hx-post
// 一致，并能在契约 JSON 中查到 DTO 字段表；脚本按它校验 provider 产出的键。
// endpoints（复数）用于同一 provider 服务多个端点（新建/更新共用一张抽屉）：
// 每个端点都要有确定的 JSON 契约与字段表，产出键必须落在**其中一个**端点的
// 字段表里。静态上无法判断本次走哪支，因此不做逐端点必填断言。
// 每个 provider 必须覆盖**真实分支**，不能只跑空输入：
//   · 每组 case 描述一个真实提交形态（新建/编辑、空值/实值、无行/有行）；
//   · 同一 provider 的所有 case 产出键合起来必须完整落进同一个端点 DTO；
//   · 某个 case 跑不起来、或键超出 DTO，整个 provider 失败。
//
// endpoint / endpoints 是该 provider 真正提交到的写端点（METHOD PATH），必须与模板
// hx-post 一致，并能在契约 JSON 中查到 DTO 字段表。endpoints（复数）用于同一
// provider 服务新建/更新两支：每个端点都要有确定的 JSON 契约，键必须完整落进
// **其中一支**；静态上无法断言本次走哪支，因此不做逐端点必填断言。
// 每个 provider 必须覆盖**真实分支**，且每个场景显式绑定它真正提交到的 METHOD+PATH：
//   · 场景可以分别落在 create / update 两支，但每个登记端点都必须至少被一个场景覆盖；
//   · 场景产出键必须完整落进它自己声明的端点 DTO，不能「任一 DTO 包含即算覆盖」；
//   · 空输入通过不代表条件字段被驱动：声明有行/有图的场景必须产出对应非空集合与其关键值。
// rowStub 构造一行 DOM 桩：get/set 由各 provider 自己声明要读的 selector，
// 不在通用代码里猜字段名 —— stub 必须映射真实读取路径，否则等于假文档。
function rowStub(cells) {
    const node = (selector) => {
        if (!(selector in cells)) return null;
        const value = cells[selector];
        if (value && typeof value === 'object') return value;
        return { value: String(value ?? ''), checked: value === true, getAttribute: () => '', closest() { return node('[self]'); }, remove() {} };
    };
    return {
        cells,
        self: true,
        querySelector(selector) { return node(selector); },
        querySelectorAll(selector) {
            const value = cells[selector];
            return Array.isArray(value) ? value : [];
        },
        getAttribute(name) { return cells['@' + name] ?? ''; },
        closest() { return this; },
        remove() {},
        parentNode: null,
    };
}

const DRIVERS = {
    'product-edit': {
        file: 'internal/templates/static/js/ui/product-edit.js',
        cases: [
            { name: '空数值与空 SKU（清空语义）', endpoint: 'POST /api/product/update',
              fields: { id: 'prod-1' }, numbers: { defaultPrice: '', weight: '' }, gallery: [] },
            { name: '实值金额/重量/SKU/图片', endpoint: 'POST /api/product/update',
              fields: { id: 'prod-1', sku: 'SKU-1', slug: 'slug-1' },
              numbers: { defaultPrice: '12.50', weight: '0.75' }, gallery: [['/a.png', '主图']],
              expect: { images: 1, imageAlts: 1 } },
            { name: '数值零（不清空）', endpoint: 'POST /api/product/update',
              fields: { id: 'prod-1' }, numbers: { defaultPrice: '0', weight: '0' } },
        ],
    },
    'product-tag': {
        file: 'internal/templates/static/js/ui/product-edit.js',
        cases: [
            { name: '新建手工标签', endpoint: 'POST /api/product/tag/create', fields: { name: '热门', kind: 'manual' } },
            { name: '编辑规则标签带参数', endpoint: 'POST /api/product/tag/update',
              fields: { id: 'tag-1', name: '高价', kind: 'rule', ruleType: 'price', minPrice: '100' } },
        ],
    },
    'product-category': {
        file: 'internal/templates/static/js/ui/product-edit.js',
        cases: [
            { name: '新建（无 id）', endpoint: 'POST /api/product/category/create', fields: { name: '分类', parentId: '' } },
            { name: '编辑（有 id）', endpoint: 'POST /api/product/category/update', fields: { id: 'cat-1', name: '分类2', parentId: 'cat-0' } },
        ],
    },
    'product-bulk-pricing': {
        file: 'internal/templates/static/js/ui/product-edit.js',
        cases: [
            { name: '无勾选商品', endpoint: 'POST /api/product/bulk-pricing', fields: { note: '整批' }, bulkIDs: [] },
            { name: '有勾选商品与全部参数', endpoint: 'POST /api/product/bulk-pricing',
              fields: { note: '整批', ruleType: 'percent', multiplier: '0.9' }, bulkIDs: ['p1', 'p2'],
              expect: { ids: 2 } },
        ],
    },
    'inventory-transfer': {
        file: 'internal/templates/static/js/ui/inventory_transfer.js',
        cases: [
            { name: '无明细行', endpoint: 'POST /api/inventory/transfer/create', fields: { fromWarehouseId: 'w1', toWarehouseId: 'w2' } },
            { name: '有明细行', endpoint: 'POST /api/inventory/transfer/create',
              fields: { fromWarehouseId: 'w1', toWarehouseId: 'w2', remark: '调拨' },
              rowSets: { '.transfer-line': [rowStub({ '[data-line-stock]': 'sku-1', '[data-line-qty]': '3' })] },
              expect: { lines: 1 },
              rowFields: { lines: { stockId: 'sku-1', quantity: 3 } } },
        ],
    },
    'navigation-translations': {
        file: 'internal/templates/static/js/ui/navigation_translations.js',
        cases: [
            { name: '无译文行', endpoint: 'POST /api/navigation/translations', fields: { action: 'translations', lang: 'en-US', project: 'p1' } },
            { name: '有译文行', endpoint: 'POST /api/navigation/translations',
              fields: { action: 'translations', lang: 'en-US', project: 'p1' },
              rowSets: { '.nav-tr-row': [rowStub({
                  'input[name]': [
                      { value: 'navigation.label', getAttribute: () => 'context' },
                      { value: 'h-1', getAttribute: () => 'hash' },
                      { value: '首页', getAttribute: () => 'target' },
                  ] })] },
              expect: { rows: 1 },
              rowFields: { rows: { context: 'navigation.label', hash: 'h-1', target: '首页' } } },
        ],
    },
    'ai-provider-models': {
        file: 'pkg/view/ui/assets/js/ui/ai-models.js',
        cases: [
            { name: '无模型', endpoint: 'POST /api/ai/provider/models/save', fields: { providerId: 'p1', version: '1' } },
            { name: '有模型', endpoint: 'POST /api/ai/provider/models/save',
              fields: { providerId: 'p1', version: '1' },
              rowSets: { ':scope > .ai-model-row': [{ cells: { '[name="modelId"]': 'gpt-x', '[name="modelDisplayName"]': 'X', '[name="modelContextWindow"]': '8000', '[name="modelMaxOutputTokens"]': '1000' }, querySelector(sel) { const c = this.cells[sel]; return c === undefined ? null : { value: String(c), checked: false }; }, querySelectorAll() { return []; } }] },
              rowsHost: true,
              expect: { models: 1 } },
        ],
    },
    'product-attribute-values': {
        file: 'pkg/view/ui/assets/js/ui/product-attribute-values.js',
        cases: [
            { name: '删光值行', endpoint: 'POST /api/product/attribute/set-values', fields: { projectId: 'p1', id: 'attr-1' }, rows: 0, expect: { values: 0 } },
            { name: '有值行', endpoint: 'POST /api/product/attribute/set-values',
              fields: { projectId: 'p1', id: 'attr-1' },
              rowSets: { '[data-attribute-value-row]': [rowStub({ '[data-attribute-field]': [
                  { value: 'red', getAttribute: () => 'key' },
                  { value: '红', getAttribute: () => 'label' },
                  { value: '1', getAttribute: () => 'sort' },
                  { value: '1', checked: true, getAttribute: () => 'enabled' },
              ] })] },
              expect: { values: 1 } },
        ],
    },
    'product-attribute-group': {
        file: 'pkg/view/ui/assets/js/ui/product-attribute-values.js',
        cases: [
            // 编辑必须带 id：更新 DTO 的 id 是必填标识，缺了会写到错误的记录上。
            { name: '编辑（有 id、无值行）', endpoint: 'POST /api/product/attribute/update',
              fields: { projectId: 'p1', id: 'attr-1', name: '颜色', sort: '1' }, rows: 0,
              expect: { id: 'attr-1' } },
            { name: '新建（无 id、有值行）', endpoint: 'POST /api/product/attribute/create',
              fields: { projectId: 'p1', name: '尺寸', sort: '2' },
              rowSets: { '[data-attribute-value-row]': [rowStub({ '[data-attribute-field]': [
                  { value: '', getAttribute: () => 'key' },
                  { value: '大', getAttribute: () => 'label' },
                  { value: '0', getAttribute: () => 'sort' },
                  { value: '1', checked: true, getAttribute: () => 'enabled' },
              ] })] },
              expect: { values: 1 } },
        ],
    },
    // 这个 provider 的签名是 (form, submitter, payload)：它往传入的 payload 上补 rows，
    // 而不是自己 new 一个对象。宿主字段（projectId/productId）由写基座预置。
    // 工作台三块业务面板：provider 只组装「整份编辑器文档 / 主题对象」，
    // 发送与交换在写基座。字段表来自面板 DTO（document / accessPassword / url /
    // themeId / settings），产出键必须完整落进去。
    'workbench-settings': {
        file: 'internal/templates/static/js/editor-app/panel-requests.js',
        cases: [
            // 同一端点两种真实提交形态：普通加载（无密码框）与「设置访问密码」
            // （密码表单自己的隐藏域）。密码明文不进文档，只出现在请求对象里。
            { name: '页面设置面板（整份文档）', endpoint: 'POST /workbench/settings' },
            { name: '设置访问密码（隐藏域有值）', endpoint: 'POST /workbench/settings',
              fields: { 'wb-access-password': 'p@ssw0rd' }, expect: { accessPassword: 'p@ssw0rd' } },
        ],
    },
    'workbench-seo': {
        file: 'internal/templates/static/js/editor-app/panel-requests.js',
        cases: [
            { name: '评分面板（文档 + 草稿地址）', endpoint: 'POST /workbench/seo-score-panel' },
        ],
    },
    'workbench-global': {
        file: 'internal/templates/static/js/editor-app/panel-requests.js',
        cases: [
            { name: '全局主题面板（主题设置对象）', endpoint: 'POST /workbench/global' },
        ],
    },
    'product-variant-rows': {
        file: 'internal/templates/static/js/ui/product_variant_write.js',
        cases: [
            { name: '空行清单（保留宿主字段）', endpoint: 'POST /api/product/variant/save',
              fields: { projectId: 'p1', productId: 'prod-1' }, seedPayload: true,
              expect: { projectId: 'p1', productId: 'prod-1' } },
            // 真实非空行：一行变体带两个字段与一条仓库明细。
            // 只跑空 rows 证明不了 variantRows/stocksOf 真的从 DOM 读到内容。
            { name: '真实变体行与库存明细', endpoint: 'POST /api/product/variant/save',
              fields: { projectId: 'p1', productId: 'prod-1' }, seedPayload: true,
              variantRows: [{ id: 'v-1', sku: 'SKU-1', optionValues: '{"颜色":"红"}',
                              stocks: [{ stockId: 'st-1', warehouseId: 'w1', skuCode: 'SKU-1', quantity: '7' }] }],
              expect: { rows: 1 },
              rowFields: { rows: { skuCode: 'SKU-1', variantId: 'v-1' } },
              nestedRows: { rows: [{ stocks: [{ stockId: 'st-1', quantity: 7, warehouseId: 'w1', skuCode: 'SKU-1' }] }] } },
        ],
    },
};

function makeForm(scenario) {
    const fields = scenario.fields || {};
    const values = Object.fromEntries(Object.entries(fields).map(([k, v]) => [k, [String(v)]]));
    class FormDataStub {
        get(name) { return values[name] ? values[name][0] : null; }
        getAll(name) { return values[name] || []; }
        *[Symbol.iterator]() { for (const [k, v] of Object.entries(values)) yield [k, v[0]]; }
    }
    const numbers = scenario.numbers || {};
    const control = name => {
        const raw = numbers[name];
        const value = raw === undefined ? '' : String(raw);
        return { value, valueAsNumber: value === '' ? NaN : Number(value), validationMessage: '数值无效', disabled: false };
    };
    // 行集合：rows>0 时返回占位行，行对象读取值时给出可判定的默认。
    const rows = scenario.rows || 0;
    const gallery = (scenario.gallery || []).map(([url, alt]) => ({
        getAttribute(name) { return name === 'data-gallery-url' ? url : ''; },
        querySelector(name) { return name === '[data-gallery-alt]' ? { value: alt } : null; },
    }));
    const makeRow = () => ({
        querySelectorAll() { return []; },
        querySelector() { return { value: '', checked: false }; },
        getAttribute() { return ''; },
        closest() { return this; },
        remove() {},
    });
    const rowSet = Array.from({ length: rows }, makeRow);
    // rowSets：selector → 该选择器应返回的行。只有显式声明的选择器才返回行，
    // 其余仍返回空，避免 stub 凭空制造 DOM。
    const rowSets = scenario.rowSets || {};
    if (gallery.length) rowSets['[data-media-gallery] [data-gallery-url]'] = gallery;
    // variantRows 把「变体行 + 后面那条库存明细行」按真实 DOM 结构桩出来：
    // 变体行 → nextElementSibling（data-variant-stocks-row）→ 面板（data-stocks-known=1）
    // → data-stock-row（data-stock-id / data-warehouse-id / data-sku-code + 数量框）。
    // 不这样映射读取路径，空 rows 场景永远无法证明 variantRows/stocksOf 读过行内容。
    const variantRows = scenario.variantRows || [];
    const variantRowEls = variantRows.map(spec => {
        const stocksRow = {
            hasAttribute(name) { return name === 'data-variant-stocks-row'; },
            querySelector(sel) {
                if (sel !== '[data-stock-panel]') return null;
                return {
                    getAttribute(name) {
                        return name === 'data-stocks-known' ? '1' : '';
                    },
                    querySelectorAll(sel) {
                        if (sel !== '[data-stock-row]') return [];
                        return (spec.stocks || []).map(stock => ({
                            getAttribute(name) {
                                return ({
                                    'data-stock-id': stock.stockId || '',
                                    'data-warehouse-id': stock.warehouseId || '',
                                    'data-sku-code': stock.skuCode || '',
                                })[name] ?? '';
                            },
                            querySelector(sel) {
                                return sel === '[data-stock-qty]' ? { value: String(stock.quantity ?? '') } : null;
                            },
                        }));
                    },
                };
            },
        };
        return {
            __variant: spec,
            getAttribute(name) {
                if (name === 'data-variant-id') return spec.id || '';
                if (name === 'data-option-values') return spec.optionValues || '{}';
                return '';
            },
            querySelector(sel) {
                return sel === '[data-variant-sku]' ? { value: spec.sku || '' } : null;
            },
            nextElementSibling: stocksRow,
        };
    });
    if (variantRowEls.length) rowSets['[data-variant-row]'] = variantRowEls;
    const rowsFor = selector => rowSets[selector] ?? rowSet;
    // rowsHost 是 [data-models-rows] 这类「行容器」桩：它自己带 querySelectorAll，
    // 按 ':scope > .row' 取行 —— 直接把场景声明的行挂上去。
    const rowsHost = scenario.rowsHost
        ? { querySelectorAll(sel) { return rowSets[sel] ?? []; }, querySelector() { return null; } }
        : null;
    // bulkIDs 走 document.querySelector('[data-list-bulk]') 的真实读取路径：
    // provider 在整页范围找列表容器，再取其下勾选的 ids。
    const bulkIDs = scenario.bulkIDs || [];
    const bulkScope = bulkIDs.length
        ? { querySelectorAll(sel) { return sel === 'input[name="ids"]:checked' ? bulkIDs.map(id => ({ value: id })) : []; } }
        : null;

    const form = {
        elements: { namedItem: control },
        querySelectorAll(selector) { return rowsFor(selector); },
        querySelector(selector) {
            if (selector === '[data-models-rows]') return rowsHost;
            return null;
        },
        matches() { return false; },
        closest() { return null; },
        getAttribute() { return null; },
    };
    form.__bulkScope = bulkScope;
    // document 级选择器：provider 里有些读取走整页作用域
    // （如变体清单 variantRowEls 用 document.querySelectorAll('[data-variant-row]')）。
    // 场景显式声明过的选择器才返回行，其余仍为空，避免凭空造 DOM。
    const documentQueryAll = selector => rowSets[selector] ?? [];
    return { form, FormDataStub, documentQueryAll };
}

// runCase 在独立 vm 里执行 provider，返回该场景的产出键。
function runCase(name, spec, scenario) {
    const abs = path.join(ROOT, spec.file);
    if (!fs.existsSync(abs)) return { error: '源文件不存在：' + spec.file };

    let builder = null;
    const { form, FormDataStub, documentQueryAll } = makeForm(scenario);
    const sandbox = {
        window: {
            WBUI: {
                writeBase: {
                    registerPayloadBuilder(n, fn) { if (n === name) builder = fn; },
                },
            },
            addEventListener() {},
            matchMedia() { return { matches: false, addEventListener() {} }; },
        },
        document: {
            querySelector(sel) {
                // bulk-pricing 从列表容器读勾选商品：真实路径是 [data-list-bulk]。
                if (sel === '[data-list-bulk]' && form.__bulkScope) return form.__bulkScope;
                return null;
            },
            querySelectorAll(sel) { return documentQueryAll(sel); },
            getElementById() { return null; },
            createElement() { return { style: {}, appendChild() {}, setAttribute() {}, addEventListener() {}, querySelector() { return null; }, querySelectorAll() { return []; } }; },
            addEventListener() {},
        },
        FormData: FormDataStub,
        console,
    };
    sandbox.window.document = sandbox.document;
    sandbox.global = sandbox.window;
    sandbox.globalThis = sandbox;
    try {
        vm.runInNewContext(fs.readFileSync(abs, 'utf8'), sandbox, { filename: spec.file });
    } catch (e) {
        return { error: '执行源文件失败：' + e.message };
    }
    if (typeof builder !== 'function') return { error: '未注册 provider：' + name };
    let out;
    try {
        // seedPayload：provider 往写基座预置的 payload 上补字段，宿主字段就在这个对象里。
        out = scenario.seedPayload ? builder(form, null, Object.assign({}, scenario.fields || {})) : builder(form);
    } catch (e) {
        return { error: '调用 provider 失败：' + e.message };
    }
    if (!out || typeof out !== 'object' || Array.isArray(out)) {
        return { error: 'provider 未返回对象' };
    }
    return { keys: Object.keys(out), object: out };
}

function main() {
    const contractsPath = process.argv[2];
    if (!contractsPath) {
        console.error('用法：node scripts/write-object-keys.js <contracts.json>');
        process.exit(2);
    }
    const ledgerPath = process.argv[3];
    const contracts = JSON.parse(fs.readFileSync(path.resolve(contractsPath), 'utf8'));
    if (!contracts || typeof contracts !== 'object' || Object.keys(contracts).length === 0) {
        console.error('对象提供器对账失败：写端点契约为空，不能只靠「provider 能跑」报通过。');
        process.exit(1);
    }

    let failed = 0;
    const verified = {};
    for (const [name, spec] of Object.entries(DRIVERS)) {
        const cases = spec.cases && spec.cases.length ? spec.cases : [];
        if (cases.length === 0) {
            console.error(`未检查 provider ${name}：没有声明任何场景`);
            failed++;
            continue;
        }
        // 第一步：逐场景校验它自己声明的端点契约（每场景显式绑定 METHOD+PATH）。
        const shapes = new Map(); // endpoint -> {dto, allowed}
        let shapeFailed = false;
        for (const scenario of cases) {
            const endpoint = scenario.endpoint;
            if (!endpoint) {
                console.error(`未检查 provider ${name} 的场景「${scenario.name}」：没有声明目标端点`);
                shapeFailed = true;
                continue;
            }
            const contract = contracts[endpoint];
            if (!contract) {
                console.error(`未检查 provider ${name} 的场景「${scenario.name}」：端点 ${endpoint} 不在写端点契约里`);
                shapeFailed = true;
                continue;
            }
            if (contract.protocol !== 'json' || contract.reason) {
                console.error(`未检查 provider ${name} 的场景「${scenario.name}」：端点 ${endpoint} 不是确定的 JSON` +
                    `（protocol=${contract.protocol} reason=${contract.reason || ''}）`);
                shapeFailed = true;
                continue;
            }
            const allowed = (contract.fields || []).map(f => f.name);
            if (!contract.dto || allowed.length === 0) {
                console.error(`未检查 provider ${name} 的场景「${scenario.name}」：端点 ${endpoint} 的 DTO/字段表为空`);
                shapeFailed = true;
                continue;
            }
            shapes.set(endpoint, { dto: contract.dto, allowed });
        }
        if (shapeFailed) {
            failed++;
            continue;
        }

        // 第二步：真实执行每个场景。
        const coverage = new Map();
        let caseFailed = false;
        for (const scenario of cases) {
            const r = runCase(name, spec, scenario);
            if (r.error) {
                console.error(`未检查 provider ${name} 的场景「${scenario.name}」：${r.error}`);
                caseFailed = true;
                continue;
            }
            const shape = shapes.get(scenario.endpoint);
            // 场景的键必须完整落进**它自己声明的端点**，不接受「任一 DTO 包含即算覆盖」： 否则 create 样本可伪装 update 覆盖。
            const extra = r.keys.filter(k => !shape.allowed.includes(k));
            if (extra.length > 0) {
                console.error(`provider ${name} 的场景「${scenario.name}」在 ${scenario.endpoint} 产出了 DTO 没有的键：${extra.join(', ')}`);
                caseFailed = true;
                continue;
            }
            // 标识语义：更新端点（契约含必填 id 的 JSON 写入口）必须带非空 id；
            // 缺 id 会按零值写到错误记录上，不能算这一支已覆盖。
            const updateEndpoint = /\/update$|\/save$/.test(scenario.endpoint) &&
                (contracts[scenario.endpoint].fields || []).some(f => f.name === 'id' && f.required);
            if (updateEndpoint && !String(r.object.id || '').trim()) {
                console.error(`provider ${name} 的场景「${scenario.name}」是更新端点 ${scenario.endpoint}，却没有产出 id`);
                caseFailed = true;
                continue;
            }
            if (scenario.expect && scenario.expect.id !== undefined && r.object.id !== scenario.expect.id) {
                console.error(`provider ${name} 的场景「${scenario.name}」id 期望 ${scenario.expect.id}，实际 ${r.object.id}`);
                caseFailed = true;
                continue;
            }
            // 必填字段：场景声明的端点契约必填项必须真的出现在产出对象里。
            // 只校验「键 ⊆ DTO」不等于这条分支已覆盖：缺必填字段的提交在真实端点会被拒。
            const requiredFields = (contracts[scenario.endpoint].fields || [])
                .filter(f => f.required).map(f => f.name);
            const missingRequired = requiredFields.filter(f => !(f in r.object) ||
                r.object[f] === '' || r.object[f] === null);
            if (missingRequired.length > 0) {
                console.error(`provider ${name} 的场景「${scenario.name}」在 ${scenario.endpoint} 缺必填字段：` +
                    missingRequired.join(', '));
                caseFailed = true;
                continue;
            }
            // 集合语义：声明了的集合必须精确非空（数量/内容），不能拿别的数组抵账。
            for (const [field, wantLen] of Object.entries(scenario.expect || {})) {
                if (field === 'id') continue;
                if (!Array.isArray(r.object[field])) {
                    if (r.object[field] !== wantLen) {
                        console.error(`provider ${name} 的场景「${scenario.name}」字段 ${field} 期望 ${wantLen}，实际 ${JSON.stringify(r.object[field])}`);
                        caseFailed = true;
                    }
                    continue;
                }
                if (r.object[field].length !== wantLen) {
                    console.error(`provider ${name} 的场景「${scenario.name}」集合 ${field} 期望 ${wantLen} 项，实际 ${r.object[field].length}`);
                    caseFailed = true;
                }
            }
            // 行桩/图片场景：必须真的读到行，不能空样本冒充。
            const declaredRows = (scenario.rows || 0) > 0 || (scenario.gallery || []).length > 0 ||
                Object.keys(scenario.rowSets || {}).length > 0 || scenario.rowsHost;
            if (declaredRows) {
                const nonEmpty = Object.values(r.object).some(v => Array.isArray(v) && v.length > 0);
                if (!nonEmpty) {
                    console.error(`provider ${name} 的场景「${scenario.name}」声明了行/图，产出对象却没有任何非空集合 —— ` +
                        '驱动没让条件分支真正读到行');
                    caseFailed = true;
                    continue;
                }
            }
            // 嵌套行集合：变体行的 stocks 这类「行里的行」必须逐层断言。
            // 只断言顶层 rows 长度，删掉 stocks 或把数量改成 0 都能通过。
            for (const [field, wantRows] of Object.entries(scenario.nestedRows || {})) {
                const rows = r.object[field];
                if (!Array.isArray(rows) || rows.length === 0) {
                    console.error(`provider ${name} 的场景「${scenario.name}」行集合 ${field} 为空，无法校验嵌套行`);
                    caseFailed = true;
                    continue;
                }
                for (let idx = 0; idx < wantRows.length && idx < rows.length; idx++) {
                    const want = wantRows[idx];
                    for (const [key, value] of Object.entries(want)) {
                        if (Array.isArray(value)) {
                            const nested = rows[idx][key];
                            if (!Array.isArray(nested) || nested.length !== value.length) {
                                console.error(`provider ${name} 的场景「${scenario.name}」${field}[${idx}].${key} ` +
                                    `期望 ${value.length} 项，实际 ${Array.isArray(nested) ? nested.length : JSON.stringify(nested)}`);
                                caseFailed = true;
                                continue;
                            }
                            for (let n = 0; n < value.length; n++) {
                                for (const [nk, nv] of Object.entries(value[n])) {
                                    if (nested[n][nk] !== nv) {
                                        console.error(`provider ${name} 的场景「${scenario.name}」` +
                                            `${field}[${idx}].${key}[${n}].${nk} 期望 ${JSON.stringify(nv)}，实际 ${JSON.stringify(nested[n][nk])}`);
                                        caseFailed = true;
                                    }
                                }
                            }
                        } else if (rows[idx][key] !== value) {
                            console.error(`provider ${name} 的场景「${scenario.name}」` +
                                `${field}[${idx}].${key} 期望 ${JSON.stringify(value)}，实际 ${JSON.stringify(rows[idx][key])}`);
                            caseFailed = true;
                        }
                    }
                }
            }
            // 行内容语义：声明 rowFields 的场景必须逐行断言关键值。
            // 「数组非空」证明不了行内容正确：把 stockId 写成别的字段名，数组长度不变。
            for (const [field, expectedKeys] of Object.entries(scenario.rowFields || {})) {
                const rows = r.object[field];
                if (!Array.isArray(rows) || rows.length === 0) {
                    console.error(`provider ${name} 的场景「${scenario.name}」行集合 ${field} 为空，无法校验行内容`);
                    caseFailed = true;
                    continue;
                }
                for (const [key, want] of Object.entries(expectedKeys)) {
                    if (!(key in rows[0])) {
                        console.error(`provider ${name} 的场景「${scenario.name}」行 ${field}[0] 缺字段 ${key} ` +
                            `（实际键：${Object.keys(rows[0]).join(', ') || '空'}）`);
                        caseFailed = true;
                        continue;
                    }
                    if (rows[0][key] !== want) {
                        console.error(`provider ${name} 的场景「${scenario.name}」行 ${field}[0].${key} 期望 ${JSON.stringify(want)}，` +
                            `实际 ${JSON.stringify(rows[0][key])}`);
                        caseFailed = true;
                    }
                }
            }
            if (!coverage.has(scenario.endpoint)) coverage.set(scenario.endpoint, []);
            coverage.get(scenario.endpoint).push({ scenario, keys: r.keys, object: r.object });
        }
        if (caseFailed) {
            failed++;
            continue;
        }
        // 每个登记端点都必须至少被一个场景覆盖。
        const uncovered = [...shapes.keys()].filter(ep => !coverage.has(ep));
        if (uncovered.length > 0) {
            console.error(`provider ${name} 声明了端点但没有任何场景覆盖：${uncovered.join(' / ')}`);
            failed++;
            continue;
        }

        for (const [endpoint, covered] of coverage) {
            const shape = shapes.get(endpoint);
            // 台账必须带齐三类证据：字段契约（名字 + required）、场景形状（名字 + 键）、
            // provider 源码指纹。任何一类变了，旧台账都不再能证明该端点已核对。
            const cases = covered.map(c => ({ name: c.scenario.name, keys: c.keys, endpoint }));
            verified[endpoint] = {
                dto: shape.dto,
                provider: name,
                source: spec.file,
                sourceHash: hashFile(spec.file),
                fields: shape.allowed,
                fieldsHash: hashShape(contracts[endpoint].fields || []),
                cases,
                caseShape: hashCases(cases),
                // 驱动定义指纹：驱动改了而台账没重跑时，字段门禁必须判旧证据过期。
                driver: DRIVER_SOURCE,
                driverHash: DRIVER_HASH,
                keys: [...new Set(covered.flatMap(c => c.keys))].sort(),
            };
            console.log(`provider ${name} → ${endpoint} (${shape.dto})：` +
                `${covered.map(c => c.keys.length).join('/')} 键 × ${covered.length} 场景`);
        }
    }
    if (failed) {
        console.error(`对象提供器对账未通过：${failed} 个 provider 不可驱动、契约不完整或键不匹配。`);
        process.exit(1);
    }
    if (ledgerPath) {
        fs.writeFileSync(path.resolve(ledgerPath), JSON.stringify(verified, null, 2) + '\n');
    }
    console.log(`对象提供器对账通过：${Object.keys(DRIVERS).length} 个 provider → 真实端点 DTO 字段表。`);
}

main();
