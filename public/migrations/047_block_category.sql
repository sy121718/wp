-- 047_block_category.sql — 全局块自由分类（category 文本字段）
-- category 是组织/筛选维度（管理端/工作台按分类分组筛选，如商品区块/文章区块/营销区块），
-- 不改变引用维度：页眉/页脚仍经主题 headerBlockId/footerBlockId 绑定，内容块仍经 globalref 按 blockId 引用。
-- 默认 "general"；值白名单 ^[a-z0-9_-]{1,50}$ 在 service 层校验，这里仅落列与索引。

ALTER TABLE blocks ADD COLUMN IF NOT EXISTS category text NOT NULL DEFAULT 'general';

-- 工程内按分类筛选的复合索引。
CREATE INDEX IF NOT EXISTS idx_blocks_category ON blocks(project_id, category);
