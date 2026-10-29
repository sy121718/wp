-- ========================================
-- 677 — block 域「并发版本冲突」的中英词条（业务区块解耦 B2）
--
-- 背景：block 保存加了乐观并发（迁移 676 的 blocks.version）。过期版本保存返回 409，
--   对外文案是 ErrBlockVersionConflict。本批只补这一条 key 的两语言行。
--
-- 为什么这一条必须自己带 seed：写成形 2（`ErrBlockVersionConflict|<版本号>`）的
--   参数化消息，译文里要有 %s 占位。没有 en-US 行时英文界面退回 zh-CN 兜底 ——
--   而这条消息是**用户可重试动作**的唯一解释（「刷新后重试」），退回中文等于没解释。
--
-- http_code 取 409（与 service 里 facing.WithStatus 声明的一致）：
--   两语言行取值必须相同，运行时取默认语言那一行。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；不改既有词条的值。
-- ========================================

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrBlockVersionConflict', 'zh-CN', '该全局块已被其他人修改（当前第 %s 版），请刷新后重试', 409, 'block', 'block/service: 保存时 expectedVersion 与库中不一致（B2 迁移 676）', 1, now(), now()),
('ErrBlockVersionConflict', 'en-US', 'This global block was changed by someone else (now at version %s); refresh and try again', 409, 'block', 'block/service: stale expectedVersion on save (B2 migration 676)', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
