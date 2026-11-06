-- 748 · 导航批量写「整批被拒」的公共主消息（中英成对）。
--
-- 背景：导航批量删除与来源批量加入都是整批语义；多项请求失败时主消息必须是一句
-- 与具体条目无关的公共文案，逐条 id / 名称 / 原因只进 data.items。
-- 单条请求仍给该条的具体业务原因（ErrNotFound / ErrPathTaken / ErrInvalidParam 等）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING，不覆盖运营译文。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('navBatchReject', 'zh-CN', '整批未执行：有条目不能处理，请按明细修正后重试', 400, 'navigation', 'internal/module/navigation/service/navigation_bulk_reject.go', 1, now(), now()),
('navBatchReject', 'en-US', 'The batch was not executed. Correct the items that cannot be processed and try again.', 400, 'navigation', 'internal/module/navigation/service/navigation_bulk_reject.go', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
