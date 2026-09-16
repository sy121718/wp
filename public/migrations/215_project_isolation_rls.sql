-- ========================================
-- 215 · 全量工程隔离 RLS（DB-009 主体）
--
-- 背景：DB-009 此前只落了 project_locales 一张表（迁移 199 的试点）。实测全库
-- 带 project_id 的对象有 **54 个**（40 张基表 + 14 个月分区子表），其余 53 个
-- 完全依赖应用层 Where("project_id = ?")。任何一处漏加条件就是跨工程读写，
-- 数据库层没有第二道防线。
--
-- 策略谓词与 199 试点逐字一致：
--     project_id = NULLIF(current_setting('app.project_id', true), '')::uuid
-- current_setting 第二参 true = 变量未设置时返回 NULL 而不是报错 ⇒ 谓词为 NULL
-- ⇒ 行不可见（**fail closed**）。绝不允许「忘记设变量就退化为全表可见」。
--
-- FORCE ROW LEVEL SECURITY：表属主（应用连接用户 root）也受策略约束 —— 不加它
-- 的话属主身份直接绕过隔离，等于没做。
--
-- 两张可空表（inventory_change_reasons / sys_translation）多一个 IS NULL 分支：
-- project_id IS NULL 表示**全局行**（内置变动原因、全局词条），对所有工程可见。
-- WITH CHECK 用**同一个**谓词而不是只允许本工程 —— 否则 seed 写内置原因会被挡死，
-- 而 seed 用的就是迁移连接（FORCE 之后属主同样受 WITH CHECK 约束）。
--
-- 分区子表单独列出：**PG 的 ENABLE/FORCE 不递归到分区**（实测父表 relrowsecurity=t
-- 而子表全为 f），不列就等于「直接查子表」这条路完全没有隔离。应用路径都走父表，
-- 所以这不是当下的漏洞，但它是将来「排障时手写分区名」的坑。
-- 新分区由 internal/partition.EnsureAhead 在建表后补同样的设置。
--
-- 幂等：判定按哨兵表 orders 上的 policy 存在（整个 DO 块是一条语句，要么全做要么
-- 整体回滚，不需要逐表判定）；表不存在时 CONTINUE（不同库的迁移进度可能不同）。
-- DROP POLICY IF EXISTS + CREATE 保证重放安全。
--
-- 注意：本迁移只建策略，**不负责给应用路径设置 app.project_id** ——
-- 那是各模块 model 的 withProjectScope（见 pkg/rls 与各 model）。
-- 迁移跑完而调用方未改造的表现是「查不到数据」而不是报错，这是 fail closed 的代价，
-- 也是它比「静默读到别人的数据」更好的地方。
--
-- 注册：public/migrations/register_analytics.go（Migration 215-project-isolation-rls）。
-- ========================================

DO $$
DECLARE
    t         text;
    pred      text;
    scoped    text := '(project_id = NULLIF(current_setting(''app.project_id'', true), '''')::uuid)';
    global    text := '(project_id IS NULL OR project_id = NULLIF(current_setting(''app.project_id'', true), '''')::uuid)';
BEGIN
    FOREACH t IN ARRAY ARRAY[
        -- 内容与站点结构
        'blocks', 'content_templates', 'navigations', 'pages', 'presentation_instances',
        'project_locales', 'themes', 'page_routes', 'page_site_slots',
        -- 商品域
        'products', 'product_attributes', 'product_brands', 'product_categories',
        'product_price_adjustments', 'product_ratings', 'product_tags',
        -- 库存与采购
        'inventory_bom_items', 'inventory_change_reasons', 'inventory_purchase_order_lines',
        'inventory_purchase_orders', 'inventory_purchase_receipt_items', 'inventory_purchase_receipts',
        'inventory_sources', 'inventory_stocks', 'inventory_warehouses',
        -- 订单与营销
        'orders', 'order_returns', 'coupons', 'coupon_redemptions',
        -- 统计与审计（含分区父表与当前已存在的子表）
        'analytics_daily_stats', 'page_views', 'page_views_daily',
        'page_views_2026_08', 'page_views_2026_09', 'page_views_2026_10',
        'page_views_2026_11', 'page_views_2026_12', 'page_views_default',
        'inventory_stock_movements',
        'inventory_stock_movements_2026_08', 'inventory_stock_movements_2026_09',
        'inventory_stock_movements_2026_10', 'inventory_stock_movements_2026_11',
        'inventory_stock_movements_2026_12', 'inventory_stock_movements_default',
        'master_data_changes',
        'master_data_changes_2026_08', 'master_data_changes_2026_09',
        'master_data_changes_2026_10', 'master_data_changes_2026_11',
        'master_data_changes_2026_12', 'master_data_changes_default',
        -- 全局字典（project_id 可空：NULL = 全工程可见）
        'sys_translation'
    ] LOOP
        IF to_regclass(t) IS NULL THEN
            CONTINUE;
        END IF;

        IF t IN ('inventory_change_reasons', 'sys_translation') THEN
            pred := global;
        ELSE
            pred := scoped;
        END IF;

        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('DROP POLICY IF EXISTS project_isolation ON %I', t);
        EXECUTE format('CREATE POLICY project_isolation ON %I USING ' || pred || ' WITH CHECK ' || pred, t);
    END LOOP;
END $$;
