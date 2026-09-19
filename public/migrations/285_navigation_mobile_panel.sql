-- 285 · 移动端菜单位置 + 菜单项悬浮面板（超级菜单）
--
-- 两条需求合并在一张表上：
--   1) 桌面与移动端的菜单**数据各自独立**（WP 式：两个位置各绑一条菜单）：
--      kind 从 ('header','footer') 放宽为 ('header','header_mobile','footer','footer_mobile')。
--      为什么走"位置扩展"而不是给记录加 device 维度：kind 本来就是位置维度，解析
--      （Tree/ResolveMenu）、依赖键（menu:{project}:{kind}）、适配器缓存都按 kind 天然区分，
--      扩展只需放宽一处 CHECK + Go 校验；加 device 列要动契约签名与全部调用点，且要处理
--      「同一位置两条 device 冲突」。存量 header/footer 记录与绑定零回归（语义等同桌面端）。
--   2) 超级菜单：菜单项可挂一个全局块作为悬浮面板（panel_block_id）+ 展示宽度（panel_width）。
--      为什么不用 source_type='block'：那是"链接来源"语义（点这一项跳到哪），
--      面板是"悬停展开显示什么"——一个字段两种含义会在渲染与依赖登记上互相干扰。
--      面板内容存块（一处改、多处复用），展示形态存菜单项（同一个块可被多项以不同宽度复用）。
--
-- 幂等：约束按名先删后建（046 的内联 CHECK 由 PG 自动命名为 navigations_kind_check）；
-- 列用 ADD COLUMN IF NOT EXISTS。判定见 register_core.go 的 CheckSQL。

-- 1) 放宽 kind 取值。
ALTER TABLE navigations DROP CONSTRAINT IF EXISTS navigations_kind_check;
DO $$
DECLARE cname text;
BEGIN
    -- 兜底：若历史内联 CHECK 被 PG 起了别的名字，按"约束定义里提到 kind"找出来删掉，
    -- 否则 ADD CONSTRAINT 会与它并存，插入移动端位置时仍被旧约束拒绝。
    FOR cname IN
        SELECT conname FROM pg_constraint
         WHERE conrelid = 'navigations'::regclass
           AND contype = 'c'
           AND pg_get_constraintdef(oid) LIKE '%kind%'
    LOOP
        EXECUTE format('ALTER TABLE navigations DROP CONSTRAINT %I', cname);
    END LOOP;
END $$;
ALTER TABLE navigations ADD CONSTRAINT navigations_kind_check
    CHECK (kind IN ('header', 'header_mobile', 'footer', 'footer_mobile'));

-- 2) 悬浮面板（超级菜单）。
ALTER TABLE navigations ADD COLUMN IF NOT EXISTS panel_block_id uuid NULL
    REFERENCES blocks(id) ON DELETE SET NULL;
ALTER TABLE navigations ADD COLUMN IF NOT EXISTS panel_width text NOT NULL DEFAULT 'auto';

ALTER TABLE navigations DROP CONSTRAINT IF EXISTS navigations_panel_width_check;
ALTER TABLE navigations ADD CONSTRAINT navigations_panel_width_check
    CHECK (panel_width IN ('auto', 'full'));

COMMENT ON COLUMN navigations.panel_block_id IS '悬浮面板引用的全局块（NULL=无面板，超级菜单）';
COMMENT ON COLUMN navigations.panel_width IS '面板展示宽度 auto=跟随内容 / full=通栏';
COMMENT ON COLUMN navigations.kind IS '菜单位置：header/header_mobile/footer/footer_mobile（桌面与移动端各自独立）';
