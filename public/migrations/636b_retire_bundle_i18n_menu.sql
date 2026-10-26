-- ========================================
-- 636b · 捆绑品（bundle）下线：词条与菜单的存量清理
--
-- 与 636_drop_bundle.sql（结构 Migration）配对：那边删表 / 列 / 约束 / 权限点，
-- 这边删**数据**——理由与 418 / 419 / 458 一致：
--
--   · 删除必须在**所有写过这些行的 seed 之后**执行，否则同一轮 RunSeeds 里
--     「先插后删」的顺序会被破坏（Migrations 全部先跑、Seeds 后跑，migrator.go）；
--   · 本批已把 156 / 191 / 232 / 247 / 252 / 314 / 315 / 430 / 449 九个 seed 的 SQL
--     与幂等条件同批收口（门禁 count 同步下调），所以两处都不会再把它们插回来。
--
-- 词条清单按来源分四类（逐条枚举，不用宽 LIKE —— 宽 LIKE 会误伤将来同前缀的新词条，
-- 而「删了不该删的」不会报错，只会静默少一条文案）：
--   · admin.product_bundle.*         捆绑配置页整页（116 菜单配套，191/232/259/444/599/449）
--   · admin.product_detail.bundle.*  商品详情页「捆绑构成」只读区块（247/259）
--   · site.fragment.bundle.*         访客面配置器片段（158）
--   · 商品类型 / 主体 SKU 相关的零散文案（239/249/314/315）：type 下拉与必填提示已随模板删除
--   · enums 常量名即 key 的那批（249/259/430）：ErrBundle* / MsgBundle* / BundleMember* /
--     BundleSource* / ErrProductType* / ErrWarehouseSKUBundleNotAllowed / VariantSkipBundleReferenced
--
-- 菜单：/admin/products/bundle 由 116 建立、250 只把它 status 置 0（保留行以便恢复）。
-- 页面与权限点都已不存在，本批改为软删（deleted_at）——保留行，角色授权按 menu_id
-- 收集权限码，删行会让已授权角色静默缩权（224 的注释里记着这条取舍）。
--
-- 幂等：DELETE / UPDATE 都是条件式的，重复执行影响 0 行。
-- ========================================

DELETE FROM sys_i18n WHERE item_key IN (
    'admin.products.label.type',
    'admin.products.type.variant',
    'admin.products.type.bundle',
    'admin.products.edit.typeNote',
    'admin.products.edit.bundleHint',
    'admin.products.ph.skuBundle',
    'admin.products.sku.bundleHint',
    'admin.products.sku.bundleManual',
    'admin.products.sku.regenerate',
    'ErrProductTypeInvalid',
    'ErrProductTypeImmutable',
    'ErrBundlePriceRequired',
    'ErrBundleSKURequired',
    'VariantSkipBundleReferenced',
    'ErrWarehouseSKUBundleNotAllowed'
);

-- 前缀类（上面四类整片）用 range 判定而不是 LIKE：保留可枚举性，且不会误伤新前缀。
DELETE FROM sys_i18n
 WHERE item_key LIKE 'admin.product_bundle.%'
    OR item_key LIKE 'admin.product_detail.bundle.%'
    OR item_key LIKE 'site.fragment.bundle.%'
    OR item_key LIKE 'ErrBundle%'
    OR item_key LIKE 'MsgBundle%'
    OR item_key LIKE 'BundleMember%'
    OR item_key LIKE 'BundleSource%';

UPDATE sys_menus
   SET deleted_at = now(), update_time = now()
 WHERE path = '/admin/products/bundle'
   AND deleted_at IS NULL;
