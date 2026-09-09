-- 055 · sys_i18n 补 category / remark 列
--
-- 背景：a2 历史库（public/backup/a2.sql）的 sys_i18n 含 category/remark，
-- 而 init_schema.sql 建表时缺失，导致词条分类与来源备注无处存放。
-- 幂等：ADD COLUMN IF NOT EXISTS / CREATE INDEX IF NOT EXISTS；
-- 注册见 register.go（CheckSQL 按 category 列是否存在判定，避免表存在即误跳过）。
ALTER TABLE sys_i18n ADD COLUMN IF NOT EXISTS category VARCHAR(20);
ALTER TABLE sys_i18n ADD COLUMN IF NOT EXISTS remark VARCHAR(200);
CREATE INDEX IF NOT EXISTS idx_sys_i18n_category ON sys_i18n(category);
COMMENT ON COLUMN sys_i18n.category IS '分类：error/ui/msg';
COMMENT ON COLUMN sys_i18n.remark IS '备注说明（词条来源文件等）';
