/* ui/htmx_form_body.js — htmx 写表单的**请求体整形**（一处实现，全站生效）。
 *
 * 它替 ui/json_write.js 承担两件「formdata 与 JSON 语义不一样」的事：
 *
 *   ① 删空值键 —— JSON 客户端可以**省略**字段（省略 = 本次不改），HTML 表单做不到：
 *      `<input name="sort" type="number" value="">` 一定发 `sort=`，后端 `*int` 会被绑成
 *      `0`（不是 nil），于是「什么都没填」变成「把排序改成 0」—— 静默改数据。
 *
 *   ② 扁平字段 → 嵌套对象 —— 有些端点的身体里有 `{ruleParams:{days:30}}` 这种子对象，
 *      而 DOM 里只有扁平的 `name=days`。模板用 `data-write-object` 声明这件事。
 *
 * 实测确认过三种形态（独立探针，gin 同版本）：
 *   不发该键   → *int = nil  （本次不改）✅
 *   sort=      → *int = 0    （会写库）  ⚠️
 *   sort=0     → *int = 0    （正确）    ✅
 *
 * 为什么用 htmx:config:request 而不是 hx-params：hx-params 在 htmx 4 里**已删除**
 *（官方 whats-new 列出）。v4 的正解就是事件式配置 —— 在 config:request 里改 ctx.request.body。
 */
