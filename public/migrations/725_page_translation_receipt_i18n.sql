-- 页面译文保存回执改为完整短词条 + 具名参数 {count}；旧点位参数来源退役。
WITH keys(old_key, new_key, retire) AS (VALUES
    ('admin.page_translations.saved', 'pageTrSaved', true),
    ('admin.page_translations.saved_rebuild', 'pageTrRebuild', true)
), guard AS MATERIALIZED (
    SELECT CASE WHEN COUNT(*) = 0 THEN 1
        ELSE ('页面译文回执冲突: ' || string_agg(k.old_key || ' -> ' || k.new_key || '/' || s.lang, ', '))::integer END AS ok
    FROM keys k
    JOIN sys_i18n s ON s.item_key = k.old_key
    JOIN sys_i18n t ON t.item_key = k.new_key AND t.lang = s.lang
    -- 本批会改写占位符，值必然与来源不同；冲突只看元数据是否被另一个目标行占用。
    WHERE ROW(t.http_code, t.category, t.remark, t.status)
        IS DISTINCT FROM ROW(s.http_code, s.category, s.remark, s.status)
), moved AS (
    UPDATE sys_i18n s
    SET item_key = k.new_key,
        item_value = REPLACE(s.item_value, '%s', '{count}')
    FROM keys k, guard g
    WHERE g.ok = 1 AND s.item_key = k.old_key
      AND NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = k.new_key AND t.lang = s.lang)
    RETURNING s.id
), retired AS (
    DELETE FROM sys_i18n s USING keys k, guard g
    WHERE g.ok = 1 AND s.item_key = k.old_key
      AND EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = k.new_key AND t.lang = s.lang)
    RETURNING s.id
)
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
SELECT d.item_key, d.lang, d.item_value, d.http_code, d.category, d.remark, d.status, d.create_time, d.update_time
FROM (VALUES
    ('pageTrSaved', 'zh-CN', '已保存 {count} 条译文。', 200, 'page', 'admin/page/page_translations_saved.html', 1, now(), now()),
    ('pageTrSaved', 'en-US', 'Saved {count} translations.', 200, 'page', 'admin/page/page_translations_saved.html', 1, now(), now()),
    ('pageTrRebuild', 'zh-CN', '已保存 {count} 条译文。译文变更已标记全站待重建（下次构建生效）。', 200, 'page', 'admin/page/page_translations_saved.html', 1, now(), now()),
    ('pageTrRebuild', 'en-US', 'Saved {count} translations. The changes marked the whole site for rebuild (they take effect at the next build).', 200, 'page', 'admin/page/page_translations_saved.html', 1, now(), now())
) AS d(item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
LEFT JOIN keys k ON k.new_key = d.item_key CROSS JOIN guard g
WHERE g.ok = 1
  AND NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = d.item_key AND t.lang = d.lang)
  AND NOT EXISTS (SELECT 1 FROM sys_i18n s WHERE s.item_key = k.old_key AND s.lang = d.lang);
