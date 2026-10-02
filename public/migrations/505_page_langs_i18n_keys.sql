-- 505 · 「key 被用但没 seed」的词条补齐：语言产出面板 16 条 + 另 4 处（批次 2 · FIX-11 / FIX-12）。
--
-- 背景：这 20 个 key 早已以 t(key, "中文兜底") 的形式在模板与 handler 里使用，但**没有任何迁移 seed 过**
-- （同一个语言面板只有 494 的两条 republish 词条被 seed）。命中失败的后果是回落源码里的中文兜底：
--   · 不报错、不 500、日志干净；默认语言（中文）下**完全看不出来**，只有切到 en-US 才暴露，而那时看到的是中文。
-- 模板 / handler 里的中文兜底**保留**（它是兜底，不是冗余）；本批只补词条台账。
--
-- 门禁：scripts/check-i18n-keys-seeded.sh（批次 2 · FIX-10 同批接入 CI）扫的正是这个方向 ——
-- 「admin. / workbench. 命名空间里被引用、但 public/ 里没有任何 seed 的 key」。补完本批，基线 21 → 1
-- （剩下那 1 条是 customers 批量操作结果词条，属 FIX-24 的机制范围，本批刻意不补 ——
--  此处**不写它的完整 key 字面量**：门禁按「文本在 public/ 出现即视为已 seed」判定，写在注释里会让它假绿）。
-- 同理，本文件与本批注册文件里出现的 key 一律是**真正要补的那 20 条**，不要顺手提及未补的 key。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；本批只新增、不改任何既有词条。
-- 判定写法：ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，key 只能写进 SQL 字面量（见 register_page_langs_i18n_keys.go）。
--
-- 值口径：zh-CN 一律**逐字取模板 / handler 里的中文兜底原文**（保证「未命中时看到的值」与「命中后中文的值」
-- 完全一致，不会出现补了词条反而中文变样）；en-US 按既有后台词条的译法对齐
-- （语言 / 状态 / 操作 / 说明 → Language / Status / Actions / Note，站点工程 → Site project，查询 → Query，
-- 恢复 → Restore，已发布 → Published，未发布 → Not published）。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.page.langs.title', 'zh-CN', '语言产出范围', 200, 'admin', 'V4 语言面板：面板标题（该页语言产出范围）', 1, now(), now()),
('admin.page.langs.title', 'en-US', 'Language output scope', 200, 'admin', 'V4 语言面板：面板标题（该页语言产出范围） (en)', 1, now(), now()),
('admin.page.langs.col.lang', 'zh-CN', '语言', 200, 'admin', 'V4 语言面板：表头「语言」', 1, now(), now()),
('admin.page.langs.col.lang', 'en-US', 'Language', 200, 'admin', 'V4 语言面板：表头「语言」 (en)', 1, now(), now()),
('admin.page.langs.col.status', 'zh-CN', '状态', 200, 'admin', 'V4 语言面板：表头「状态」', 1, now(), now()),
('admin.page.langs.col.status', 'en-US', 'Status', 200, 'admin', 'V4 语言面板：表头「状态」 (en)', 1, now(), now()),
('admin.page.langs.col.note', 'zh-CN', '说明', 200, 'admin', 'V4 语言面板：表头「说明」', 1, now(), now()),
('admin.page.langs.col.note', 'en-US', 'Note', 200, 'admin', 'V4 语言面板：表头「说明」 (en)', 1, now(), now()),
('admin.page.langs.col.actions', 'zh-CN', '操作', 200, 'admin', 'V4 语言面板：表头「操作」', 1, now(), now()),
('admin.page.langs.col.actions', 'en-US', 'Actions', 200, 'admin', 'V4 语言面板：表头「操作」 (en)', 1, now(), now()),
('admin.page.langs.empty', 'zh-CN', '这个页面还没有可展示的语言（站点语言清单为空）。', 200, 'admin', 'V4 语言面板：空态整行', 1, now(), now()),
('admin.page.langs.empty', 'en-US', 'This page has no languages to show yet (the site language list is empty).', 200, 'admin', 'V4 语言面板：空态整行 (en)', 1, now(), now()),
('admin.page.langs.action.exclude', 'zh-CN', '排除', 200, 'admin', 'V4 语言面板：行内动作「排除」', 1, now(), now()),
('admin.page.langs.action.exclude', 'en-US', 'Exclude', 200, 'admin', 'V4 语言面板：行内动作「排除」 (en)', 1, now(), now()),
('admin.page.langs.action.restore', 'zh-CN', '恢复', 200, 'admin', 'V4 语言面板：行内动作「恢复」', 1, now(), now()),
('admin.page.langs.action.restore', 'en-US', 'Restore', 200, 'admin', 'V4 语言面板：行内动作「恢复」 (en)', 1, now(), now()),
('admin.page.langs.action.keep', 'zh-CN', '始终产出', 200, 'admin', 'V4 语言面板：默认语言不可排除的提示', 1, now(), now()),
('admin.page.langs.action.keep', 'en-US', 'Always published', 200, 'admin', 'V4 语言面板：默认语言不可排除的提示 (en)', 1, now(), now()),
('admin.page.langs.note', 'zh-CN', '排除某语言会同时下线该语言已发布的产物（访问面入口随之消失），它也不再出现在语言切换器、hreflang 与 sitemap 里；恢复只解除排除，重新上线请走常规发布。', 200, 'admin', 'V4 语言面板：面板底部说明', 1, now(), now()),
('admin.page.langs.note', 'en-US', 'Excluding a language also takes its published artifacts offline (the public entry disappears with it), and it drops out of the language switcher, hreflang and sitemap; restoring only lifts the exclusion, so use the normal publish entry to put it back online.', 200, 'admin', 'V4 语言面板：面板底部说明 (en)', 1, now(), now()),
('admin.page.langs.excluded', 'zh-CN', '已排除该语言并下线其产物', 200, 'admin', 'V4 语言面板：排除成功回执', 1, now(), now()),
('admin.page.langs.excluded', 'en-US', 'Language excluded and its artifacts taken offline', 200, 'admin', 'V4 语言面板：排除成功回执 (en)', 1, now(), now()),
('admin.page.langs.restored', 'zh-CN', '已解除排除：该语言重新参与本页的产出（重新发布走常规发布入口）', 200, 'admin', 'V4 语言面板：恢复成功回执', 1, now(), now()),
('admin.page.langs.restored', 'en-US', 'Exclusion lifted: the language participates in the output of this page again (use the normal publish entry to put it back online)', 200, 'admin', 'V4 语言面板：恢复成功回执 (en)', 1, now(), now()),
('admin.page.langs.status.excluded', 'zh-CN', '已排除（不产出）', 200, 'admin', 'V4 语言面板：行状态「已排除」', 1, now(), now()),
('admin.page.langs.status.excluded', 'en-US', 'Excluded (no output)', 200, 'admin', 'V4 语言面板：行状态「已排除」 (en)', 1, now(), now()),
('admin.page.langs.status.published', 'zh-CN', '已发布', 200, 'admin', 'V4 语言面板：行状态「已发布」', 1, now(), now()),
('admin.page.langs.status.published', 'en-US', 'Published', 200, 'admin', 'V4 语言面板：行状态「已发布」 (en)', 1, now(), now()),
('admin.page.langs.status.draft', 'zh-CN', '未发布', 200, 'admin', 'V4 语言面板：行状态「未发布」', 1, now(), now()),
('admin.page.langs.status.draft', 'en-US', 'Not published', 200, 'admin', 'V4 语言面板：行状态「未发布」 (en)', 1, now(), now()),
('admin.page.langs.note.default', 'zh-CN', '站点默认语言，始终产出', 200, 'admin', 'V4 语言面板：默认语言行的说明', 1, now(), now()),
('admin.page.langs.note.default', 'en-US', 'Site default language, always published', 200, 'admin', 'V4 语言面板：默认语言行的说明 (en)', 1, now(), now()),
('admin.page.translation_misses.filter_project', 'zh-CN', '站点工程', 200, 'admin', 'W4 缺译报告：工程筛选标签', 1, now(), now()),
('admin.page.translation_misses.filter_project', 'en-US', 'Site project', 200, 'admin', 'W4 缺译报告：工程筛选标签 (en)', 1, now(), now()),
('admin.page.translation_misses.filter_submit', 'zh-CN', '查询', 200, 'admin', 'W4 缺译报告：筛选提交按钮', 1, now(), now()),
('admin.page.translation_misses.filter_submit', 'en-US', 'Query', 200, 'admin', 'W4 缺译报告：筛选提交按钮 (en)', 1, now(), now()),
('admin.pages.action.langs', 'zh-CN', '语言', 200, 'admin', '页面列表行内动作「语言」（打开语言产出面板）', 1, now(), now()),
('admin.pages.action.langs', 'en-US', 'Languages', 200, 'admin', '页面列表行内动作「语言」（打开语言产出面板） (en)', 1, now(), now()),
('admin.products.seo.drawerHint', 'zh-CN', '评测按商品页的权重档案，覆盖标题、描述、关键词、内容与图片等维度。分数是提示性的：它不会拦住保存，也不会写库或改动产物。', 200, 'admin', '商品 SEO 抽屉：评测说明', 1, now(), now()),
('admin.products.seo.drawerHint', 'en-US', 'Scoring follows the product page weight profile and covers title, description, keywords, content and images. The score is advisory: it never blocks a save, never writes to the database and never changes artifacts.', 200, 'admin', '商品 SEO 抽屉：评测说明 (en)', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
