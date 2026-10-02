-- ========================================
-- 507 · 分区子表的工程隔离对账（审计 DB-02）
--
-- 【缺陷】迁移 215 用**静态数组**列出要加策略的分区名：
--     page_views_2026_08 … page_views_2026_12、inventory_stock_movements_2026_08…12、
--     master_data_changes_2026_08…12 及各自的 _default
-- 而迁移 173 是按**部署时间动态**建分区的（to_char(cur,'YYYY_MM')，从 MIN(分区键) 到 now()+2 月），
-- internal/partition.EnsureAhead 又只回补 [上月, +3]（永不回头补历史分区）。
-- 两者一叠加，结论就很具体：**部署月不在 2026-08..12 的库里**，
-- 173 与 EnsureAhead 建出来的分区不落在那份名单里，于是没有策略；
-- 而 PostgreSQL 的 ENABLE / FORCE ROW LEVEL SECURITY **不递归到分区**
-- （215 的文件头自己写明了这一点，policy 也不继承）。
--
-- 影响面要说准：应用读写都走父表，父表策略会一并约束，**功能路径不受影响**。
-- 真正裸奔的是「按分区名直查」这条路 —— 排障、归档、报表、手写 SQL，
-- 全都绕开工程隔离而不报错。
--
-- 【修法：不重复任何字面量】
-- 本迁移**不抄**那份分区名单，也**不抄**策略谓词，更**不假定策略名** ——
-- 它把父表上的**每一条**策略逐字段复制到每个叶子分区。判据因此是一句自我维持的话：
--     「根表上有的策略，它的每个叶子分区都要有同款」
--   · 没有第二份分区名单 —— 新增分区自动纳入（这是本迁移存在的全部意义）；
--   · 没有第二份谓词字面量 —— 逐字复制父表的 pg_get_expr 输出；
--   · 没有第二份策略名约定 —— 全库实际存在 4 个策略名（`project_isolation` 与
--     `p_comments_project` / `p_membership_tiers_project` / `p_membership_assignments_project`，
--     见 462/466 系列），按名字过滤的判据会把后三张表整片当成「没有隔离」。
--     复制全部策略而不是「复制那条叫 project_isolation 的」。
--   · 白名单不需要维护 —— 父表没有任何策略（刻意豁免的表）本迁移就不碰。
--
-- 五维度逐字段复制（少复制任何一个都是一种静默偏差）：
--   polname（策略名）、polpermissive（PERMISSIVE / RESTRICTIVE）、polcmd（SELECT/INSERT/
--   UPDATE/DELETE/ALL）、polroles（TO 哪些角色，0 = PUBLIC）、polqual + polwithcheck（谓词）。
-- 子句按 cmd 组装而不是一律 USING + WITH CHECK：PostgreSQL 对不匹配的子句会直接报错
-- （INSERT 策略不接受 USING，SELECT / DELETE 策略不接受 WITH CHECK）。
--
-- 用 pg_partition_tree() 而不是只 join 一层 pg_inherits：嵌套分区（分区再分区）下，
-- 叶子分区的直接父级是中间层而不是根表，只看一层会按中间层的策略判定 ——
-- 中间层没有策略就整片跳过，缺口静默保留。isleaf 直接表达「这是真实存储表」。
--
-- 【幂等】稳态（ENABLE + FORCE + 策略名集合覆盖父表）整体 CONTINUE，只做只读检查、零 DDL ——
-- 这一点是刻意的：ALTER TABLE … ENABLE ROW LEVEL SECURITY 取 ACCESS EXCLUSIVE 锁，
-- 而本迁移注册时**不带 TableName**（见 register_partition_rls_reconcile.go），
-- 于是每次启动都会执行；不做快速路径就等于每次启动给所有分区上一遍排它锁。
--
-- 有意保留的边界：快速路径只比**策略名集合**，不比谓词内容。父表谓词若被改写，
-- 本迁移不会把分区上的旧谓词换掉 —— pg_get_expr 的文本归一化（::text 强转、括号层级）
-- 让逐字比对不可靠，拿它当判据会退化成「每次启动重建全部策略」。真要改谓词，
-- 请在**同一条新迁移**里显式 DROP POLICY 后重建，不要指望这里。
-- ========================================

DO $$
DECLARE
    rec     RECORD;
    rec_pol RECORD;
    v_cmd   text;
    v_kind  text;
    v_roles text;
    v_using text;
    v_check text;
    v_stmt  text;
