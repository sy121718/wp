-- 账号中心模拟状态只影响视图，不能调用真实访客账号接口。
INSERT INTO sys_i18n (item_key, lang, item_value) VALUES
('site.component.userForms.preview.loading','zh-CN','正在加载账号信息…'),
('site.component.userForms.preview.loading','en-US','Loading account information…'),
('site.component.userForms.preview.empty','zh-CN','还没有可显示的资料。'),
('site.component.userForms.preview.empty','en-US','There is no account information to show yet.'),
('site.component.userForms.preview.validationError','zh-CN','请检查已填写的信息后重试。'),
('site.component.userForms.preview.validationError','en-US','Check the information you entered and try again.'),
('site.component.userForms.preview.serviceError','zh-CN','暂时无法连接，请稍后重试。'),
('site.component.userForms.preview.serviceError','en-US','Unable to connect. Please try again later.'),
('site.component.userForms.preview.submitting','zh-CN','正在提交，请稍候…'),
('site.component.userForms.preview.submitting','en-US','Submitting. Please wait…'),
('site.component.userForms.preview.success','zh-CN','操作已完成，你的账号信息已更新。'),
('site.component.userForms.preview.success','en-US','Your account information has been updated.'),
('site.component.userForms.preview.forbidden','zh-CN','此操作暂不可用，请重新登录后重试。'),
('site.component.userForms.preview.forbidden','en-US','This action is unavailable. Sign in again and retry.'),
('site.component.userForms.actionUnavailable','zh-CN','已登录。目标暂不可用，你可以在这里继续管理账号。'),
('site.component.userForms.actionUnavailable','en-US','You are signed in. Continue managing your account here while the destination is unavailable.')
ON CONFLICT (item_key, lang) DO NOTHING;
