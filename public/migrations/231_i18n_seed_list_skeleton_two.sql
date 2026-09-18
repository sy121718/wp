-- 231 · i18n 词条 seed（列表页标准骨架第二轮：变更记录页签 + 通用件补齐）
--
-- 背景：本轮后台页面改造（信息架构 / 布局效率 / 操作逻辑 / 认知负担 四维，规则见 admin-ui-logic）
--       引入了跨页面复用的通用文案位（说明按钮、操作列表头、预览动作、字数提示）与
--       变更记录页的页签组。模板里的 t() 兜底只在缺词条时显示中文，**英文界面会回落中文**，
--       所以新 key 必须成对 seed（门禁：internal/templates/admin_group_f_i18n_test.go）。
-- 覆盖：20 个 key / zh-CN 20 行 / en-US 20 行（人工编写）。
-- 语义：ON CONFLICT DO NOTHING —— seed 是默认值来源，不是真相来源。
-- 幂等：注册见 register_i18n_layer.go，ConditionSQL 取本批 3 个代表 key 的 zh-CN 行数作门槛。
-- 未覆盖：本轮其余页面新增的文案位（共 223 个 key，含大量领域说明）尚未翻译成英文，
--         清单与中文兜底见 docs/02-I-admin-i18n-todo.md —— 中文界面不受影响，
--         缺的只是英文词条（英文界面这些位置回落中文）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.common.help.label', 'en-US', 'Show help', 200, 'admin', '通用：页头说明按钮的无障碍标签', 1, now(), now()),
('admin.common.help.label', 'zh-CN', '查看说明', 200, 'admin', '通用：页头说明按钮的无障碍标签', 1, now(), now()),
('admin.common.col.actions', 'en-US', 'Actions', 200, 'admin', '通用：操作列表头', 1, now(), now()),
('admin.common.col.actions', 'zh-CN', '操作', 200, 'admin', '通用：操作列表头', 1, now(), now()),
('admin.common.action.preview', 'en-US', 'Preview', 200, 'admin', '通用动作：预览（行内不重复实体名）', 1, now(), now()),
('admin.common.action.preview', 'zh-CN', '预览', 200, 'admin', '通用动作：预览（行内不重复实体名）', 1, now(), now()),
('admin.common.action.pages', 'en-US', 'Pages', 200, 'admin', '通用入口：页面管理', 1, now(), now()),
('admin.common.action.pages', 'zh-CN', '页面管理', 200, 'admin', '通用入口：页面管理', 1, now(), now()),
('admin.common.field.keyword', 'en-US', 'Keyword', 200, 'admin', '通用字段：关键词筛选标签', 1, now(), now()),
('admin.common.field.keyword', 'zh-CN', '关键词', 200, 'admin', '通用字段：关键词筛选标签', 1, now(), now()),
('admin.common.counter.chars', 'en-US', '{n} / {max} characters', 200, 'admin', '通用：字段字数提示（{n}/{max} 由前端替换）', 1, now(), now()),
('admin.common.counter.chars', 'zh-CN', '{n} / {max} 字符', 200, 'admin', '通用：字段字数提示（{n}/{max} 由前端替换）', 1, now(), now()),
('admin.masterdata.help.label', 'en-US', 'Show help', 200, 'admin', 'admin/masterdata_changes.html: 说明按钮无障碍标签', 1, now(), now()),
('admin.masterdata.help.label', 'zh-CN', '查看说明', 200, 'admin', 'admin/masterdata_changes.html: 说明按钮无障碍标签', 1, now(), now()),
('admin.masterdata.view.label', 'en-US', 'View', 200, 'admin', 'admin/masterdata_changes.html: 页签组无障碍标签', 1, now(), now()),
('admin.masterdata.view.label', 'zh-CN', '视图', 200, 'admin', 'admin/masterdata_changes.html: 页签组无障碍标签', 1, now(), now()),
('admin.masterdata.view.records', 'en-US', 'Individual records', 200, 'admin', 'admin/masterdata_changes.html: 页签一', 1, now(), now()),
('admin.masterdata.view.records', 'zh-CN', '逐条记录', 200, 'admin', 'admin/masterdata_changes.html: 页签一', 1, now(), now()),
('admin.masterdata.view.entities', 'en-US', 'By entity', 200, 'admin', 'admin/masterdata_changes.html: 页签二', 1, now(), now()),
('admin.masterdata.view.entities', 'zh-CN', '按实体汇总', 200, 'admin', 'admin/masterdata_changes.html: 页签二', 1, now(), now()),
('admin.masterdata.rows.aria', 'en-US', 'Field-level change history', 200, 'admin', 'admin/masterdata_changes.html: 记录表无障碍标签', 1, now(), now()),
('admin.masterdata.rows.aria', 'zh-CN', '字段级变更记录', 200, 'admin', 'admin/masterdata_changes.html: 记录表无障碍标签', 1, now(), now()),
('admin.masterdata.rows.emptyTitle', 'en-US', 'No matching change records', 200, 'admin', 'admin/masterdata_changes.html: 记录视图空态标题', 1, now(), now()),
('admin.masterdata.rows.emptyTitle', 'zh-CN', '没有符合条件的变更记录', 200, 'admin', 'admin/masterdata_changes.html: 记录视图空态标题', 1, now(), now()),
('admin.masterdata.entities.aria', 'en-US', 'Grouped by entity', 200, 'admin', 'admin/masterdata_changes.html: 汇总表无障碍标签', 1, now(), now()),
('admin.masterdata.entities.aria', 'zh-CN', '按实体汇总', 200, 'admin', 'admin/masterdata_changes.html: 汇总表无障碍标签', 1, now(), now()),
('admin.masterdata.entities.emptyTitle', 'en-US', 'No change records at all', 200, 'admin', 'admin/masterdata_changes.html: 汇总视图空态标题', 1, now(), now()),
('admin.masterdata.entities.emptyTitle', 'zh-CN', '没有任何变更记录', 200, 'admin', 'admin/masterdata_changes.html: 汇总视图空态标题', 1, now(), now()),
('admin.masterdata.label.entity_id', 'en-US', 'Entity id', 200, 'admin', 'admin/masterdata_changes.html: 筛选字段标签', 1, now(), now()),
('admin.masterdata.label.entity_id', 'zh-CN', '实体 id', 200, 'admin', 'admin/masterdata_changes.html: 筛选字段标签', 1, now(), now()),
('admin.masterdata.label.keyword', 'en-US', 'Entity name', 200, 'admin', 'admin/masterdata_changes.html: 筛选字段标签', 1, now(), now()),
('admin.masterdata.label.keyword', 'zh-CN', '实体名', 200, 'admin', 'admin/masterdata_changes.html: 筛选字段标签', 1, now(), now()),
('admin.masterdata.label.range', 'en-US', 'Date range', 200, 'admin', 'admin/masterdata_changes.html: 区间筛选标签', 1, now(), now()),
('admin.masterdata.label.range', 'zh-CN', '时间区间', 200, 'admin', 'admin/masterdata_changes.html: 区间筛选标签', 1, now(), now()),
('admin.masterdata.ph.until', 'en-US', 'End date', 200, 'admin', 'admin/masterdata_changes.html: 区间结束输入的占位与无障碍标签', 1, now(), now()),
('admin.masterdata.ph.until', 'zh-CN', '结束日期', 200, 'admin', 'admin/masterdata_changes.html: 区间结束输入的占位与无障碍标签', 1, now(), now()),
('admin.datarules.help.label', 'en-US', 'Show help', 200, 'admin', 'admin/datarules.html: 说明按钮无障碍标签', 1, now(), now()),
('admin.datarules.help.label', 'zh-CN', '查看说明', 200, 'admin', 'admin/datarules.html: 说明按钮无障碍标签', 1, now(), now()),
('admin.analytics.help.label', 'en-US', 'Show help', 200, 'admin', 'admin/analytics.html: 说明按钮无障碍标签', 1, now(), now()),
('admin.analytics.help.label', 'zh-CN', '查看说明', 200, 'admin', 'admin/analytics.html: 说明按钮无障碍标签', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
