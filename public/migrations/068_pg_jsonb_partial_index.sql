-- 068 · PG 特性优化（第一批）：块引用 JSONB 路径查询索引 + 软删除部分索引
--
-- 背景（两处 MySQL 式做法的 PG 化）：
--   ① 「哪些页面引用了这个块」此前写成
--        draft_document::text LIKE '%"blockId": "<blockID>"%'
--      把 JSONB 序列化成 text 再逐行子串匹配：类型信息、索引全部放弃，
--      每行都要把整个文档渲染成文本，pages 表随站点规模必然全表扫。
--      查询侧已改写为 jsonb_path_query_array(...) @> jsonb_build_array(?::text)
--      （internal/module/page/model/page_model.go 的 blockRefMatchCond）。
--   ② pages 是软删除表（deleted_at），索引里混着已删行且越积越大。
--
-- 幂等：所有语句 IF NOT EXISTS；register.go 用索引名判定（pages 表早已存在，
-- 默认「表存在即跳过」必然误跳过，与 037/038 同一手法）。

-- ---------------------------------------------------------------------------
-- 1) 块引用：表达式 GIN 索引（部分索引，仅存活页面）
--
-- 查询形态：任意深度的 blockId（core.globalref 可嵌在容器里）→ 必须递归。
-- 为什么不是 GIN(draft_document jsonb_path_ops) + @? '$.**.blockId ? (...)':
--   实测 PG 18 下 jsonb_path_ops 无法索引递归通配，Bitmap Index Scan 返回
--   索引内全部条目（2 万行实验室：18182 行 recheck 后被丢弃，比 seq scan 更慢）。
-- 因此把「文档中全部 blockId 值」物化成 JSONB 数组作为索引表达式，
-- 查询侧用数组包含 @>（jsonb_ops 默认 opclass，数组元素进 GIN 倒排）。
-- 索引体积只随被引用块个数增长，与文档大小无关（2 万行实验室：约 110 kB）。
--
-- 注意：表达式必须用双括号 —— GIN (expr) 里单层括号会被当成「索引方法参数
-- 列表」，多行写法直接语法报错；只有 ((expr)) 才是表达式索引。
--
-- 表达式必须与 page_model.go 的 blockRefMatchCond 一致（PG 按解析后的
-- 表达式树比较，空白无关），否则查询静默退化为全表扫
-- （public/test/page/unit 有 EXPLAIN 断言守住）。
CREATE INDEX IF NOT EXISTS idx_pages_blockref
    ON pages USING GIN ((jsonb_path_query_array(draft_document, '$.**.blockId')))
    WHERE deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- 2) 同一查询的 structure 分支：页眉/页脚绑定的 btree 表达式索引
--
-- 查询形态（page_model.go）：draft_document->'settings'->'structure'->>'headerBlockId' = ?
-- 只给块引用分支建索引不够：OR 里有索引不可用的分支时 planner 无法 BitmapOr，
-- 实测退化为全表扫（55ms）。三个分支各有一个索引后，planner 用 BitmapOr 合并
-- 三个 Bitmap Index Scan（实测 0.27ms）。
--
-- 保留 ->> 的 text 等值语义（而非 jsonb 值比较）：与旧查询逐字等价，
-- 数字/布尔型绑定值的行为不变。
CREATE INDEX IF NOT EXISTS idx_pages_structure_header
    ON pages ((draft_document->'settings'->'structure'->>'headerBlockId'))
    WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_pages_structure_footer
    ON pages ((draft_document->'settings'->'structure'->>'footerBlockId'))
    WHERE deleted_at IS NULL;

-- ---------------------------------------------------------------------------
-- 3) 按主题批量刷新/重挂：部分索引（仅存活页面）
--
-- 查询形态（page_model.go 的 RefreshThemeForTheme / RefreshStructureForTheme /
-- MarkStaleForTheme / ReattachProjectPagesToTheme / ListAll(themeID)）：
--   WHERE theme_id = ? AND deleted_at IS NULL [ORDER BY updated_at DESC]
-- 复合 (theme_id, updated_at DESC) 同时服务等值命中与列表排序；
-- 部分条件 deleted_at IS NULL 与查询条件一致，索引里不留已删行。
CREATE INDEX IF NOT EXISTS idx_pages_theme_alive
    ON pages (theme_id, updated_at DESC) WHERE deleted_at IS NULL;

COMMENT ON INDEX idx_pages_blockref IS
    '块引用（core.globalref 的 blockId）表达式 GIN 索引，仅存活页面；表达式须与 page_model.go blockRefMatchCond 一致';
COMMENT ON INDEX idx_pages_structure_header IS
    'settings.structure 页眉绑定（headerBlockId）表达式索引，仅存活页面';
COMMENT ON INDEX idx_pages_structure_footer IS
    'settings.structure 页脚绑定（footerBlockId）表达式索引，仅存活页面';
COMMENT ON INDEX idx_pages_theme_alive IS
    '按主题刷新/重挂与主题内页面列表，仅存活页面（部分索引 deleted_at IS NULL）';

-- ---------------------------------------------------------------------------
-- 4) 明确跳过的表（不在本迁移建索引，避免「为用 PG 特性而用」）：
--    · presentation_instances.deleted_at：Go 侧 InstanceEntity 无该字段，
--      全部查询按 id / entity_type+entity_id / url_path，从不带 deleted_at 条件；
--      部分索引不会被使用（条件不匹配），故跳过。
--    · content_objects.deleted_at：artifact 模块只做幂等写入（OnConflict DoNothing），
--      无按 deleted_at 的查询；GC 尚未落地，等真正出现查询再加。
--    · sys_* 表的软删除列名为 deleted_time / status（非 deleted_at），
--      不在本批范围（下一批按同样方法核对后处理）。
