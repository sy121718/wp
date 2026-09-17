-- ========================================
-- 225 · 主题架构简化：删除主题包导入导出权限点 + 「系统页面」菜单并入主题管理
--
-- 产品决定：主题只服务本地样式调整（token / 样式 / 模板的本地定制），不做跨站
-- 打包导入导出（VIS-014 线下线）。代码侧已删除 /api/theme/{export,import} 路由、
-- service 编排、dto 与专用测试；本迁移清理三类存量数据。
--
-- 同时把后台导航的「系统页面」（/admin/site-slots 入口菜单）撤下：槽位绑定页面
-- （/admin/site-slots）、/api/page/site-slot/* 端点与 page:site_slot_* 权限点全部
-- 保留（那是 page 模块的功能），运营从「主题管理」页顶部的入口进入。
--
-- 注意 140 / 220 / 221 三个 seed 已随本迁移一并注销（注册与 SQL 同批删除），
-- 否则它们的存在性判定会在下次启动把删掉的数据重新灌回。
--
-- 幂等：全部为条件删除，重复执行无副作用。
-- 注册：public/migrations/register_core.go（225-remove-theme-bundle-and-site-slots-menu）。
-- ========================================

-- 1) Casbin p 策略：v3 = 权限点码（220 给超管插过的行）。
DELETE FROM sys_casbin_rule
WHERE ptype = 'p'
  AND v3 IN ('project:theme_export', 'project:theme_import');

-- 2) 权限点两行。
DELETE FROM sys_permission
WHERE permission_code IN ('project:theme_export', 'project:theme_import');

-- 3) 「系统页面」菜单行：140 seed 的 /site-slots 与 224 归位后的 /admin/site-slots 都覆盖。
DELETE FROM sys_menus
WHERE type = 2
  AND permission_code = 'page:site_slot_list'
  AND path IN ('/site-slots', '/admin/site-slots');

-- 4) 主题包业务文案词条：只删随导入导出移除的 13 个 key；
--    ErrThemeBundleAssetMissing / ErrThemeBundlePortUnavailable 仍被保留的资产端口使用，不删。
DELETE FROM sys_i18n
WHERE item_key IN (
    'ErrThemeBundleFileRequired', 'ErrThemeBundleFormatUnknown', 'ErrThemeBundleMissingManifest',
    'ErrThemeBundleManifestInvalid', 'ErrThemeBundleVersionTooNew', 'ErrThemeBundleVersionInvalid',
    'ErrThemeBundleUnsafeEntry', 'ErrThemeBundleTooLarge', 'ErrThemeBundleTokensInvalid',
    'ErrThemeBundleBlockMissing', 'ErrThemeBundleBlockCycle',
    'MsgThemeBundleImported', 'MsgThemeBundleImportedPartial'
);
