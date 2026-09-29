-- 454 — 工作台 HTMX 片段与画布桥接脚本的文案词条（i18n 收口第二批）。
--
-- 范围（本批唯一新增词条的地方）：
--   · workbench.ui.settings.*   页面设置 / 全局设置片段（settings_panel.html、global_panel.html）
--   · workbench.ui.seo.*        SEO 评分片段（seo_score.html，被页面 / 文章 / 商品三处复用）
--   · workbench.ui.history.*    修订历史片段（history_list.html）
--   · workbench.bridge.*        画布桥接脚本注入 iframe 的可见文案（editor_bridge.go，Go 侧取词）
--   · admin.article.import.*    文章导入预览片段（article_import.html）的正文句子
--
-- 为什么这批是「片段」而不是后台页面：fragments/*.html 的渲染 data 是 gin.H（map），
-- 不经 shell.Prepare，所以 t 必须由**每个渲染入口**单独注入 —— 只改模板不注入 t 的后果
-- 不是报错，而是 Jet 把缺失的 t 调用求值成空串（页面结构完整、状态码 200、日志干净），
-- 因此本批同时改了全部调用点（见各 handler 的 `"t": templates.TranslateFunc(...)`）。
--
-- 为什么每条都成对给中英：sys_i18n 的主键是 (item_key, lang)，英文界面取 en-US 一行；
-- zh-CN 的值与代码 / 模板里的中文兜底逐字一致 ——「i18n 未初始化」与「词条已 seed」
-- 两种环境下响应体相同（否则测试里看到兜底文案、生产里看到词条）。
-- 占位符用 {name}（本批只有 admin.article.import.resultCount 的 {count}）。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
-- —— workbench.ui.settings.*：页面设置面板（fragments/settings_panel.html）——
('workbench.ui.settings.layoutMode', 'zh-CN', '版心模式', 200, 'ui', 'settings panel: layout mode label', 1, now(), now()),
('workbench.ui.settings.layoutMode', 'en-US', 'Content width', 200, 'ui', 'settings panel: layout mode label', 1, now(), now()),
('workbench.ui.settings.layoutFull', 'zh-CN', '全宽', 200, 'ui', 'settings panel: layout mode option', 1, now(), now()),
('workbench.ui.settings.layoutFull', 'en-US', 'Full width', 200, 'ui', 'settings panel: layout mode option', 1, now(), now()),
('workbench.ui.settings.layoutBoxed', 'zh-CN', '定宽', 200, 'ui', 'settings panel: layout mode option', 1, now(), now()),
('workbench.ui.settings.layoutBoxed', 'en-US', 'Boxed', 200, 'ui', 'settings panel: layout mode option', 1, now(), now()),
('workbench.ui.settings.mainLandmark', 'zh-CN', '页面地标', 200, 'ui', 'settings panel: main landmark label', 1, now(), now()),
('workbench.ui.settings.mainLandmark', 'en-US', 'Page landmark', 200, 'ui', 'settings panel: main landmark label', 1, now(), now()),
('workbench.ui.settings.landmarkNone', 'zh-CN', '不用', 200, 'ui', 'settings panel: no main landmark option', 1, now(), now()),
('workbench.ui.settings.landmarkNone', 'en-US', 'None', 200, 'ui', 'settings panel: no main landmark option', 1, now(), now()),
('workbench.ui.settings.seoTitle', 'zh-CN', 'SEO 标题', 200, 'ui', 'settings panel: SEO title label', 1, now(), now()),
('workbench.ui.settings.seoTitle', 'en-US', 'SEO title', 200, 'ui', 'settings panel: SEO title label', 1, now(), now()),
('workbench.ui.settings.seoTitlePlaceholder', 'zh-CN', '浏览器标签页与搜索结果标题', 200, 'ui', 'settings panel: SEO title placeholder', 1, now(), now()),
('workbench.ui.settings.seoTitlePlaceholder', 'en-US', 'Title for the browser tab and search results', 200, 'ui', 'settings panel: SEO title placeholder', 1, now(), now()),
('workbench.ui.settings.seoDescription', 'zh-CN', 'SEO 描述', 200, 'ui', 'settings panel: SEO description label', 1, now(), now()),
('workbench.ui.settings.seoDescription', 'en-US', 'SEO description', 200, 'ui', 'settings panel: SEO description label', 1, now(), now()),
('workbench.ui.settings.seoDescriptionPlaceholder', 'zh-CN', '搜索结果里的摘要文案', 200, 'ui', 'settings panel: SEO description placeholder', 1, now(), now()),
('workbench.ui.settings.seoDescriptionPlaceholder', 'en-US', 'Snippet shown in search results', 200, 'ui', 'settings panel: SEO description placeholder', 1, now(), now()),
('workbench.ui.settings.focusKeyword', 'zh-CN', '焦点关键词', 200, 'ui', 'settings panel: focus keyword label', 1, now(), now()),
('workbench.ui.settings.focusKeyword', 'en-US', 'Focus keyword', 200, 'ui', 'settings panel: focus keyword label', 1, now(), now()),
('workbench.ui.settings.focusKeywordPlaceholder', 'zh-CN', '如 disposable vape', 200, 'ui', 'settings panel: focus keyword placeholder', 1, now(), now()),
('workbench.ui.settings.focusKeywordPlaceholder', 'en-US', 'e.g. disposable vape', 200, 'ui', 'settings panel: focus keyword placeholder', 1, now(), now()),
('workbench.ui.settings.canonical', 'zh-CN', 'Canonical 链接', 200, 'ui', 'settings panel: canonical label', 1, now(), now()),
('workbench.ui.settings.canonical', 'en-US', 'Canonical URL', 200, 'ui', 'settings panel: canonical label', 1, now(), now()),
('workbench.ui.settings.canonicalPlaceholder', 'zh-CN', '规范链接，留空则由页面 URL 推导', 200, 'ui', 'settings panel: canonical placeholder', 1, now(), now()),
('workbench.ui.settings.canonicalPlaceholder', 'en-US', 'Canonical link; leave empty to derive it from the page URL', 200, 'ui', 'settings panel: canonical placeholder', 1, now(), now()),
('workbench.ui.settings.ogImage', 'zh-CN', '社交分享图', 200, 'ui', 'settings panel: OG image label', 1, now(), now()),
('workbench.ui.settings.ogImage', 'en-US', 'Social share image', 200, 'ui', 'settings panel: OG image label', 1, now(), now()),
('workbench.ui.settings.ogImagePlaceholder', 'zh-CN', 'OG/Twitter 卡片图，建议 1200×630', 200, 'ui', 'settings panel: OG image placeholder', 1, now(), now()),
('workbench.ui.settings.ogImagePlaceholder', 'en-US', 'OG/Twitter card image, 1200×630 recommended', 200, 'ui', 'settings panel: OG image placeholder', 1, now(), now()),
('workbench.ui.settings.schemaType', 'zh-CN', '结构化数据', 200, 'ui', 'settings panel: schema type label', 1, now(), now()),
('workbench.ui.settings.schemaType', 'en-US', 'Structured data', 200, 'ui', 'settings panel: schema type label', 1, now(), now()),
('workbench.ui.settings.schemaAuto', 'zh-CN', '自动', 200, 'ui', 'settings panel: schema type option', 1, now(), now()),
('workbench.ui.settings.schemaAuto', 'en-US', 'Auto', 200, 'ui', 'settings panel: schema type option', 1, now(), now()),
('workbench.ui.settings.schemaWebsite', 'zh-CN', '网站', 200, 'ui', 'settings panel: schema type option', 1, now(), now()),
('workbench.ui.settings.schemaWebsite', 'en-US', 'Website', 200, 'ui', 'settings panel: schema type option', 1, now(), now()),
('workbench.ui.settings.schemaArticle', 'zh-CN', '文章', 200, 'ui', 'settings panel: schema type option', 1, now(), now()),
('workbench.ui.settings.schemaArticle', 'en-US', 'Article', 200, 'ui', 'settings panel: schema type option', 1, now(), now()),
('workbench.ui.settings.schemaProduct', 'zh-CN', '商品', 200, 'ui', 'settings panel: schema type option', 1, now(), now()),
('workbench.ui.settings.schemaProduct', 'en-US', 'Product', 200, 'ui', 'settings panel: schema type option', 1, now(), now()),
('workbench.ui.settings.robotsIndex', 'zh-CN', '搜索引擎收录', 200, 'ui', 'settings panel: robots index label', 1, now(), now()),
('workbench.ui.settings.robotsIndex', 'en-US', 'Search engine indexing', 200, 'ui', 'settings panel: robots index label', 1, now(), now()),
('workbench.ui.settings.indexOn', 'zh-CN', '索引', 200, 'ui', 'settings panel: robots index option', 1, now(), now()),
('workbench.ui.settings.indexOn', 'en-US', 'Index', 200, 'ui', 'settings panel: robots index option', 1, now(), now()),
('workbench.ui.settings.indexOff', 'zh-CN', '不索引', 200, 'ui', 'settings panel: robots index option', 1, now(), now()),
('workbench.ui.settings.indexOff', 'en-US', 'No index', 200, 'ui', 'settings panel: robots index option', 1, now(), now()),
('workbench.ui.settings.robotsFollow', 'zh-CN', '链接跟踪', 200, 'ui', 'settings panel: robots follow label', 1, now(), now()),
('workbench.ui.settings.robotsFollow', 'en-US', 'Link following', 200, 'ui', 'settings panel: robots follow label', 1, now(), now()),
('workbench.ui.settings.followOn', 'zh-CN', '跟随', 200, 'ui', 'settings panel: robots follow option', 1, now(), now()),
('workbench.ui.settings.followOn', 'en-US', 'Follow', 200, 'ui', 'settings panel: robots follow option', 1, now(), now()),
('workbench.ui.settings.followOff', 'zh-CN', '不跟随', 200, 'ui', 'settings panel: robots follow option', 1, now(), now()),
('workbench.ui.settings.followOff', 'en-US', 'No follow', 200, 'ui', 'settings panel: robots follow option', 1, now(), now()),
('workbench.ui.settings.secondaryKeywords', 'zh-CN', '次级关键词', 200, 'ui', 'settings panel: secondary keywords label', 1, now(), now()),
('workbench.ui.settings.secondaryKeywords', 'en-US', 'Secondary keywords', 200, 'ui', 'settings panel: secondary keywords label', 1, now(), now()),
('workbench.ui.settings.secondaryKeywordsPlaceholder', 'zh-CN', '空格分隔，如 puff count battery', 200, 'ui', 'settings panel: secondary keywords placeholder', 1, now(), now()),
('workbench.ui.settings.secondaryKeywordsPlaceholder', 'en-US', 'Space separated, e.g. puff count battery', 200, 'ui', 'settings panel: secondary keywords placeholder', 1, now(), now()),
('workbench.ui.settings.intent', 'zh-CN', '查询意图', 200, 'ui', 'settings panel: search intent label', 1, now(), now()),
('workbench.ui.settings.intent', 'en-US', 'Search intent', 200, 'ui', 'settings panel: search intent label', 1, now(), now()),
('workbench.ui.settings.intentInformational', 'zh-CN', '信息型', 200, 'ui', 'settings panel: search intent option', 1, now(), now()),
('workbench.ui.settings.intentInformational', 'en-US', 'Informational', 200, 'ui', 'settings panel: search intent option', 1, now(), now()),
('workbench.ui.settings.intentCommercial', 'zh-CN', '商业型', 200, 'ui', 'settings panel: search intent option', 1, now(), now()),
('workbench.ui.settings.intentCommercial', 'en-US', 'Commercial', 200, 'ui', 'settings panel: search intent option', 1, now(), now()),
('workbench.ui.settings.intentTransactional', 'zh-CN', '交易型', 200, 'ui', 'settings panel: search intent option', 1, now(), now()),
('workbench.ui.settings.intentTransactional', 'en-US', 'Transactional', 200, 'ui', 'settings panel: search intent option', 1, now(), now()),
('workbench.ui.settings.intentLocal', 'zh-CN', '本地型', 200, 'ui', 'settings panel: search intent option', 1, now(), now()),
('workbench.ui.settings.intentLocal', 'en-US', 'Local', 200, 'ui', 'settings panel: search intent option', 1, now(), now()),
('workbench.ui.settings.themeOverride', 'zh-CN', '主题覆盖（留空 = 跟随站点主题）', 200, 'ui', 'settings panel: theme override group heading', 1, now(), now()),
('workbench.ui.settings.themeOverride', 'en-US', 'Theme overrides (empty = follow the site theme)', 200, 'ui', 'settings panel: theme override group heading', 1, now(), now()),
('workbench.ui.settings.themeOverridePlaceholder', 'zh-CN', '留空跟随站点主题', 200, 'ui', 'settings panel: theme override field placeholder', 1, now(), now()),
('workbench.ui.settings.themeOverridePlaceholder', 'en-US', 'Leave empty to follow the site theme', 200, 'ui', 'settings panel: theme override field placeholder', 1, now(), now()),
-- —— workbench.ui.settings.*：全局设置（主题）面板（fragments/global_panel.html）——
--   noTheme 这个 key 已在迁移 452 seeded（workbench 画布 layout.html 的空态复用同一句），
--   本批只新增它的后半句；片段里的两次取词用的是同一个 key，读的是同一行词条。
('workbench.ui.settings.noThemeHint', 'zh-CN', '到后台「主题管理」创建并激活主题后，这里可就地调整。', 200, 'ui', 'global settings panel: no-theme hint', 1, now(), now()),
('workbench.ui.settings.noThemeHint', 'en-US', 'Create and activate a theme under Themes in the admin, then you can adjust it right here.', 200, 'ui', 'global settings panel: no-theme hint', 1, now(), now()),
('workbench.ui.settings.saveHint', 'zh-CN', '保存后主题设置批量合入全部页面文档，已发布页面需重新构建才会带新主题。', 200, 'ui', 'global settings panel: save hint', 1, now(), now()),
('workbench.ui.settings.saveHint', 'en-US', 'Saving merges the theme settings into every page document; published pages need a rebuild before they pick up the new theme.', 200, 'ui', 'global settings panel: save hint', 1, now(), now()),
-- —— workbench.ui.seo.*：SEO 评分片段（fragments/seo_score.html）——
('workbench.ui.seo.totalTip', 'zh-CN', 'SEO 评分（0-100）', 200, 'ui', 'seo score fragment: total tip', 1, now(), now()),
('workbench.ui.seo.totalTip', 'en-US', 'SEO score (0-100)', 200, 'ui', 'seo score fragment: total tip', 1, now(), now()),
('workbench.ui.seo.profileWeight', 'zh-CN', '页型权重：', 200, 'ui', 'seo score fragment: profile weight prefix', 1, now(), now()),
('workbench.ui.seo.profileWeight', 'en-US', 'Page-type weights: ', 200, 'ui', 'seo score fragment: profile weight prefix', 1, now(), now()),
('workbench.ui.seo.dupPrefix', 'zh-CN', '标题重复：另有 ', 200, 'ui', 'seo score fragment: duplicate title prefix, followed by a count', 1, now(), now()),
('workbench.ui.seo.dupPrefix', 'en-US', 'Duplicate title: ', 200, 'ui', 'seo score fragment: duplicate title prefix, followed by a count', 1, now(), now()),
('workbench.ui.seo.dupSuffix', 'zh-CN', ' 个页面用了同一个标题', 200, 'ui', 'seo score fragment: duplicate title suffix, preceded by a count', 1, now(), now()),
('workbench.ui.seo.dupSuffix', 'en-US', ' other page(s) share the same title', 200, 'ui', 'seo score fragment: duplicate title suffix, preceded by a count', 1, now(), now()),
('workbench.ui.seo.jumpHint', 'zh-CN', '点击定位到需要修改的位置', 200, 'ui', 'seo score fragment: issue jump tooltip', 1, now(), now()),
('workbench.ui.seo.jumpHint', 'en-US', 'Click to jump to what needs fixing', 200, 'ui', 'seo score fragment: issue jump tooltip', 1, now(), now()),
('workbench.ui.seo.unavailable', 'zh-CN', '评分不可用（草稿为空或评分失败）', 200, 'ui', 'seo score fragment: empty state', 1, now(), now()),
('workbench.ui.seo.unavailable', 'en-US', 'Score unavailable (empty draft or scoring failed)', 200, 'ui', 'seo score fragment: empty state', 1, now(), now()),
-- —— workbench.ui.history.*：修订历史片段（fragments/history_list.html）——
('workbench.ui.history.empty', 'zh-CN', '还没有修订历史。每次保存草稿都会生成一条快照。', 200, 'ui', 'history panel: empty state', 1, now(), now()),
('workbench.ui.history.empty', 'en-US', 'No revision history yet. Every draft save creates a snapshot.', 200, 'ui', 'history panel: empty state', 1, now(), now()),
('workbench.ui.history.restore', 'zh-CN', '恢复', 200, 'ui', 'history panel: restore button', 1, now(), now()),
('workbench.ui.history.restore', 'en-US', 'Restore', 200, 'ui', 'history panel: restore button', 1, now(), now()),
-- —— admin.article.import.*：文章导入预览片段（fragments/article_import.html）——
--   resultCount 带 {count} 命名占位符，由 Go 侧 i18n.FillTranslate 填充（模板拿不到填充函数）。
('admin.article.import.resultCount', 'zh-CN', '转换结果：{count} 个组件', 200, 'ui', 'article import preview: component count line, {count} placeholder', 1, now(), now()),
('admin.article.import.resultCount', 'en-US', 'Conversion result: {count} components', 200, 'ui', 'article import preview: component count line, {count} placeholder', 1, now(), now()),
('admin.article.import.lossless', 'zh-CN', '无损转换：这些内容都能在画布里继续编辑', 200, 'ui', 'article import preview: lossless badge', 1, now(), now()),
('admin.article.import.lossless', 'en-US', 'Lossless conversion: all of this stays editable on the canvas', 200, 'ui', 'article import preview: lossless badge', 1, now(), now()),
('admin.article.import.lossy', 'zh-CN', '有损转换：下面这些内容在导入时发生了降级', 200, 'ui', 'article import preview: lossy badge', 1, now(), now()),
('admin.article.import.lossy', 'en-US', 'Lossy conversion: the items below were downgraded during import', 200, 'ui', 'article import preview: lossy badge', 1, now(), now()),
('admin.article.import.empty', 'zh-CN', '还没有预览。点「预览转换结果」看看正文会变成多少个组件、有没有损失。', 200, 'ui', 'article import preview: empty state', 1, now(), now()),
('admin.article.import.empty', 'en-US', 'No preview yet. Use "Preview conversion" to see how many components the body becomes and whether anything is lost.', 200, 'ui', 'article import preview: empty state', 1, now(), now()),
-- —— workbench.bridge.*：画布桥接脚本注入 iframe 的可见文案（editor_bridge.go）——
--   这些串拼进 <script> 后由 iframe 内 JS 输出（按钮文本 / 右键菜单 / 快捷条 title），
--   与模板取词同性质：都是给人看的文案，所以走同一批词条 + shell.TranslateFor(c)。
('workbench.bridge.insert', 'zh-CN', '插入组件', 200, 'ui', 'canvas bridge: insert-here floating button', 1, now(), now()),
('workbench.bridge.insert', 'en-US', 'Insert component', 200, 'ui', 'canvas bridge: insert-here floating button', 1, now(), now()),
('workbench.bridge.editText', 'zh-CN', '编辑文本', 200, 'ui', 'canvas bridge: context menu / quick bar edit item', 1, now(), now()),
('workbench.bridge.editText', 'en-US', 'Edit text', 200, 'ui', 'canvas bridge: context menu / quick bar edit item', 1, now(), now()),
('workbench.bridge.copy', 'zh-CN', '复制', 200, 'ui', 'canvas bridge: context menu / quick bar copy item', 1, now(), now()),
('workbench.bridge.copy', 'en-US', 'Duplicate', 200, 'ui', 'canvas bridge: context menu / quick bar copy item', 1, now(), now()),
('workbench.bridge.cut', 'zh-CN', '剪切', 200, 'ui', 'canvas bridge: context menu cut item', 1, now(), now()),
('workbench.bridge.cut', 'en-US', 'Cut', 200, 'ui', 'canvas bridge: context menu cut item', 1, now(), now()),
('workbench.bridge.pasteInside', 'zh-CN', '粘贴到内部', 200, 'ui', 'canvas bridge: context menu paste-inside item', 1, now(), now()),
('workbench.bridge.pasteInside', 'en-US', 'Paste inside', 200, 'ui', 'canvas bridge: context menu paste-inside item', 1, now(), now()),
('workbench.bridge.moveUp', 'zh-CN', '上移', 200, 'ui', 'canvas bridge: context menu move-up item', 1, now(), now()),
('workbench.bridge.moveUp', 'en-US', 'Move up', 200, 'ui', 'canvas bridge: context menu move-up item', 1, now(), now()),
('workbench.bridge.moveDown', 'zh-CN', '下移', 200, 'ui', 'canvas bridge: context menu move-down item', 1, now(), now()),
('workbench.bridge.moveDown', 'en-US', 'Move down', 200, 'ui', 'canvas bridge: context menu move-down item', 1, now(), now()),
('workbench.bridge.delete', 'zh-CN', '删除', 200, 'ui', 'canvas bridge: context menu / quick bar delete item', 1, now(), now()),
('workbench.bridge.delete', 'en-US', 'Delete', 200, 'ui', 'canvas bridge: context menu / quick bar delete item', 1, now(), now()),
('workbench.bridge.entranceGroup', 'zh-CN', '入场动画', 200, 'ui', 'canvas bridge: context menu entrance animation group', 1, now(), now()),
('workbench.bridge.entranceGroup', 'en-US', 'Entrance animation', 200, 'ui', 'canvas bridge: context menu entrance animation group', 1, now(), now()),
('workbench.bridge.hoverLift', 'zh-CN', '悬浮上浮', 200, 'ui', 'canvas bridge: context menu hover item', 1, now(), now()),
('workbench.bridge.hoverLift', 'en-US', 'Hover lift', 200, 'ui', 'canvas bridge: context menu hover item', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
