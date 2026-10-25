// automation/graph.js — 画布的两个**小工具**：读注入数据、把侧栏一行文本解析成参数。
//
// 渲染与布局已搬到 render.js / layout.js（那两个是纯函数，能直接在 node 里跑），
// 本文件只剩这两件事：
//
//   · readJSON  —— 服务端把图与 meta 注入成 <script type="application/json">，
//                  避免内联可执行脚本（CSP / 评审规则都要求）；
//   · parseParam —— 侧栏那个文本框里的一行字 → 该类型的 params 对象。
//
// 叫什么名字是历史包袱（它曾经承担渲染），改名会让缓存版本与引用一起漂，故保留。

/** readJSON 从页面的 script[type=application/json] 里读数据（服务端注入，避免内联脚本）。 */
export function readJSON(id, fallback) {
    var el = document.getElementById(id);
    if (!el) { return fallback; }
    try {
        return JSON.parse(el.textContent || '');
    } catch (e) {
        return fallback;
    }
}

/**
 * PARAM_HINT_FALLBACK 参数提示的兜底文案。
 *
 * 节点类型的**显示名**一律取服务端注入的 meta.nodeTypes（见 render.js 的 nodeLabel）——
 * 这里不再维护第二份 NODE_LABEL：两处名字一对不上，用户就在 palette 里看到一种说法、
 * 在节点上看到另一种。
 *
 * 参数提示仍然留在前端：它是「这一格该填什么」的输入提示，不是业务语义，
 * 而服务端的类型表只给到 label / hint 两句。缺条目时渲染空串即可。
 */
export var PARAM_HINT_FALLBACK = {
    trigger: '入口不需要参数。',
    delay: '等待分钟数，例如 1440 表示一天。',
    email: '邮件模板的 template_key。',
    tag: '要加的标签（逗号分隔），减标签用「- 标签」。',
    filter: '条件（逗号分隔）：opened / clicked / subscribed / has_tag:标签。',
    list_status_branch: '按联系人状态分流，不需要参数。',
    ab_test: '变体与权重在图上编辑；权重之和必须是 100。',
    webhook: 'https 回调地址（内网地址会被拒绝）。',
    end: '结束不需要参数。'
};

/**
 * parseParam 把侧栏里的一行文本解析成该类型的 params 对象。
 *
 * 返回 { ok, params, error }。**这是唯一一份文本↔参数规则**：侧栏是唯一的文本编辑入口，
 * 不再有「表单页」需要对齐（那个页面已删除）。
 */
export function parseParam(type, raw) {
    var v = (raw || '').trim();
    if (type === 'delay') {
        var n = parseInt(v, 10);
        if (!isFinite(n) || n <= 0) { return { ok: false, error: '等待分钟数要填正整数' }; }
        return { ok: true, params: { minutes: n } };
    }
    if (type === 'email') {
        if (!v) { return { ok: false, error: '发信节点要选邮件模板' }; }
        return { ok: true, params: { template_key: v } };
    }
    if (type === 'tag') {
        var tags = splitList(v);
        if (!tags.length) { return { ok: false, error: '标签节点要填要加的标签' }; }
        return { ok: true, params: { add: tags } };
    }
    if (type === 'branch' || type === 'filter') {
        var conds = splitList(v);
        if (!conds.length) { return { ok: false, error: '条件分支至少填一个条件' }; }
        return { ok: true, params: { conditions: conds } };
    }
    if (type === 'webhook') {
        if (!/^https:\/\//i.test(v)) { return { ok: false, error: '回调地址必须以 https:// 开头' }; }
        return { ok: true, params: { url: v } };
    }
    return { ok: true, params: {} };
}

/** splitList 逗号 / 中文逗号 / 顿号分隔 → 去空白去空项（与后端 splitFormList 一致）。 */
export function splitList(raw) {
    return (raw || '').replace(/[，、]/g, ',').split(',')
        .map(function (s) { return s.trim(); })
        .filter(function (s) { return s !== ''; });
}
