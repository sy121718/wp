-- 745 · 清理已退役的枚举 i18n key（只删这 11 对，保留自定义译文与额外语言）
--
-- 背景：这些 key 已在代码侧改名（MsgInternalError→uiInternalErr、
-- ErrInvalidParams→sysErrBadParam、ErrPermissionDenied→sysErrNoPerm、
-- ErrRequestEntityTooLarge→sysErrTooLarge、msg_operation_success→uiOpSuccess、
-- msg_delete_confirm→uiDelConfirm），738 只清理过一遍，而 058 仍在播种它们。
-- 058 已停止播种；这里额外做一次精确清理，不依赖 738 恰好重跑。
--
-- 安全边界：只按 (item_key, lang) 精确删除，不碰其它 key、不按前缀匹配；
-- 额外语言（如 ja-JP）不在清单里，不会被删。
DELETE FROM sys_i18n
WHERE (item_key, lang) IN (
  ('MsgInternalError', 'zh-CN'), ('MsgInternalError', 'en-US'),
  ('ErrInvalidParams', 'zh-CN'), ('ErrInvalidParams', 'en-US'),
  ('ErrPermissionDenied', 'zh-CN'), ('ErrPermissionDenied', 'en-US'),
  ('ErrRequestEntityTooLarge', 'zh-CN'), ('ErrRequestEntityTooLarge', 'en-US'),
  ('msg_operation_success', 'zh-CN'), ('msg_operation_success', 'en-US'),
  ('msg_delete_confirm', 'zh-CN'), ('msg_delete_confirm', 'en-US'));
