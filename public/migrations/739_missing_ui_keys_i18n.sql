-- 补齐四处被模板引用但词库缺失的界面词条（中英成对）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.membership.tier.deleteConfirm', 'zh-CN', '删除这个等级？等级上还挂着会员归属时无法删除。', 200, 'membership', 'admin/membership/membership.html: 删除等级确认', 1, now(), now()),
('admin.membership.tier.deleteConfirm', 'en-US', 'Delete this tier? It cannot be deleted while members are still assigned to it.', 200, 'membership', 'admin/membership/membership.html: 删除等级确认', 1, now(), now()),
('admin.returns.ph.bulk_reject_remark', 'zh-CN', '批量拒绝理由（必填）', 200, 'order', 'admin/order/returns.html: 批量拒绝备注框', 1, now(), now()),
('admin.returns.ph.bulk_reject_remark', 'en-US', 'Reason for bulk rejection (required)', 200, 'order', 'admin/order/returns.html: 批量拒绝备注框', 1, now(), now()),
('http.error.conflict', 'zh-CN', '数据发生冲突，请检查后重试', 409, 'error', 'admin/errors/failure.html: 409 兜底文案', 1, now(), now()),
('http.error.conflict', 'en-US', 'Data conflict; please review and retry', 409, 'error', 'admin/errors/failure.html: 409 兜底文案', 1, now(), now()),
('http.error.title', 'zh-CN', '请求未完成', 200, 'error', 'admin/errors/page.html: 整页错误标题', 1, now(), now()),
('http.error.title', 'en-US', 'Request not completed', 200, 'error', 'admin/errors/page.html: 整页错误标题', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
