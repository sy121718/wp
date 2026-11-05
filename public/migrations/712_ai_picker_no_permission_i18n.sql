-- 712 · AI 候选弹窗「没有追加权限」的界面词条
--
-- 为什么单独一批：把候选与追加拆成两个独立权限点之后（ai:provider_models_candidates /
--   ai:provider_models_append），弹窗在**没有追加权限**时不渲染提交表单，改出一句说明。
--   那句话必须有词库词条，否则门禁 check-i18n-keys-seeded.sh 判它「被引用但未 seed」，
--   而缺词时用户看到的是归口文案，看不出是权限问题。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；判据见 register_ai_picker_no_permission_i18n.go。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.ai.picker.noPermission', 'zh-CN', '没有追加模型的权限。', 200, 'ai', 'admin/ai/provider_picker.html: 无 ai:provider_models_append 时的说明', 1, now(), now()),
('admin.ai.picker.noPermission', 'en-US', 'You do not have permission to add models.', 200, 'ai', 'admin/ai/provider_picker.html: shown when ai:provider_models_append is absent', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
