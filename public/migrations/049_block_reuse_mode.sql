-- 049_block_reuse_mode.sql — 全局块复用方式维度（docs/02-D §5/§8.1）
-- reuse_mode 区分两种复用语义：
--   global   全局引用：页面存 block_id，改一处 → stale 传播 → 所有引用页重建（现状语义，默认）；
--   template 一次性复制：插入页面时复制完整 AST（重生成 Node ID），此后与源块互不影响，不传播 stale。
-- 存量块全部视为 global，语义不变，向后兼容。

ALTER TABLE blocks
    ADD COLUMN IF NOT EXISTS reuse_mode text NOT NULL DEFAULT 'global'
        CHECK (reuse_mode IN ('global', 'template'));

-- 工程内按复用方式筛选的复合索引（复用资产列表筛选）。
CREATE INDEX IF NOT EXISTS idx_blocks_reuse_mode ON blocks(project_id, reuse_mode);
