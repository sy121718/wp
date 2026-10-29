INSERT INTO sys_i18n (item_key,lang,item_value) VALUES
('page.err.actionTargetInUse','zh-CN','这个目标仍被页面或区块引用，请先替换引用后再删除'),
('page.err.actionTargetInUse','en-US','This target is still referenced by pages or blocks. Replace the references before deleting it.'),
('presentation.err.actionTargetInUse','zh-CN','这个目标仍被页面或区块引用，请先替换引用后再删除'),
('presentation.err.actionTargetInUse','en-US','This target is still referenced by pages or blocks. Replace the references before deleting it.')
ON CONFLICT (item_key,lang) DO NOTHING;
