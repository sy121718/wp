/* ai-models.js — 模型目录区的草稿行编辑与请求对象组装。
 *
 * 保存目录（POST /api/ai/provider/models/save）是明确的 JSON 输入：目录行是结构化对象，
 * 表单里的 modelId / modelDisplayName / modelContextWindow / modelMaxOutputTokens /
 * input_text_N / input_image_N 只是编辑草稿的载体。写基座按 data-write-builder
 * 找这里注册的组装器，在提交前把当前 DOM 转成 SaveModelsReq.models，发送仍由 HTMX 执行。
 *
 * 「加一行 / 移除一行」是**纯前端本地**动作：草稿行是用户还没保存的表单状态，
 * 改它在本地改 DOM 就够了；为它开一个接口既多一次往返，又会把「未保存」与「已保存」
 * 两种事实搅在一起。行序号与勾选状态也在这里算，服务端不再下发改好的索引。
 *
 * 行的结构由 provider_models.html 里的 <template id="tpl-ai-model-row"> 提供，
 * 文案在渲染期已按请求语言取好；本文件只克隆、填值、显隐，不持有任何句子。
 */
(function (global) {
    'use strict';

    var ui = global.WBUI;
    if (!ui || !ui.writeBase || typeof ui.writeBase.registerPayloadBuilder !== 'function') { return; }
    if (ui.aiModels) { return; }

    var ROW_TPL = 'tpl-ai-model-row';
    var HOST_SEL = '[data-ai-models-host]';

    function byId(id) { return document.getElementById(id); }
    function str(v) { return v === undefined || v === null ? '' : String(v); }

    function setField(root, name, value) {
        var nodes = root.querySelectorAll('[data-field="' + name + '"]');
        for (var i = 0; i < nodes.length; i++) {
            var node = nodes[i];
            if (node.tagName === 'INPUT' || node.tagName === 'SELECT' || node.tagName === 'TEXTAREA') {
                node.value = str(value);
            } else {
                node.textContent = str(value);
            }
        }
    }

    function setChecked(root, name, on) {
        var nodes = root.querySelectorAll('[data-check="' + name + '"]');
        for (var i = 0; i < nodes.length; i++) { nodes[i].checked = !!on; }
    }

    function buildRow(row, index) {
        var tpl = byId(ROW_TPL);
        if (!tpl) { return null; }
        var node = tpl.content.cloneNode(true).firstElementChild;
        if (!node) { return null; }

        // 索引是「表单里第几行」的锚点：提交时服务端按 index 取 input_text_N / input_image_N，
        // 所以勾选框名都要按当前重绘后的顺序重写。
        setField(node, 'id', row.id);
        setField(node, 'displayName', row.displayName);
        setField(node, 'contextWindow', row.contextWindow);
        setField(node, 'maxOutputTokens', row.maxOutputTokens);
        // 勾选状态由 InputTypes 现算：服务端只交事实，不再下发「该不该打勾」。
        var types = Array.isArray(row.inputTypes) ? row.inputTypes : [];
        setChecked(node, 'text', types.indexOf('text') >= 0);
        setChecked(node, 'image', types.indexOf('image') >= 0);

        reindex(node, index);
        return node;
    }

    // reindex 重写「表单里第几条」：提交时服务端按序号取 input_text_N / input_image_N，
    // 因此增删一行之后，后面所有行的字段名都要跟着改。
    function reindex(node, index) {
        var inputs = node.querySelectorAll('[data-index-name]');
        for (var i = 0; i < inputs.length; i++) {
            var base = inputs[i].getAttribute('data-index-name');
            inputs[i].setAttribute('name', base + '_' + index);
        }
    }

    function reindexAll(rowsHost) {
        var rows = rowsHost.querySelectorAll(':scope > .ai-model-row');
        for (var i = 0; i < rows.length; i++) { reindex(rows[i], i); }
    }

    // syncEmpty 切空态：一行都没有时露出提示，有行时藏起来。
    function syncEmpty(host) {
        var rowsHost = host.querySelector('[data-models-rows]');
        var empty = host.querySelector('[data-models-empty]');
        if (!rowsHost || !empty) { return; }
        var count = rowsHost.querySelectorAll(':scope > .ai-model-row').length;
        if (count === 0) { empty.removeAttribute('hidden'); } else { empty.setAttribute('hidden', ''); }
    }

    function addRow(host) {
        var rowsHost = host.querySelector('[data-models-rows]');
        if (!rowsHost) { return false; }
        var node = buildRow({ id: '', displayName: '', contextWindow: '', maxOutputTokens: '', inputTypes: [] },
            rowsHost.querySelectorAll(':scope > .ai-model-row').length);
        if (!node) { return false; }
        rowsHost.appendChild(node);
        syncEmpty(host);
        return true;
    }

    // removeRow 从本地摘掉一行。草稿行还没落库，删它就是删 DOM —— 不必先问服务端。
    //
    // 宿主必须在**摘掉这一行之前**取：一旦 removeChild，按钮就脱离了文档，
    // closest 再也走不到 [data-ai-models-host]。先摘后找的写法会让空态永远不刷新。
    function removeRow(button) {
        var row = button && button.closest ? button.closest('.ai-model-row') : null;
        if (!row || !row.parentNode) { return false; }
        var host = button.closest(HOST_SEL);
        var rowsHost = row.parentNode;
        rowsHost.removeChild(row);
        reindexAll(rowsHost);
        if (host) { syncEmpty(host); }
        return true;
    }

    // collectModels 把当前 DOM 里的草稿行读成 SaveModelsReq.models。
    //
    // 数字字段走 Number 并在空串/非数字时落 0：ModelEntry 的这两个字段是 int64，
    // 提交成字符串会绑定失败。勾选框按「有没有勾上」决定，不靠名字里带什么类型。
    // 模型 ID 为空的草稿行整行丢弃 —— 服务端要求有 ID，空行是用户加了还没填的。
    function collectModels(form) {
        var rowsHost = form.querySelector('[data-models-rows]');
        if (!rowsHost) { return []; }
        var rows = rowsHost.querySelectorAll(':scope > .ai-model-row');
        var out = [];
        for (var i = 0; i < rows.length; i++) {
            var row = rows[i];
            var id = valueOf(row, '[name="modelId"]');
            if (id === '') { continue; }
            // 勾选框一律按**行内语义标记**取，不拼 name="input_text_N"：
            // 序号只影响提交用的 name，取值不再依赖重排是否到位。
            var types = [];
            if (checkedOf(row, '[data-check="text"]')) { types.push('text'); }
            if (checkedOf(row, '[data-check="image"]')) { types.push('image'); }
            out.push({
                id: id,
                displayName: valueOf(row, '[name="modelDisplayName"]'),
                contextWindow: numOf(row, '[name="modelContextWindow"]'),
                maxOutputTokens: numOf(row, '[name="modelMaxOutputTokens"]'),
                inputTypes: types
            });
        }
        return out;
    }

    function valueOf(row, sel) {
        var node = row.querySelector(sel);
        return node ? str(node.value).trim() : '';
    }

    function numOf(row, sel) {
        var raw = valueOf(row, sel);
        if (raw === '') { return 0; }
        var n = Number(raw);
        return isFinite(n) ? n : 0;
    }

    function checkedOf(row, sel) {
        var node = row.querySelector(sel);
        return !!(node && node.checked);
    }

    // 点一次本地改一次：草稿行的增删没有接口，所以不走写基座，直接委托到各自的按钮。
    // 用 document 委托而不是加载时逐个 addEventListener —— 行是克隆出来的，
    // 加载时那几个节点里根本没有它们。
    document.addEventListener('click', function (event) {
        var target = event.target;
        if (!target || !target.closest) { return; }
        var remove = target.closest('[data-row-remove]');
        if (remove) {
            event.preventDefault();
            removeRow(remove);
            return;
        }
        var add = target.closest('[data-row-add]');
        if (add) {
            event.preventDefault();
            var host = add.closest(HOST_SEL);
            if (host) { addRow(host); }
        }
    });

    // 模型目录的 payload：模板上声明 data-write-builder="ai-provider-models" 后，
    // 写基座提交前会调这里，把草稿行转成 models[]。发送仍由 HTMX 执行。
    ui.writeBase.registerPayloadBuilder('ai-provider-models', function (form) {
        return {
            providerId: numOf(form, '[name="providerId"]'),
            version: numOf(form, '[name="version"]'),
            models: collectModels(form)
        };
    });

    global.WBUI.aiModels = {
        addRow: addRow, removeRow: removeRow, collectModels: collectModels
    };
}(window));
