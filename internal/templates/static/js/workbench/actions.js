// 动作目标只来自组件声明和本工程的已发布对象；作者不填写协议或API。
export function editorLabel(key, fallback) {
    var labels = document.getElementById('wb-editor-labels');
    return labels && labels.getAttribute('data-' + key) || fallback;
}

export function actionSpecs(node, declarations) {
    return ((declarations || {})[node.type] || []).filter(function (spec) {
        return spec.allowedKinds && spec.allowedKinds.some(function (kind) { return kind === 'page' || kind === 'presentation'; });
    });
}

export function publishedTargets(rows, kind, projectId, lang) {
    if (!Array.isArray(rows)) return [];
    return rows.flatMap(function (row) {
        if (!row || row.projectId !== projectId) return [];
        var publication = (row.publications || []).find(function (pub) {
            return pub.lang === lang && pub.artifactId && pub.artifactHash && pub.activePath;
        });
        return publication ? [{kind:kind, id:row.id, label:publication.activePath}] : [];
    });
}

export function applyActionTarget(self, node, key, value, choices) {
    if (value === 'keep') return;
    var selected = choices.find(function (choice) { return choice.kind + ':' + choice.id === value; });
    if (value && !selected) return;
    self.snapshot();
    node.actions = node.actions || {};
    if (!selected) delete node.actions[key];
    else node.actions[key] = selected.kind === 'page' ? {kind:'page',pageId:selected.id} : {kind:'presentation',instanceId:selected.id};
    if (!Object.keys(node.actions).length) delete node.actions;
    self.renderTree(); self.refreshCanvas(); self.renderUI();
}

export function renderActionTargets(ctx, meta) {
    if (ctx.self.tab !== 'content') return;
    var specs = actionSpecs(ctx.node, meta.componentActions);
    if (!specs.length || !meta.projectId) return;
    var frame = document.getElementById('wb-canvas');
    var lang = frame && frame.contentDocument && frame.contentDocument.documentElement.lang;
    var group = document.createElement('details');
    group.className = 'wb-control-group'; group.open = true;
    var summary = document.createElement('summary');
    summary.textContent = editorLabel('destinations', '跳转到'); group.appendChild(summary);
    ctx.panel.appendChild(group);
    if (!lang) {
        var pending = document.createElement('p'); pending.className = 'wb-empty';
        pending.textContent = editorLabel('preview-first', '画布就绪后可选择目标'); group.appendChild(pending); return;
    }
    var fields = specs.map(function (spec) {
        var field = document.createElement('div'); field.className = 'wb-field';
        var label = document.createElement('label');
        label.textContent = editorLabel('action-' + spec.key, '跳转到');
        var select = document.createElement('select'); select.id = 'wb-action-' + ctx.node.id + '-' + spec.key;
        select.setAttribute('aria-label', label.textContent); select.setAttribute('data-wb-action', spec.key); label.htmlFor = select.id;
        var fallback = document.createElement('option'); fallback.value = '';
        fallback.textContent = editorLabel('default-flow', '默认流程'); select.appendChild(fallback);
        var current = ctx.node.actions && ctx.node.actions[spec.key];
        var currentValue = current && (current.kind === 'page' ? 'page:' + current.pageId : current.kind === 'presentation' ? 'presentation:' + current.instanceId : 'keep');
        if (current) {
            var retained = document.createElement('option'); retained.value = currentValue;
            retained.textContent = currentValue === 'keep' ? editorLabel('current-flow', '当前区块内流程') : editorLabel('current-target', '当前已选目标');
            select.appendChild(retained); select.value = currentValue;
        }
        select.disabled = true; field.appendChild(label); field.appendChild(select); group.appendChild(field);
        return {spec:spec,select:select,currentValue:currentValue};
    });
    var message = document.createElement('p'); message.className = 'wb-empty'; message.setAttribute('role','status');
    message.textContent = editorLabel('targets-loading', '正在读取已发布目标…'); group.appendChild(message);
    var query = new URLSearchParams({projectId:meta.projectId,lang:lang});
    function read(path) {
        return fetch(path + '?' + query.toString(), {credentials:'same-origin',redirect:'error',headers:{Accept:'application/json'}}).then(function (response) {
            return response.json().catch(function () { throw new Error(editorLabel('targets-failed','目标读取失败，请重试')); }).then(function (body) {
                if (!response.ok || !body || (body.code !== 0 && body.code !== 200)) throw new Error(body && body.message || editorLabel('targets-failed','目标读取失败，请重试'));
                return body.data;
            });
        });
    }
    Promise.allSettled([read('/api/page/list'),read('/api/presentation/list')]).then(function (results) {
        if (!ctx.panel.contains(group) || ctx.self.findNode(ctx.node.id) !== ctx.node) return;
        var data = results.map(function (result) { return result.status === 'fulfilled' ? result.value : []; });
        var failures = results.filter(function (result) { return result.status === 'rejected'; });
        if (failures.length === results.length) throw failures[0].reason;
        var targets = publishedTargets(data[0],'page',meta.projectId,lang).concat(publishedTargets(data[1],'presentation',meta.projectId,lang));
        fields.forEach(function (field) {
            var choices = targets.filter(function (target) { return field.spec.allowedKinds.indexOf(target.kind) !== -1; });
            choices.forEach(function (choice) {
                var value = choice.kind + ':' + choice.id;
                var existing = Array.from(field.select.options).find(function (option) { return option.value === value; });
                var option = existing || document.createElement('option'); option.value = value;
                option.textContent = editorLabel(choice.kind, choice.kind === 'page' ? '页面' : '展示页') + ' · ' + choice.label;
                if (!existing) field.select.appendChild(option);
            });
            field.select.value = field.currentValue || ''; field.select.disabled = false;
            field.select.addEventListener('change', function () {
                if (!ctx.panel.contains(group) || ctx.self.findNode(ctx.node.id) !== ctx.node) return;
                applyActionTarget(ctx.self, ctx.node, field.spec.key, field.select.value, choices);
            });
        });
        message.textContent = failures.length ? editorLabel('targets-partial', '部分目标无法读取；仍可选择已列出的页面。') : targets.length ? '' : editorLabel('targets-empty', '尚无已发布目标，请先发布目标页面');
    }).catch(function (error) {
        if (!ctx.panel.contains(group)) return;
        message.textContent = error.message; message.setAttribute('role','alert');
        var retry = document.createElement('button'); retry.type = 'button'; retry.className = 'wb-btn wb-btn-ghost wb-btn-sm';
        retry.textContent = editorLabel('retry','重试'); retry.addEventListener('click', function () { ctx.self.syncInspector(); }); group.appendChild(retry);
    });
}
