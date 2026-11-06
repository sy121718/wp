// 编辑器写操作：保存与发布只经宿主适配器；工具不认识具体应用的端点或回执形状。
import { meta, getHost } from '../core.js';

export const apiMethods = {
    async request(url, body) {
        var response = await fetch(url, {
            method: 'POST', credentials: 'same-origin',
            headers: getHost().csrfHeaders({ 'Content-Type': 'application/json' }),
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
    async persistDraft(savedDocument) {
        return getHost().save(this, savedDocument);
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
            // 宿主钩子只接收「本次提交的文档快照」：应用用它判断保存期间是否又有输入。
            getHost().afterSave(this, savedDocument);
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
        data = data || {};
        this.draftVersion = data.draftVersion || data.version || this.draftVersion;
        // 请求发出后的输入尚未持久化，不能被迟到的成功响应标记为已保存。
        var unchanged = savedDocument === JSON.stringify(this.doc);
        this.saveState = unchanged ? 'saved' : 'dirty';
        if (unchanged) this.clearBackup(); else this.backupDoc();
        getHost().onSaved(this, data);
        this.renderUI();
    },
    async publishFlow() {
        if (this.busy) return false;
        return getHost().publish(this);
    },
    buildAndPublish() {
        var self = this;
        if (this.busy) return;
        getHost().confirmPublish(this, function () { self.publishFlow(); });
    }
};
