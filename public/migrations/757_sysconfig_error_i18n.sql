-- 757 · sysconfig 业务错误词条（ErrVersionRequired / ErrGroupNotFound / ErrVersionConflict）
--
-- 背景：sysconfig 的三个业务错误只声明了 key（enums 里 Err(key) 不内嵌文案），
-- 词库里却一直没有这三条。JSON 出口取词失败后按共享规则回落到「系统内部错误」，
-- 于是「配置分组不存在」「缺少版本号」「版本冲突」这类可修复的业务提示被说成系统故障。
-- ErrInvalidParam 是通用 key，058 已 seed，这里不重复。
--
-- 状态码与该模块 enums 的声明保持一致：400 / 400 / 409。
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— 运营改过的文案不会被回滚。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrVersionRequired', 'zh-CN', '缺少版本号，请刷新页面后重试', 400, 'error', 'internal/module/sysconfig/service/sysconfig.go: 乐观锁更新缺版本号', 1, now(), now()),
('ErrVersionRequired', 'en-US', 'Missing version; refresh the page and retry', 400, 'error', 'internal/module/sysconfig/service/sysconfig.go: 乐观锁更新缺版本号', 1, now(), now()),
('ErrGroupNotFound', 'zh-CN', '配置分组不存在', 400, 'error', 'internal/module/sysconfig/contract/sysconfig_service.go: ErrGroupNotFound', 1, now(), now()),
('ErrGroupNotFound', 'en-US', 'Config group not found', 400, 'error', 'internal/module/sysconfig/contract/sysconfig_service.go: ErrGroupNotFound', 1, now(), now()),
('ErrVersionConflict', 'zh-CN', '配置已被其他人修改，请刷新页面后重试', 409, 'error', 'internal/module/sysconfig/service/sysconfig.go: 乐观锁版本冲突', 1, now(), now()),
('ErrVersionConflict', 'en-US', 'This config was changed by someone else; refresh the page and retry', 409, 'error', 'internal/module/sysconfig/service/sysconfig.go: 乐观锁版本冲突', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
