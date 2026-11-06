// 独立规格展示器的选品入口；加购按钮不使用作者选品。
import { meta } from '../../../editor/core.js';
import { get, set, commit } from '../../../editor/methods/controls/base.js';

// 商品选品属于应用业务扩展面板，由宿主注入检查器 extras 调用。
export function productBindingPanel(ctx) {
    var root = document.createElement('div');
    root.className = 'wb-product-binding';
    ctx.panel.prepend(root);
    var mode = get(ctx, 'props.productSource') || 'current';
    var title = document.createElement('label'); title.textContent = '商品来源';
    var segment = document.createElement('div'); segment.className = 'wb-seg';
    [['current', '当前商品'], ['specific', '指定商品']].forEach(function (choice) {
        var button = document.createElement('button'); button.type = 'button';
        button.className = 'wb-seg-btn'; button.textContent = choice[1];
        button.classList.toggle('is-active', mode === choice[0]);
        button.setAttribute('aria-pressed', String(mode === choice[0]));
        button.addEventListener('click', function () {
            if (choice[0] === mode) return;
            commit(ctx, 'props.productSource', choice[0], function () {
                set(ctx, 'props.productId', ''); set(ctx, 'props.variantId', '');
            });
            ctx.self.syncInspector();
        });
        segment.appendChild(button);
    });
    var modeField = document.createElement('div'); modeField.className = 'wb-field';
    modeField.append(title, segment); root.appendChild(modeField);
    var hint = document.createElement('p'); hint.className = 'wb-field-hint';
    hint.textContent = mode === 'current' ? '使用商品详情页或商品卡片里的当前商品。普通页面请选择“指定商品”。' : '搜索已发布的商品，再选择需要的规格。';
    root.appendChild(hint);
    if (mode !== 'specific') return;

    var searchRow = document.createElement('div'); searchRow.className = 'wb-inline-btns';
    var search = document.createElement('input'); search.type = 'search'; search.placeholder = '搜索商品名称';
    search.setAttribute('aria-label', '搜索商品');
    var searchButton = document.createElement('button'); searchButton.type = 'button';
    searchButton.className = 'wb-btn wb-btn-secondary wb-btn-sm'; searchButton.textContent = '搜索';
    searchRow.append(search, searchButton); root.appendChild(searchRow);
    var product = selectField('已发布商品');
    var status = document.createElement('p'); status.className = 'wb-field-hint'; status.setAttribute('role', 'status');
    root.appendChild(status);
    var searchSeq = 0, optionSeq = 0, timer;
    var configured = get(ctx, 'props.productId') || '';
    var selectedName = configured ? '已选商品（正在读取）' : '';
    replaceProducts([]);

    function active() {
        return root.isConnected && (!ctx.self.findNode || ctx.self.findNode(ctx.node.id) === ctx.node);
    }
    function selectField(label) {
        var wrap = document.createElement('div'); wrap.className = 'wb-field';
        var caption = document.createElement('label'); caption.textContent = label;
        var control = document.createElement('select'); control.setAttribute('aria-label', label);
        wrap.append(caption, control); root.appendChild(wrap); return control;
    }
    function option(control, value, name) {
        var row = document.createElement('option'); row.value = value; row.textContent = name; control.appendChild(row);
    }
    function replaceProducts(items) {
        product.replaceChildren(); option(product, '', '请选择商品');
        if (configured && !items.some(function (item) { return item.id === configured; })) option(product, configured, selectedName || '已选商品');
        items.forEach(function (item) { option(product, item.id, item.name); });
        product.value = configured;
    }
    async function read(path, params) {
        var query = new URLSearchParams(Object.assign({ projectId: meta.projectId || '', lang: meta.lang || '' }, params));
        var response = await fetch(path + '?' + query.toString(), { credentials: 'same-origin', headers: { Accept: 'application/json' } });
        var body = await response.json();
        if (!response.ok || body.code !== 200) throw new Error(body.message || '读取失败，请重试');
        return body.data;
    }
    async function searchProducts() {
        var seq = ++searchSeq; status.textContent = '正在搜索商品…';
        try {
            var data = await read('/api/site/product/catalog', { q: search.value.trim(), page: '1', pageSize: '20' });
            if (!active() || seq !== searchSeq) return;
            var items = data.items || []; replaceProducts(items);
            status.textContent = items.length ? '选择商品后可继续配置规格。' : '没有找到已发布商品，请换个关键词。';
        } catch (err) {
            if (active() && seq === searchSeq) status.textContent = err.message;
        }
    }
    async function loadOptions(id) {
        var seq = ++optionSeq;
        if (!id) return;
        status.textContent = '正在读取商品规格…';
        try {
            var data = await read('/api/site/product/options', { productId: id });
            if (!active() || seq !== optionSeq || configured !== id) return;
            selectedName = data.name;
            Array.from(product.options).forEach(function (item) { if (item.value === id) item.textContent = data.name; });
            status.textContent = (data.variants || []).length ? '' : '此商品没有可用规格，请选择其他商品。';
        } catch (err) {
            if (active() && seq === optionSeq && configured === id) status.textContent = err.message + '，原选择已保留。';
        }
    }
    product.addEventListener('change', function () {
        configured = product.value;
        commit(ctx, 'props.productId', configured, function () { set(ctx, 'props.variantId', ''); });
        loadOptions(configured);
    });
    searchButton.addEventListener('click', searchProducts);
    search.addEventListener('keydown', function (event) { if (event.key === 'Enter') { event.preventDefault(); clearTimeout(timer); searchProducts(); } });
    search.addEventListener('input', function () { clearTimeout(timer); timer = setTimeout(function () { if (active()) searchProducts(); }, 300); });
    searchProducts();
    if (configured) loadOptions(configured);
}