BEGIN
    FOR rec IN
        SELECT root.oid     AS root_oid,
               leaf.relname AS child
          FROM pg_class root
          JOIN pg_namespace n ON n.oid = root.relnamespace
          CROSS JOIN LATERAL pg_partition_tree(root.oid) AS pt
          JOIN pg_class leaf ON leaf.oid = pt.relid
         WHERE n.nspname = current_schema()
           AND root.relkind = 'p'
           AND pt.isleaf
           AND EXISTS (SELECT 1 FROM pg_policy pol WHERE pol.polrelid = root.oid)
         ORDER BY 1, 2
    LOOP
        -- 稳态快速路径：ENABLE + FORCE 齐备，且父表的策略名集合已被分区覆盖。
        -- 「父表策略名 EXCEPT 分区策略名 = 空」即覆盖（见上面的边界说明：只比名字）。
        IF EXISTS (
                SELECT 1
                  FROM pg_class c
                  JOIN pg_namespace n ON n.oid = c.relnamespace
                 WHERE n.nspname = current_schema()
                   AND c.relname = rec.child
                   AND c.relrowsecurity
                   AND c.relforcerowsecurity)
           AND NOT EXISTS (
                SELECT pol.polname FROM pg_policy pol WHERE pol.polrelid = rec.root_oid
                EXCEPT
                SELECT pol2.polname
                  FROM pg_policy pol2
                  JOIN pg_class c2 ON c2.oid = pol2.polrelid
                  JOIN pg_namespace n2 ON n2.oid = c2.relnamespace
                 WHERE n2.nspname = current_schema()
                   AND c2.relname = rec.child) THEN
            CONTINUE;
        END IF;

        EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', rec.child);
        EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', rec.child);

        FOR rec_pol IN
            SELECT pol.polname,
                   pol.polpermissive,
                   pol.polcmd,
                   pol.polroles,
                   pg_get_expr(pol.polqual, pol.polrelid)      AS qual,
                   pg_get_expr(pol.polwithcheck, pol.polrelid) AS wcheck
              FROM pg_policy pol
             WHERE pol.polrelid = rec.root_oid
             ORDER BY pol.polname
        LOOP
            v_cmd := CASE rec_pol.polcmd
                         WHEN 'r' THEN 'SELECT'
                         WHEN 'a' THEN 'INSERT'
                         WHEN 'w' THEN 'UPDATE'
                         WHEN 'd' THEN 'DELETE'
                         ELSE 'ALL'
                     END;
            v_kind := CASE WHEN rec_pol.polpermissive THEN 'PERMISSIVE' ELSE 'RESTRICTIVE' END;
            -- polroles = {0} 表示 PUBLIC；pg_roles 里没有 oid = 0 的行，
            -- 于是 string_agg 返回 NULL，由 COALESCE 落到 'PUBLIC'。
            v_roles := COALESCE(
                (SELECT string_agg(quote_ident(r.rolname), ', ' ORDER BY r.rolname)
                   FROM unnest(rec_pol.polroles) AS ro(oid)
                   JOIN pg_roles r ON r.oid = ro.oid),
                'PUBLIC');
            -- 直接取父表的两个谓词，**不做任何补齐**：缺省就是缺省。
            -- 「WITH CHECK 缺省时默认等于 USING」是 PostgreSQL 自己的语义，
            -- 这里把 USING 复制成 WITH CHECK 虽然等价，却让子表多出一条父表没有的子句 ——
            -- 于是「逐字复制」这条判据失效（测试与人工核对都会看到不一致，
            -- 而排查者需要先知道 PG 的这条默认规则才能确认它无害）。
            v_using := rec_pol.qual;
            v_check := rec_pol.wcheck;

            -- DROP + CREATE 而不是 CREATE IF NOT EXISTS：PG 的 CREATE POLICY 没有 IF NOT EXISTS，
            -- 而「策略存在但 ENABLE 缺失」「策略名在但谓词旧」这些半残状态必须能修（重放安全）。
            EXECUTE format('DROP POLICY IF EXISTS %I ON %I', rec_pol.polname, rec.child);

            -- 子句按 cmd 与「父表给了什么」两段判断：
            --   · SELECT / DELETE 不接受 WITH CHECK；INSERT 不接受 USING（PG 会直接报错）；
            --   · ALL 形态下父表若只给了 USING，子表也只给 USING（见上）。
            IF v_cmd IN ('SELECT', 'DELETE') THEN
                v_stmt := format('CREATE POLICY %I ON %I AS %s FOR %s TO %s USING %s',
                                 rec_pol.polname, rec.child, v_kind, v_cmd, v_roles,
                                 COALESCE(v_using, 'true'));
            ELSIF v_cmd = 'INSERT' THEN
                v_stmt := format('CREATE POLICY %I ON %I AS %s FOR %s TO %s WITH CHECK %s',
                                 rec_pol.polname, rec.child, v_kind, v_cmd, v_roles,
                                 COALESCE(v_check, 'true'));
            ELSIF v_using IS NULL THEN
                v_stmt := format('CREATE POLICY %I ON %I AS %s FOR %s TO %s WITH CHECK %s',
                                 rec_pol.polname, rec.child, v_kind, v_cmd, v_roles,
                                 COALESCE(v_check, 'true'));
            ELSIF v_check IS NULL THEN
                v_stmt := format('CREATE POLICY %I ON %I AS %s FOR %s TO %s USING %s',
                                 rec_pol.polname, rec.child, v_kind, v_cmd, v_roles, v_using);
            ELSE
                v_stmt := format('CREATE POLICY %I ON %I AS %s FOR %s TO %s USING %s WITH CHECK %s',
                                 rec_pol.polname, rec.child, v_kind, v_cmd, v_roles, v_using, v_check);
            END IF;
            EXECUTE v_stmt;
        END LOOP;
    END LOOP;
END $$;
