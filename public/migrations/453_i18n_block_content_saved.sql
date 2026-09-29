-- 453 — block 模块「保存块内容」成功回执的文案。
--
-- 原先硬编码在 handler 里（`gin.H{"message": "已保存，关联页面将标记为待重建"}`），
-- 是该文件里唯一没走 response + enums 出口、也是英文界面上唯一说中文的一句。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；门槛判据逐条枚举本批自己的 item_key
-- （上界封闭）—— 用 LIKE 前缀或全库总量都不行（见 AGENTS.md §数据库·迁移）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.blocks.content.savedRebuild', 'zh-CN', '已保存，关联页面将标记为待重建', 200, 'admin', 'block content saved; related pages marked stale', 1, now(), now()),
('admin.blocks.content.savedRebuild', 'en-US', 'Saved. Related pages are marked for rebuild.', 200, 'admin', 'block content saved; related pages marked stale', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
