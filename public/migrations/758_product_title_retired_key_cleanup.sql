-- 758 · 清理 735 已退役的四组页面标题 key 在中英对照上的单侧残留
--
-- 背景：735 把管理员 / 权限资源 / 部门管理 / 数据权限四个标题迁成短 key
-- （adminTitle / permTitle / deptTitle / dataruleTitle）并删除了旧 key 行。
-- 但 058（现由 747a 承接）的 SQL 仍按历史原文播种这四组 key 的 zh-CN 行，
-- 而 058 的幂等条件又把这四组语言对算作「本批未完成」——于是第二次启动
-- 会重播 058、把四个 zh-CN 行单独造回来，形成中英不成对的单侧 key。
--
-- 058 的 SQL 是哈希归档（register_registry_test.go 钉住原文），不得回改；
-- 它的守门也必须与自身 SQL 逐对一致（TestSharedFeedback058GuardMatchesExactPairs）。
-- 因此用本 seed 在 735 之后做终态清理：只要这四组旧 key 还有行就删掉，
-- 删干净后条件恒为 1，不再执行。
--
-- 幂等：DELETE 精确到 (item_key, lang)；没有残留时不命中任何行。
DELETE FROM sys_i18n
WHERE (item_key, lang) IN (
  ('MsgAdministratorsTitle', 'zh-CN'), ('MsgAdministratorsTitle', 'en-US'),
  ('MsgDatarulesTitle', 'zh-CN'), ('MsgDatarulesTitle', 'en-US'),
  ('MsgDepartmentsTitle', 'zh-CN'), ('MsgDepartmentsTitle', 'en-US'),
  ('MsgPermissionsTitle', 'zh-CN'), ('MsgPermissionsTitle', 'en-US')
);
