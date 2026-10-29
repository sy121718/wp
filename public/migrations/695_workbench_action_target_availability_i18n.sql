INSERT INTO sys_i18n (item_key, lang, item_value, category, remark, status, http_code, create_time, update_time) VALUES
('workbench.err.previewActionTargetUnavailable', 'zh-CN', '所选跳转目标当前不可用。请选中相关组件，在内容检查器的「跳转到」中重新选择可用的已发布目标，或恢复默认流程；需要保留该目标时，请先重新发布目标页面。', 'error', '', 1, 422, now(), now()),
('workbench.err.previewActionTargetUnavailable', 'en-US', 'The selected destination is unavailable. Select the component and choose an available published destination under Destination in the content inspector, or restore its default flow. Republish the destination page if you need to keep using it.', 'error', '', 1, 422, now(), now()),
('workbench.ui.editor.target-unavailable', 'zh-CN', '当前目标不可用，请重新选择', 'text', '', 1, 0, now(), now()),
('workbench.ui.editor.target-unavailable', 'en-US', 'Current destination unavailable, choose another', 'text', '', 1, 0, now(), now()),
('workbench.ui.editor.target-reselect', 'zh-CN', '当前跳转目标不可用，请重新选择已发布目标或恢复默认流程。', 'text', '', 1, 0, now(), now()),
('workbench.ui.editor.target-reselect', 'en-US', 'This destination is unavailable. Choose an available published destination or restore the default flow.', 'text', '', 1, 0, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING
