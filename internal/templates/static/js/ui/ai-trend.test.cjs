/* ai-trend.test.cjs — 趋势图几何（node:test + vm，无浏览器）。
 *
 * 守的是这几处：
 *
 *   1. 逐日数组没有换成坐标 —— 服务端已不再给 Points，画不出来就是一条空图。
 *   2. 峰值归一算错 —— 高位被裁到画布外，或低位全贴顶，看不出趋势。
 *   3. 单点窗口画成折线 —— 一个点连不成线，polyline 是空元素。
 *   4. 空数组被当成「一天 0 用量」—— 画出基线上一条平线，看起来像有数据。
 *   5. 重复渲染叠出多条线。
 *
 * 夹具与服务端 TrendSeries 的 json 形状一致：values 是逐日 token 数。
 *
 * 运行：node --test internal/templates/static/js/ui/ai-trend.test.cjs
 */
const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
// 公共控件属主在 pkg/view/ui；本测试读取真实源，不复制到旧位置。
const UI_SRC = path.join(__dirname, '../../../../..', 'pkg/view/ui/assets/js/ui');

const SRC = path.join(UI_SRC, 'ai-trend.js');

function makeNode(tag, ns) {
    const el = {
        tagName: String(tag || 'div').toUpperCase(),
        namespaceURI: ns || '',
        children: [],
        attrs: {},
        parentNode: null,
        appendChild(c) { this.children.push(c); c.parentNode = this; return c; },
        removeChild(c) {
            const i = this.children.indexOf(c);
            if (i >= 0) { this.children.splice(i, 1); c.parentNode = null; }
            return c;
        },
        setAttribute(k, v) { this.attrs[k] = String(v); },
        getAttribute(k) { return Object.prototype.hasOwnProperty.call(this.attrs, k) ? this.attrs[k] : null; },
        hasAttribute(k) { return Object.prototype.hasOwnProperty.call(this.attrs, k); },
        querySelector(sel) { return this._find(sel)[0] || null; },
        querySelectorAll(sel) { return this._find(sel); },
        closest(sel) {
            let n = this;
            while (n) {
                if (matches(n, sel)) { return n; }
                n = n.parentNode;
            }
            return null;
        },
        _find(sel) {
            const group = String(sel).split(',').map((x) => x.trim()).filter(Boolean);
            const out = [];
            const walk = (n) => {
                (n.children || []).forEach((c) => {
                    if (group.some((g) => matches(c, g))) { out.push(c); }
                    walk(c);
                });
            };
            walk(this);
            return out;
        },
        get firstChild() { return this.children[0] || null; }
    };
    return el;
}

