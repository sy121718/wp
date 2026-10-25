-- 621a — 图定义 v2 的两条新校验明细词条（中英成对）。
--
-- 为什么必须同批补：这两个 key 由 service/automation_definition.go 与 mail_automation.go
-- 的校验器产出（graphErr），读侧（inbound/http/mail_err.go 的 mailDetailText）只认
-- **在 ErrDetailFallbacks 里登记过的 key**。没词条时页面回落到 enums 里的中文兜底 ——
-- 中文界面完全看不出来，只有切到 en-US 才暴露，而那时看到的是中文。
--
-- 为什么只有两条：出口名与条件模式是本批（v1→v2 形状升级）真正新增的失败形态；
-- T4 的四个新节点参数校验（变体 / 权重 / 回调地址）会跟它们自己的校验**同批**补词条，
-- 而不是提前声明一批没有生产者的 key（无生产者的词条是「看起来校验已经有了」的假象）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；门槛判据在
-- register_mail_automation_definition_v2_i18n.go 里**逐条枚举本批 2 个 item_key**
--（上界封闭，2 × 2 = 4 行），不用 LIKE 前缀、也不用全库总量。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.mail.detail.unknownOutlet', 'zh-CN', '节点 {node}: 未知出口名: {outlet}', 200, 'admin', 'admin.mail.detail.unknownOutlet', 1, now(), now()),
('admin.mail.detail.unknownOutlet', 'en-US', 'Node {node}: unknown outlet name: {outlet}', 200, 'admin', 'admin.mail.detail.unknownOutlet', 1, now(), now()),
('admin.mail.detail.badCondition', 'zh-CN', '节点 {node}: 条件不合法: {reason}', 200, 'admin', 'admin.mail.detail.badCondition', 1, now(), now()),
('admin.mail.detail.badCondition', 'en-US', 'Node {node}: invalid condition: {reason}', 200, 'admin', 'admin.mail.detail.badCondition', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
