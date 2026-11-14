-- 753 · 文章正文导入结论词条改为短 key（规范：仅 ASCII 字母数字、首字符为字母、1–15 字符）。
--
-- 750a 引入了两条点分长 key（admin.article.ok.imported / admin.article.edit.visualOpen），
-- 不符合 UI 词条 key 规则。这里按「旧 key → 新 key」原位改名：
--   · 旧行原位 UPDATE item_key，保留 id、全部语言、运营自定义译文与元数据；
--   · 目标 (item_key, lang) 已存在时先整批核对元数据，一致才删旧重复行；
--   · 任一处冲突整条语句报可定位的 `old -> new/lang` 并回滚，绝不覆盖/加后缀/静默选择；
--   · 旧、新都不存在的语言才补默认，且放在搬迁之后，避免先种默认制造冲突。
WITH keys(old_key, new_key) AS (VALUES
    ('admin.article.ok.imported', 'artImportOk'),
    ('admin.article.edit.visualOpen', 'artVisualOpen')
), guard AS MATERIALIZED (
    SELECT CASE WHEN COUNT(*) = 0 THEN 1
        ELSE ('导入结论词条冲突: ' || string_agg(
            k.old_key || ' -> ' || k.new_key || '/' || s.lang, ', ' ORDER BY k.old_key, s.lang
        ))::integer END AS ok
    FROM keys k
    JOIN sys_i18n s ON s.item_key = k.old_key
    JOIN sys_i18n t ON t.item_key = k.new_key AND t.lang = s.lang
    WHERE ROW(t.item_value, t.http_code, t.category, t.remark, t.status, t.create_time, t.update_time)
        IS DISTINCT FROM ROW(s.item_value, s.http_code, s.category, s.remark, s.status, s.create_time, s.update_time)
), moved AS (
    UPDATE sys_i18n s SET item_key = k.new_key
    FROM keys k, guard g
    WHERE g.ok = 1 AND s.item_key = k.old_key
      AND NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = k.new_key AND t.lang = s.lang)
    RETURNING s.id
), deduped AS (
    DELETE FROM sys_i18n s USING keys k, guard g
    WHERE g.ok = 1 AND s.item_key = k.old_key
      AND EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = k.new_key AND t.lang = s.lang)
    RETURNING s.id
)
-- 搬迁与补种分开执行：同一条语句里的 UPDATE 结果对后面的 NOT EXISTS 不可见，
-- 先改名再补默认会撞上刚搬过来的行（23505）。guard 在这里再读一次，冲突时值为 0。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
SELECT d.item_key, d.lang, d.item_value, d.http_code, d.category, d.remark, d.status, now(), now()
FROM (VALUES
    ('artImportOk', 'zh-CN', '页面草稿已创建。', 200, 'admin', 'admin/content/article_imported.html: 导入结论', 1),
    ('artImportOk', 'en-US', 'Page draft created.', 200, 'admin', 'admin/content/article_imported.html: 导入结论', 1),
    ('artVisualOpen', 'zh-CN', '打开可视化编辑器', 200, 'admin', 'admin/content/article_imported.html: 导入后续链接', 1),
    ('artVisualOpen', 'en-US', 'Open the visual editor', 200, 'admin', 'admin/content/article_imported.html: 导入后续链接', 1)
) AS d(item_key, lang, item_value, http_code, category, remark, status)
CROSS JOIN guard g
WHERE g.ok = 1
  AND NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = d.item_key AND t.lang = d.lang)
  AND NOT EXISTS (SELECT 1 FROM sys_i18n s JOIN keys k ON s.item_key = k.old_key WHERE k.new_key = d.item_key AND s.lang = d.lang);
