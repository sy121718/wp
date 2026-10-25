-- 621b — 画布保存提示的措辞修正（原词条指向已删除的表单编辑页）。
--
-- 190 seed 的 `admin.mail.automation_canvas.save_hint.*` 说「它会走**和表单页**同一套图校验」，
-- 而表单编辑页（/admin/mail/automation/edit）已随「画布是唯一编辑器」删除 —— 提示指向一个
-- 不存在的页面，用户会去找那个入口。
--
-- 为什么不改 190 本身：历史迁移 SQL 不改（public/CLAUDE.md）。已安装库要的是**更新**，
-- 所以这里用 UPDATE 覆盖成新文案，并用 `item_value = 旧值` 作门槛 —— 运营手改过的行不被覆盖。
UPDATE sys_i18n SET item_value = '改完点下面的「保存结构」——服务端会走', update_time = now()
 WHERE item_key = 'admin.mail.automation_canvas.save_hint.lead' AND lang = 'zh-CN'
   AND item_value = '改完点下面的「保存结构」——它会走和表单页';
UPDATE sys_i18n SET item_value = 'the same graph validation', update_time = now()
 WHERE item_key = 'admin.mail.automation_canvas.save_hint.strong' AND lang = 'en-US'
   AND item_value = 'the same graph validation as the form page';
