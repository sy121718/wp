/* daterange.test.cjs — 日期区间控件的档位计算（node:test + vm，无浏览器、无依赖）。
 *
 * 只测 rangeOf 这一块纯函数：界面部分要在真实浏览器里看（日历布局、弹层定位），
 * 而区间算错的表现是「选本周，结果少一天」—— 这种缺陷在界面上看着完全正常。
 * 固定 base 而不是用真实今天：跨月/跨年/月末的边界只有在确定日期上才测得到。
 */
const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');

const SRC = fs.readFileSync(path.join(__dirname, 'daterange.js'), 'utf8');
const UTIL = fs.readFileSync(path.join(__dirname, '_util.js'), 'utf8');

function load() {
  const listeners = [];
  const document = {
    addEventListener: (name) => listeners.push(name),
    documentElement: { getAttribute: () => 'zh-CN' },
  };
  const sandbox = { console, Intl, Date, WeakMap, Event: function () {} };
  sandbox.window = sandbox;
  sandbox.document = document;
  vm.createContext(sandbox);
  vm.runInContext(UTIL, sandbox);
  vm.runInContext(SRC, sandbox);
  return sandbox.WBUI.daterange;
}

const p = (y, m, d) => ({ y, m, d });
// 结果来自 vm 上下文里的另一个 realm：它的 Array 与此处的 Array 不同原型，
// deepStrictEqual 会因原型不同判不等（值明明一样），所以先拉回本 realm。
const range = (ctrl, key, base) => {
  const r = ctrl.rangeOf(key, base);
  return r ? Array.from(r) : r;
};

test('today / yesterday 是单日区间', () => {
  const c = load();
  assert.deepStrictEqual(range(c, 'today', p(2026, 3, 15)), ['2026-03-15', '2026-03-15']);
  assert.deepStrictEqual(range(c, 'yesterday', p(2026, 3, 1)), ['2026-02-28', '2026-02-28']);
});

test('本周从周一起：周日当天往回退 6 天', () => {
  const c = load();
  // 2026-03-15 是周日 → 本周一是 03-09。
  assert.deepStrictEqual(range(c, 'week', p(2026, 3, 15)), ['2026-03-09', '2026-03-15']);
  // 周一本就是起点。
  assert.deepStrictEqual(range(c, 'week', p(2026, 3, 9)), ['2026-03-09', '2026-03-09']);
});

test('本月 / 上个月覆盖月末边界', () => {
  const c = load();
  assert.deepStrictEqual(range(c, 'month', p(2026, 3, 15)), ['2026-03-01', '2026-03-15']);
  assert.deepStrictEqual(range(c, 'lastMonth', p(2026, 3, 15)), ['2026-02-01', '2026-02-28']);
  assert.deepStrictEqual(range(c, 'lastMonth', p(2024, 3, 31)), ['2024-02-01', '2024-02-29']);
});

test('近半年把日裁到目标月最后一天', () => {
  const c = load();
  assert.deepStrictEqual(range(c, 'halfYear', p(2026, 8, 31)), ['2026-02-28', '2026-08-31']);
  assert.deepStrictEqual(range(c, 'halfYear', p(2024, 8, 31)), ['2024-02-29', '2024-08-31']);
});

test('本年从 1 月 1 日起', () => {
  const c = load();
  assert.deepStrictEqual(range(c, 'year', p(2026, 10, 26)), ['2026-01-01', '2026-10-26']);
});

test('弹层尺寸：够宽双月，窄屏退单月并收进视口', () => {
  const c = load();
  // 同样是跨 realm 的对象 → 先归一化。
  const m = (v) => JSON.parse(JSON.stringify(c.popMetrics(v)));
  assert.deepStrictEqual(m(1248), { width: 660, months: 2 });
  assert.deepStrictEqual(m(768), { width: 660, months: 2 });
  // 683 以下就不够双月（660 + 两侧各 12px 边距 = 684）。
  assert.deepStrictEqual(m(683), { width: 300, months: 1 });
  assert.deepStrictEqual(m(375), { width: 300, months: 1 });
  // 极端窄屏：宽度收到可用宽度，不能比视口还宽。
  assert.deepStrictEqual(m(320), { width: 296, months: 1 });
  assert.deepStrictEqual(m(0), { width: 0, months: 1 });
});

test('认不出的档位返回 null（调用方据此不动两个 date）', () => {
  const c = load();
  assert.strictEqual(range(c, 'custom', p(2026, 3, 15)), null);
  assert.strictEqual(range(c, 'days7', p(2026, 3, 15)), null);
});
