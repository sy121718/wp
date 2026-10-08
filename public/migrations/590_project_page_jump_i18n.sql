-- ========================================
-- 590 — project 域后台页写动作成功回执的新增词条
--
-- 背景：project 域页面写动作的结论原先靠 303 + `?err=` / `?ok=` / `?locales_saved=1`
--   回列表页 / 回本页，本批改成由 shell.RenderJump 渲染**整页提示**
--（对应 ThinkPHP 的 success() / error()）。成功回执因此需要一句**成品文案**
--   （走响应体、不进 URL），而主题的新建 / 激活 / 删除此前是「静默 303」——没有任何回执。
--
-- 本批只新增主题 CRUD 的三条成功回执（2 语言共 6 行）；其余三条复用既有词条：
--   · MsgSiteSettingsSaved          站点设置保存成功（058 zh-CN / 447 en-US）
--   · admin.settings.locales.saved  语言清单保存成功（187）
--   · admin.theme_settings.ok.saved 主题设置保存成功（451）
--
-- 幂等：INSERT ... ON CONFLICT (item_key, lang) DO NOTHING；本批**不修改任何既有词条的值**。
-- ========================================

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.theme.ok.created', 'zh-CN', '主题已创建', 200, 'project', 'project/inbound/http: 主题新建成功回执（整页提示）', 1, now(), now()),
('admin.theme.ok.created', 'en-US', 'Theme created', 200, 'project', 'project/inbound/http: 主题新建成功回执（整页提示）', 1, now(), now()),
('admin.theme.ok.activated', 'zh-CN', '主题已激活，该主题下页面已标记待重建', 200, 'project', 'project/inbound/http: 主题激活成功回执（整页提示）', 1, now(), now()),
('admin.theme.ok.activated', 'en-US', 'Theme activated; pages under it are marked for rebuild', 200, 'project', 'project/inbound/http: 主题激活成功回执（整页提示）', 1, now(), now()),
('admin.theme.ok.deleted', 'zh-CN', '主题已删除', 200, 'project', 'project/inbound/http: 主题删除成功回执（整页提示）', 1, now(), now()),
('admin.theme.ok.deleted', 'en-US', 'Theme deleted', 200, 'project', 'project/inbound/http: 主题删除成功回执（整页提示）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
