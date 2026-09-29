-- 459 — page / mail 两模块业务错误**补充说明**的词条（i18n.ErrorDetail 协议）。
--
-- 背景：这两个模块的写侧此前把 builder 校验 / 流程图校验的**中文原文**直接拼进业务错误的
-- tail（`ErrInvalidDocument: 顶级节点 0: 组件树深度 11 超过上限 10…`、
-- `mail.err.automationGraphInvalid: 流程里有环: a → b`），而读侧又把它原样拼回响应
--（page 的 pageFacingText / pageErrorMessage、mail 的 translateMailFacing）——
-- 英文界面下必然中英混排，且经 302 的 ?err= 会进页面与浏览器历史。
--
-- 本批把这两族的 tail 改成 i18n.ErrorDetail 协议（控制字符 + 词条 key + 具名参数，可多段），
-- 读侧按当前语言取词并填 `{name}` 占位符；**不是词条形态的 tail 一律丢弃并落日志**。
-- 占位符一律命名形态（pkg/i18n/placeholder.go）：不用 %s + Sprintf，词条被运营改出裸 %
-- 或中英占位符错配时不会渲染出乱码。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；门槛判据在
-- register_page_mail_err_detail_i18n.go 里**逐条枚举本批 22 个 item_key**
--（上界封闭，22 × 2 = 44 行），不用 LIKE 前缀、也不用全库总量。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
-- —— page：页面文档校验明细（写侧 page/service/page_document_detail.go）——
('admin.page.detail.documentEmpty', 'zh-CN', '页面文档为空，没有可保存的内容', 200, 'admin', 'admin.page.detail.documentEmpty', 1, now(), now()),
('admin.page.detail.documentEmpty', 'en-US', 'The page document is empty, so there is nothing to save.', 200, 'admin', 'admin.page.detail.documentEmpty', 1, now(), now()),
('admin.page.detail.nodeDepthExceed', 'zh-CN', '顶级节点 {index} 的组件树深度 {depth} 超过上限 {max}（嵌套失控，请简化结构）', 200, 'admin', 'admin.page.detail.nodeDepthExceed', 1, now(), now()),
('admin.page.detail.nodeDepthExceed', 'en-US', 'Top-level node {index} is {depth} levels deep, which is over the limit of {max} levels (the nesting has run away; simplify the structure).', 200, 'admin', 'admin.page.detail.nodeDepthExceed', 1, now(), now()),
('admin.page.detail.nodeInvalid', 'zh-CN', '顶级节点 {index} 的配置不合法', 200, 'admin', 'admin.page.detail.nodeInvalid', 1, now(), now()),
('admin.page.detail.nodeInvalid', 'en-US', 'Top-level node {index} has an invalid configuration.', 200, 'admin', 'admin.page.detail.nodeInvalid', 1, now(), now()),
('admin.page.detail.settingsInvalid', 'zh-CN', '页面设置不合法', 200, 'admin', 'admin.page.detail.settingsInvalid', 1, now(), now()),
('admin.page.detail.settingsInvalid', 'en-US', 'The page settings are invalid.', 200, 'admin', 'admin.page.detail.settingsInvalid', 1, now(), now()),
('admin.page.detail.structureInvalid', 'zh-CN', '页面文档结构不合法', 200, 'admin', 'admin.page.detail.structureInvalid', 1, now(), now()),
('admin.page.detail.structureInvalid', 'en-US', 'The page document structure is invalid.', 200, 'admin', 'admin.page.detail.structureInvalid', 1, now(), now()),
-- —— mail：流程图校验明细（写侧 mail/service/mail_graph_err.go + mail_automation_graph.go）——
('admin.mail.detail.graphEmpty', 'zh-CN', '流程定义不能为空', 200, 'admin', 'admin.mail.detail.graphEmpty', 1, now(), now()),
('admin.mail.detail.graphEmpty', 'en-US', 'The flow definition is empty.', 200, 'admin', 'admin.mail.detail.graphEmpty', 1, now(), now()),
('admin.mail.detail.requestNotJSON', 'zh-CN', '流程定义的 JSON 解析失败: {reason}', 200, 'admin', 'admin.mail.detail.requestNotJSON', 1, now(), now()),
('admin.mail.detail.requestNotJSON', 'en-US', 'The flow definition is not valid JSON: {reason}', 200, 'admin', 'admin.mail.detail.requestNotJSON', 1, now(), now()),
('admin.mail.detail.notGraph', 'zh-CN', '流程定义不是合法的图结构: {reason}', 200, 'admin', 'admin.mail.detail.notGraph', 1, now(), now()),
('admin.mail.detail.notGraph', 'en-US', 'The flow definition is not a valid graph structure: {reason}', 200, 'admin', 'admin.mail.detail.notGraph', 1, now(), now()),
('admin.mail.detail.noNode', 'zh-CN', '流程里至少要有一个节点', 200, 'admin', 'admin.mail.detail.noNode', 1, now(), now()),
('admin.mail.detail.noNode', 'en-US', 'A flow needs at least one node.', 200, 'admin', 'admin.mail.detail.noNode', 1, now(), now()),
('admin.mail.detail.nodeKeyMissing', 'zh-CN', '存在没有 key 的节点', 200, 'admin', 'admin.mail.detail.nodeKeyMissing', 1, now(), now()),
('admin.mail.detail.nodeKeyMissing', 'en-US', 'There is a node without a key.', 200, 'admin', 'admin.mail.detail.nodeKeyMissing', 1, now(), now()),
('admin.mail.detail.nodeKeyDuplicate', 'zh-CN', '节点 key 重复: {node}', 200, 'admin', 'admin.mail.detail.nodeKeyDuplicate', 1, now(), now()),
('admin.mail.detail.nodeKeyDuplicate', 'en-US', 'Duplicate node key: {node}', 200, 'admin', 'admin.mail.detail.nodeKeyDuplicate', 1, now(), now()),
('admin.mail.detail.needMinutes', 'zh-CN', '节点 {node}: 等待节点需要正数的 minutes', 200, 'admin', 'admin.mail.detail.needMinutes', 1, now(), now()),
('admin.mail.detail.needMinutes', 'en-US', 'Node {node}: a delay node needs a positive number of minutes', 200, 'admin', 'admin.mail.detail.needMinutes', 1, now(), now()),
('admin.mail.detail.needTemplate', 'zh-CN', '节点 {node}: 发信节点需要 template_key', 200, 'admin', 'admin.mail.detail.needTemplate', 1, now(), now()),
('admin.mail.detail.needTemplate', 'en-US', 'Node {node}: an email node needs a template_key', 200, 'admin', 'admin.mail.detail.needTemplate', 1, now(), now()),
('admin.mail.detail.needTwoArms', 'zh-CN', '节点 {node}: 条件分支需要 yes 与 no 两条出边', 200, 'admin', 'admin.mail.detail.needTwoArms', 1, now(), now()),
('admin.mail.detail.needTwoArms', 'en-US', 'Node {node}: a condition branch needs both the yes and no edges', 200, 'admin', 'admin.mail.detail.needTwoArms', 1, now(), now()),
('admin.mail.detail.needCondition', 'zh-CN', '节点 {node}: 条件分支需要至少一个条件', 200, 'admin', 'admin.mail.detail.needCondition', 1, now(), now()),
('admin.mail.detail.needCondition', 'en-US', 'Node {node}: a condition branch needs at least one condition', 200, 'admin', 'admin.mail.detail.needCondition', 1, now(), now()),
('admin.mail.detail.needTagAction', 'zh-CN', '节点 {node}: 标签节点需要 add 或 remove', 200, 'admin', 'admin.mail.detail.needTagAction', 1, now(), now()),
('admin.mail.detail.needTagAction', 'en-US', 'Node {node}: a tag node needs add or remove', 200, 'admin', 'admin.mail.detail.needTagAction', 1, now(), now()),
('admin.mail.detail.unknownNodeType', 'zh-CN', '节点 {node}: 未知节点类型: {type}', 200, 'admin', 'admin.mail.detail.unknownNodeType', 1, now(), now()),
('admin.mail.detail.unknownNodeType', 'en-US', 'Node {node}: unknown node type: {type}', 200, 'admin', 'admin.mail.detail.unknownNodeType', 1, now(), now()),
('admin.mail.detail.entryMissing', 'zh-CN', '没有指定入口节点', 200, 'admin', 'admin.mail.detail.entryMissing', 1, now(), now()),
('admin.mail.detail.entryMissing', 'en-US', 'No entry node is set.', 200, 'admin', 'admin.mail.detail.entryMissing', 1, now(), now()),
('admin.mail.detail.entryNotExist', 'zh-CN', '入口节点不存在: {node}', 200, 'admin', 'admin.mail.detail.entryNotExist', 1, now(), now()),
('admin.mail.detail.entryNotExist', 'en-US', 'The entry node does not exist: {node}', 200, 'admin', 'admin.mail.detail.entryNotExist', 1, now(), now()),
('admin.mail.detail.edgeTargetMissing', 'zh-CN', '节点 {from} 指向了不存在的节点 {to}', 200, 'admin', 'admin.mail.detail.edgeTargetMissing', 1, now(), now()),
('admin.mail.detail.edgeTargetMissing', 'en-US', 'Node {from} points to a node that does not exist: {to}', 200, 'admin', 'admin.mail.detail.edgeTargetMissing', 1, now(), now()),
('admin.mail.detail.cycle', 'zh-CN', '流程里有环: {path}', 200, 'admin', 'admin.mail.detail.cycle', 1, now(), now()),
('admin.mail.detail.cycle', 'en-US', 'The flow contains a cycle: {path}', 200, 'admin', 'admin.mail.detail.cycle', 1, now(), now()),
('admin.mail.detail.unreachable', 'zh-CN', '有节点从入口走不到: {nodes}', 200, 'admin', 'admin.mail.detail.unreachable', 1, now(), now()),
('admin.mail.detail.unreachable', 'en-US', 'Some nodes are unreachable from the entry: {nodes}', 200, 'admin', 'admin.mail.detail.unreachable', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
