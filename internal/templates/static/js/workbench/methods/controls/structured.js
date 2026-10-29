// 表格和通用表单的结构化内容编辑；直接维护现有 props，不引入第二份文档形状。
import { clone } from '../../core.js';
import { get, set, commit, heading, field, checkbox } from './base.js';

// 表头与每一行一起提交，一个结构动作对应一次撤销。
function updateProps(ctx, change) {
    var props = clone(ctx.node.props || {});
    change(props);
    commit(ctx, 'props', props);
}

function action(parent, label, run, disabled) {
    var button = document.createElement('button');
    button.type = 'button';
    button.className = 'wb-btn wb-btn-secondary wb-btn-sm';
    button.textContent = label;
    button.disabled = !!disabled;
    button.addEventListener('click', run);
    parent.appendChild(button);
    return button;
}

function tableColumns(props) {
    return Math.max((props.headers || []).length, ...(props.rows || []).map(function (row) { return row.length; }), 0);
}

function addTableColumn(props) {
    var count = tableColumns(props);
    if (props.headers && props.headers.length) props.headers.push('列' + (count + 1));
    props.rows = (props.rows || []).map(function (row) { return row.concat(''); });
    if (!count) props.headers = ['列1'];
}

function removeTableColumn(props, column) {
    if (props.headers) props.headers.splice(column, 1);
    (props.rows || []).forEach(function (row) { row.splice(column, 1); });
}

export function tablePanel(ctx) {
    heading(ctx, '表格内容');
    var props = ctx.node.props || {};
    var columns = tableColumns(props);
    var scroller = document.createElement('div');
    scroller.className = 'wb-table-editor';
    var grid = document.createElement('table');
    var head = document.createElement('thead');
    var top = document.createElement('tr');
    var spacer = document.createElement('th');
    spacer.textContent = '行'; top.appendChild(spacer);
    for (let col = 0; col < columns; col++) {
        var th = document.createElement('th');
        var input = document.createElement('input');
        input.type = 'text';
        input.value = (props.headers || [])[col] || '';
        input.placeholder = '列' + (col + 1);
        input.setAttribute('aria-label', '第' + (col + 1) + '列表头');
        input.addEventListener('change', function (event) {
            updateProps(ctx, function (next) {
                if (!next.headers || !next.headers.length) next.headers = Array.from({length: columns}, function (_, index) { return '列' + (index + 1); });
                next.headers[col] = event.target.value;
            });
        });
        th.appendChild(input);
        action(th, '删除第' + (col + 1) + '列', function () {
            updateProps(ctx, function (next) { removeTableColumn(next, col); });
            ctx.self.syncInspector();
        }, columns <= 1);
        top.appendChild(th);
    }
    head.appendChild(top); grid.appendChild(head);
    var body = document.createElement('tbody');
    (props.rows || []).forEach(function (row, rowIndex) {
        var tr = document.createElement('tr');
        var label = document.createElement('th');
        label.scope = 'row'; label.textContent = String(rowIndex + 1);
        action(label, '删除第' + (rowIndex + 1) + '行', function () {
            updateProps(ctx, function (next) { next.rows.splice(rowIndex, 1); });
            ctx.self.syncInspector();
        });
        tr.appendChild(label);
        row.forEach(function (value, column) {
            var td = document.createElement('td');
            var input = document.createElement('input');
            input.type = 'text'; input.value = value;
            input.setAttribute('aria-label', '第' + (rowIndex + 1) + '行第' + (column + 1) + '列');
            input.addEventListener('change', function (event) {
                commit(ctx, 'props.rows.' + rowIndex + '.' + column, event.target.value);
            });
            td.appendChild(input); tr.appendChild(td);
        });
        body.appendChild(tr);
    });
    grid.appendChild(body); scroller.appendChild(grid); ctx.panel.appendChild(scroller);
    var toolbar = document.createElement('div'); toolbar.className = 'wb-structured-actions';
    action(toolbar, '+ 添加行', function () {
        updateProps(ctx, function (next) {
            if (!tableColumns(next)) next.headers = ['列1'];
            next.rows = next.rows || [];
            next.rows.push(Array(tableColumns(next)).fill(''));
        });
        ctx.self.syncInspector();
    });
    action(toolbar, '+ 添加列', function () {
        updateProps(ctx, addTableColumn);
        ctx.self.syncInspector();
    });
    ctx.panel.appendChild(toolbar);
}

