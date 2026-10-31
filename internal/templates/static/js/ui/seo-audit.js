/*
 * seo-audit.js — 产物 SEO 体检的结果展示。向写基座注册 publication.seo-audit，
 * 只往模板预置的节点里填事实：本文件不持有任何句子，词条 / 级别名 / 成品句格式
 * 全部来自 #seo-audit-dictionary 里的 <template>。
 *
 * 三条结构性事实（改代码前先核对 seo.html，否则会写出「测试绿、页面空」的实现）：
 *   1. 表单与结果区是兄弟，共同祖先只有 [data-seo-audit-scope] 的 <section>；
 *      关联只能从 form 往上找 scope、再在 scope 里往下找 —— 不能从 form 里找，
 *      也不能 document.querySelector（会误配别的卡）。
 *   2. 外层词典是 <template>：内容在 .content 上，嵌套 <template> 的内容同样在各自
 *      .content 里，querySelector 不穿透。
 *   3. 行文案预置在行模板里，克隆体已带着；按整行 textContent 回读会把级别、路径、
 *      details 一起拼进去，所以一律沿用克隆体。
 */
(function (global) {
    'use strict';

    var ui = global.WBUI;
    if (!ui || !ui.writeBase || !ui.writeBase.registerResultHandler) return;
    // 重复加载（页面把脚本引两次）时保留第一次的注册，不重复登记。
    if (ui.seoAudit) return;

    var HANDLER = 'publication.seo-audit';
    var SCOPE = '[data-seo-audit-scope]';
    var RESULT = '[data-seo-audit-result]';
    var DICTIONARY_ID = 'seo-audit-dictionary';

    function str(value) {
        return value === undefined || value === null ? '' : String(value);
    }

    function text(node, value) {
        if (node) node.textContent = str(value);
    }

    // within 在 scope 内部按 [attr="value"] 找一个；找不到返回 null。
    function within(scope, attribute, value) {
        if (!scope) return null;
        if (value === undefined) {
            return scope.querySelector('[' + attribute + ']');
        }
        var all = scope.querySelectorAll('[' + attribute + ']');
        for (var i = 0; i < all.length; i++) {
            if (all[i].getAttribute(attribute) === str(value)) return all[i];
        }
        return null;
    }

    // 词典片段：顶层子节点 + 递归展开嵌套 <template> 的 .content。
    // 为什么递归：issue 行模板本身也是 <template>，它的文案只存在于自己的 .content 里。
    function dictionary(scope) {
        // 只在本 section 内找：词典是这一块的模板，跟别的卡没有关系。
        // 一页多份体检卡时，全局回落会拿到别人的词条。
        var node = scope ? scope.querySelector('#' + DICTIONARY_ID) : null;
        if (!node) return null;
        var fragment = node.content || node;
        return {
            find: function (attribute, value) {
                return search(fragment, attribute, value);
            },
        };
    }

    function search(root, attribute, value) {
        if (!root) return null;
        var kids = root.children || [];
        for (var i = 0; i < kids.length; i++) {
            var kid = kids[i];
            if (kid.getAttribute && kid.getAttribute(attribute) === str(value)) return kid;
            // 嵌套 <template> 的内容单独递归（真实 DOM 里它们不在 kid.children 上）。
            if (kid.content) {
                var deep = search(kid.content, attribute, value);
                if (deep) return deep;
            }
        }
        return null;
    }

    // 文案：从 <template> 的 .content 取；不是 template 就直接读自己的 textContent。
    function copyOf(tpl) {
        return tpl ? str(tpl.content ? tpl.content.textContent : tpl.textContent).trim() : '';
    }

    function cloneOf(tpl) {
        if (!tpl || !tpl.content) return null;
        return tpl.content.cloneNode(true);
    }

    function fill(format, values) {
        var out = str(format);
        Object.keys(values).forEach(function (key) {
            out = out.split('{' + key + '}').join(str(values[key]));
        });
        return out;
    }

    // ── 一条问题 ────────────────────────────────────────────────────────────

    // 级别列：模板给「已取好词的级别名 + tone token」，未收录的 level 走
    // data-seo-level-missing 兜底。
    //
    // 徽标形态只允许有一份（admin/partials/badge.html）。模板侧因此只交
    // 「tone token + 已取好词的文案」，class 在这里拼成同一形态 —— 模板里再写一遍
    // <span class="badge badge-…"> 就会让徽标退化成两份实现（门禁按字面量数处数）。
    function levelBadge(dict, level) {
        var tpl = dict ? dict.find('data-seo-level-item', level) : null;
        if (!tpl || !tpl.content) tpl = dict.find('data-seo-level-missing', '');
        var node = cloneOf(tpl);
        var host = node ? node.querySelector('[data-seo-badge-tone]') : null;
        if (host) host.className = 'badge badge-' + (host.getAttribute('data-seo-badge-tone') || 'mute');
        return node;
    }

    function detailInto(dict, host, name, value) {
        if (value === undefined || value === null || str(value) === '') return;
        if (!host) return;
        var tpl = dict.find('data-seo-issue-detail', name);
        var node = cloneOf(tpl);
        if (!node) return;
        text(node.querySelector('[data-seo-detail-value]'), value);
        host.appendChild(node);
    }

    function pathsInto(dict, host, paths) {
        if (!Array.isArray(paths) || paths.length === 0) return;
        var list = host.querySelector('[data-seo-issue-paths]');
        if (!list) return;
        var tpl = dict.find('data-seo-path-template', '');
        paths.forEach(function (value) {
            var row = cloneOf(tpl);
            if (!row) return;
            // 每条路径都要完整可见：逐个 <code> 列出，不塞进 title 属性。
            text(row.querySelector('[data-seo-path]'), value);
            list.appendChild(row);
        });
    }

    // buildRow 造一行并返回它的 <tr>（不在任何文档里）。
    //
    // 返回值必须是 <tr> 而不是 cloneNode 出来的 DocumentFragment：
    // Fragment 的 children[0] 才是 <tr>，对它 replaceChildren 会把整行的问题列与
    // 路径一并抹掉 —— 级别列必须精确定位到 tr 的首个 <td>。
    //
    // 任何一步不成形就返回 null，由调用方决定整批失败（不静默丢行）。
    function buildRow(dict, issue) {
        if (!issue || typeof issue !== 'object') return null;
        var item = str(issue.item);
        var tpl = item ? dict.find('data-seo-issue-item', item) : null;
        var known = !!(tpl && tpl.content);
        if (!known) tpl = dict.find('data-seo-issue-item', 'unknown');
        var fragment = cloneOf(tpl);
        var row = fragment && fragment.children ? fragment.children[0] : null;
        if (!row || !row.querySelector('[data-seo-issue-label]')) return null;

        var params = issue.params || {};

        // 文案沿用克隆体里预置的那一份。未收录的 item 显示通用提示，并在下面补原标识。
        var detailHost = row.querySelector('[data-seo-issue-details]');
        if (detailHost) {
            if (!known) detailInto(dict, detailHost, 'item', item);
            detailInto(dict, detailHost, 'title', params.title);
            detailInto(dict, detailHost, 'src', params.src);
            detailInto(dict, detailHost, 'href', params.href);
        }
        pathsInto(dict, row, params.paths);

        // 级别列：行模板里是一个空的首个 <td>，徽章整块来自模板。
        var levelCell = row.querySelector('[data-seo-level-cell]')
            || (row.children && row.children[0]) || null;
        var badge = levelBadge(dict, issue.level);
        if (levelCell) {
            // 只清空这一格 —— 其余两列（路径、问题）原样保留。
            levelCell.replaceChildren();
            if (badge) levelCell.appendChild(badge);
        }

        text(row.querySelector('[data-seo-issue-path]'), issue.path);
        return row;
    }

    // ── 四档状态 ────────────────────────────────────────────────────────────

    function showState(panel, name) {
        var states = panel.querySelectorAll('[data-seo-audit-state]');
        for (var i = 0; i < states.length; i++) {
            states[i].hidden = states[i].getAttribute('data-seo-audit-state') !== name;
        }
    }

    function summaryOf(dict, scanned, count) {
        var tpl = dict ? dict.find('data-seo-summary-template', '') : null;
        // 格式来自不可变的模板：每轮都从模板重算，不读上一次写进 DOM 的成品句。
        return fill(copyOf(tpl), { scanned: scanned, count: count });
    }

    // renderIssues 先在离线的 DocumentFragment 里把整批行造完，全部成形才动真实 DOM。
    // 边清边造的做法一旦中途失败，旧结论已经被毁掉 —— 那是最难排查的一类「刷新后空白」。
    function renderIssues(dict, panel, data) {
        var state = within(panel, 'data-seo-audit-state', 'issues');
        if (!state) return false;
        var body = state.querySelector('[data-seo-audit-rows]');
        if (!body) return false;

        var staged = global.document.createDocumentFragment();
        for (var i = 0; i < data.issues.length; i++) {
            var row = buildRow(dict, data.issues[i]);
            if (!row) return false;                 // 有 null issue / 缺模板：整批作废
            staged.appendChild(row);
        }
        body.replaceChildren();
        body.appendChild(staged);

        text(state.querySelector('[data-seo-audit-summary]'), summaryOf(dict, data.scanned, data.count));
        showState(panel, 'issues');
        return true;
    }

    function isCount(value) {
        return typeof value === 'number' && Number.isInteger(value) && value >= 0;
    }

    // 契约形状：这些是「后端说法自洽」的判据，不是业务补算。
    function goodContract(data) {
        if (!data) return false;
        if (!isCount(data.scanned) || !isCount(data.count)) return false;
        if (!Array.isArray(data.issues)) return false;
        if (data.count !== data.issues.length) return false;
        if (data.scanned <= 0 && data.issues.length > 0) return false;
        if (data.count > 0 && data.scanned <= 0) return false;
        return true;
    }

    function render(panel, data) {
        if (!panel) return false;
        var scope = panel.closest && panel.closest(SCOPE) || panel;
        var dict = dictionary(scope);
        if (!dict) return false;

        // 形状在边界上判死：不完整就返回 false —— 契约被破坏时保留上一次的结论，不猜。
        if (!goodContract(data)) return false;

        if (data.scanned <= 0) {
            showState(panel, 'empty');
        } else if (data.count <= 0) {
            var clean = within(panel, 'data-seo-audit-state', 'clean');
            if (!clean) return false;
            text(clean.querySelector('[data-seo-audit-scanned]'), data.scanned);
            showState(panel, 'clean');
        } else if (!renderIssues(dict, panel, data)) {
            return false;
        }
        // 新结论成立：收起上一次的失败提示。
        var box = panel.querySelector('[data-seo-audit-failed]');
        if (box) {
            text(box, '');
            box.hidden = true;
        }
        panel.removeAttribute('data-seo-audit-error');
        panel.setAttribute('aria-busy', 'false');
        return true;
    }

    // ── 失败：保留已有结论与已填输入，只加一条失败提示 ──────────────────────
    function fail(panel, message) {
        if (!panel) return false;
        // 提示写在面板里预置的 <p data-seo-audit-failed> 上；面板自身只留一个状态属性，
        // 用不同的名字，避免「面板匹配到自己的选择器」这种自指。
        var box = panel.querySelector('[data-seo-audit-failed]');
        if (box) {
            text(box, message);
            box.hidden = !message;
        }
        if (message) panel.setAttribute('data-seo-audit-error', 'true');
        else panel.removeAttribute('data-seo-audit-error');
        panel.setAttribute('aria-busy', 'false');
        return true;
    }

    function scopeOf(form) {
        if (!form) return null;
        if (form.closest) {
            var hit = form.closest(SCOPE);
            if (hit) return hit;
        }
        return null;
    }

    function panelOf(form) {
        var scope = scopeOf(form);
        return scope ? scope.querySelector(RESULT) : null;
    }

    // 协议错误的文案：从结果区里那个已经取过词的节点读静态文案，本文件不编句子。
    //
    // 读法是「先存文案、再清节点」：fail() 会把 message 写进同一个节点，
    // 所以不能事后回读 —— 那时它已经是传给 fail 的那句。
    function protocolMessage(panel) {
        var box = panel ? panel.querySelector('[data-seo-audit-failed]') : null;
        if (!box) return '';
        return str(box.getAttribute('data-seo-audit-message') || box.textContent).trim();
    }

    ui.seoAudit = { render: render, fail: fail, handler: HANDLER };

    ui.writeBase.registerResultHandler(HANDLER, function (context) {
        var form = context.form;
        var panel = panelOf(form);

        if (!panel) {
            // 连结果区都没有：一样是契约破了，抛出去让基座按失败收尾。
            throw new Error('seo-audit: result panel missing');
        }
        var message = protocolMessage(panel);
        if (!render(panel, context.data)) {
            // 不能 return true：那会把「没拿到结论」当成一次成功写动作。
            // 抛 Error 让基座 catch 分支写 error 状态；上一次的结论原样保留。
            fail(panel, message);
            throw new Error(message || 'seo-audit: payload rejected');
        }
        // 只读检查：消费结果，不导航、不刷新、不关抽屉。
        return true;
    });
})(window);
