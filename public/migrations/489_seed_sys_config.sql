-- 489 · sys_config 初始行：i18n 组（全局默认语言 + 站点语言 URL 方案）。
--
-- 为什么由这条迁移灌：484 建表；本批把「默认语言 / 站点语言 URL 方案 / 语言码覆盖」的
-- **唯一来源**从 config.yaml 搬到本表（config.yaml 那四个键已删除）。没有初始行的话，
-- 首次启动读不到组 → 退回代码内常量（zh-CN / default_plain），行为与搬迁前一致、
-- 但配置面是空的（后台设置页读不到可改的行）。
--
-- 幂等：ON CONFLICT (group_key) DO NOTHING —— 重复执行不覆盖运维改过的值。
-- **单语句**：seed 由 migrator 一次 Exec 执行（不拆语句），所以这里只能有一条语句。
--
-- 值口径：
--   default_lang        = zh-CN（与 pkg/i18n 的 fallbackDefaultLang 一致，搬迁前后默认语言逐字不变）
--   site_lang_url_mode  = default_plain（默认方案：默认语言无前缀 + 非默认语言短码）
--   lang_url_codes      **刻意不灌**：内置表 +「主语言子标签小写」确定性回退已覆盖常见情形；
--                       需要覆盖时在后台配置（组内可选键，读侧按「没配 = 无覆盖」处理）。
--
-- 注册见 register_sys_reference.go（registerSeed，版本 489-seed-sys-config）：
-- 门槛判据按**本批自己的 key 逐条枚举**（group_key = 'i18n' 且组内两个键都在），
-- 不用 LIKE 前缀、不用全库总量 —— 那样的判据会因别的批次的行而虚高，把本批静默跳过（058/076）。

INSERT INTO sys_config (group_key, group_name, config_data, remark, status, version, create_by, update_by)
VALUES (
    'i18n',
    '国际化',
    '{"default_lang": "zh-CN", "site_lang_url_mode": "default_plain"}'::jsonb,
    '全局默认语言与站点语言 URL 方案（唯一来源；工程级覆盖在 projects.settings）',
    1, 1, 0, 0
)
ON CONFLICT (group_key) DO NOTHING;