function updateFormFields(ctx, change) {
    updateProps(ctx, function (props) {
        props.fields = props.fields || [];
        change(props.fields);
    });
}

export function formPanel(ctx) {
    heading(ctx, '表单字段');
    var fields = get(ctx, 'props.fields') || [];
    fields.forEach(function (item, index) {
        var group = document.createElement('details');
        group.className = 'wb-form-field';
        group.open = index === (ctx.self._formEditingIndex || 0);
        var summary = document.createElement('summary');
        summary.textContent = (index + 1) + '. ' + (item.label || '未命名字段');
        group.appendChild(summary);
        group.addEventListener('toggle', function () { if (group.open) ctx.self._formEditingIndex = index; });
        var panel = document.createElement('div'); panel.className = 'wb-form-field-body'; group.appendChild(panel);
        var nested = { panel: panel, node: ctx.node, self: ctx.self };
        var path = 'props.fields.' + index;
        field(nested, '字段类型', path + '.type', 'select', [
            ['text', '单行文本'], ['email', '邮箱'], ['textarea', '多行文本'], ['select', '下拉选择'], ['checkbox', '复选框']
        ], function (_, value) {
            if (value === 'select' && !(get(nested, path + '.options') || []).length) set(nested, path + '.options', ['选项一']);
            ctx.self._formEditingIndex = index;
            ctx.self.syncInspector();
        });
        field(nested, '字段标签', path + '.label', 'input');
        field(nested, '提交字段名', path + '.name', 'input');
        checkbox(nested, '必填', path + '.required');
        if (item.type !== 'checkbox' && item.type !== 'select') field(nested, '占位提示', path + '.placeholder', 'input');
        if (item.type === 'select') {
            var label = document.createElement('label'); label.className = 'wb-field'; label.textContent = '选项（每行一个）';
            var options = document.createElement('textarea'); options.rows = 4;
            options.value = (item.options || []).join('\n');
            options.addEventListener('change', function () { commit(nested, path + '.options', options.value.split('\n').filter(function (value) { return value !== ''; })); });
            label.appendChild(options); panel.appendChild(label);
        }
        var tools = document.createElement('div'); tools.className = 'wb-structured-actions';
        function move(to) {
            updateFormFields(ctx, function (next) { var moved = next.splice(index, 1)[0]; next.splice(to, 0, moved); });
            ctx.self._formEditingIndex = to; ctx.self.syncInspector();
        }
        action(tools, '上移', function () { move(index - 1); }, index === 0);
        action(tools, '下移', function () { move(index + 1); }, index === fields.length - 1);
        action(tools, '删除字段', function () {
            updateFormFields(ctx, function (next) { next.splice(index, 1); });
            ctx.self._formEditingIndex = Math.max(0, index - 1); ctx.self.syncInspector();
        }, fields.length <= 1);
        panel.appendChild(tools); ctx.panel.appendChild(group);
    });
    action(ctx.panel, '+ 添加字段', function () {
        updateFormFields(ctx, function (next) {
            var number = next.length + 1;
            while (next.some(function (item) { return item.name === 'field' + number; })) number++;
            next.push({type: 'text', label: '新字段', name: 'field' + number});
            ctx.self._formEditingIndex = next.length - 1;
        });
        ctx.self.syncInspector();
    });
    var hint = document.createElement('p'); hint.className = 'wb-hint';
    hint.textContent = '提交字段名用于接收数据。发布前请填写提交地址，并测试接收服务。';
    ctx.panel.appendChild(hint);
}
