/* 属性值行只编辑 DOM；写入仍由统一写基座提交。 */
(function (global) {
    'use strict';
    function snapshot(row) {
        var target = row.querySelector('template[name="values"][data-row]');
        if (!target) return;
        target.content.replaceChildren();
        row.querySelectorAll('[data-attribute-field]').forEach(function (source) {
            var field = document.createElement('input');
            field.type = 'hidden'; // js-base-skip: template 内仅供 JSON 序列化的隐藏字段。
            field.name = source.getAttribute('data-attribute-field');
            field.value = source.type === 'checkbox' ? String(source.checked) : source.value;
            target.content.appendChild(field);
        });
    }
    function update(event) {
        var row = event.target.closest('[data-attribute-value-row]');
        if (row) snapshot(row);
    }
    document.addEventListener('input', update);
    document.addEventListener('change', update);
    document.addEventListener('click', function (event) {
        var add = event.target.closest('[data-attribute-add]');
        if (add) {
            var host = add.closest('[data-attribute-values]');
            var fragment = host.querySelector('[data-attribute-prototype]').content.cloneNode(true);
            var row = fragment.querySelector('[data-attribute-value-row]');
            snapshot(row);
            host.querySelector('[data-attribute-value-list]').appendChild(fragment);
            row.querySelector('[data-attribute-field="label"]').focus();
            return;
        }
        var remove = event.target.closest('[data-attribute-remove]');
        if (remove) remove.closest('[data-attribute-value-row]').remove();
    });
    // 写基座先加载，并在 document 捕获阶段同步组装请求体；window 捕获先于
    // document，确保自动填充或直接赋值未触发 input/change 时也先更新快照。
    global.addEventListener('submit', function (event) {
        event.target.querySelectorAll('[data-attribute-value-row]').forEach(snapshot);
    }, true);
    if (global.WBUI && global.WBUI.register) {
        global.WBUI.register(function (scope) {
            (scope || document).querySelectorAll('[data-attribute-value-row]').forEach(function (row) {
                if (global.WBUI.markOnce(row, 'AttributeValues')) snapshot(row);
            });
        });
    }
})(window);
