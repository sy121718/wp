// editor-app/host.js — 应用宿主适配器：端点、权限、业务面板与保存/发布宿主操作。
// 纯工厂，无模块副作用：应用在启动时把依赖显式传入，Node 测试可直接求值。
export function createEditorHost(deps) {
    var meta = deps.meta || {};
    var request = deps.request;
    var csrfHeaders = deps.csrfHeaders;
    var normalizeMedia = deps.normalizeMedia || function (v) { return String(v == null ? '' : v); };
    var openBusinessPreset = deps.openBusinessPreset;

    function requestPreviewURL(path, state) {
        var url = new URL(path, window.location.href);
        if (state && state !== 'default') url.searchParams.set('previewState', state);
        else url.searchParams.delete('previewState');
        return url.pathname + url.search;
    }

    function saveBody(wb, savedDocument) {
        var spec = (meta.target && meta.target.saveBody) || {};
        var body = {};
        body[spec.idKey || 'id'] = meta.pageId;
        body[spec.documentKey || 'draftDocument'] = JSON.parse(savedDocument);
        if (spec.versionKey || !meta.target) body[spec.versionKey || 'expectedVersion'] = wb.draftVersion;
        if (spec.pathKey && meta.draftPath !== undefined) body[spec.pathKey] = meta.draftPath;
        if (!meta.target) body.draftPath = meta.draftPath;
        Object.keys(spec.extras || {}).forEach(function (key) {
            var from = spec.extras[key];
            if (meta[from] !== undefined) body[key] = meta[from];
        });
        return body;
    }

    function safeReturnURL() {
        var path = String(meta.returnUrl || '').trim();
        if (!path || path.length > 512 || path[0] !== '/' || path.indexOf('//') !== -1 || /[\\\x00-\x1f\x7f]/.test(path)) return '';
        var target = new URL(path, window.location.href);
        return target.origin === window.location.origin ? target.href : '';
    }

    function previewRequestURL(path, state) {
        return requestPreviewURL(path, state);
    }

    var host = {
        csrfHeaders: csrfHeaders,
        normalizeAssetURL: normalizeMedia,
        blocks: function () { return meta.blocks || []; },
        presets: function () { return meta.presets || []; },
        presetIdentity: function (preset) {
            return preset.presetKey ? 'business:' + preset.presetKey + '@' + preset.presetVersion : preset.id;
        },
        openBusinessPreset: function (wb, preset) { return openBusinessPreset(wb, preset); },
        cloneBlock: function (block) {
            return request('/api/block/clone', { id: block.id }).then(function (data) {
                return data && data.document ? data.document.root : [];
            });
        },
        structureSlots: function () {
            var current = (deps.currentDoc ? deps.currentDoc() : null) || { settings: {} };
            var struct = (current.settings && current.settings.structure) || {};
            var themeStruct = meta.themeSettings || {};
            var slots = [
                { label: '页眉', key: 'headerBlockId', kind: 'header' },
                { label: '页脚', key: 'footerBlockId', kind: 'footer' }
            ];
            var out = [];
            slots.forEach(function (s) {
                var id = struct[s.key] || themeStruct[s.key] || '';
                if (!id) return;
                var blk = (meta.blocks || []).find(function (b) { return b.id === id; });
                out.push({ label: s.label, id: id, name: blk ? blk.name : id.slice(0, 8) + '…' });
            });
            return out;
        },
        openBlockEditor: function (id) { window.open('/workbench?block=' + encodeURIComponent(id), '_blank'); },
        slotFacts: function (wb, slot) {
            var labels = { header: '页眉', footer: '页脚', announcement: '公告条' };
            var label = labels[slot.slot] || slot.slot;
            var refName = '';
            (meta.blocks || []).forEach(function (b) { if (b.id === slot.ref) refName = b.name; });
            var isTemplate = slot.refKind === 'template';
            return {
                label: label, ref: slot.ref, refName: refName, degradable: !!slot.degradable,
                editURL: (isTemplate ? '/workbench?template=' : '/workbench?block=') + encodeURIComponent(slot.ref) +
                    '&returnUrl=' + encodeURIComponent(location.pathname + location.search),
                editLabel: isTemplate ? '结构模板' : '全局块'
            };
        },
        fetchInspector: function (wb, node) {
            var body = new URLSearchParams();
            body.set('nodeId', node.id);
            body.set('document', JSON.stringify(wb.doc));
            body.set('tab', '');
            if (meta.projectId) body.set('projectId', meta.projectId);
            return fetch('/workbench/inspector', {
                method: 'POST', credentials: 'same-origin',
                headers: csrfHeaders({ 'Content-Type': 'application/x-www-form-urlencoded' }),
                body: body.toString()
            }).then(function (r) { if (!r.ok) throw new Error('面板请求失败'); return r.text(); });
        },
        fetchOutline: function (wb) {
            var body = new URLSearchParams();
            body.set('document', JSON.stringify(wb.doc));
            body.set('selectedId', wb.selectedId || '');
            body.set('filter', wb.filter || '');
            return fetch('/workbench/outline', {
                method: 'POST', credentials: 'same-origin',
                headers: csrfHeaders({ 'Content-Type': 'application/x-www-form-urlencoded' }),
                body: body.toString()
            }).then(function (r) { if (!r.ok) throw new Error('outline'); return r.text(); });
        },
        createNavigation: function (wb, item) {
            if (!meta.projectId) return Promise.reject(new Error('当前文档没有站点工程上下文，无法新建菜单项。'));
            var body = new URLSearchParams();
            body.set('projectId', meta.projectId);
            body.set('title', item.title);
            body.set('path', item.path);
            body.set('kind', item.kind);
            return fetch('/workbench/navigation/create', {
                method: 'POST', credentials: 'same-origin',
                headers: csrfHeaders({ 'Content-Type': 'application/x-www-form-urlencoded' }),
                body: body.toString()
            }).then(function (r) { return r.json(); }).then(function (j) {
                if (!j || (j.code && j.code >= 400) || !j.data) throw new Error((j && j.message) || '新建失败，请稍后重试。');
                return j.data;
            });
        },
        uploadURL: '/api/media/upload',
        collections: function () {
            return fetch('/api/content/collections', { credentials: 'same-origin', headers: { Accept: 'application/json' } })
                .then(function (r) { return r.json(); })
                .then(function (j) { return (j && j.data && j.data.collections) || []; });
        },
        loadActionTargets: function (wb, lang) {
            var query = new URLSearchParams({ projectId: meta.projectId, lang: lang, actionTargetsOnly: 'true' });
            function read(path) {
                return fetch(path + '?' + query.toString(), { credentials: 'same-origin', redirect: 'error', headers: { Accept: 'application/json' } })
                    .then(function (response) {
                        return response.json().then(function (body) {
                            if (!response.ok || !body || (body.code !== 0 && body.code !== 200)) throw new Error((body && body.message) || '目标读取失败，请重试');
                            return body.data;
                        });
                    });
            }
            return Promise.allSettled([read('/api/page/list'), read('/api/presentation/list')]).then(function (results) {
                var failures = results.filter(function (r) { return r.status === 'rejected'; });
                if (failures.length === results.length) throw failures[0].reason;
                var data = results.map(function (r) { return r.status === 'fulfilled' ? r.value : []; });
                function published(rows, kind) {
                    return (rows || []).flatMap(function (row) {
                        if (!row || row.projectId !== meta.projectId) return [];
                        var publication = (row.publications || []).find(function (pub) {
                            return pub.lang === lang && pub.artifactId && pub.artifactHash && pub.activePath;
                        });
                        return publication ? [{ kind: kind, id: row.id, label: publication.activePath }] : [];
                    });
                }
                return { targets: published(data[0], 'page').concat(published(data[1], 'presentation')), failures: failures.length };
            });
        },
        save: function (wb, savedDocument) {
            var target = meta.target;
            var url = target && target.save && target.save.path;
            if (!url) throw new Error('编辑器未提供保存端点');
            var body = saveBody(wb, savedDocument);
            return request(url, body).catch(function (error) {
                if (error.status === 409 && target && target.type === 'instance' &&
                    window.confirm(error.message || '这次改动会让本商品转为独立文档，继续？')) {
                    body.confirmDetach = true;
                    return request(url, body);
                }
                throw error;
            });
        },
        afterSave: function (wb, savedDocument) {
            if (!meta.target || meta.target.type !== 'block' || savedDocument !== JSON.stringify(wb.doc)) return;
            var href = safeReturnURL();
            if (href) window.location.assign(href);
        },
        onSaved: function (wb, data) {
            if (data && data.renderMode && data.renderMode !== meta.renderMode) {
                meta.renderMode = data.renderMode;
                var badge = document.getElementById('wb-instance-mode');
                if (badge) badge.textContent = '独立文档（只影响这个商品）';
            }
        },
        publish: function (wb) {
            var savedDocument = JSON.stringify(wb.doc);
            wb.busy = true;
            wb.notice = '';
            wb.renderUI();
            return wb.persistDraft(savedDocument).then(function (data) {
                wb.afterSave(data, savedDocument);
                return request('/api/page/build', { id: meta.pageId, expectedVersion: wb.draftVersion });
            }).then(function () {
                return request('/api/page/publish', { id: meta.pageId });
            }).then(function () {
                wb.setNotice(wb.saveState === 'dirty' ? '已发布保存时的版本；后续修改尚未保存。' : '页面已发布');
                return true;
            }).catch(function (error) {
                wb.saveState = 'error';
                wb.setNotice(error.message || '发布失败，请重试。');
                return false;
            }).finally(function () {
                wb.busy = false;
                wb.renderUI();
            });
        },
        confirmPublish: function (wb, run) {
            if (meta.saveBase === 'block' || meta.saveBase === 'template') return wb.saveDraft();
            window.WBUI.confirm('将保存当前草稿并构建发布，确认？', run, { title: '发布页面', ok: '保存并发布' });
        },
        submitPreview: function (wb, frame) {
            var form = document.getElementById('wb-preview-form');
            if (!form) {
                form = document.createElement('form');
                form.id = 'wb-preview-form';
                form.method = 'POST';
                form.target = frame.name || 'wb-canvas';
                form.style.display = 'none';
                ['id', 'expectedVersion', 'draftDocument', 'csrf_token', 'projectId', 'instance', 'entityType', 'entityId'].forEach(function (name) {
                    var el = document.createElement('input'); el.name = name; form.appendChild(el);
                });
                document.body.appendChild(form);
            }
            if (meta.saveBase === 'template' || meta.saveBase === 'instance') {
                form.action = requestPreviewURL('/workbench/template/preview', wb.previewState);
                form.elements.id.value = meta.saveBase === 'instance' ? '' : meta.pageId;
                form.elements.instance.value = meta.saveBase === 'instance' ? meta.pageId : '';
                form.elements.entityType.value = meta.entityType || '';
                form.elements.entityId.value = meta.entityId || '';
            } else {
                form.action = requestPreviewURL(meta.saveBase === 'block' ? '/workbench/block/preview' : '/workbench/preview', wb.previewState);
                form.elements.id.value = meta.pageId;
                form.elements.expectedVersion.value = wb.draftVersion;
            }
            form.elements.projectId.value = meta.projectId || '';
            form.elements.draftDocument.value = JSON.stringify(wb.doc);
            form.elements.csrf_token.value = deps.csrfToken ? deps.csrfToken() : '';
            form.submit();
        },
        fetchPreview: function (wb) {
            var body = new URLSearchParams();
            var url;
            if (meta.saveBase === 'template' || meta.saveBase === 'instance') {
                url = requestPreviewURL('/workbench/template/preview', wb.previewState);
                body.set('id', meta.saveBase === 'instance' ? '' : meta.pageId);
                if (meta.saveBase === 'instance') body.set('instance', meta.pageId);
                body.set('entityType', meta.entityType || '');
                body.set('entityId', meta.entityId || '');
                body.set('projectId', meta.projectId || '');
            } else {
                url = requestPreviewURL(meta.saveBase === 'block' ? '/workbench/block/preview' : '/workbench/preview', wb.previewState);
                body.set('id', meta.pageId);
                body.set('expectedVersion', wb.draftVersion);
                body.set('projectId', meta.projectId || '');
            }
            body.set('draftDocument', JSON.stringify(wb.doc));
            body.set('csrf_token', deps.csrfToken ? deps.csrfToken() : '');
            return fetch(url, {
                method: 'POST', credentials: 'same-origin',
                headers: csrfHeaders({ 'Content-Type': 'application/x-www-form-urlencoded' }),
                body: body.toString()
            }).then(function (res) { if (!res.ok) return Promise.reject(new Error('preview ' + res.status)); return res.text(); });
        }
    };
    return host;
}
