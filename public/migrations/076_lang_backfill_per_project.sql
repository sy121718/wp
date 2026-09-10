-- ========================================
-- go_wp — 按工程修正多工程环境下的语言回填错误
--
-- 背景（全项目审查发现）：
--   061_page_artifacts_lang.sql 的回填取的是「全局第一个工程」的默认语言：
--     SELECT ... FROM projects ORDER BY created_at LIMIT 1  INTO v_lang
--     UPDATE page_artifacts SET lang = COALESCE(v_lang, 'zh-CN')   -- 全表
--   多工程环境下，所有工程的页面都被标成同一个语言（配置更早的那个工程的默认语言）。
--   062 的 page_publications 回填又直接继承 page_artifacts.lang，错误被复制一份。
--
-- 本迁移按「页面归属工程的 defaultLang」逐行重算，修正已跑过 061 的历史库。
-- 061/062 的源文件已同步修正，新库不再产生该错误。
--
-- 注册：public/migrations/register.go（Migration 076-lang-backfill-per-project）。
-- ========================================
UPDATE page_artifacts a
SET lang = COALESCE(
        (SELECT NULLIF(TRIM(COALESCE(p.settings ->> 'defaultLang', p.settings ->> 'default_lang')), '')
           FROM pages pg
           JOIN projects p ON p.id = pg.project_id
          WHERE pg.id = a.page_id),
        'zh-CN')
WHERE a.lang IS DISTINCT FROM COALESCE(
        (SELECT NULLIF(TRIM(COALESCE(p.settings ->> 'defaultLang', p.settings ->> 'default_lang')), '')
           FROM pages pg
           JOIN projects p ON p.id = pg.project_id
          WHERE pg.id = a.page_id),
        'zh-CN')
  AND EXISTS (SELECT 1 FROM pages pg WHERE pg.id = a.page_id);
