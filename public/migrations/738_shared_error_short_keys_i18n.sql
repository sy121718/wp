-- 共享错误/消息出口的短 key 整改：旧 key 含下划线或超过 15 字符，不合规。
-- 新 key 沿用旧译文（中英各一行），旧 key 在本迁移末尾精确清理。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('sysErrBadParam', 'zh-CN', '请求参数错误', 400, 'error', 'pkg/enums/errors.go: ErrInvalidParams', 1, now(), now()),
('sysErrBadParam', 'en-US', 'Invalid request parameters', 400, 'error', 'pkg/enums/errors.go: ErrInvalidParams', 1, now(), now()),
('sysErrNoPerm', 'zh-CN', '无权限访问', 403, 'error', 'pkg/enums/errors.go: ErrPermissionDenied', 1, now(), now()),
('sysErrNoPerm', 'en-US', 'Permission denied', 403, 'error', 'pkg/enums/errors.go: ErrPermissionDenied', 1, now(), now()),
('sysErrTooLarge', 'zh-CN', '请求体过大', 413, 'error', 'pkg/enums/errors.go: ErrRequestEntityTooLarge', 1, now(), now()),
('sysErrTooLarge', 'en-US', 'Request entity too large', 413, 'error', 'pkg/enums/errors.go: ErrRequestEntityTooLarge', 1, now(), now()),
('uiOpSuccess', 'zh-CN', '操作成功', 200, 'ui', 'admin/partials/write_success.html', 1, now(), now()),
('uiOpSuccess', 'en-US', 'Operation successful', 200, 'ui', 'admin/partials/write_success.html', 1, now(), now()),
('uiDelConfirm', 'zh-CN', '确定要删除吗？', 200, 'ui', 'admin/system/i18n.html', 1, now(), now()),
('uiDelConfirm', 'en-US', 'Are you sure to delete?', 200, 'ui', 'admin/system/i18n.html', 1, now(), now()),
('brandNoneSel', 'zh-CN', '没有勾选任何品牌，列表未改动。', 400, 'product', 'admin/product/product_brands.html: 批量条空选提示', 1, now(), now()),
('brandNoneSel', 'en-US', 'No brands selected; the list is unchanged.', 400, 'product', 'admin/product/product_brands.html: 批量条空选提示', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;

-- 旧 key 已无任何消费者（Go 常量值、模板与测试均已改到新 key），精确清理。
DELETE FROM sys_i18n
WHERE (item_key, lang) IN (
  ('MsgInternalError', 'zh-CN'), ('MsgInternalError', 'en-US'),
  ('ErrInvalidParams', 'zh-CN'), ('ErrInvalidParams', 'en-US'),
  ('ErrPermissionDenied', 'zh-CN'), ('ErrPermissionDenied', 'en-US'),
  ('ErrRequestEntityTooLarge', 'zh-CN'), ('ErrRequestEntityTooLarge', 'en-US'),
  ('msg_operation_success', 'zh-CN'), ('msg_operation_success', 'en-US'),
  ('msg_delete_confirm', 'zh-CN'), ('msg_delete_confirm', 'en-US'));
