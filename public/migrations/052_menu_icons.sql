-- ========================================
-- go_wp — 后台菜单/目录/按钮图标补全
--
-- 背景：sys_menus 的历史数据里 type=2 菜单 6 条、type=3 按钮 23 条均无图标，
-- type=1 目录为旧格式 i-ep:set-up（Element Plus 图标名，当前图标库不识别）。
-- 本迁移统一替换为内置图标库（internal/templates/static/js/icons.js，lucide 风格）的名称。
--
-- 幂等：仅更新「图标为空 或 为旧格式 i-ep:*」的行，重复执行安全。
-- 注册：public/migrations/register.go（Seed 052-menu-icons）。
-- ========================================
UPDATE sys_menus m
SET icon = v.icon, update_time = NOW()
FROM (VALUES
    -- 目录（type=1）
    ('站点工程',   'layout-grid'),
    -- 菜单（type=2）
    ('项目管理',   'folder-kanban'),
    ('页面管理',   'file-text'),
    ('区块管理',   'blocks'),
    ('媒体管理',   'image'),
    ('构建产物',   'package'),
    ('发布管理',   'rocket'),
    ('插件管理',   'puzzle'),
    -- 按钮（type=3）：项目
    ('新建项目',   'folder-plus'),
    ('项目详情',   'file-search'),
    ('更新项目',   'pencil'),
    -- 按钮：页面
    ('新建页面',   'file-plus'),
    ('页面详情',   'file-search'),
    ('保存草稿',   'save'),
    ('修订记录',   'clock'),
    ('构建页面',   'hammer'),
    ('发布页面',   'rocket'),
    ('回滚页面',   'rotate-ccw'),
    ('更新页面URL','link'),
    -- 按钮：区块
    ('新建区块',   'plus'),
    ('区块详情',   'file-search'),
    ('更新区块',   'pencil'),
    ('删除区块',   'trash-2'),
    -- 按钮：媒体
    ('上传媒体',   'upload'),
    ('媒体详情',   'file-search'),
    ('更新媒体',   'pencil'),
    ('删除媒体',   'trash-2'),
    ('媒体分类树', 'folder-tree'),
    ('新建分类',   'folder-plus'),
    ('更新分类',   'pencil'),
    ('删除分类',   'trash-2')
) AS v(title, icon)
WHERE m.title = v.title
  AND m.deleted_at IS NULL
  AND (m.icon IS NULL OR m.icon = '' OR m.icon LIKE 'i-ep:%');
