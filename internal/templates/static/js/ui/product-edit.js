/* 商品主体编辑只收集复杂业务对象；请求和 HTML 交换由 HTMX 执行。 */
(function (global) {
    'use strict';
    var ui = global.WBUI;
    if (!ui || !ui.writeBase) return;

    ui.writeBase.registerPayloadBuilder('product-edit', function (form) {
        var fields = new FormData(form);
        var payload = {
            id: fields.get('id'),
            projectId: fields.get('projectId'),
            name: fields.get('name'),
            subtitle: fields.get('subtitle'),
            status: fields.get('status'),
            unit: fields.get('unit'),
            defaultImage: fields.get('defaultImage'),
            brandId: fields.get('brandId'),
            primaryCategoryId: fields.get('primaryCategoryId'),
            description: { html: fields.get('descriptionHtml') || '' },
            attributeIds: fields.getAll('attributeIds'),
            categoryIds: fields.getAll('categoryIds'),
            tagIds: fields.getAll('tagIds'),
            images: [],
            imageAlts: []
        };
        // 空 SKU 与路径沿用“不修改”；其余空文本和空集合明确提交清空。
        if (fields.get('sku')?.trim()) payload.sku = fields.get('sku');
        if (fields.get('slug')?.trim()) payload.slug = fields.get('slug');

        var price = form.elements.namedItem('defaultPrice');
        var weight = form.elements.namedItem('weight');
        // 只读取原生数值，不做金额计算；留空使用明确清空字段，不编码成零。
        if (price.value === '') payload.clearDefaultPrice = true;
        else payload.defaultPrice = price.valueAsNumber;
        if (weight.value === '') payload.clearWeight = true;
        else payload.weight = weight.valueAsNumber;
        if (price.value !== '' && !Number.isFinite(payload.defaultPrice) || weight.value !== '' && !Number.isFinite(payload.weight)) {
            throw new Error(price.validationMessage || weight.validationMessage);
        }

        form.querySelectorAll('[data-media-gallery] [data-gallery-url]').forEach(function (cell) {
            payload.images.push(cell.getAttribute('data-gallery-url'));
            payload.imageAlts.push(cell.querySelector('[data-gallery-alt]').value);
        });
        return payload;
    });
})(window);

/* 标签规则参数是结构化对象：只提供请求对象，响应仍由 Jet 输出。 */
(function (global) {
    'use strict';
    var ui = global.WBUI;
    if (!ui || !ui.writeBase) return;
    ui.writeBase.registerPayloadBuilder('product-tag', function (form) {
        var fields = new FormData(form);
        var payload = {
            projectId: fields.get('projectId') || '',
            name: fields.get('name') || '',
            slug: fields.get('slug') || '',
            kind: fields.get('kind') || 'manual',
            ruleType: fields.get('ruleType') || '',
            sort: Number(fields.get('sort') || 0)
        };
        if (fields.get('id')) payload.id = fields.get('id');
        var params = {};
        ['days', 'minPrice', 'maxPrice'].forEach(function (name) {
            var value = fields.get(name);
            if (value !== null && String(value).trim() !== '') params[name] = String(value).trim();
        });
        payload.ruleParams = payload.kind === 'rule' ? params : {};
        return payload;
    });
    ui.writeBase.registerPayloadBuilder('product-bulk-pricing', function (form) {
        var fields = new FormData(form);
        // 作用范围是列表里勾选的商品；抽屉在列表表单之外，ids 从列表范围收集。
        var scope = document.querySelector('[data-list-bulk]');
        var ids = [];
        (scope ? scope.querySelectorAll('input[name="ids"]:checked') : []).forEach(function (node) { ids.push(node.value); });
        return {
            projectId: fields.get('projectId') || '',
            ids: ids,
            note: fields.get('note') || '',
            ruleType: fields.get('ruleType') || '',
            multiplier: fields.get('multiplier') || '',
            amount: fields.get('amount') || '',
            margin: fields.get('margin') || '',
            rounding: fields.get('rounding') || ''
        };
    });
})(window);

/* 分类父级可为空：标准表单无法区分「留空」与「未提交」，用显式对象提交。 */
(function (global) {
    'use strict';
    var ui = global.WBUI;
    if (!ui || !ui.writeBase) return;
    ui.writeBase.registerPayloadBuilder('product-category', function (form) {
        var fields = new FormData(form);
        return {
            projectId: fields.get('projectId') || '',
            id: fields.get('id') || '',
            name: fields.get('name') || '',
            slug: fields.get('slug') || '',
            parentId: fields.get('parentId') || '',
            sort: Number(fields.get('sort') || 0),
            image: fields.get('image') || '',
            description: fields.get('description') || ''
        };
    });
})(window);
