-- 269 · navigation 内部错误归口文案（审计 CQ-009）。
--
-- 背景：navigation 的 5 个 JSON 接口（Create / Update / Get / List / Delete）此前都是
-- `response.ErrorWithMessage(c, navigationErrorStatus(err), err.Error())`，而
-- navigationErrorStatus 的 default 分支是 500 —— service 上抛的 PostgreSQL 原文
-- （表名、唯一约束名、SQLSTATE）会随 500 原样直出给前端。
-- 现在统一走 internal/module/navigation/inbound/http/navigation_err.go 的助手：
-- 命中本模块 enums 白名单（NavigationFacingMessages）→ 原样返回；否则记结构化日志
-- （带场景与 user_id）并返回本词条作为对外归口文案。细节只留在日志里。
--
-- 常量值即 key（internal/module/navigation/enums/navigation_enums.go 的 ErrInternal）。
-- 值带模块前缀是刻意的：sys_i18n 主键是 (item_key, lang)，裸 key "ErrInternal" 已被
-- admin 批占用（迁移 268）。
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('navigation.err.internal', 'zh-CN', '操作失败，请稍后重试', 500, 'error', 'internal/module/navigation/enums/navigation_enums.go', 1, now(), now()),
    ('navigation.err.internal', 'en-US', 'Operation failed, please try again later', 500, 'error', 'internal/module/navigation/enums/navigation_enums.go', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
