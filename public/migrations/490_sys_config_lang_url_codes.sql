-- 490 · sys_config 的 i18n 组补 `lang_url_codes`（语言码 → URL 短码覆盖）。
--
-- 为什么是**新迁移**而不是改 489：489 已经执行过（i18n 组那一行已存在），而它是
-- 「ON CONFLICT (group_key) DO NOTHING」的单语句 seed —— 改 489 只会在**全新库**上生效，
-- 存量库那行已存在、什么都不会发生，两边从此分叉。补键必须是一条自己的迁移。
--
-- 值：{"en-AU": "en"} —— 内置表把 en-AU 映射成 `en-au`（访问路径成 /en-au/...），
-- 站点原先在 config.yaml 里覆盖成 `en`（/en/...）。本批把默认值搬进 sys_config 时漏了这条：
-- en-AU 一旦成为**非默认语言**（或方案切到 all_prefix），已发布产物的路径会从 /en/...
-- 变成 /en-au/...，旧链接全部悬空。当前 en-AU 是默认语言 + default_plain（默认语言不带
-- 前缀），所以症状还没出现 —— 属于「现在不疼、改一次就疼」的雷。
--
-- 幂等：只在缺该键时写（NOT jsonb_exists(...)）。已有该键时整条 UPDATE 命中 0 行，
-- 不会覆盖运维后来改过的覆盖表（「宁可重跑，不可静默跳过」的同一取向：这里重跑是安全的）。
-- **单语句**：seed 由 migrator 一次 Exec 执行（不拆语句）。
--
-- **为什么推进 version（`version = version + 1`）**：本语句改的是 config_data，而 sys_config 的
-- 乐观锁检查的正是 version（484 的设计：整组读-改-写必须带 version 条件）。不推进的话
-- 「version 代表这一组数据的状态」这件事就破了：管理员手里握着旧 version 做整组保存时，
-- 他的 UPDATE 仍会命中 —— 于是刚补的 lang_url_codes 被静默覆盖，而这正是乐观锁本该拦住的
-- 丢失更新。「等下一次启动由门槛判据补回」是把拦截换成事后修复，方向不对。
--
-- 注：开发库已按**旧语句**（不带 version + 1）执行过这条迁移，重跑时条件不成立、不会再执行，
-- 因此那里的 version 停在 1。这不影响正确性：version 只用于 CAS 比较，绝对值没有意义，
-- 只要单调即可（本次改动之后的所有写入都会正常推进它）。全新库会带着 version = 2 建立。
--
-- 注册见 register_sys_reference.go（registerSeed，版本 490-seed-sys-config-lang-url-codes）：
-- 门槛判据用 jsonb_exists(config_data, 'lang_url_codes')（本批自己的键，上界封闭）。
-- 不用 `?` 操作符：Seed 的 ConditionSQL 由 gorm 直接执行，`?` 在 PostgreSQL 里不是占位符。

UPDATE sys_config
   SET config_data = config_data || '{"lang_url_codes": {"en-AU": "en"}}'::jsonb,
       version = version + 1,
       update_time = now()
 WHERE group_key = 'i18n'
   AND NOT jsonb_exists(config_data, 'lang_url_codes');
