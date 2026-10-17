/* ui/json_write.js — 原始控件：JSON 写表单基座（前端 fetch 直调现有 JSON API）。
 *
 * 存在的理由：后台写动作原本是「页面 POST → handler → POST/Redirect/GET」，页面 POST 只做
 * 薄转发（收表单字段、转成 service 入参、渲染结论）。这条链路把「结论」交给服务端渲染，
 * 于是要么整页刷新（丢输入）、要么走提示页（多一跳），而 JSON API 本来就存在、契约更严。
 * 现在前端直接调 JSON API，成功由 JS 导航、失败在原地点出并**保住用户已填的内容**。
 *
 * 契约（全部声明式，模板里不写内联 JS；无 JS 时这些控件是 hidden 的，见下）：
 *
 *   <form data-write-api="/api/block/create"
 *         data-write-mode="create"           single | bulk | create（别名 form）
 *         data-write-nav="/workbench?block={id}"   create 成功后的跳转（{id} 取响应 data.id）
 *         data-msg-…="…"                     文案由服务端 tr() 渲染后带进来（见下表）
 *         data-confirm="…">                  与 ui/confirm.js 共用确认框
 *     <p data-write-status role="status"></p>  结果/错误就地显示区（必需）
 *
 * 声明可以写在**表单**上，也可以写在**提交按钮**上；两者都有时**按钮优先**（见 attrFrom）。
 * 按钮上一个 data-write-api 都没有 = 这个按钮不由本文件接管，走原生提交 —— 这是「一个表单
 * 并排几个动作」的落点：改状态这类**没有 JSON 端点**的动作保持页面 POST，不会被邻居的声明
 * 拖进 fetch。回车提交没有 submitter，此时只认表单级声明（表单没声明就原生提交）。
 *
 *   - mode=single：payload = 表单里 name="id" 的隐藏域（+ 可选 force，+ data-write-extra
 *     点名的额外字段，例如多工程下的 projectId）。成功 → 整页刷新
 *     （列表是服务端渲染的，刷新才能让「影响面 / 待重建」这些派生列同源）。
 *   - mode=bulk  ：payload = {ids:[…]}，ids 取自表单内已勾选的 [name=ids]；成功 → 页内结果区
 *     给出「已成功 N / 跳过 M + 逐条原因」，并在结果区放一个「刷新列表」入口（不自动刷新，
 *     避免结果一闪而过）。去重与 200 条上限在客户端先拦一次只为快速反馈，
 *     **服务端才是边界**（/api/block/bulk-delete 自己会去重与限量）。
 *     · data-write-extra="add,remove"：**显式点名**的额外字段（逗号分隔），从表单按 name 取值
 *       一起发，single 与 bulk 两档都生效。同名字段优先取本次 submitter 的值，其余从表单取。
 *       必须点名而不是「把表单字段全带上」：single 的形状是 {id}、bulk 是 {ids} + 该动作自己的
 *       参数，多带字段会让端点收到两种互不认识的身体。id / ids 永远最后写，额外字段不可能覆盖它。
 *       它解决的正是「页面渲染了某个隐藏域、但端点需要它、而单值档只发 id」这一类：
 *       例如商品删除在多工程部署下必须带 projectId，否则 service 只能走唯一工程兜底而被拒。
 *     · data-write-outcome="receipt"：显示服务端成品 message，按 skipped 标记部分失败，
 *       保留逐项失败原因与刷新入口；noop 不计失败，也不移除列表行。
 *     · data-write-outcome="review"：回执以服务端 message 成品为准，结果区保留刷新入口。
 *       data.Affected / data.Status 是实际响应字段（无 json tag，大小写固定），分别记录到
 *       data-write-affected / data-write-review-status；不推算 skipped，零 Affected 仍显示回执，
 *       不移除任何行。下一次结果会清除旧回执属性，失败继续保留输入与选择。
 *     · data-write-outcome="delete"（默认）| "update"：服务端结果的口径。delete 是破坏性动作
 *       —— 成功条目按 items[].deleted 把该行从表格移除；update 不是（打标签、改状态），成功
 *       计数读 data.updated 且**不移除任何行**（把还在的联系人行删掉，刷新才发现没删）。
 *     · data-write-number="id"：**显式点名**「按数字发」的字段（逗号分隔，single/create/bulk
 *       三档都生效）。DOM 里所有值都是字符串，而严格 API 会拒绝 "5" 这种字符串数字
 *       （mail 联系人端点就是）；但也不能全局猜数字 —— block 的 id 是 UUID，一律 Number()
 *       会把它转坏。所以只转**点名**的字段，且只在值是十进制非负整数、且不超过
 *       Number.MAX_SAFE_INTEGER 时才转；空值 / 负数 / "1e3" / 超安全整数一律原样发字符串，
 *       由严格 API 自己回 400（页面替调用方猜一次类型，就是替它造了一个它没说的值）。
 *       bulk 的 ids 数组仅在显式点名 ids 时逐元素按同一规则转换；未声明保持字符串数组。
 *     · data-write-bool="autoReceive"：**显式点名**「按布尔发」的字段（逗号分隔，create/bulk
 *       两档生效）。与 data-write-number 同一条理由：DOM 里的值都是字符串，而严格 API 的布尔
 *       字段收到 "1" 会整条 400；但也不能全局猜 —— 只转**点名**的字段，且只认
 *       1/true/on/yes 与 0/false/off/no（大小写不敏感；空串按 false）。
 *       认不出的取值（"maybe" / "2"）原样发字符串，由严格 API 自己回 400：
 *       页面替调用方猜一次类型，就是替它造了一个它没说的值。未勾选的 checkbox 根本不在
 *       payload 里 —— 点名也只处理**已经存在的**字段。
 *     · data-write-array="tagIds,categoryIds"：**显式点名**「同名多值收成数组」的字段（create/form 档）。
 *       多选 checkbox 组默认是「后者覆盖」，覆盖的失效模式是「勾了三个、只存最后一个」。
 *       一个都没勾时发**空数组**（不是不发）：UpdateReq 里 nil = 本次不改、[] = 解绑全部，
 *       而「一个都不勾」是明确的解绑意图。
 *     · data-write-lines="images,imageAlts"：**显式点名**「按行切成字符串数组」的字段。
 *       图集与 alt 在页面里是「一个隐藏域 + 多行文本」，端点要的却是 []string —— 整表单
 *       直接把 "a\nb" 送过去，严格解码只回一句 400。空文本按空数组发（清空是明确意图）。
 *     · data-write-html="descriptionHtml:description"：把裸 HTML 字段包成 {"html": "…"} 写到
 *       目标键（冒号后可省，缺省 = 源字段名）。富文本控件的值是裸 HTML，而库里是 jsonb 对象；
 *       这层包装原在 handler 里做，页面迁到基座后必须跟过来。不 trim：正文空白是排版。
 *     · data-write-float="defaultPrice,weight"：**显式点名**「按小数发」的字段（与
 *       data-write-number 分档：那个只认非负整数，id 类字段用它最安全）。只认十进制小数，
 *       认不出的取值原样发字符串。
 *     · data-write-skip-empty="slug,sku,defaultPrice"：**显式点名**「空就不发」的字段。
 *       它守的是「留空即不改」：整表单提交会把没填的输入框发成 ""，于是「只改个名字」变成
 *       「顺手把 URL 段置空」。过滤发生在类型转换之前。
 *     · data-write-rows=".nav-tr-row;context,hash,target;rows"：**声明式行数组**（只在 create 档生效）。
 *       分号前是「一行一个容器」的选择器，第二段是这一行的字段名，**第三段是 payload 的键名**
 *       （可省，缺省 rows）：每个容器收成一个对象，按 DOM 顺序组装成 payload[key]（容器里缺某个
 *       字段就跳过该字段，字段全缺的容器整行跳过）。
 *       字段名按**精确名**取，取不到再按「.后缀」取 —— 属性值编辑器那种 values[0].key 的扁平带
 *       索引名，容器里每一行的下标都不同，用不了固定 name。
 *       容器内**所有**具名控件都不参与顶层标量（不再只看点名的几个）：同一个 name 交给端点两次，
 *       严格白名单只能整条请求拒掉。它**不改**「重复 name」的单值语义（顶层重名仍是后者覆盖）：
 *       整表提交要的是「行」，不是「一个字段多个值」。行数组最后写，不可能被其它字段覆盖。
 *   - mode=create：payload = 表单全部具名字段（+ data-write-rows 的行数组）。成功 → location.assign(nav)。
 *     **别名 form**：语义与 create 逐字相同（发整张表单、成功后导航或整页刷新），名字表明
 *     「这是一张编辑/新建表单」。新建抽屉与编辑抽屉的表单形状本来就一样（只差一个 id 隐藏域），
 *     分成两个 mode 会让模板作者以为 update 该用 single —— 而 single 只发 {id}，编辑表单的
 *     字段会被整张丢掉（端点收到空 name 直接 400，看起来像「保存没反应」）。
 *     保留 create 这个名字是因为它已在 order / mail / navigation 等 19 处模板里，改名的收益为零。
 *
 * 文案键（模板用 tr() 渲染好的成品句，占位符 {count}/{deleted}/{updated}/{added}/{skipped}/{max} 由本文件填）：
 *   data-msg-none-selected / data-msg-all-deleted / data-msg-all-updated / data-msg-all-added
 *   data-msg-all-skipped / data-msg-partial / data-msg-too-many / data-msg-failed / data-msg-refresh
 *   （outcome=update 只读 all-updated、outcome=add 只读 all-added，**都不回落 all-deleted** ——
 *     打标签 / 加入菜单的成功绝不能显示成「已删除」；缺这句配置时结果区不给句子，
 *     只列逐条原因与刷新入口，宁可少说也不撒谎。）
 *
 * 工程约束（与仓库既有控件同源，别绕开）：
 *   1. **不引第三方库**：fetch + 原生 DOM；
 *   2. **CSRF**：写请求带 X-CSRF-Token（<meta name="csrf-token">，登录页兜底 sessionStorage）；
 *      /api 组挂了 SessionAuth + CSRFMiddleware + Casbin，缺头即 403；
 *   3. **不自己造确认框**：data-confirm 交给 ui/confirm.js —— 它拦截那一次 submit，确认后
 *      **临时摘掉 data-confirm 再重派发**（见 confirm.js 的 submitConfirmed）。所以：
 *      - 本文件看到「还带着 data-confirm」= 用户尚未确认 → 直接 return（否则确认框还没弹
 *        请求就发出去了）；
 *      - 不读任何「已确认」标记：标记会残留成 DOM 状态，表现是「第二次点同一个删除表单
 *        不再弹确认」；
 *   4. **一次点击只发一次请求**：按表单粒度加 in-flight 锁（回车提交、确认后连点都不经过
 *      按钮的 disabled 状态），请求结束（成功/失败/异常）释放；
 *   5. **可反复增强**：htmx 片段换进来后 WBUI.scan 会重跑，初始化靠 markOnce 幂等。
 */
