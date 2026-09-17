-- 227 · 抽屉表单的通用操作词条（取消 / 保存）
--
-- 背景：列表页的「新建/编辑」统一改为右侧抽屉承载后，每个抽屉底部都要一对「取消 / 保存」。
--   仓库里此前是逐页各写一份同义词条（admin.admins.action.cancel、admin.roles.action.save…，
--   193 一个文件里就有 6 组），这次不再复制第七份。
-- 归属：internal/module/admin/enums（后台通用交互文案），使用方在各模块的后台页面模板。
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING（seed 是默认值来源，不是真相来源）。
-- 判定限定在自己的 key 上：不数全库行数（058 那种「全库 zh-CN 计数」在存量库永远判定已灌满，
--   新词条不会被灌进去）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.common.action.cancel', 'en-US', 'Cancel', 200, 'admin', 'internal/module/admin/enums', 1, now(), now()),
('admin.common.action.edit', 'en-US', 'Edit', 200, 'admin', 'internal/module/admin/enums', 1, now(), now()),
('admin.common.action.edit', 'zh-CN', '编辑', 200, 'admin', 'internal/module/admin/enums', 1, now(), now()),
('admin.common.action.cancel', 'zh-CN', '取消', 200, 'admin', 'internal/module/admin/enums', 1, now(), now()),
('admin.common.action.save', 'en-US', 'Save', 200, 'admin', 'internal/module/admin/enums', 1, now(), now()),
('admin.common.action.save', 'zh-CN', '保存', 200, 'admin', 'internal/module/admin/enums', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