(function () {
    'use strict';

    if (!window.htmx || typeof window.htmx.on !== 'function') return;

    // 会被整理的动词。GET/HEAD 没有 body，不在此列。
    var WRITE_VERBS = { post: 1, put: 1, patch: 1, delete: 1 };

    // ── ② data-write-object 的规格解析 ──
    //
    // 格式："<容器选择器>;<payload 键名>;<驱动控件名>[;<条件 name=值>]"
    //
    // 例子（product 的标签表单）：
    //   data-write-object=".tag-rule-fields;ruleParams;ruleType;kind=rule"
    //   → 把 .tag-rule-fields 里、由 ruleType 当前选项点名的键收成 ruleParams 对象，
    //     仅当 kind=rule 时执行。
    //
    // 为什么「哪些键进对象」由**驱动控件的 option** 决定（data-rule-keys）而不是容器里的全部输入框：
    // 同一个容器里的输入框服务不同规则子类型，多发一个键会被服务端白名单整条拒掉
    //（product 的 rejectUnknownRuleKeys：新品只要 days、价格区间只要 minPrice/maxPrice）。
    function specOf(el) {
        var host = el.closest ? el.closest('[data-write-object]') : null;
        if (!host) return null;
        var parts = String(host.getAttribute('data-write-object') || '').split(';');
        var selector = String(parts[0] || '').trim();
        var key = String(parts[1] || '').trim();
        var driver = String(parts[2] || '').trim();
        var when = String(parts[3] || '').trim();
        if (!selector || !key || !driver) return null;
        var cond = null;
        if (when) {
            var eq = when.indexOf('=');
            if (eq > 0) cond = { name: when.slice(0, eq).trim(), value: when.slice(eq + 1).trim() };
        }
        return { host: host, selector: selector, key: key, driver: driver, when: cond };
    }

    function fieldNamed(scope, name) {
        var out = null;
        var els = scope.querySelectorAll('[name]');
        for (var i = 0; i < els.length; i++) {
            if (els[i].getAttribute('name') === name) out = els[i];
        }
        return out;
    }

    function selectedOption(el) {
        if (el.selectedOptions && el.selectedOptions.length) return el.selectedOptions[0];
        var opts = el.options || [];
        for (var i = 0; i < opts.length; i++) if (opts[i].selected) return opts[i];
        return null;
    }

    // 嵌套对象里的值按 JSON 数字发（能完整解析成十进制数才转）。
    //
    // ruleParams 这类结构参数的值天然是数字：发 "30" 会被服务端的 ruleInt 当字符串拒掉，
    // 症状是「保存没反应」。但也不一律 Number()：认不出的取值（"abc"、"1e3"）原样发字符串，
    // 由严格 API 报参数错误 —— 页面替调用方猜一次类型，就是替它造了一个它没说的值。
    function scalar(raw) {
        var s = String(raw == null ? '' : raw).trim();
        if (s === '') return null;
        if (!/^-?\d+(\.\d+)?$/.test(s)) return s;
        var n = Number(s);
        if (!isFinite(n) || Math.abs(n) > Number.MAX_SAFE_INTEGER) return s;
        return n;
    }

    // objectOf 把容器里「驱动控件当前选项点名的键」收成一个对象；返回 null 表示不发。
    function objectOf(body, spec) {
        if (spec.when) {
            var condEl = fieldNamed(spec.host, spec.when.name);
            var condValue = condEl ? String(condEl.value == null ? '' : condEl.value).trim() : '';
            if (condValue !== spec.when.value) return null;
        }
        var box = spec.host.querySelector(spec.selector);
        if (!box) return null;
        var driver = fieldNamed(box, spec.driver) || fieldNamed(spec.host, spec.driver);
        if (!driver) return null;

        var option = selectedOption(driver);
        var keys = option ? String(option.getAttribute('data-rule-keys') || '') : '';
        var out = {};
        keys.split(',').forEach(function (rawKey) {
            var name = String(rawKey || '').trim();
            if (!name) return;
            var el = fieldNamed(box, name);
            if (!el) return;
            var v = scalar(el.value);
            // 空值不进对象（与顶层同口径）：价格区间只填一个价时另一个键根本不该出现 ——
            // 发 "" 会被解码成 0，命中集合整个错掉而没有任何报错。
            if (v === null) return;
            out[name] = v;
        });
        return { driver: spec.driver, driverValue: String(driver.value == null ? '' : driver.value).trim(), value: out };
    }

    function dropEmpty(body) {
        var drop = [];
        body.forEach(function (value, key) {
            // File 永远保留：空文件输入根本不会出现在 FormData 里，这里只是防御。
            if (typeof value === 'string' && value.trim() === '') drop.push(key);
        });
        for (var i = 0; i < drop.length; i++) body.delete(drop[i]);
    }

    // applyObject 把嵌套对象写进 body，并删掉容器里那些**已进对象**的扁平键。
    //
    // 两条语义都是从 ui/json_write.js 的 objectNamesOf / objectOf 原样接过来的：
    //
    //   ① 容器里**全部**具名控件都不进顶层（与 conditions 无关）。
    //      同一个 name 交给端点两次（一次扁平、一次在对象里），严格白名单只能整条请求拒掉。
    //      注意这一条是**无条件**的 —— 即使下面的条件不满足、对象根本不发，
    //      容器里的字段也不该以扁平形态漏出去。
    //   ② 条件不满足时，整个对象**连同驱动字段**都不发。
    //      手工标签带 ruleType 会被服务端明确拒绝（product 的 normalizeTagShapeOnCreate），
    //      而类型是运行期选的，模板决定不了。
    function applyObject(body, spec) {
        var box = spec.host.querySelector(spec.selector);
        if (!box) return;

        // ① 先无条件清掉容器内的扁平键（含驱动控件本身）。
        var inBox = box.querySelectorAll('[name]');
        for (var i = 0; i < inBox.length; i++) {
            body.delete(inBox[i].getAttribute('name'));
        }
        body.delete(spec.driver);

        // ② 条件不满足 → 到此为止：对象与驱动字段都不发。
        var got = objectOf(body, spec);
        if (!got) return;

        body.set(spec.key, JSON.stringify(got.value));
        // 驱动字段（如 ruleType）单独再发一次：端点用它判「用哪条规则」。
        body.set(got.driver, got.driverValue);
    }

    window.htmx.on('htmx:config:request', function (evt) {
        var ctx = (evt && evt.detail && evt.detail.ctx) || {};
        var req = ctx.request || {};
        var method = String(req.method || '').toLowerCase();
        if (!WRITE_VERBS[method]) return;

        var body = req.body;
        // v4 的 body 是 FormData 或 URLSearchParams，两者都有 forEach / delete / set。
        if (!body || typeof body.forEach !== 'function' || typeof body.delete !== 'function') return;

        // 顺序要紧：先删顶层空值，再组装对象。
        //   · 反过来会出问题：applyObject 之后 body 里的 ruleParams 是 "{}"（非空串），
        //     本身不会被删；但驱动字段（ruleType）若为空会被 dropEmpty 删掉，
        //     而它是端点判「用哪条规则」的依据 —— 那种情况下整条请求本来就该被拒，
        //     少发一个键只会让错误信息更难读。
        //   · 更关键的是：dropEmpty 要作用在**原始表单值**上，才能在组装前把
        //     「没填的参数输入框」排除掉；组装时再判一次空值（scalar 返回 null）。
        dropEmpty(body);

        // 再组装嵌套对象（读容器里的原始值；它自己也跳过空值）。
        var src = ctx.sourceElement;
        var spec = src ? specOf(src) : null;
        if (spec) applyObject(body, spec);
    });
})();