function matches(node, sel) {
    let s = String(sel).trim();
    if (s.startsWith('.')) {
        const cls = s.slice(1);
        return String(node.getAttribute('class') || '').split(/\s+/).includes(cls);
    }
    const tagPart = /^([a-zA-Z][a-zA-Z0-9]*)/.exec(s);
    if (tagPart) {
        if (String(node.tagName).toLowerCase() !== tagPart[1].toLowerCase()) { return false; }
        s = s.slice(tagPart[1].length);
    }
    const attrs = s.match(/\[[^\]]+\]/g) || [];
    return attrs.every((a) => {
        const body = a.slice(1, -1);
        const eq = body.indexOf('=');
        if (eq < 0) { return node.hasAttribute(body); }
        return node.getAttribute(body.slice(0, eq)) === body.slice(eq + 1).replace(/^["']|["']$/g, '');
    });
}

function makeEnv(series, peak) {
    const wrap = makeNode('div');
    wrap.setAttribute('class', 'chart-trend');

    const svg = makeNode('svg', 'http://www.w3.org/2000/svg');
    svg.setAttribute('class', 'chart-trend-svg');
    svg.setAttribute('data-trend-svg', '');
    svg.setAttribute('data-trend-peak', String(peak));
    wrap.appendChild(svg);

    const legend = makeNode('ul');
    legend.setAttribute('data-trend-legend', '');
    series.forEach((vals, i) => {
        const li = makeNode('li');
        li.setAttribute('data-trend-series', String(i));
        li.setAttribute('data-values', vals.join(' '));
        legend.appendChild(li);
    });
    wrap.appendChild(legend);

    const doc = {
        createElementNS: (ns, name) => makeNode(name, ns),
        querySelector: (sel) => wrap._find(sel)[0] || null,
        querySelectorAll: (sel) => wrap._find(sel)
    };
    const win = { document: doc, WBUI: {} };
    win.window = win;
    const ctx = vm.createContext(win);
    vm.runInContext(fs.readFileSync(SRC, 'utf8'), ctx, { filename: SRC });
    return { wrap, svg, legend, trend: win.WBUI.aiTrend };
}

test('逐日数组换成等距坐标，纵轴按峰值归一', () => {
    const env = makeEnv([[0, 50, 100]], 100);
    assert.equal(env.trend.render(env.svg), true);

    const lines = env.svg.querySelectorAll('polyline');
    assert.equal(lines.length, 1, '一条系列应画出一条折线');
    const pts = lines[0].getAttribute('points').split(' ').map((p) => p.split(',').map(Number));
    assert.equal(pts.length, 3, '三天应有三个点');

    // 横轴等距：首点左贴内边距，末点右贴内边距。
    assert.equal(pts[0][0], 6);
    assert.equal(pts[2][0], 994);

    // 纵轴：0 在基线（200-12=188），峰值贴顶（12），中间值居中。
    assert.equal(pts[0][1], 188);
    assert.equal(pts[2][1], 12);
    assert.equal(pts[1][1], 188 - Math.round(50 * (200 - 24) / 100));
    assert.ok(pts[1][1] < pts[0][1] && pts[1][1] > pts[2][1], '中间值应落在两者之间');
});

test('单点窗口画方块而不是折线', () => {
    const env = makeEnv([[42]], 42);
    env.trend.render(env.svg);

    assert.equal(env.svg.querySelectorAll('polyline').length, 0, '一个点连不成线');
    const rects = env.svg.querySelectorAll('rect');
    assert.equal(rects.length, 1);
    assert.equal(rects[0].getAttribute('x'), '500', '单点居中');
});

test('空数组不画任何东西（不是一天 0 用量）', () => {
    const env = makeEnv([[]], 0);
    env.trend.render(env.svg);
    assert.equal(env.svg.children.length, 0, '没有数据就不该出现基线平线');
});

test('全零窗口画平线在基线上', () => {
    const env = makeEnv([[0, 0, 0]], 0);
    env.trend.render(env.svg);
    const lines = env.svg.querySelectorAll('polyline');
    assert.equal(lines.length, 1);
    const ys = lines[0].getAttribute('points').split(' ').map((p) => Number(p.split(',')[1]));
    assert.deepEqual(ys, [188, 188, 188], '零用量是真实情况，画在基线上');
});

test('多系列各画一条，配色按序号取槽位', () => {
    const env = makeEnv([[1, 2], [3, 4]], 4);
    env.trend.render(env.svg);
    const lines = env.svg.querySelectorAll('polyline');
    assert.equal(lines.length, 2);
    assert.match(lines[0].getAttribute('style'), /chart-c1/);
    assert.match(lines[1].getAttribute('style'), /chart-c2/);
});

test('重复渲染不叠线', () => {
    const env = makeEnv([[1, 2, 3]], 3);
    env.trend.render(env.svg);
    env.trend.render(env.svg);
    assert.equal(env.svg.querySelectorAll('polyline').length, 1, '重画应先清空');
});

test('valuesOf 把空格分隔的数字解析成数组', () => {
    const env = makeEnv([], 0);
    // 跨 vm 边界返回的是另一个 realm 的 Array，deepEqual 会因为原型不同而失败 ——
    // 用 JSON 往返把它拉回本 realm 再比。
    const back = (raw) => JSON.parse(JSON.stringify(env.trend.valuesOf(raw)));
    assert.deepEqual(back('1 2 3'), [1, 2, 3]);
    assert.deepEqual(back(''), []);
    assert.deepEqual(back('0'), [0]);
});
