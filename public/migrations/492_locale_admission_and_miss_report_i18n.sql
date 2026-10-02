-- 492 · 语言准入（U1）与缺译报告（U2）的词条（中英各 13 条）。
--
-- 为什么必须有一条迁移：页面上的文案是 `t()` 取词、模板里的中文只是**兜底**；
-- 词条命中时显示的是库里的值。只改模板不 seed，英文界面上会显示中文兜底。
-- 另外 `TestProjectPageErrKeysAreRegisteredInMigrations` 会检查写侧写进 ?err= 的 key
-- 必须在迁移 SQL 里出现过 —— 漏了就是「页面上直接显示 ErrLocaleNoTranslations 裸 key」。
--
-- 两类词条：
--   · ErrLocaleNoTranslations —— U1 的准入失败文案（新增/新启用的语言没有任何启用词条）；
--     **文案里必须给出下一步**（去文案管理补词条），否则运营只会反复点保存。
--   · admin.page.translation_misses.* —— U2 报告页的标题 / 表头 / 空态 / 操作 / 结果文案。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；本批只新增、不改任何既有词条。
-- 判定写法：ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，key 只能写进 SQL 字面量。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrLocaleNoTranslations', 'zh-CN', '该语言还没有任何界面词条，不能启用：请先在「文案管理」里补齐该语言的词条，再回来启用。', 400, 'project', 'U1 站点级语言准入：新增/新启用的语言在 sys_i18n 的启用词条数必须 > 0', 1, now(), now()),
('ErrLocaleNoTranslations', 'en-US', 'This language has no interface strings yet and cannot be enabled. Add its strings in "Translations" first, then enable it.', 400, 'project', 'U1 site-level locale admission: a newly added/enabled language needs at least one active sys_i18n entry', 1, now(), now()),
('admin.page.translation_misses.title', 'zh-CN', '内容缺译报告', 200, 'admin', 'U2 缺译报告页标题', 1, now(), now()),
('admin.page.translation_misses.title', 'en-US', 'Missing content translations', 200, 'admin', 'U2 missing-translation report title', 1, now(), now()),
('admin.page.translation_misses.back', 'zh-CN', '返回页面列表', 200, 'admin', 'U2 返回入口', 1, now(), now()),
('admin.page.translation_misses.back', 'en-US', 'Back to pages', 200, 'admin', 'U2 back link', 1, now(), now()),
('admin.page.translation_misses.hint', 'zh-CN', '这里列出「语言可用、但页面内容还没译」的组合：界面词条齐了（站点级准入已放行），缺的是页面内容译文。缺失数是构建期取词未命中的次数，与候选字段数量纲不同，因此不给百分比。取消该语言会把这一页从这个语言撤下来（产物下线，切换器 / hreflang / sitemap 同步移除）；补齐内容译文后重新发布即可回来。', 200, 'admin', 'U2 报告页说明（含与 U1 的分工口径）', 1, now(), now()),
('admin.page.translation_misses.hint', 'en-US', 'These page/language pairs have untranslated content. The interface strings for the language exist (site-level admission already passed); what is missing is page content. The count is the number of render-time lookup misses, which is a different unit from the candidate field count, so no percentage is shown. Cancelling a language takes this page out of that language (artifacts go offline and it leaves the switcher / hreflang / sitemap); republishing after translating brings it back.', 200, 'admin', 'U2 report hint (states the U1 vs U2 split)', 1, now(), now()),
('admin.page.translation_misses.col.page', 'zh-CN', '页面', 200, 'admin', 'U2 表头：页面', 1, now(), now()),
('admin.page.translation_misses.col.page', 'en-US', 'Page', 200, 'admin', 'U2 column: page', 1, now(), now()),
('admin.page.translation_misses.col.lang', 'zh-CN', '语言', 200, 'admin', 'U2 表头：语言', 1, now(), now()),
('admin.page.translation_misses.col.lang', 'en-US', 'Language', 200, 'admin', 'U2 column: language', 1, now(), now()),
('admin.page.translation_misses.col.misses', 'zh-CN', '缺失情况', 200, 'admin', 'U2 表头：缺失情况', 1, now(), now()),
('admin.page.translation_misses.col.misses', 'en-US', 'Missing', 200, 'admin', 'U2 column: missing', 1, now(), now()),
('admin.page.translation_misses.col.actions', 'zh-CN', '操作', 200, 'admin', 'U2 表头：操作', 1, now(), now()),
('admin.page.translation_misses.col.actions', 'en-US', 'Actions', 200, 'admin', 'U2 column: actions', 1, now(), now()),
('admin.page.translation_misses.empty', 'zh-CN', '没有缺译的页面：所有已产出的语言都取到了译文。', 200, 'admin', 'U2 空态', 1, now(), now()),
('admin.page.translation_misses.empty', 'en-US', 'No missing translations: every produced language resolved all its strings.', 200, 'admin', 'U2 empty state', 1, now(), now()),
('admin.page.translation_misses.unit.misses', 'zh-CN', '处取词未命中', 200, 'admin', 'U2 缺失量单位（取词未命中次数，非百分比）', 1, now(), now()),
('admin.page.translation_misses.unit.misses', 'en-US', 'lookup misses', 200, 'admin', 'U2 miss unit (lookup misses, not a percentage)', 1, now(), now()),
('admin.page.translation_misses.candidates', 'zh-CN', '候选字段', 200, 'admin', 'U2 参考量标签（候选字段数，不是分母）', 1, now(), now()),
('admin.page.translation_misses.candidates', 'en-US', 'candidate fields', 200, 'admin', 'U2 reference label (candidate fields, not a denominator)', 1, now(), now()),
('admin.page.translation_misses.action.cancel', 'zh-CN', '取消该语言', 200, 'admin', 'U2 操作：把这一页从该语言撤下', 1, now(), now()),
('admin.page.translation_misses.action.cancel', 'en-US', 'Cancel this language', 200, 'admin', 'U2 action: take this page out of that language', 1, now(), now()),
('admin.page.translation_misses.canceled', 'zh-CN', '已取消该语言：产物已下线，它也不再出现在切换器 / hreflang / sitemap 里', 200, 'admin', 'U2 取消成功回执', 1, now(), now()),
('admin.page.translation_misses.canceled', 'en-US', 'Language cancelled: its artifacts went offline and it left the switcher / hreflang / sitemap', 200, 'admin', 'U2 cancel success receipt', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
