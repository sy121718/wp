-- 226 · 数据规则配置校验词条（zh-CN 4 行 / en-US 4 行）
--
-- 背景：数据域白名单收口后，规则配置只能引用「该域声明过的字段」与「该字段声明过的操作符」，
--   越界不再静默落库，而是明确报错。这 4 个 key 让调用方知道究竟哪一项越界。
-- 归属：internal/module/admin/enums/admin_enums.go。
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，不是真相来源
--   （后台改过的词条不会被后续迁移覆盖）。
-- 为什么另起迁移而不塞进 058：058 的 ConditionSQL 以「全库 zh-CN 行数」为门槛，
--   存量库早已远超该阈值，往它里面补词条不会被执行。这里把判定限定在自己的 key 上。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrRuleConfigInvalid', 'en-US', 'Invalid data rule config', 400, 'error', 'internal/module/admin/enums/admin_enums.go', 1, now(), now()),
('ErrRuleConfigInvalid', 'zh-CN', '数据规则配置不合法', 400, 'error', 'internal/module/admin/enums/admin_enums.go', 1, now(), now()),
('ErrRuleFieldNotAllowed', 'en-US', 'Rule references a field outside the data domain white list', 400, 'error', 'internal/module/admin/enums/admin_enums.go', 1, now(), now()),
('ErrRuleFieldNotAllowed', 'zh-CN', '规则引用了数据域白名单之外的字段', 400, 'error', 'internal/module/admin/enums/admin_enums.go', 1, now(), now()),
('ErrRuleLogicNotAllowed', 'en-US', 'Condition group logic must be AND or OR', 400, 'error', 'internal/module/admin/enums/admin_enums.go', 1, now(), now()),
('ErrRuleLogicNotAllowed', 'zh-CN', '条件组合逻辑只允许 AND 或 OR', 400, 'error', 'internal/module/admin/enums/admin_enums.go', 1, now(), now()),
('ErrRuleOpNotAllowed', 'en-US', 'Rule uses an operator not declared for that field', 400, 'error', 'internal/module/admin/enums/admin_enums.go', 1, now(), now()),
('ErrRuleOpNotAllowed', 'zh-CN', '规则使用了该字段未声明的操作符', 400, 'error', 'internal/module/admin/enums/admin_enums.go', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
