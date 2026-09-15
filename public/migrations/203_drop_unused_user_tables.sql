-- 203 · 删除开发阶段用不上的 user 表（会话台账 + 三张「预留给将来」的空表）。
--
-- 删除对象与理由：
--   * user_sessions —— 访客会话与登录设备台账改为**全部在 Redis**：会话状态本来就在 Redis
--     （判断「这个请求算不算已登录」必须走它），台账只是设备视图的第二份真源，却要额外维护
--     保留期声明 + 每日清理任务。设备视图现由 Redis ZSET 索引支撑
--     （gwp:userauth:user:<userID>:sess），见 internal/module/user/service/user_session_store.go。
--   * user_app_passwords —— 对标 WP 的 application passwords（给 API 访问）。本系统没有面向
--     访客的 API 认证面（访客只能经会话操作自己的账号），零消费方。
--   * user_meta —— 「只给插件」的 key-value。当前插件体系没有用户侧扩展点，且它自己的注释
--     就写着「核心功能一旦依赖它就会长出第二张 wp_usermeta」。
--   * user_oauth_bindings —— 第三方登录绑定。没有任何 OAuth provider，pkg/oauth 也不存在；
--     等真要接 provider 时再加一条迁移，成本与当初一样。
--
-- 幂等：DROP TABLE IF EXISTS 可重复执行；四张表都没有外键依赖（没有其它表引用它们）。
DROP TABLE IF EXISTS user_oauth_bindings;
DROP TABLE IF EXISTS user_meta;
DROP TABLE IF EXISTS user_app_passwords;
DROP TABLE IF EXISTS user_sessions;
