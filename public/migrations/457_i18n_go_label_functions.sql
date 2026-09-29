-- 457 — Go 侧「返回中文的展示标签函数」收口到词条。
--
-- 背景：这些函数的返回值**会进 HTTP 响应或模板渲染**（不是日志），所以在英文后台 /
-- 英文站点上恒显示中文。形态统一为 (i18n key, 中文兜底)：调用点 tr(key, fallback)，
-- 命中出译文、未命中出中文兜底，key 为空（认不出的取值）直接用兜底值 / 原值。
--
-- 覆盖三处出口：
--   · contenttemplate 列表页的实体类型 / 模板角色 / 结构槽位（模板直接渲染的徽章与引用列）；
--   · 集合源展示名（GET /api/content/collections 的 label，工作台集合源 / 字段下拉）；
--   · mail 自动化状态回执与测试发送失败回执（写侧拼进 302 的 query、读侧白名单整句比对，
--     两侧必须引用同一份拼装，否则运营点完按钮页面上什么都没有）。
--
-- **本批不新增的**（复用库内既有词条，已在代码里改成同一批 key）：
--   · admin.menus.type.dir / menu / button / link / unknown（后台模板早就在用，
--     Go 侧的权限树类型标签此前是中文硬编码）；
--   · admin.mail.automation.status.active / paused / draft（状态徽章已在用）；
--   · admin.content.templates.entityHeader / entityFooter（筛选下拉已在用）；
--   · admin.returns.status.*（迁移 448 已 seed，order 的 service 改为出口取词）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；门槛判据在
-- register_go_label_functions_i18n.go 里逐条枚举本批的 item_key（上界封闭）——
-- 用 LIKE 前缀会让别批次已有同前缀行把计数抬高、本批被静默跳过（058 的故障），
-- 也会在将来新增同前缀 key 时永远追不平、每次启动都重跑（076 的故障）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.content.templates.entityProduct', 'zh-CN', '商品详情', 200, 'admin', 'content template entity type: product detail', 1, now(), now()),
('admin.content.templates.entityProduct', 'en-US', 'Product detail', 200, 'admin', 'content template entity type: product detail', 1, now(), now()),
('admin.content.templates.entityArticle', 'zh-CN', '文章详情', 200, 'admin', 'content template entity type: article detail', 1, now(), now()),
('admin.content.templates.entityArticle', 'en-US', 'Article detail', 200, 'admin', 'content template entity type: article detail', 1, now(), now()),
('admin.content.templates.entityCategory', 'zh-CN', '分类归档', 200, 'admin', 'content template entity type: category archive', 1, now(), now()),
('admin.content.templates.entityCategory', 'en-US', 'Category archive', 200, 'admin', 'content template entity type: category archive', 1, now(), now()),
('admin.content.templates.entityTag', 'zh-CN', '标签归档', 200, 'admin', 'content template entity type: tag archive', 1, now(), now()),
('admin.content.templates.entityTag', 'en-US', 'Tag archive', 200, 'admin', 'content template entity type: tag archive', 1, now(), now()),
('admin.content.templates.entityBrand', 'zh-CN', '品牌归档', 200, 'admin', 'content template entity type: brand archive', 1, now(), now()),
('admin.content.templates.entityBrand', 'en-US', 'Brand archive', 200, 'admin', 'content template entity type: brand archive', 1, now(), now()),
('admin.content.templates.roleArchive', 'zh-CN', '归档页', 200, 'admin', 'content template role: archive page', 1, now(), now()),
('admin.content.templates.roleArchive', 'en-US', 'Archive page', 200, 'admin', 'content template role: archive page', 1, now(), now()),
('admin.content.templates.roleDetail', 'zh-CN', '详情页', 200, 'admin', 'content template role: detail page', 1, now(), now()),
('admin.content.templates.roleDetail', 'en-US', 'Detail page', 200, 'admin', 'content template role: detail page', 1, now(), now()),
('admin.content.templates.slotHeader', 'zh-CN', '页眉', 200, 'admin', 'content template structure slot: header', 1, now(), now()),
('admin.content.templates.slotHeader', 'en-US', 'Header', 200, 'admin', 'content template structure slot: header', 1, now(), now()),
('admin.content.templates.slotFooter', 'zh-CN', '页脚', 200, 'admin', 'content template structure slot: footer', 1, now(), now()),
('admin.content.templates.slotFooter', 'en-US', 'Footer', 200, 'admin', 'content template structure slot: footer', 1, now(), now()),
('admin.collection.article', 'zh-CN', '文章列表', 200, 'admin', 'collection source label: article list (workbench dropdown)', 1, now(), now()),
('admin.collection.article', 'en-US', 'Article list', 200, 'admin', 'collection source label: article list (workbench dropdown)', 1, now(), now()),
('admin.collection.product', 'zh-CN', '商品列表', 200, 'admin', 'collection source label: product list (workbench dropdown)', 1, now(), now()),
('admin.collection.product', 'en-US', 'Product list', 200, 'admin', 'collection source label: product list (workbench dropdown)', 1, now(), now()),
('admin.collection.category', 'zh-CN', '分类列表', 200, 'admin', 'collection source label: category list (workbench dropdown)', 1, now(), now()),
('admin.collection.category', 'en-US', 'Category list', 200, 'admin', 'collection source label: category list (workbench dropdown)', 1, now(), now()),
('admin.mail.automation.status.unknown', 'zh-CN', '未知状态', 200, 'admin', 'mail automation status notice: unknown status', 1, now(), now()),
('admin.mail.automation.status.unknown', 'en-US', 'Unknown status', 200, 'admin', 'mail automation status notice: unknown status', 1, now(), now()),
('admin.mail.automation.statusChanged', 'zh-CN', '状态已更新为 {status}。', 200, 'admin', 'mail automation status changed notice ({status} = localized status label)', 1, now(), now()),
('admin.mail.automation.statusChanged', 'en-US', 'Status changed to {status}.', 200, 'admin', 'mail automation status changed notice ({status} = localized status label)', 1, now(), now()),
('admin.mail.test_send.temporary', 'zh-CN', '测试邮件发送失败（可重试的临时故障），详情见服务端日志。', 200, 'admin', 'mail test send failed: temporary failure', 1, now(), now()),
('admin.mail.test_send.temporary', 'en-US', 'The test email could not be sent (a temporary failure that may succeed if you retry). See the server log for details.', 200, 'admin', 'mail test send failed: temporary failure', 1, now(), now()),
('admin.mail.test_send.permanent', 'zh-CN', '测试邮件发送失败（被对方永久拒绝），详情见服务端日志。', 200, 'admin', 'mail test send failed: permanently rejected', 1, now(), now()),
('admin.mail.test_send.permanent', 'en-US', 'The test email could not be sent (the receiving server rejected it permanently). See the server log for details.', 200, 'admin', 'mail test send failed: permanently rejected', 1, now(), now()),
('admin.mail.test_send.configuration', 'zh-CN', '测试邮件发送失败（配置问题，需人工处理），详情见服务端日志。', 200, 'admin', 'mail test send failed: configuration problem', 1, now(), now()),
('admin.mail.test_send.configuration', 'en-US', 'The test email could not be sent (a configuration problem that needs a manual fix). See the server log for details.', 200, 'admin', 'mail test send failed: configuration problem', 1, now(), now()),
('admin.mail.test_send.unknown', 'zh-CN', '测试邮件发送失败（未分类），详情见服务端日志。', 200, 'admin', 'mail test send failed: unclassified', 1, now(), now()),
('admin.mail.test_send.unknown', 'en-US', 'The test email could not be sent (unclassified). See the server log for details.', 200, 'admin', 'mail test send failed: unclassified', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
