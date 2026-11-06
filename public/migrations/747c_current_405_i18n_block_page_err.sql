-- 当前词条回补源；历史原文 405_i18n_block_page_err.sql 保持归档，不再执行。
-- ========================================
-- 405 — block 域「工作台保存块内容」失败出口收口所需的词条
--
-- 背景：`POST /admin/blocks/save-content` 是 **JSON 端点**（不是页面路由）：
--   调用方是工作台的 saveDraft（internal/templates/static/js/workbench/methods/api.js），
--   它对响应做 `r.json()`，再按 `j.code >= 400` 取 `j.message` 弹提示。而它的两条失败分支
--   返回的却是 `c.String(400, "参数不合法")` / `c.String(404, "全局块不存在")` 纯文本 ——
--   `r.json()` 直接 reject，落到 `.catch()`：用户只看到保存状态变红，**连一句提示都没有**。
--
-- 本批把这两处换成模块既有归口出口（paramBindFail / blockErrorStatus + blockErrorMessage），
-- 响应形状回到 JSON、文案回到 i18n key。这里只补 ErrBlockNotFound 的 en-US
-- （其余 block 存量 key 的缺口不在本批范围）；基础设施归口 key 已退役为 uiInternalErr，
-- 其全部语言由 747 安全迁移与 738 默认补种统一负责，本文件不再复制或播种旧 key。
--
-- 幂等：INSERT ... ON CONFLICT (item_key, lang) DO NOTHING；本批不修改任何既有词条的值。
-- ========================================

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrBlockNotFound', 'en-US', 'The global block does not exist', 404, 'block', 'block/inbound/http: 工作台保存块内容时块不存在（本批补 en-US）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
