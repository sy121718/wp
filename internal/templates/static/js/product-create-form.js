/* product-create-form.js — 商品建表单的「主体 SKU」增强（原内联在 products.html 页尾）。
   触发点两处，缺任一处都会让某条入口静默失去增强：
     · wbui:drawer-open —— 列表页抽屉形态（内容从 <template> 克隆进 document，只能在打开后取节点）；
     · DOMContentLoaded —— 新建整页 /admin/products/new（表单直接在文档里，没有抽屉事件）。
   背景：捆绑商品的主体 SKU 是**必填**的（服务端 ErrBundleSKURequired），但运营不该从零手打 ——
   打开抽屉 / 类型切到 bundle 时按商品段预填建议值 <商品段>_B，可改、可一键重新生成；
   变体商品保持原状：不 required、不预填，留空仍由服务端按 URL 段派生。
   幂等：同一个表单只增强一次（dataset 标记），重复触发不会叠加监听。 */
(function () {
    'use strict';

    // 商品段派生：与 Go 侧的 productCodeSegment 同口径 —— 逐字符 Unicode 大写，只保留 A-Z 与 0-9。
    // 多字符展开（ß 变 SS、ﬁ 变 FI）直接丢弃：Go 的 unicode.ToUpper 按单码点映射，
    // 展开前的字符本身非 ASCII，同样会被丢掉。
    function codeSegment(raw) {
        var text = String(raw == null ? '' : raw), out = '', i, up;
        for (i = 0; i < text.length; i++) {
            up = text.charAt(i).toUpperCase();
            if (up.length === 1 && ((up >= 'A' && up <= 'Z') || (up >= '0' && up <= '9'))) {
                out += up;
            }
        }
        return out;
    }

    function trim(value) {
        return String(value == null ? '' : value).replace(/^\s+|\s+$/g, '');
    }

    // 显隐用行内 display 而不是只挂 hidden：.btn 自带 display:inline-flex，
    // 作者样式会盖掉 [hidden] 的 UA 规则（那样按钮在变体商品下也一直可见）。
    function show(el, on) {
        if (!el) { return; }
        el.style.display = on ? '' : 'none';
        if (on) {
            el.removeAttribute('hidden');
        } else {
            el.setAttribute('hidden', 'hidden');
        }
    }

    function enhance(form) {
        if (!form || form.dataset.skuEnhanced === '1') { return; }
        var input = form.querySelector('[data-sku-input]');
        var typeSelect = form.querySelector('select[name="type"]');
        if (!input || !typeSelect) { return; }
        form.dataset.skuEnhanced = '1';

        var slugInput = form.querySelector('input[name="slug"]');
        var nameInput = form.querySelector('input[name="name"]');
        var regen = form.querySelector('[data-sku-regenerate]');
        var hint = form.querySelector('[data-sku-hint]');
        var manual = form.querySelector('[data-sku-manual]');
        var placeholderVariant = input.getAttribute('placeholder') || '';
        var placeholderBundle = input.getAttribute('data-sku-placeholder-bundle') || placeholderVariant;
        // 运营是否在这个字段上打过字。系统预填只在「没被打过字」时写入 ——
        // 覆盖用户手写的值比少一次预填糟糕得多；想回到建议值有「重新生成」按钮。
        var dirty = false;

        function suggestion() {
            // 与 Go 侧一致：URL 段填了就用它（派生不出商品段时也不回落名称），没填才用商品名；
            // 两者都派生不出 → 留空，由提示行要求手填。
            var slug = trim(slugInput && slugInput.value);
            var segment = codeSegment(slug !== '' ? slug : (nameInput ? nameInput.value : ''));
            return segment === '' ? '' : segment + '_B';
        }

        function apply(follow) {
            var bundle = typeSelect.value === 'bundle';
            var want = bundle ? suggestion() : '';
            input.required = bundle;
            input.setAttribute('aria-required', bundle ? 'true' : 'false');
            input.setAttribute('placeholder', bundle ? placeholderBundle : placeholderVariant);
            if (bundle && follow && !dirty) { input.value = want; }
            show(regen, bundle);
            show(hint, bundle);
            show(manual, bundle && want === '' && trim(input.value) === '');
            if (sourceSelect) {
                // 捆绑商品不存在于仓库：这个入口对 bundle 不成立（服务端同款硬拒兜底）。
                // 禁用后浏览器不提交这个字段，服务端把空值归一成「自己创建」—— 正是想要的语义。
                sourceSelect.disabled = bundle;
                if (bundle) { sourceSelect.value = 'custom'; }
            }
            applySource();
        }

        // —— SKU 编码：**可搜索下拉 + 可自由输入**（2026-09-19 口径，docs/14 §1.1）——
        // 前端只做两件不涉及业务规则的事：把候选按**勾选的仓库**过滤、把结果预览出来。
        // 「编码在该仓已存在 → 复用那一行」「新码 → 建行」都由服务端按 (仓, 裸码) 判定，
        // 前端不参与命名决策 —— 预览骗人比没有预览更糟。
        var sourceSelect = form.querySelector('[data-sku-source]');
        var warehouseChecks = Array.prototype.slice.call(form.querySelectorAll('[data-warehouse-check]'));
        var skuPick = form.querySelector('[data-warehouse-sku]');
        var candidateList = form.querySelector('[data-sku-candidates]');
        var externalInput = form.querySelector('[data-external-sku-input]');
        var trackSwitch = form.querySelector('[data-track-quantity]');
        var quantityInput = form.querySelector('[data-quantity-input]');
        var preview = form.querySelector('[data-sku-preview]');

        // 仓库候选（服务端只渲一次的那份机器可读清单）：按仓分组的 optgroup。
        function warehouseGroups() {
            if (!skuPick) { return []; }
            return Array.prototype.slice.call(skuPick.querySelectorAll('optgroup'));
        }

        // 勾选的仓（按**表内顺序**）：第一个即认领仓 —— 与服务端「取第一个」同一条规则。
        function checkedWarehouses() {
            return warehouseChecks.filter(function (box) { return box.checked; });
        }

        function claimWarehouse() {
            var checked = checkedWarehouses();
            if (checked.length > 0) { return checked[0]; }
            for (var i = 0; i < warehouseChecks.length; i++) {
                if (warehouseChecks[i].dataset.default === '1') { return warehouseChecks[i]; }
            }
            return null;
        }

        // 正在生效的仓集合：不勾任何仓 = 默认仓（与服务端的兜底一致）。
        function effectiveWarehouses() {
            var checked = checkedWarehouses();
            if (checked.length > 0) { return checked; }
            return warehouseChecks.filter(function (box) { return box.dataset.default === '1'; });
        }

        // 候选裸码表（大写 → {code, external}）：只收当前生效的仓。
        function candidateCodes() {
            var ids = {};
            effectiveWarehouses().forEach(function (box) { ids[box.value] = true; });
            var codes = {};
            warehouseGroups().forEach(function (group) {
                if (!ids[group.dataset.warehouse]) { return; }
                Array.prototype.slice.call(group.querySelectorAll('option')).forEach(function (option) {
                    var code = trim(option.value);
                    if (code !== '') {
                        codes[code.toUpperCase()] = { code: code, external: trim(option.dataset.external) };
                    }
                });
            });
            return codes;
        }

        // 把 <datalist> 的候选按生效的仓收窄：不生效的置 disabled（不可选，但仍可自由输入）。
        function syncCandidates() {
            if (!candidateList) { return; }
            var ids = {};
            effectiveWarehouses().forEach(function (box) { ids[box.value] = true; });
            Array.prototype.slice.call(candidateList.querySelectorAll('option')).forEach(function (option) {
                option.disabled = !ids[option.dataset.warehouse];
            });
        }

        // 预览「将要生成的主体 SKU」：拼法必须与服务端的 attachWarehousePrefix 同口径。
        // 那条规则是幂等的：编码已经以仓短码 + '_' 开头时不再重复拼接，
        // 朴素拼接会显示成 SZ_SZ_xxx，而服务端实际存的是 SZ_xxx。
        function previewContainerSKU(warehouseCode, skuCode) {
            var code = String(warehouseCode == null ? '' : warehouseCode).toUpperCase().replace(/^\s+|\s+$/g, '');
            var value = String(skuCode == null ? '' : skuCode);
            if (code !== '' && value.toUpperCase().indexOf(code + '_') !== 0) {
                return code + '_' + value;
            }
            return value;
        }

        function syncPreview() {
            if (!preview) { return; }
            var text = '';
            var value = trim(input && input.value);
            var bundle = typeSelect.value === 'bundle';
            if (!bundle && value !== '') {
                var claim = claimWarehouse();
                var claimCode = claim && claim.dataset.code ? claim.dataset.code : '';
                var known = candidateCodes()[value.toUpperCase()];
                if (known) {
                    // 已存在：服务端会复用那一行（不新建），顺带把该仓登记的外码带出来。
                    text = (preview.dataset.reuseLabel || '') + previewContainerSKU(claimCode, known.code);
                    if (known.external && externalInput && trim(externalInput.value) === '') {
                        externalInput.value = known.external;
                    }
                } else {
                    text = (preview.dataset.previewLabel || '') + previewContainerSKU(claimCode, value);
                }
            }
            preview.textContent = text;
            show(preview, text !== '');
        }

        // 数量开关（迁移 261）：不跟踪（无限）时数量框**禁用且留空** —— 绝不预填 0。
        // 0 是「明确没货」这个具体事实，只有运营自己打出来才算数。
        function syncQuantity() {
            if (!quantityInput) { return; }
            var tracked = !!(trackSwitch && trackSwitch.checked);
            quantityInput.disabled = !tracked;
            if (!tracked) { quantityInput.value = ''; }
        }

        function applySource() {
            syncCandidates();
            syncQuantity();
            syncPreview();
        }

        if (sourceSelect) {
            sourceSelect.addEventListener('change', function () {
                var fromWarehouse = sourceSelect.value === 'warehouse';
                if (input) {
                    // 「从仓库选」这条隐藏来路（接口 / 脚本仍在用）：主体 SKU 由服务端拼成，
                    // 手填没有意义，所以置只读并清空（服务端在这条路径上不读 sku 字段）。
                    input.readOnly = fromWarehouse;
                    if (fromWarehouse) { input.value = ''; }
                }
                if (skuPick) { skuPick.required = fromWarehouse; }
                applySource();
            });
        }
        warehouseChecks.forEach(function (box) {
            box.addEventListener('change', applySource);
        });
        if (trackSwitch) { trackSwitch.addEventListener('change', syncQuantity); }
        if (input) { input.addEventListener('input', syncPreview); }
        if (skuPick) {
            skuPick.addEventListener('change', function () {
                var option = skuPick.options[skuPick.selectedIndex];
                if (option && option.value && externalInput && trim(externalInput.value) === '') {
                    // 带入那条货登记的对方编码；没登记就用它自己的 SKU。
                    // 运营仍可改 —— 服务端以显式填的为准。
                    var external = trim(option.dataset.external);
                    externalInput.value = external !== '' ? external : trim(option.value);
                }
                syncPreview();
            });
        }

        // 打开抽屉先算一次（类型默认是变体商品：不预填、不 required）。
        apply(true);
        typeSelect.addEventListener('change', function () { apply(true); });
        input.addEventListener('input', function () {
            dirty = true;
            apply(false);
        });
        if (regen) {
            regen.addEventListener('click', function () {
                dirty = false;
                apply(true);
                input.focus();
            });
        }
        // 商品名 / URL 段改动时跟随建议值 —— 同样只在字段没被打过字时生效
        //（选了捆绑再补 URL 段，是这条路径最常见的用法）。
        function followSource() {
            if (typeSelect.value === 'bundle' && !dirty) { apply(true); }
        }
        if (slugInput) { slugInput.addEventListener('input', followSource); }
        if (nameInput) { nameInput.addEventListener('input', followSource); }
    }

    document.addEventListener('wbui:drawer-open', function (event) {
        var body = event.detail && event.detail.body;
        if (!body) { return; }
        enhance(body.querySelector('form[data-product-create-form]'));
    });
    // 新建整页形态：表单直接在文档里（没有抽屉打开事件），DOM 就绪后增强一次。
    document.addEventListener('DOMContentLoaded', function () {
        enhance(document.querySelector('form[data-product-create-form]'));
    });
})();
