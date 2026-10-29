// 工作台写操作：保存、构建和发布共用请求校验与状态收尾。
import { meta, csrfHeaders } from '../core.js';

export const apiMethods = {
    async request(url, body) {
        var response = await fetch(url, {
            method: 'POST', credentials: 'same-origin',
            headers: csrfHeaders({ 'Content-Type': 'application/json' }),
            body: JSON.stringify(body)
        });
        var result;
        try { result = await response.json(); }
        catch (_) { throw new Error('服务返回了无法识别的结果，请重试。'); }
        if (!response.ok || !result || result.code >= 400) {
            var error = new Error((result && result.message) || '操作失败，请重试。');
            error.status = response.status;
            error.data = result && result.data;
            throw error;
        }
        return result.data || {};
    },
    saveBody(savedDocument) {
        var spec = (meta.target && meta.target.saveBody) || {};
        var body = {};
        body[spec.idKey || 'id'] = meta.pageId;
        body[spec.documentKey || 'draftDocument'] = JSON.parse(savedDocument);
        if (spec.versionKey || !meta.target) body[spec.versionKey || 'expectedVersion'] = this.draftVersion;
        if (spec.pathKey && meta.draftPath !== undefined) body[spec.pathKey] = meta.draftPath;
        if (!meta.target) body.draftPath = meta.draftPath;
        Object.keys(spec.extras || {}).forEach(function (key) {
            var from = spec.extras[key];
            if (meta[from] !== undefined) body[key] = meta[from];
        });
        return body;
    },
    async persistDraft(savedDocument) {
        var target = meta.target;
        var url = target && target.save && target.save.path || '/api/' + (meta.saveBase || 'page') + '/draft/save';
        var body = this.saveBody(savedDocument);
        try { return await this.request(url, body); }
        catch (error) {
            // 独立文档切换仍由服务端判定，确认只用于既有实例契约。
            if (error.status === 409 && target && target.type === 'instance' &&
                window.confirm(error.message || '这次改动会让本商品转为独立文档，继续？')) {
                body.confirmDetach = true;
                return this.request(url, body);
            }
            throw error;
        }
    },
    async saveDraft() {
        if (this.busy) return false;
        var savedDocument = JSON.stringify(this.doc);
        this.busy = true;
        this.notice = '';
        this.renderUI();
        try {
            var data = await this.persistDraft(savedDocument);
            this.afterSave(data, savedDocument);
            return true;
        } catch (error) {
            this.saveState = 'error';
            this.setNotice(error.message || '网络连接失败，请重试；当前修改仍保留。');
            return false;
        } finally {
            this.busy = false;
            this.renderUI();
        }
    },
    afterSave(data, savedDocument) {
        this.draftVersion = data.draftVersion || this.draftVersion;
        // 请求发出后的输入尚未持久化，不能被迟到的成功响应标记为已保存。
        var unchanged = savedDocument === JSON.stringify(this.doc);
        this.saveState = unchanged ? 'saved' : 'dirty';
        if (unchanged) this.clearBackup(); else this.backupDoc();
        if (data.renderMode && data.renderMode !== meta.renderMode) {
            meta.renderMode = data.renderMode;
            var badge = document.getElementById('wb-instance-mode');
            if (badge) badge.textContent = '独立文档（只影响这个商品）';
        }
        this.renderUI();
    },
    async publishFlow() {
        if (this.busy) return false;
        if (meta.saveBase === 'block' || meta.saveBase === 'template') return this.saveDraft();
        var savedDocument = JSON.stringify(this.doc);
        this.busy = true;
        this.notice = '';
        this.renderUI();
        try {
            var data = await this.persistDraft(savedDocument);
            this.afterSave(data, savedDocument);
            await this.request('/api/page/build', { id: meta.pageId, expectedVersion: this.draftVersion });
            await this.request('/api/page/publish', { id: meta.pageId });
            this.setNotice(this.saveState === 'dirty' ? '已发布保存时的版本；后续修改尚未保存。' : '页面已发布');
            return true;
        } catch (error) {
            this.saveState = 'error';
            this.setNotice(error.message || '发布失败，请重试。');
            return false;
        } finally {
            this.busy = false;
            this.renderUI();
        }
    },
    buildAndPublish() {
        var self = this;
        if (this.busy) return;
        if (meta.saveBase === 'block' || meta.saveBase === 'template') return this.saveDraft();
        WBUI.confirm('将保存当前草稿并构建发布，确认？', function () { self.publishFlow(); },
            { title: '发布页面', ok: '保存并发布' });
    }
};