(function (global) {
    'use strict';

    var WBUI = global.WBUI || (global.WBUI = {});
    global.WBUI = WBUI;
    WBUI.controls = WBUI.controls || [];

    // 与 shell.MaxBulkIDs 同值。客户端拦截只为「别把 500 条发出去」，判据仍是服务端。
    var MAX_BULK_IDS = 200;

    var API_ATTR = 'data-write-api';
    var STATUS_SEL = '[data-write-status]';
    var payloadBuilders = Object.create(null);
    global.WPJsonWrite = global.WPJsonWrite || {};
    global.WPJsonWrite.registerPayloadBuilder = function (name, builder) {
        if (typeof builder !== 'function' || !name) { throw new TypeError('Invalid payload builder'); }
        payloadBuilders[name] = builder;
    };

    function buildPayload(form, submitter, payload) {
        var name = attrFrom(form, submitter, 'data-write-builder');
        if (!name) { return payload; }
        var builder = payloadBuilders[name];
        if (!builder) { throw new Error('Missing payload builder'); }
        var result = builder(form, submitter, payload);
        if (!result || typeof result !== 'object' || Array.isArray(result)) {
            throw new Error('Invalid payload');
        }
        return result;
    }

    function attr(el, name) { return (el && el.getAttribute) ? el.getAttribute(name) : null; }

    // attrFrom 读一条声明属性：**提交按钮优先、表单兜底**。
    //
    // 为什么不是「读表单就够了」：一个表单里可以并排放几个动作（批量删除 / 改状态 / 打标签），
    // 它们端点不同、结果口径也不同，而 HTML 的表单提交事件只有一个 target=form —— 能区分
    // 「谁触发的」的只有 submitter。带上 submitter 就等于表单级是默认值、按钮级是覆盖。
    // 按钮上不留任何 data-write-api 时，apiOf 返回空串 → 调用方直接放行原生提交。
    function attrFrom(form, submitter, name) {
        var own = attr(submitter, name);
        if (own !== null) { return own; }
        return attr(form, name);
    }

    function apiOf(form, submitter) { return attrFrom(form, submitter, API_ATTR) || ''; }

    // hasConfirm 判「确认框还没走完」：表单级与**按钮级**都要看。
    // 只判表单会让带 data-confirm 的按钮在确认弹窗还没关掉时就被本文件抢先发请求。
    function hasConfirm(form, submitter) {
        return (submitter && submitter.hasAttribute && submitter.hasAttribute('data-confirm')) ||
            (form.hasAttribute && form.hasAttribute('data-confirm'));
    }

    // csrfToken 取 CSRF token：优先服务端注入（每次页面渲染新鲜），退回登录时写入的 sessionStorage。
    function csrfToken() {
        var meta = document.querySelector('meta[name="csrf-token"]');
        if (meta && attr(meta, 'content')) { return attr(meta, 'content'); }
        try { return global.sessionStorage.getItem('csrf_token') || ''; } catch (e) { return ''; }
    }

    // fill 把服务端渲染好的文案里的命名占位符替换成值（模板不拼字符串，只给成品句）。
    function fill(tpl, params) {
        if (!tpl) { return ''; }
        return String(tpl).replace(/\{(\w+)\}/g, function (match, key) {
            return Object.prototype.hasOwnProperty.call(params, key) ? String(params[key]) : match;
        });
    }

    function attrMsg(form, submitter, key) { return attrFrom(form, submitter, 'data-msg-' + key) || ''; }

    // fillTooMany 填「一次最多操作 N 项」那句话：两种占位符形态都要认。
    //
    // 为什么不能只用命名形态：这句词条（shell.err.bulkIdsTooMany）是**服务端与页面共用**的
    // 同一份句子 —— 服务端经 response.translate 按 %s **位置**填参（上限、本次条数），
    // 页面这侧拿到的就是那条 %s 句子。若这里只认 {max}/{count}，用户会看到裸的 "%s"
    //（错得安静：没有异常、没有日志、只有一句看不懂的话）。
    // 因此：先按命名形态填；还剩 %s 就按 (上限, 条数) 的顺序位置填 —— 与 translate 同序。
    function fillTooMany(tpl, max, count) {
        var out = fill(tpl, { max: max, count: count });
        if (out.indexOf('%s') === -1) { return out; }
        var values = [String(max), String(count)];
        var i = 0;
        return out.replace(/%s/g, function () { return i < values.length ? values[i++] : '%s'; });
    }

    // statusOf 找就地结果区：表单内优先，其次表单所在的 host（片段场景下它可能在 form 之外）。
    function statusOf(form) {
        if (form.querySelector) {
            var own = form.querySelector(STATUS_SEL);
            if (own) { return own; }
        }
        var host = form.closest ? form.closest('[data-write-host]') : null;
        if (host && host.querySelector) { return host.querySelector(STATUS_SEL); }
        return null;
    }

    function setStatus(form, text, kind, extra) {
        var box = statusOf(form);
        if (!box) { return; }
        box.textContent = '';
        box.removeAttribute('data-write-affected');
        box.removeAttribute('data-write-review-status');
        box.hidden = false;
        // 一句结论 + 可选的补充块（批量失败的逐条原因）。
        if (text) {
            var line = document.createElement('span');
            line.textContent = text;
            box.appendChild(line);
        }
        if (extra && extra.length > 0) {
            // 逐条原因各自成行：`.stack` 是页面里既有的纵向布局类（flex column + gap），
            // 不引新样式，也不用 <br> 拼行。
            var list = document.createElement('div');
            list.className = 'stack';
            list.setAttribute('data-write-reasons', '');
            extra.forEach(function (item) {
                var li = document.createElement('span');
                li.className = 'text-mute text-sm';
                li.textContent = item;
                list.appendChild(li);
            });
            box.appendChild(list);
        }
        box.className = 'badge ' + (kind === 'error' ? 'badge-warning' : '');
        box.setAttribute('data-write-state', kind || 'ok');
    }

    // ── 跨刷新的结论（data-write-flash）──
    // 有一类写操作成功后**必须整页刷新**：服务端以库为准重算了结果（变体清单就是这种），
    // 就地改 DOM 会与库不一致。可这类操作的结论往往不止「成功」两个字（例如「跳过 2 条：
    // 被引用」），而刷新会把结果区一起抹掉 —— 于是结论存进 sessionStorage，落地后由
    // renderFlash 写进页面上的 [data-write-flash-target="同名"]，读完即删（刷新两次不重现）。
    var FLASH_PREFIX = 'json_write_flash:';

    function flashKey(name) { return FLASH_PREFIX + name; }

    // storeFlash 在 reload / 跳转**之前**调用。隐私模式下 sessionStorage 会抛错：
    // 结论就丢了，但不能因此让写操作变红。
    function storeFlash(form, submitter, text) {
        var name = attrFrom(form, submitter, 'data-write-flash');
        if (!name || !text) { return; }
        try { global.sessionStorage.setItem(flashKey(name), text); } catch (e) { /* 写不进去就算了 */ }
    }

    function renderFlash() {
        var targets = global.document.querySelectorAll('[data-write-flash-target]');
        for (var i = 0; i < targets.length; i++) {
            var box = targets[i];
            var name = box.getAttribute('data-write-flash-target');
            var text = '';
            try {
                text = global.sessionStorage.getItem(flashKey(name)) || '';
                global.sessionStorage.removeItem(flashKey(name));
            } catch (e) { text = ''; }
            if (!text) { continue; }
            box.textContent = '';
            var line = global.document.createElement('span');
            line.textContent = text;
            box.appendChild(line);
            box.hidden = false;
        }
    }
    // refreshEntry 结果区里的「刷新列表」入口：不自动刷新，让人先看完再刷新。
    function refreshEntry(form, submitter) {
        var label = attrMsg(form, submitter, 'refresh');
        if (!label) { return null; }
        var btn = document.createElement('button');
        btn.type = 'button';
        btn.className = 'btn btn-sm btn-ghost';
        btn.setAttribute('data-write-refresh', '');
        btn.textContent = label;
        btn.addEventListener('click', function () { global.location.reload(); });
        return btn;
    }

    // 收集 payload 的三条分支。
    //
    // 关于「数字字段」：DOM 里**所有**值都是字符串，而严格 API 明确拒绝 "5" 这种字符串数字
    //（mail 联系人端点的 id 就是；见 internal/module/mail/inbound/http/mail_contact_write.go）。
    // 反过来也不能全局猜数字：block 的 id 是 UUID，把值一律 Number() 会把它转坏。
    // 所以转换只认**动作显式点名**的字段（data-write-number="id"），规则见 numericValue。
    function numberFields(form, submitter) {
        var spec = attrFrom(form, submitter, 'data-write-number');
        var names = {};
        if (!spec) { return names; }
        String(spec).split(',').forEach(function (rawName) {
            var name = String(rawName || '').trim();
            if (name) { names[name] = true; }
        });
        return names;
    }

    // numericValue 把一个显式点名的字段值转成数字，三条边界都是刻意的：
    //
    //   · 只认 ^\d+$（十进制非负整数）：""、"-1"、"1e3"、"0x10"、" 5"、小数一律**原样发字符串**，
    //     让严格 API 自己回 400 —— 页面侧替调用方猜一次类型，就等于替它造了一个它没说的值；
    //   · 超过 Number.MAX_SAFE_INTEGER 的十进制串原样保留：JS 双精度会丢精度，
    //     转过去可能把 id 静默变成另一个**恰好存在**的 id（比 400 危险得多）；
    //   · 不是字符串的值直接返回（未来若扩展成已解析结构，不该被二次处理）。
    function numericValue(raw) {
        if (typeof raw !== 'string') { return raw; }
        if (!/^\d+$/.test(raw)) { return raw; }
        var n = Number(raw);
        if (!isFinite(n) || n > Number.MAX_SAFE_INTEGER) { return raw; }
        return n;
    }

    // applyNumberFields 只处理 payload 里**已经存在**的同名字段：声明了但没提交的字段不该凭空出现。
    function applyNumberFields(payload, names) {
        Object.keys(names).forEach(function (name) {
            if (!Object.prototype.hasOwnProperty.call(payload, name)) { return; }
            payload[name] = numericValue(payload[name]);
        });
        return payload;
    }

    // boolFields 解析 data-write-bool（**显式点名**的布尔字段），与 numberFields 同形。
    // floatFields 解析 data-write-float="defaultPrice,weight"：**显式点名**「按小数发」的字段。
    //
    // 为什么不并进 data-write-number：那个档只转十进制**非负整数**（id 类字段用它最安全），
    // 而金额 / 重量是小数。并档会让既有 19 处商品域外的调用点跟着改口径 —— 一个为金额加的
    // 「接受小数点」会顺手让 block id 这类字段开始接受 "1.5"。
    function floatFields(form, submitter) {
        var spec = attrFrom(form, submitter, 'data-write-float');
        var names = {};
        if (!spec) { return names; }
        String(spec).split(',').forEach(function (rawName) {
            var name = String(rawName || '').trim();
            if (name) { names[name] = true; }
        });
        return names;
    }

    // floatValue 十进制小数（含负数）才转数字；"1e3" / 空串 / "abc" 原样发，
    // 由严格 API 自己回 400 —— 页面替调用方猜一次类型，就是替它造了一个它没说的值。
    function floatValue(raw) {
        if (typeof raw !== 'string') { return raw; }
        var v = raw.trim();
        if (!/^-?\d+(\.\d+)?$/.test(v)) { return raw; }
        var n = Number(v);
        return isFinite(n) ? n : raw;
    }

    function applyFloatFields(payload, names) {
        Object.keys(names).forEach(function (name) {
            if (!Object.prototype.hasOwnProperty.call(payload, name)) { return; }
            payload[name] = floatValue(payload[name]);
        });
        return payload;
    }

    // arrayFields 解析 data-write-array="tagIds,categoryIds"：**显式点名**「收成数组」的字段。
    //
    // 默认同名控件是「后者覆盖」（单值语义），而多选 checkbox 组（分类 / 标签 / 属性组）必须
    // 收成数组：覆盖的失效模式是「勾了三个、只存最后一个」，不报错、也不崩。
    // 没勾任何一项时发**空数组**（不是不发）：UpdateReq 里 nil = 本次不改、[] = 解绑全部，
    // 而「一个都没勾」是明确的解绑意图。
    function arrayFields(form, submitter) {
        var spec = attrFrom(form, submitter, 'data-write-array');
        var names = {};
        if (!spec) { return names; }
        String(spec).split(',').forEach(function (rawName) {
            var name = String(rawName || '').trim();
            if (name) { names[name] = true; }
        });
        return names;
    }

    // linesFields 解析 data-write-lines="images,imageAlts"：**显式点名**「按行切成字符串数组」的字段。
    //
    // 图集与 alt 在页面里是「一个隐藏域 + 多行文本」（admin/partials/media_gallery.html），
    // 而端点的 Images / ImageAlts 是 []string —— 整表单提交直接把 "a\nb" 送过去，
    // 严格解码只回一句 400。空文本按空数组发（清空图集是明确意图，与「nil = 不改、[] = 清空」对齐）。
    function linesFields(form, submitter) {
        var spec = attrFrom(form, submitter, 'data-write-lines');
        var names = {};
        if (!spec) { return names; }
        String(spec).split(',').forEach(function (rawName) {
            var name = String(rawName || '').trim();
            if (name) { names[name] = true; }
        });
        return names;
    }

    // splitLines 与 handler 侧 splitFormLines 同口径：统一换行、去首尾空白、丢弃空行。
    function splitLines(raw) {
        if (typeof raw !== 'string') { return []; }
        var out = [];
        raw.replace(/\r\n/g, '\n').split('\n').forEach(function (line) {
            var v = line.trim();
            if (v) { out.push(v); }
        });
        return out;
    }

    // skipEmptyFields 解析 data-write-skip-empty="slug,sku"：**显式点名**「空就不发」的字段。
    //
    // 它守的是「留空即不改」这条既有语义：端点的 slug / sku 是 *string，nil = 不改、
    // 空串 = 非法参数（或清空）。整表单提交会把没填的输入框发成 ""，于是「只想改个名字」
    // 变成「顺手把 URL 段置空」—— 400 还是好的，最坏是静默清空。
    function skipEmptyFields(form, submitter) {
        var spec = attrFrom(form, submitter, 'data-write-skip-empty');
        var names = {};
        if (!spec) { return names; }
        String(spec).split(',').forEach(function (rawName) {
            var name = String(rawName || '').trim();
            if (name) { names[name] = true; }
        });
        return names;
    }

    // htmlFields 解析 data-write-html="descriptionHtml:description"：把裸 HTML 字段包成
    // {"html": "..."} 写到目标键（冒号后可省略，缺省目标键 = 源字段名）。
    //
    // 商品描述在库里是 jsonb {"html": "..."}，富文本控件提交的却是裸 HTML 字符串 —— 这层包装
    // 原来在 handler 里做（ProductsUpdate 的 json.Marshal），页面迁到基座后 handler 不再经手
    // 表单，包装必须跟着落到这里。不 trim：正文里的空白是有意义的排版。
    function htmlFields(form, submitter) {
        var spec = attrFrom(form, submitter, 'data-write-html');
        var pairs = {};
        if (!spec) { return pairs; }
        String(spec).split(',').forEach(function (rawPair) {
            var pair = String(rawPair || '').trim();
            if (!pair) { return; }
            var colon = pair.indexOf(':');
            var src = colon < 0 ? pair : pair.slice(0, colon).trim();
            var target = colon < 0 ? src : pair.slice(colon + 1).trim();
            if (src && target) { pairs[src] = target; }
        });
        return pairs;
    }

    function boolFields(form, submitter) {
        var spec = attrFrom(form, submitter, 'data-write-bool');
        var names = {};
        if (!spec) { return names; }
        String(spec).split(',').forEach(function (rawName) {
            var name = String(rawName || '').trim();
            if (name) { names[name] = true; }
        });
        return names;
    }

    // boolValue 把一个显式点名的字段值转成布尔：口径与 numericValue **完全对称** ——
    //   只认点名、只认明确写法、认不出就原样发。空串按 false（表单里没有 checkbox 那种
    //   「不勾」时 select 会发空串）；"maybe" 这类既不是真也不是假的取值原样发字符串，
    //   由严格 API 回 400，而不是被悄悄判成 false 做出「静默没生效」的动作。
    function boolValue(raw) {
        if (typeof raw !== 'string') { return raw; }
        var v = raw.trim().toLowerCase();
        if (v === '') { return false; }
        if (formBool(v)) { return true; }
        if (v === '0' || v === 'false' || v === 'off' || v === 'no') { return false; }
        return raw;
    }

    // applyBoolFields 只处理 payload 里**已经存在**的同名字段：声明了但没提交的字段不该凭空出现
    //（与 applyNumberFields 同口径）。
    function applyBoolFields(payload, names) {
        Object.keys(names).forEach(function (name) {
            if (!Object.prototype.hasOwnProperty.call(payload, name)) { return; }
            payload[name] = boolValue(payload[name]);
        });
        return payload;
    }

    function singlePayload(form, submitter) {
        // 点名的额外字段先写、id 最后写：与 bulk 档「ids 永远最后写」同一口径 ——
        // 声明出来的字段不可能覆盖掉这次操作的对象。
        var payload = extraPayload(form, submitter);
        var id = submitter && submitter.getAttribute("name") === "id" ? submitter : form.querySelector('[name="id"]');
        payload.id = id ? String(id.value || '').trim() : '';
        var force = form.querySelector('[name="force"]');
        if (force && formBool(force.value)) { payload.force = true; }
        return applyNumberFields(payload, numberFields(form, submitter));
    }

    // rowsSpecOf 解析 data-write-rows="<容器选择器>;<字段名>,<字段名>…[;<payload 键名>]"。
    //
    // 解析不出来（缺选择器 / 缺字段名）返回 null：那时按「没声明」处理，与不带该属性一样，
    // 而不是发一个形状可疑的行数组给端点。
    function rowsSpecOf(form, submitter) {
        var spec = attrFrom(form, submitter, 'data-write-rows');
        if (!spec) { return null; }
        var parts = String(spec).split(';');
        var selector = String(parts[0] || '').trim();
        var fields = String(parts[1] || '').split(',').map(function (name) {
            return String(name || '').trim();
        }).filter(function (name) { return !!name; });
        var key = String(parts[2] || '').trim() || 'rows';
        if (!selector || fields.length === 0) { return null; }
        return { selector: selector, fields: fields, key: key };
    }

    // rowElementsOf 收行容器（一个容器 = 一行）。
    function rowElementsOf(form, spec) {
        return Array.prototype.slice.call(form.querySelectorAll(spec.selector));
    }

    // rowFieldValue 取一行里的一个字段：先按**精确名**找，找不到再按「.后缀」找
    //（values[0].key 这种带索引的扁平名每行下标都不同，固定 name 匹配不到）。
    //
    // 命中多个时取**最后一个有效值**：hidden(0) + checkbox(1) 同名打底时，勾选后 checkbox 在
    // DOM 末尾，于是勾选得 "1"、未勾选时 checkbox 返回 null 而 hidden 的 "0" 留下。
    function rowFieldValue(box, name) {
        var suffix = '.' + name;
        var value = null;
        // 一次遍历同时认「精确名」与「.后缀名」：后者服务 values[0].key 这种带索引的扁平名。
        // 不用 [name$=".key"] 这类选择器 —— 名字里带点，属性选择器要靠引号内的字面点，
        // 而它的解析依赖浏览器实现细节；遍历比较是同一件事的直白写法。
        Array.prototype.forEach.call(box.querySelectorAll('[name]'), function (el) {
            var n = el.getAttribute('name') || '';
            if (n !== name && n.slice(-suffix.length) !== suffix) { return; }
            var v = fieldValue(el);
            if (v !== null) { value = v; }
        });
        return value;
    }

    // rowObjectsOf 按容器选择器收行：一个容器 = 一个对象，字段按点名的顺序取。
    //
    // 行内字段同样吃 data-write-number / data-write-bool 的点名：DOM 里的值都是字符串，
    // 而 dto 里 values[].sort 是 int、enabled 是 *bool —— 顶层转、行内不转，端点收到的
    // 是 "0" 与 "1"，严格解码整条 400（症状是「保存没反应」）。
    function rowObjectsOf(form, spec, numberNames, boolNames) {
        var rows = [];
        rowElementsOf(form, spec).forEach(function (box) {
            var row = {};
            var found = false;
            spec.fields.forEach(function (name) {
                var value = rowFieldValue(box, name);
                if (value === null) { return; }   // 未勾选的 checkbox/radio 不发（与顶层同口径）
                if (numberNames && numberNames[name]) {
                    value = numericValue(value);
                } else if (boolNames && boolNames[name]) {
                    value = boolValue(value);
                }
                row[name] = value;
                found = true;
            });
            if (found) { rows.push(row); }
        });
        return rows;
    }

    // rowNamesOf 收集行容器里的**全部**具名控件 —— 它们都不进顶层标量。
    //
    // 判据用容器而不是「点名的字段名」：容器里没点名的字段（values[0].id 这种）若漏到顶层，
    // 端点按严格白名单只能整条拒掉，而错在模板、症状却是「保存没反应」。
    function rowNamesOf(form, spec) {
        var names = {};
        if (!spec) { return names; }
        rowElementsOf(form, spec).forEach(function (box) {
            Array.prototype.forEach.call(box.querySelectorAll('[name]'), function (el) {
                var name = el.getAttribute('name');
                if (name) { names[name] = true; }
            });
        });
        return names;
    }

    // objectSpecOf 解析 data-write-object="<容器选择器>;<payload 键名>;<驱动控件名>[;<条件 name=值>]"。
    //
    // 它解决的是**嵌套对象**这一类：端点的身体里有 {ruleParams:{days:30}} 这种子对象，
    // 而 DOM 里只有扁平的 name=days。与 data-write-rows 的差别是「一个对象」而不是「一组行」，
    // 而且**哪些键进对象由驱动控件当前选项声明**（option 的 data-rule-keys）—— 同一个容器里的
    // 输入框服务不同子类型，多发一个键就会被服务端的白名单整条拒掉
    // （product 的 rejectUnknownRuleKeys：新品只要 days、价格区间只要 minPrice/maxPrice）。
    // 第四段是启用条件：不满足时整个对象**连同驱动字段**都不发
    //（手工标签带 ruleType 会被服务端明确拒绝，而类型是运行期选的，模板决定不了）。
    function objectSpecOf(form, submitter) {
        var spec = attrFrom(form, submitter, 'data-write-object');
        if (!spec) { return null; }
        var parts = String(spec).split(';');
        var selector = String(parts[0] || '').trim();
        var key = String(parts[1] || '').trim();
        var driver = String(parts[2] || '').trim();
        var when = String(parts[3] || '').trim();
        if (!selector || !key || !driver) { return null; }
        var cond = null;
        if (when) {
            var eq = when.indexOf('=');
            if (eq > 0) { cond = { name: when.slice(0, eq).trim(), value: when.slice(eq + 1).trim() }; }
        }
        return { selector: selector, key: key, driver: driver, when: cond };
    }

    // namedField 在容器里按**精确名**找一个具名控件（不用属性选择器：字段名可能带点）。
    function namedField(box, name) {
        var found = null;
        Array.prototype.forEach.call(box.querySelectorAll('[name]'), function (el) {
            if (el.getAttribute('name') === name) { found = el; }
        });
        return found;
    }

    // selectedOptionOf 取 select 的当前选中项（selectedOptions 缺失时退回逐个看 selected）。
    function selectedOptionOf(el) {
        if (el.selectedOptions && el.selectedOptions.length) { return el.selectedOptions[0]; }
        var opts = el.options || [];
        for (var i = 0; i < opts.length; i++) { if (opts[i].selected) { return opts[i]; } }
        return null;
    }

    // objectOf 把容器里「驱动控件当前选项点名的键」收成一个对象。
    //
    // 空值不发（与顶层同口径）：价格区间只填了一个价时另一个键根本不该出现 ——
    // 发 "" 会被解码成 0，于是命中集合整个错掉而没有任何报错。
    // objScalar 嵌套对象里的值按 JSON 数字发（能完整解析成十进制数才转）。
    //
    // ruleParams 这类**结构参数**的值天然是数字：发 "30" 会被服务端的 ruleInt 当字符串拒掉，
    // 症状是「保存没反应」。但也不能一律 Number()：认不出的取值（"abc"、"1e3"）原样发字符串，
    // 由严格 API 报参数错误 —— 页面替调用方猜一次类型，就是替它造了一个它没说的值。
    function objScalar(raw) { return floatValue(raw); }

    function objectOf(form, spec) {
        if (spec.when) {
            var cond = namedField(form, spec.when.name);
            var condValue = cond ? String(cond.value == null ? '' : cond.value).trim() : '';
            if (condValue !== spec.when.value) { return null; }
        }
        var box = form.querySelector(spec.selector);
        if (!box) { return null; }
        var driver = namedField(box, spec.driver) || namedField(form, spec.driver);
        if (!driver) { return null; }
        var option = selectedOptionOf(driver);
        var keys = option ? String(option.getAttribute('data-rule-keys') || '') : '';
        var out = {};
        keys.split(',').forEach(function (rawKey) {
            var name = String(rawKey || '').trim();
            if (!name) { return; }
            var el = namedField(box, name);
            if (!el) { return; }
            var value = fieldValue(el);
            if (value === null) { return; }
            if (typeof value === 'string' && value.trim() === '') { return; }
            out[name] = objScalar(value);
        });
        return {
            driver: spec.driver,
            driverValue: String(driver.value == null ? '' : driver.value).trim(),
            value: out
        };
    }

    // objectNamesOf 容器里的**全部**具名控件都不进顶层标量（与 rowNamesOf 同一条理由：
    // 同一个 name 交给端点两次，严格白名单只能整条请求拒掉）。
    function objectNamesOf(form, spec) {
        var names = {};
        if (!spec) { return names; }
        var box = form.querySelector(spec.selector);
        if (!box) { return names; }
        Array.prototype.forEach.call(box.querySelectorAll('[name]'), function (el) {
            var name = el.getAttribute('name');
            if (name) { names[name] = true; }
        });
        return names;
    }

    // isDisabled 判「这个控件会不会随表单提交」：浏览器**不发** disabled 控件，
    // 也不发 <fieldset disabled> 里的控件。基座是手动遍历，必须模拟同一条规则 ——
    // 否则「不跟踪库存」时那个 disabled 的 quantity 空输入会被发成 ""，
    // 严格解码的数值字段收到空串直接 400，而页面上只看到「保存失败」，
    // 字段本身看起来完全正常。
    function isDisabled(el) {
        if (el.disabled) { return true; }
        if (typeof el.closest !== 'function') { return false; }
        return !!el.closest('fieldset[disabled]');
    }

    // assertFieldDeclarations 检查「同一字段被两档声明点名」——语义不确定，必须当场炸。
    //
    // 以前的行为是「按流水线顺序走，后一个赢」：错序不报错，症状是某个字段悄悄按另一种
    // 形状发了出去（既点 array 又点 lines，得到 ["整段文本"]，合法、不报错、也不是想要的）。
    // skip-empty 不在这条检查里：它是**发送策略**（空就不发），与任何一档都可叠加 ——
    // defaultPrice「按小数发 + 留空就不发」是刻意组合。
    function assertFieldDeclarations(form, submitter) {
        var groups = [
            { attr: 'data-write-array', names: arrayFields(form, submitter) },
            { attr: 'data-write-lines', names: linesFields(form, submitter) },
            { attr: 'data-write-html', names: htmlFields(form, submitter) },
            { attr: 'data-write-number', names: numberFields(form, submitter) },
            { attr: 'data-write-float', names: floatFields(form, submitter) },
            { attr: 'data-write-bool', names: boolFields(form, submitter) }
        ];
        var seen = {};
        for (var i = 0; i < groups.length; i++) {
            var names = groups[i].names;
            for (var name in names) {
                if (!Object.prototype.hasOwnProperty.call(names, name)) { continue; }
                if (seen[name]) {
                    throw new Error('表单字段 "' + name + '" 被 ' + seen[name] + ' 与 ' +
                        groups[i].attr + ' 同时点名（端点 ' +
                        (attrFrom(form, submitter, 'data-write-api') || '未声明') +
                        '）：不知道该按哪种形状发');
                }
                seen[name] = groups[i].attr;
            }
        }
    }

    function createPayload(form, submitter) {
        // 声明冲突在组装之前就炸：发出去的身体只能是「用户点的那个意思」的一种解释。
        assertFieldDeclarations(form, submitter);
        var spec = rowsSpecOf(form, submitter);
        var objSpec = objectSpecOf(form, submitter);
        var rowNames = rowNamesOf(form, spec);
        var objNames = objectNamesOf(form, objSpec);
        var arrayNames = arrayFields(form, submitter);
        var skipNames = skipEmptyFields(form, submitter);
        var lineNames = linesFields(form, submitter);
        var htmlNames = htmlFields(form, submitter);
        var payload = {};
        Array.prototype.forEach.call(form.querySelectorAll('[name]'), function (el) {
            var name = el.getAttribute('name');
            if (!name || name === 'csrf_token') { return; }
            // disabled 控件不随表单提交（原生表单同理）：漏掉这一条，被 JS 关掉的输入
            // 会带着空值发给端点，而用户看到的是「字段明明填过」。
            if (isDisabled(el)) { return; }
            // 行内字段不是顶层字段：同一个 name 交给端点两次，严格白名单只能整条请求拒掉。
            if (rowNames[name] || objNames[name]) { return; }
            var tag = (el.tagName || '').toUpperCase();
            var type = (el.type || '').toLowerCase();
            if ((type === 'checkbox' || type === 'radio') && !el.checked) { return; }
            if (tag === 'SELECT' && el.multiple) {
                payload[name] = Array.prototype.slice.call(el.selectedOptions || []).map(function (o) { return o.value; });
                return;
            }
            // 点名的数组字段：同名多值逐个收集（多选 checkbox 组），恒为数组。
            if (arrayNames[name]) {
                if (!Array.isArray(payload[name])) { payload[name] = []; }
                payload[name].push(el.value);
                return;
            }
            payload[name] = el.value;
        });
        var numberNames = numberFields(form, submitter);
        var boolNames = boolFields(form, submitter);
        if (spec) { payload[spec.key] = rowObjectsOf(form, spec, numberNames, boolNames); }
        if (objSpec) {
            var obj = objectOf(form, objSpec);
            // 对象与驱动字段同进同出：条件不满足时两者都不发（只发一半 = 端点收到
            // 「有类型没参数」或「有参数没类型」，两种都是整条 400）。
            if (obj) {
                payload[objSpec.key] = obj.value;
                payload[obj.driver] = obj.driverValue;
            }
        }
        Object.keys(arrayNames).forEach(function (name) {
            if (!Object.prototype.hasOwnProperty.call(payload, name)) { payload[name] = []; }
        });
        // 空值过滤在类型转换之前：留着空串去转数字只会得到 ""，再被删掉，白绕一圈。
        Object.keys(skipNames).forEach(function (name) {
            if (!Object.prototype.hasOwnProperty.call(payload, name)) { return; }
            var v = payload[name];
            if (typeof v === 'string' && v.trim() === '') { delete payload[name]; }
        });
        // 按行切分在空值过滤之后：点名的多行字段空着时**发空数组**（清空），不是消失。
        Object.keys(lineNames).forEach(function (name) {
            if (!Object.prototype.hasOwnProperty.call(payload, name)) { return; }
            payload[name] = splitLines(payload[name]);
        });
        payload = applyNumberFields(payload, numberNames);
        payload = applyFloatFields(payload, floatFields(form, submitter));
        payload = applyBoolFields(payload, boolNames);
        // 富文本包装放最后：它读的是已经定型的标量，包装后原键消失（端点的键是 description）。
        Object.keys(htmlNames).forEach(function (src) {
            if (!Object.prototype.hasOwnProperty.call(payload, src)) { return; }
            var html = { html: String(payload[src] == null ? '' : payload[src]) };
            delete payload[src];
            payload[htmlNames[src]] = html;
        });
        return payload;
    }

    function formBool(raw) {
        raw = String(raw == null ? '' : raw).toLowerCase();
        return raw === '1' || raw === 'true' || raw === 'on' || raw === 'yes';
    }

    // fieldValue 读一个表单字段成 payload 值：与 createPayload 同口径（未勾选的
    // checkbox/radio 不发、多选 select 发数组），只是按点名的 name 取。
    function fieldValue(el) {
        // 与 createPayload 同一条规则：不提交的控件不给值（返回 null = 不进 payload）。
        if (isDisabled(el)) { return null; }
        var type = (el.type || '').toLowerCase();
        if ((type === 'checkbox' || type === 'radio') && !el.checked) { return null; }
        if ((el.tagName || '').toUpperCase() === 'SELECT' && el.multiple) {
            return Array.prototype.slice.call(el.selectedOptions || []).map(function (o) { return o.value; });
        }
        return el.value;
    }

    // extraPayload single / bulk 两档的额外字段：`data-write-extra="add,remove"` 里点名的那几个。
    //
    // **只认点名的字段**：批量档的身体是 {ids:[…]} + 该动作自己的参数，若把表单字段一股脑带上，
    // 端点会同时收到「批量形状」和一堆单条字段，只能靠猜。ids 与 csrf_token 一律跳过
    //（ids 由调用方最后写，csrf_token 走请求头）。
    function extraPayload(form, submitter) {
        var spec = attrFrom(form, submitter, 'data-write-extra');
        var payload = {};
        if (!spec) { return payload; }
        String(spec).split(',').forEach(function (rawName) {
            var name = String(rawName || '').trim();
            if (!name || name === 'ids' || name === 'csrf_token') { return; }
            var el = attr(submitter, 'name') === name ? submitter : form.querySelector('[name="' + name + '"]');
            if (!el) { return; }
            var value = fieldValue(el);
            if (value === null) { return; }
            payload[name] = value;
        });
        payload = applyNumberFields(payload, numberFields(form, submitter));
        return applyBoolFields(payload, boolFields(form, submitter));
    }

    // idsScope 解析 data-write-ids-scope：ids 装在**别的表单**里的场景。
    //
    // 现有唯一用例是商品列表的「按规则改价」抽屉：抽屉内容被克隆到 document 级，而勾选框留在
    // 页面那个批量表单里 —— 在抽屉表单内部找 [name=ids] 永远是空数组，表现是「点了应用调价，
    // 什么也没发生」（基座只回一句「没勾选任何项」，把人指向错误的原因）。
    //
    // 不声明时仍收本表单内的，与从前逐字一致；声明了但选择器打错 / 目标还没渲染时**退回本表单**，
    // 也不静默变成空数组。
    function idsScope(form, submitter) {
        var spec = attrFrom(form, submitter, 'data-write-ids-scope');
        if (!spec) { return form; }
        var root = global.document.querySelector(String(spec));
        return root || form;
    }

    // bulkIDs 收集已勾选的 id：trim → 去空 → 去重保序（与 shell.BulkIDs 的口径一致）。
    //
    // 只认两种载体：**已勾选的复选框**（表格行选择）与**隐藏域**（页面按筛选条件预置的 id 列表）。
    // 其它类型（文本框/搜索框）即使撞名也不是 id 载体：把用户敲进去的文本当成记录 id 发出去，
    // 请求长得完全合法，错得安静（服务端只会说「这个 id 不存在」）。
    function bulkIDs(form, submitter) {
        var name = attrFrom(form, submitter, 'data-write-ids') || 'ids';
        var seen = {}, out = [];
        Array.prototype.forEach.call(idsScope(form, submitter).querySelectorAll('[name="' + name + '"]'), function (el) {
            var type = (el.type || '').toLowerCase();
            if (type === 'checkbox' ? !el.checked : type !== 'hidden') { return; }
            var v = String(el.value == null ? '' : el.value).trim();
            if (v === '' || seen[v]) { return; }
            seen[v] = true;
            out.push(v);
        });
        return out;
    }

    // rowsOf 批量结果对应的表格行（成功删掉的要移除，避免「行还在但已经没了」）。
    function rowOfID(scope, id) {
        var box = scope.querySelector('[name="ids"][value="' + String(id).replace(/"/g, '\\"') + '"]');
        return box && box.closest ? box.closest('tr') : null;
    }

    function jsonOf(res) {
        return res.text().then(function (text) {
            try { return JSON.parse(text); } catch (e) { return null; }
        });
    }

    function request(api, payload) {
        try {
            return global.fetch(api, {
                method: 'POST',
                credentials: 'same-origin',
                headers: {
                    'Content-Type': 'application/json',
                    'X-CSRF-Token': csrfToken(),
                    'X-Requested-With': 'XMLHttpRequest'
                },
                body: JSON.stringify(payload)
            });
        } catch (err) {
            // Synchronous serialization failures use the same cleanup as rejected fetches.
            return Promise.reject(err);
        }
    }

    // busy 提交期间锁住提交按钮：一次点击只发一次请求（失败时恢复）。
    function lock(form, submitter, locked) {
        var btn = submitter || form.querySelector('button[type="submit"]');
        if (!btn) { return null; }
        if (locked) {
            if (btn.disabled) { return null; }   // 已经是锁定状态，本函数不负责恢复它
            btn.disabled = true;
            return btn;
        }
        btn.disabled = false;
        return null;
    }

    // inFlight 正在提交的表单集合：**一次点击只发一次请求**。
    //
    // 为什么按钮 disabled 不够：回车提交不经过按钮、确认框确认后的重派发也不看按钮状态 ——
    // 重复请求落在批量删除上是「照服务端的结果删两遍」，落在创建上就是两条记录。
    // 按钮 disabled 只是视觉反馈，真正的闸门在这里（按表单粒度，请求结束才释放）。
    var inFlight = new WeakSet();

    // beginWrite 进入提交：已有一个请求在飞时返回 false（调用方直接忽略这一次）。
    function beginWrite(form) {
        if (inFlight.has(form)) { return false; }
        inFlight.add(form);
        if (form.setAttribute) { form.setAttribute('aria-busy', 'true'); }
        return true;
    }

    // finishWrite 释放提交锁：成功（跳转 / 刷新）路径不调用，页面本来就要走了。
    function finishWrite(form, submitter) {
        inFlight.delete(form);
        if (form.removeAttribute) { form.removeAttribute('aria-busy'); }
        lock(form, submitter, false);
    }

    // okKeyOf 服务端结果里的「成功」计数键：删除是 data.deleted，非破坏动作是 data.updated
    // 或 data.added（来源批量加入：加进菜单的两件不同的事 —— 加与删不能共用计数键，
    // 否则「已加入 3 项」会被读成「已删除 3 项」）。
    function okKeyOf(outcome) {
        if (outcome === 'update') { return 'updated'; }
        if (outcome === 'add') { return 'added'; }
        return 'deleted';
    }

    // summaryText 拼摘要句：计数按 outcome 的口径读，文案里的 {deleted}/{updated}/{added}/{count}/{skipped}
    // 都填同一个「成功数」——模板写 {count} 或 {deleted} 都行，两种写法在各页都出现过。
    function summaryText(form, submitter, data, okKey) {
        var ok = Number(data && data[okKey]) || 0;
        var skipped = Number(data && data.skipped) || 0;
        var params = { deleted: ok, updated: ok, added: ok, skipped: skipped, count: ok + skipped };
        if (ok === 0 && skipped === 0) { return attrMsg(form, submitter, 'none-selected'); }
        if (skipped === 0) {
            // 非破坏动作只认自己的成品句：缺 data-msg-all-updated / data-msg-all-added 时
            // **不给句子**（setStatus 只渲染逐条原因与刷新入口），而不是回落到 all-deleted ——
            // 「成功了」与「把它从列表里删掉」不是同一件事，打标签成功却显示「已删除 N 位」
            // 正是 data-write-outcome 要分开的东西。
            var own = okKey === 'updated' ? 'all-updated' : (okKey === 'added' ? 'all-added' : 'all-deleted');
            return fill(attrMsg(form, submitter, own), params);
        }
        if (ok === 0) { return fill(attrMsg(form, submitter, 'all-skipped'), params); }
        return fill(attrMsg(form, submitter, 'partial'), params);
    }

    // failReasons 批量里每一条失败的原因：服务端给 `message`（当前请求语言成品句，
    // 删除被拒时还带引用明细），没有 message 时退回 text / id。
    //
    // **一条都不省**：曾经只显示前 3 条加一个省略号，被省掉的恰恰是操作者最需要的东西 ——
    // 「哪几个块没删掉、为什么」。全列不会撑破布局（结果区在页面里本来就可滚动），
    // 而漏报会让人反复重试同一个必然失败的操作。
    function failureLine(item) {
        var reason = String(item.message || item.text || '');
        var id = item.id === undefined || item.id === null ? '' : String(item.id);
        return id && reason ? id + ': ' + reason : (reason || id);
    }

    function failReasons(data, okKey) {
        var items = (data && data.items) || [];
        var rows = [];
        items.forEach(function (item) {
            if (!item || item[okKey]) { return; }
            rows.push(failureLine(item));
        });
        return { lines: rows, items: items };
    }

    function runBulk(form, submitter, outcome) {
        var okKey = okKeyOf(outcome);
        var ids = bulkIDs(form, submitter);
        // 本地就能判定的两种情况：给提示即可，但**必须释放提交锁** —— 否则表单会卡在
        // 「正在提交」，用户勾上再点反而没反应（本地返回没有请求，也就没有回调来解锁）。
        if (ids.length === 0) {
            setStatus(form, attrMsg(form, submitter, 'none-selected'), 'error');
            finishWrite(form, submitter);
            return;
        }
        if (ids.length > MAX_BULK_IDS) {
            setStatus(form, fillTooMany(attrMsg(form, submitter, 'too-many'), MAX_BULK_IDS, ids.length), 'error');
            finishWrite(form, submitter);
            return;
        }
        var payload = extraPayload(form, submitter);
        payload.ids = numberFields(form, submitter).ids ? ids.map(numericValue) : ids; // 批量主体最后写，数字转换仅认显式 ids 声明
        lock(form, submitter, true);
        request(apiOf(form, submitter), payload).then(jsonOf).then(function (body) {
            if (!body || body.code >= 400) {
                setStatus(form, (body && body.message) || attrMsg(form, submitter, 'failed'), 'error');
                finishWrite(form, submitter);
                return;
            }
            var data = body.data || {};
            if (outcome === 'review') {
                // ReviewResp 无 json tag：保留实际 Affected/Status，零改写同样是成功回执。
                setStatus(form, body.message || '', 'ok');
                var reviewBox = statusOf(form);
                if (reviewBox) {
                    if (Object.prototype.hasOwnProperty.call(data, 'Affected')) {
                        reviewBox.setAttribute('data-write-affected', String(data.Affected));
                    }
                    if (Object.prototype.hasOwnProperty.call(data, 'Status')) {
                        reviewBox.setAttribute('data-write-review-status', String(data.Status));
                    }
                    var reviewEntry = refreshEntry(form, submitter);
                    if (reviewEntry) { reviewBox.appendChild(reviewEntry); }
                }
                finishWrite(form, submitter);
                return;
            }
            if (outcome === "receipt") {
                var receiptReasons = [];
                ((data && data.items) || []).forEach(function (item) {
                    if (item && !item.updated && !item.noop && (item.message || item.text)) { receiptReasons.push(failureLine(item)); }
                });
                setStatus(form, body.message || "", (Number(data.skipped) || 0) > 0 ? "error" : "ok", receiptReasons);
                var receiptBox = statusOf(form);
                var receiptEntry = refreshEntry(form, submitter);
                if (receiptBox && receiptEntry) { receiptBox.appendChild(receiptEntry); }
                finishWrite(form, submitter);
                return;
            }
            var reasons = failReasons(data, okKey);
            // 只有破坏性动作才按服务端结果移除行：update（打标签/改状态）成功后行还在，
            // 删了会让人以为联系人没了，刷新才发现还在。
            if (okKey === 'deleted') {
                // 服务端是唯一的成功真源：按它的结果移除行，而不是按我们发出去的 ids。
                reasons.items.forEach(function (item) {
                    if (!item || !item.deleted) { return; }
                    // 行在 ids 的来源作用域里（跨表单场景下它不在本表单内）。
                    var row = rowOfID(idsScope(form, submitter), item.id);
                    if (row && row.parentNode) { row.parentNode.removeChild(row); }
                });
            }
            setStatus(form, summaryText(form, submitter, data, okKey), (Number(data.skipped) || 0) > 0 ? 'error' : 'ok', reasons.lines);
            var entry = refreshEntry(form, submitter);
            var box = statusOf(form);
            if (entry && box) { box.appendChild(entry); }
            finishWrite(form, submitter);
        }).catch(function () {
            // 网络层失败（断网 / 网关 5xx 落到 HTML）：输入与勾选一律不动，只点一句错。
            setStatus(form, attrMsg(form, submitter, 'failed'), 'error');
            finishWrite(form, submitter);
        });
    }

    function runSingle(form, submitter) {
        var payload = singlePayload(form, submitter);
        lock(form, submitter, true);
        request(apiOf(form, submitter), payload).then(function (res) {
            return jsonOf(res).then(function (body) { return { res: res, body: body }; });
        }).then(function (r) {
            if (!r.body || r.body.code >= 400 || !r.res.ok) {
                setStatus(form, (r.body && r.body.message) || attrMsg(form, submitter, 'failed'), 'error');
                finishWrite(form, submitter);
                return;
            }
            // 成功整页刷新：列表的派生列（影响面 / 待重建）由服务端重算，就地改 DOM 会同源不上。
            global.location.reload();
        }).catch(function () {
            setStatus(form, attrMsg(form, submitter, 'failed'), 'error');
            finishWrite(form, submitter);
        });
    }

    function runCreate(form, submitter) {
        var payload;
        try { payload = buildPayload(form, submitter, createPayload(form, submitter)); }
        catch (err) {
            // 组装失败的现场只进控制台（声明冲突 / 自定义 builder 抛错）：结果区给通用文案，
            // 否则一次模板写错会表现成「保存一直失败、原因不明」。
            if (global.console && global.console.error) {
                global.console.error('[json_write] 载荷组装失败', err);
            }
            setStatus(form, attrMsg(form, submitter, 'failed'), 'error');
            finishWrite(form, submitter);
            return;
        }
        lock(form, submitter, true);
        request(apiOf(form, submitter), payload).then(function (res) {
            return jsonOf(res).then(function (body) { return { res: res, body: body }; });
        }).then(function (r) {
            if (!r.body || r.body.code >= 400 || !r.res.ok) {
                // 失败：表单原地保留（不 reset、不重新渲染），只把原因放到结果区。
                setStatus(form, (r.body && r.body.message) || attrMsg(form, submitter, 'failed'), 'error');
                finishWrite(form, submitter);
                return;
            }
            var data = r.body.data || {};
            // 结论先存（下面的 reload / 跳转会立刻把结果区抹掉，这是它唯一的去处）。
            storeFlash(form, submitter, r.body.message || '');
            var nav = attrFrom(form, submitter, 'data-write-nav');
            if (nav) { global.location.assign(fill(nav, { id: data.id || '' })); return; }
            global.location.reload();
        }).catch(function () {
            setStatus(form, attrMsg(form, submitter, 'failed'), 'error');
            finishWrite(form, submitter);
        });
    }

    document.addEventListener('submit', function (e) {
        var form = e.target;
        if (!form || !form.hasAttribute) { return; }
        var submitter = e.submitter || null;
        // 这个按钮（或表单）没有声明端点 → 不接管，走原生提交。
        // 混合表单里「改状态」这类没有 JSON 端点的动作用的就是这条路（表单级也不声明，
        // 否则会被继承成 fetch）。回车提交没有 submitter，只认表单级声明。
        if (!apiOf(form, submitter)) { return; }
        // 属性还在 = 用户还没确认（ui/confirm.js 确认后会把 data-confirm 临时摘掉再重派发，
        // 见 confirm.js 的 submitConfirmed / activateConfirmed）：这一趟交给确认框，不发请求。
        if (hasConfirm(form, submitter)) { return; }
        e.preventDefault();
        // 已在提交中：忽略这一次（按钮 disabled 挡不住回车提交与确认后的连点）。
        if (!beginWrite(form)) { return; }
        var mode = attrFrom(form, submitter, 'data-write-mode') || 'single';
        var outcome = attrFrom(form, submitter, 'data-write-outcome') || 'delete';
        if (mode === 'bulk') { runBulk(form, submitter, outcome); return; }
        if (mode === 'create' || mode === 'form') { runCreate(form, submitter); return; }
        runSingle(form, submitter);
    }, false);

    // 无 JS 时这些控件是 hidden 的（模板里带 hidden + data-requires-js），有 JS 才放出来。
    // 不用 <noscript><style>：<style> 在 body 里不是合法流内容，而 hidden 是纯标记事实。
    //
    // 同时摘掉 disabled：整表提交的保存按钮在无 JS 时必须是**真的按不动**（浏览器对
    // 「默认提交按钮 disabled」的表单不做隐式提交，回车也不会把整表 POST 到一个不存在的页面上），
    // 所以模板把它写成 disabled + data-requires-js；只放出来不摘 disabled 会让「有 JS 也点不动」。
    // 上次写操作留下的结论（表单一成功就刷新，结果区留不住）：页面一落地就补写进结果槽。
    if (global.document.readyState === 'loading') {
        global.document.addEventListener('DOMContentLoaded', renderFlash, false);
    } else {
        renderFlash();
    }

    WBUI.register(function (scope) {
        WBUI.each(WBUI.$$('[data-requires-js]', scope), function (el) {
            if (!WBUI.markOnce(el, 'RequiresJS')) { return; }
            el.hidden = false;
            el.removeAttribute('hidden');
            if (el.disabled) { el.disabled = false; }
        });
    });
})(window);
