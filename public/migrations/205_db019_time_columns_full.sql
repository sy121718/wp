-- 205 · DB-019 全量收口：时间列命名统一为 create_time / update_time。
--
-- 决策（YG 拍板，见迁移 201 注释）：时间列一律用 *_time 一套，放弃 *_at。
-- 201 只改了 5 张零入度叶子表的 created_at（且漏掉了它们的 updated_at），
-- 本迁移把剩余 46 张表的 created_at 与 33 张表的 updated_at 一次收口。
--
-- 口径：
--   · 显式表清单 + 列存在性判定，不用 information_schema 全表扫描 —— 后者会把
--     将来新建表的列也一并改掉；迁移不该有「跟着未来走」的行为。
--   · 分区子表（inventory_stock_movements_* / master_data_changes_*）一并列入：
--     PG 的 RENAME COLUMN 会级联到分区，谁先执行谁生效，存在性判定让后执行者自动跳过。
--   · 索引 / 视图 / 触发器里的列引用由 PG 自动跟随，无需重建（201 已实测）。
--
-- 注册侧判定：目标列一条都不剩（含分区）才算完成，否则整段重放。

DO $$
DECLARE
  t text;
BEGIN
  FOREACH t IN ARRAY ARRAY[
    'blueprint_versions', 'blueprints', 'content_objects',
    'content_template_versions', 'content_templates', 'contents',
    'document_snapshots', 'inventory_bom_items', 'inventory_purchase_order_lines',
    'inventory_purchase_orders', 'inventory_purchase_receipt_items', 'inventory_purchase_receipts',
    'inventory_sources', 'inventory_stock_movements', 'inventory_stock_movements_2026_09',
    'inventory_stock_movements_2026_10', 'inventory_stock_movements_2026_11', 'inventory_stock_movements_default',
    'inventory_stocks', 'inventory_warehouses', 'master_data_changes',
    'master_data_changes_2026_09', 'master_data_changes_2026_10', 'master_data_changes_2026_11',
    'master_data_changes_default', 'navigations', 'page_artifacts',
    'page_revisions', 'pages', 'presentation_artifacts',
    'presentation_instances', 'product_attributes', 'product_brands',
    'product_categories', 'product_price_adjustment_items', 'product_price_adjustments',
    'product_ratings', 'product_tags', 'product_variants',
    'products', 'project_locales', 'projects',
    'publication_events', 'themes', 'webhook_deliveries',
    'webhook_endpoints'
  ] LOOP
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = current_schema() AND table_name = t AND column_name = 'created_at') THEN
      EXECUTE format('ALTER TABLE %I RENAME COLUMN created_at TO create_time', t);
    END IF;
  END LOOP;
END $$;

DO $$
DECLARE
  t text;
BEGIN
  FOREACH t IN ARRAY ARRAY[
    'blocks', 'blueprints', 'content_templates',
    'contents', 'inventory_bom_items', 'inventory_change_reasons',
    'inventory_purchase_order_lines', 'inventory_purchase_orders', 'inventory_sources',
    'inventory_stocks', 'inventory_warehouses', 'navigations',
    'page_publications', 'page_routes', 'page_site_slots',
    'page_stagings', 'pages', 'plugin_registry',
    'presentation_instances', 'presentation_publications', 'product_attributes',
    'product_brands', 'product_categories', 'product_ratings',
    'product_tags', 'product_variants', 'products',
    'project_locales', 'projects', 'sys_translation',
    'themes', 'webhook_deliveries', 'webhook_endpoints'
  ] LOOP
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = current_schema() AND table_name = t AND column_name = 'updated_at') THEN
      EXECUTE format('ALTER TABLE %I RENAME COLUMN updated_at TO update_time', t);
    END IF;
  END LOOP;
END $$;
