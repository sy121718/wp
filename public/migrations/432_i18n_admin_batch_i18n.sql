-- 432 · 后台散词条补漏（69 key × 2 语言 + Go 侧 2 key × 2 语言 = 71 key）
--
-- 背景：本批是全量扫描的产物，不由单一需求驱动。做法是：
--   ① 扫模板里的取词调用、② 扫 Go 侧的取词调用、③ 与 sys_i18n 的 zh-CN 行做差集。
--   **取词调用有三种形态，只扫第一种会漏掉近一半**（本轮实测踩过）：
--     · {{ .["t"]("key", "兜底") }}            —— 直调，最显眼；
--     · {{tr := .["t"]}} … {{tr("key", "兜底")}} —— range 内复用（模板的事实主流，
--       因为 .["t"](...) 不能出现在赋值右侧与 yield 参数里，见 internal/templates/CLAUDE.md）；
--     · Go 侧 shell.TranslateFor(c) 拿到的 tr("key", "中文") —— 与模板无关键词面上没关系。
--   另有两种局部变量名（gfTr 等），所以判据取「模板里所有形如小写点分标识符的字符串字面量」
--   作为上界，再排除文件名（*.html）、域名示例（*.example.com）、工作台设置键（settings.*）
--   与表单字段名（slots.*），最后与库比对 —— 两种口径（限定取词形式 / 点分字面量上界）
--   算出的缺失集**逐条一致**，互为交叉验证。
--
-- 本文件收录：实体页面级的新功能词条（文章列表与新建页的待重建影响面、可视化编辑入口）、
--   块列表的影响面、页面列表的全站待重建卡、主题与站点槽位的空态、多语言路径方案选项、
--   以及 Go 侧库存调整方向的两个下拉项。**批量操作条的按钮与确认文案另见 434**，
--   代客建单整页另见 433。
--
-- 为什么之前缺：这些 key 分属不同批次的页面改动，改动时模板加了取词调用、却没有同批 seed ——
--   于是英文界面回落模板里的中文兜底（卡片标题、空态说明、字段标签全露中文）。
--
-- 分段词条与首尾空格（改这批词条前必读）：
--   一批词条是**拼接片段**，与 {{…}} 数字或别的段拼在一起，段边界的空格必须落在值里：
--     · staleLead / staleMoreLead / impact.moreLead / impact.more_lead / coupons.status.unknown_lead
--       中文以空格**结尾**，英文同；
--     · staleTail / staleMoreTail / impact.moreTail / impact.more_tail / pages.impact.count_tail
--       中文以空格**开头**，英文同；
--     · article.edit.visualIntro 后面接 <strong> 段，英文以空格结尾。
--   模板里就是这么写的（例：`{{t("…staleLead", "当前有 ")}}{{.Total}}{{t("…staleTail", " 个页面…")}}`），
--   照抄才能拼成「当前有 3 个页面处于…」而不是「当前有3 个页面…」。
--
-- 与 400 的重叠：admin.depts.filter_empty / admin.menus.filter_empty 在 400
--   （register_client_filter_empty_i18n.go）里已有完整 seed，但**当前未落库**（库中 0 行）。
--   两条的值与 400 逐字一致；此处收录是让「模板在用、库里没有」一次收口，幂等不冲突。
--   400 的判定按 zh-CN 计数（>=2）—— 本批写入后该判定自然成立，400 转为跳过，行为等价。
--
-- 幂等：INSERT ... ON CONFLICT (item_key, lang) DO NOTHING（sys_i18n 主键是 (item_key, lang)），
--   重复执行影响 0 行；本批只新增、不修改任何既有词条，故无 UPDATE。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.article.edit.visualCreate', 'zh-CN', '创建页面并进入可视化编辑', 200, 'admin', 'admin/content/article_edit.html: 可视化编辑卡主按钮', 1, now(), now()),
    ('admin.article.edit.visualCreate', 'en-US', 'Create a page and open the visual editor', 200, 'admin', 'admin/content/article_edit.html: 可视化编辑卡主按钮', 1, now(), now()),
    ('admin.article.edit.visualGo', 'zh-CN', '去可视化编辑', 200, 'admin', 'admin/content/article_new.html: 未保存时的「去可视化编辑」按钮', 1, now(), now()),
    ('admin.article.edit.visualGo', 'en-US', 'Open the visual editor', 200, 'admin', 'admin/content/article_new.html: 未保存时的「去可视化编辑」按钮', 1, now(), now()),
    ('admin.article.edit.visualHeading', 'zh-CN', '可视化编辑', 200, 'admin', 'admin/content/article_edit.html: 可视化编辑卡标题', 1, now(), now()),
    ('admin.article.edit.visualHeading', 'en-US', 'Visual editing', 200, 'admin', 'admin/content/article_edit.html: 可视化编辑卡标题', 1, now(), now()),
    ('admin.article.edit.visualIntro', 'zh-CN', '把正文转换成', 200, 'admin', 'admin/content/article_new.html: 可视化编辑卡说明 ①（拼接段，英文以空格结尾）', 1, now(), now()),
    ('admin.article.edit.visualIntro', 'en-US', 'Convert the body into ', 200, 'admin', 'admin/content/article_new.html: 可视化编辑卡说明 ①（拼接段，英文以空格结尾）', 1, now(), now()),
    ('admin.article.list.staleHeading', 'zh-CN', '待重建影响面', 200, 'admin', 'admin/content/articles.html: 待重建影响面卡标题', 1, now(), now()),
    ('admin.article.list.staleHeading', 'en-US', 'Rebuild impact', 200, 'admin', 'admin/content/articles.html: 待重建影响面卡标题', 1, now(), now()),
    ('admin.article.list.staleLead', 'zh-CN', '当前有 ', 200, 'admin', 'admin/content/articles.html: 待重建影响面计数 ①（拼接段，中英均以空格结尾）', 1, now(), now()),
    ('admin.article.list.staleLead', 'en-US', 'There are currently ', 200, 'admin', 'admin/content/articles.html: 待重建影响面计数 ①（拼接段，中英均以空格结尾）', 1, now(), now()),
    ('admin.article.list.staleMoreLead', 'zh-CN', '（清单只列了前 ', 200, 'admin', 'admin/content/articles.html: 截断清单说明 ①（拼接段，中英均以空格结尾）', 1, now(), now()),
    ('admin.article.list.staleMoreLead', 'en-US', '(the list shows only the first ', 200, 'admin', 'admin/content/articles.html: 截断清单说明 ①（拼接段，中英均以空格结尾）', 1, now(), now()),
    ('admin.article.list.staleMoreTail', 'zh-CN', ' 条，其余只有计数；完整清单请按页面视图逐个查看。）', 200, 'admin', 'admin/content/articles.html: 截断清单说明 ②（拼接段，中英均以空格开头）', 1, now(), now()),
    ('admin.article.list.staleMoreTail', 'en-US', ' items; the rest are counted only. Walk the page list to see the full set.)', 200, 'admin', 'admin/content/articles.html: 截断清单说明 ②（拼接段，中英均以空格开头）', 1, now(), now()),
    ('admin.article.list.staleNone', 'zh-CN', '当前没有待重建的页面：所有页面的产物都与最新内容一致。', 200, 'admin', 'admin/content/articles.html: 无待重建页面时空态', 1, now(), now()),
    ('admin.article.list.staleNone', 'en-US', 'Nothing is pending a rebuild right now: every page artifact matches the latest content.', 200, 'admin', 'admin/content/articles.html: 无待重建页面时空态', 1, now(), now()),
    ('admin.article.list.staleTail', 'zh-CN', ' 个页面处于「有更新未发布」：内容、块、主题或导航改动过，产物的字节还停在旧版本。重新构建这些页面后新内容才会出现在访问面。', 200, 'admin', 'admin/content/articles.html: 待重建影响面计数 ②（拼接段，中英均以空格开头）', 1, now(), now()),
    ('admin.article.list.staleTail', 'en-US', ' pages are updated but not published: content, blocks, theme or navigation changed, so the artifact bytes still sit at the old version. Rebuild these pages and the new content appears on the live site.', 200, 'admin', 'admin/content/articles.html: 待重建影响面计数 ②（拼接段，中英均以空格开头）', 1, now(), now()),
    ('admin.article.new.formHeading', 'zh-CN', '正文', 200, 'admin', 'admin/content/article_new.html: 正文编辑区标题', 1, now(), now()),
    ('admin.article.new.formHeading', 'en-US', 'Body', 200, 'admin', 'admin/content/article_new.html: 正文编辑区标题', 1, now(), now()),
    ('admin.article.new.heading', 'zh-CN', '新建文章', 200, 'admin', 'admin/content/article_new.html: 页头标题', 1, now(), now()),
    ('admin.article.new.heading', 'en-US', 'New article', 200, 'admin', 'admin/content/article_new.html: 页头标题', 1, now(), now()),
    ('admin.article.new.intro', 'zh-CN', '左边写正文，右边跟着实时预览。保存后自动进入编辑页，在那里补摘要、封面与 SEO 字段，并把正文导入画布做可视化编辑。', 200, 'admin', 'admin/content/article_new.html: 页头说明', 1, now(), now()),
    ('admin.article.new.intro', 'en-US', 'Write the body on the left, watch the live preview on the right. Saving takes you straight to the edit page, where you add the excerpt, cover and SEO fields and import the body into the canvas for visual editing.', 200, 'admin', 'admin/content/article_new.html: 页头说明', 1, now(), now()),
    ('admin.article.new.previewHeading', 'zh-CN', '实时预览', 200, 'admin', 'admin/content/article_new.html: 实时预览卡标题', 1, now(), now()),
    ('admin.article.new.previewHeading', 'en-US', 'Live preview', 200, 'admin', 'admin/content/article_new.html: 实时预览卡标题', 1, now(), now()),
    ('admin.article.new.previewHint', 'zh-CN', '正文改动约 0.2 秒后同步到这里。预览在独立文档里渲染（白底正文排版），后台的主题样式不会影响它 —— 看到的接近访客最终看到的。', 200, 'admin', 'admin/content/article_new.html: 实时预览卡说明', 1, now(), now()),
    ('admin.article.new.previewHint', 'en-US', 'Body edits sync here after about 0.2 seconds. The preview renders in its own document (body typography on white), so the admin theme does not affect it — you see close to what visitors get.', 200, 'admin', 'admin/content/article_new.html: 实时预览卡说明', 1, now(), now()),
    ('admin.article.new.slugStable', 'zh-CN', '路径（slug）是文章的稳定身份', 200, 'admin', 'admin/content/article_new.html: slug 稳定性提示 ①（拼接段）', 1, now(), now()),
    ('admin.article.new.slugStable', 'en-US', 'The path (slug) is the stable identity of the article', 200, 'admin', 'admin/content/article_new.html: slug 稳定性提示 ①（拼接段）', 1, now(), now()),
    ('admin.article.new.slugStableTail', 'zh-CN', '：创建后不可修改。', 200, 'admin', 'admin/content/article_new.html: slug 稳定性提示 ②', 1, now(), now()),
    ('admin.article.new.slugStableTail', 'en-US', ': it cannot be changed after creation.', 200, 'admin', 'admin/content/article_new.html: slug 稳定性提示 ②', 1, now(), now()),
    ('admin.article.new.submit', 'zh-CN', '保存并继续编辑', 200, 'admin', 'admin/content/article_new.html: 提交按钮', 1, now(), now()),
    ('admin.article.new.submit', 'en-US', 'Save and keep editing', 200, 'admin', 'admin/content/article_new.html: 提交按钮', 1, now(), now()),
    ('admin.article.new.submitHint', 'zh-CN', '保存后自动进入编辑页：摘要、封面、SEO 字段与发布都在那边。', 200, 'admin', 'admin/content/article_new.html: 提交按钮旁说明', 1, now(), now()),
    ('admin.article.new.submitHint', 'en-US', 'Saving takes you to the edit page: excerpt, cover, SEO fields and publishing all live there.', 200, 'admin', 'admin/content/article_new.html: 提交按钮旁说明', 1, now(), now()),
    ('admin.article.new.titlePlaceholder', 'zh-CN', '文章标题', 200, 'admin', 'admin/content/article_new.html: 标题输入框占位符', 1, now(), now()),
    ('admin.article.new.titlePlaceholder', 'en-US', 'Article title', 200, 'admin', 'admin/content/article_new.html: 标题输入框占位符', 1, now(), now()),
    ('admin.article.new.visualLockedHint', 'zh-CN', '这篇文章还没保存 —— 先点「保存并继续编辑」，在编辑页里就能一键去可视化编辑。', 200, 'admin', 'admin/content/article_new.html: 未保存时的可视化编辑提示', 1, now(), now()),
    ('admin.article.new.visualLockedHint', 'en-US', 'This article is not saved yet — click Save and keep editing first; from the edit page you can jump into the visual editor in one click.', 200, 'admin', 'admin/content/article_new.html: 未保存时的可视化编辑提示', 1, now(), now()),
    ('admin.blocks.col.impact', 'zh-CN', '影响面', 200, 'admin', 'admin/block/blocks.html: 列表「影响面」列', 1, now(), now()),
    ('admin.blocks.col.impact', 'en-US', 'Impact', 200, 'admin', 'admin/block/blocks.html: 列表「影响面」列', 1, now(), now()),
    ('admin.blocks.impact.heading', 'zh-CN', '待重建影响面', 200, 'admin', 'admin/block/blocks.html: 待重建影响面卡标题', 1, now(), now()),
    ('admin.blocks.impact.heading', 'en-US', 'Rebuild impact', 200, 'admin', 'admin/block/blocks.html: 待重建影响面卡标题', 1, now(), now()),
    ('admin.blocks.impact.moreLead', 'zh-CN', '（清单只列了前 ', 200, 'admin', 'admin/block/blocks.html: 截断清单说明 ①（拼接段，中英均以空格结尾）', 1, now(), now()),
    ('admin.blocks.impact.moreLead', 'en-US', '(the list shows only the first ', 200, 'admin', 'admin/block/blocks.html: 截断清单说明 ①（拼接段，中英均以空格结尾）', 1, now(), now()),
    ('admin.blocks.impact.moreTail', 'zh-CN', ' 条，其余只有计数；完整清单请到页面列表逐个查看。）', 200, 'admin', 'admin/block/blocks.html: 截断清单说明 ②（拼接段，中英均以空格开头）', 1, now(), now()),
    ('admin.blocks.impact.moreTail', 'en-US', ' items; the rest are counted only. Walk the page list for the full set.)', 200, 'admin', 'admin/block/blocks.html: 截断清单说明 ②（拼接段，中英均以空格开头）', 1, now(), now()),
    ('admin.blocks.impact.none', 'zh-CN', '当前没有待重建的页面：所有页面的产物都与最新内容一致。', 200, 'admin', 'admin/block/blocks.html: 无待重建页面时空态', 1, now(), now()),
    ('admin.blocks.impact.none', 'en-US', 'Nothing is pending a rebuild right now: every page artifact matches the latest content.', 200, 'admin', 'admin/block/blocks.html: 无待重建页面时空态', 1, now(), now()),
    ('admin.blocks.lastError', 'zh-CN', '上一次操作未完成：', 200, 'admin', 'admin/block/blocks.html: 页顶错误前缀', 1, now(), now()),
    ('admin.blocks.lastError', 'en-US', 'The last operation did not finish:', 200, 'admin', 'admin/block/blocks.html: 页顶错误前缀', 1, now(), now()),
    ('admin.common.default', 'zh-CN', '默认', 200, 'admin', 'admin/product/products_new.html: 「默认」选项', 1, now(), now()),
    ('admin.common.default', 'en-US', 'Default', 200, 'admin', 'admin/product/products_new.html: 「默认」选项', 1, now(), now()),
    ('admin.common.media.clear', 'zh-CN', '清除', 200, 'admin', 'admin/content/article_edit.html: 媒体选择器「清除」按钮', 1, now(), now()),
    ('admin.common.media.clear', 'en-US', 'Clear', 200, 'admin', 'admin/content/article_edit.html: 媒体选择器「清除」按钮', 1, now(), now()),
    ('admin.common.media.pick', 'zh-CN', '媒体库', 200, 'admin', 'admin/content/article_edit.html: 媒体选择器「媒体库」按钮', 1, now(), now()),
    ('admin.common.media.pick', 'en-US', 'Media library', 200, 'admin', 'admin/content/article_edit.html: 媒体选择器「媒体库」按钮', 1, now(), now()),
    ('admin.coupons.status.unknown_lead', 'zh-CN', '（未知状态：', 200, 'admin', 'admin/order/coupons.html: 未知状态文案 ①（拼接段，英文以空格结尾）', 1, now(), now()),
    ('admin.coupons.status.unknown_lead', 'en-US', '(unknown status: ', 200, 'admin', 'admin/order/coupons.html: 未知状态文案 ①（拼接段，英文以空格结尾）', 1, now(), now()),
    ('admin.coupons.status.unknown_tail', 'zh-CN', '）', 200, 'admin', 'admin/order/coupons.html: 未知状态文案 ②', 1, now(), now()),
    ('admin.coupons.status.unknown_tail', 'en-US', ')', 200, 'admin', 'admin/order/coupons.html: 未知状态文案 ②', 1, now(), now()),
    ('admin.customers.filter.hint.clickable', 'zh-CN', '上面的徽章本身就是筛选：点一下就按那个口径筛一遍，可以叠加（叠加时会有多个徽章同时高亮），点「全部」回到不按状态 / 邮箱 / 锁定筛。', 200, 'admin', 'admin/user/customers.html: 徽章筛选说明', 1, now(), now()),
    ('admin.customers.filter.hint.clickable', 'en-US', 'The badges above are the filters themselves: click one to filter by that dimension. Filters stack (several badges highlight at once); click All to go back to no status / email / lock filtering.', 200, 'admin', 'admin/user/customers.html: 徽章筛选说明', 1, now(), now()),
    ('admin.depts.filter_empty', 'zh-CN', '没有匹配的部门。', 200, 'admin', 'admin/system/departments.html: 客户端筛选无结果提示（与 400 同值，见文件头）', 1, now(), now()),
    ('admin.depts.filter_empty', 'en-US', 'No departments match.', 200, 'admin', 'admin/system/departments.html: 客户端筛选无结果提示（与 400 同值，见文件头）', 1, now(), now()),
    ('admin.mail.marketing.contact_status.pick', 'zh-CN', '（选择目标状态）', 200, 'admin', 'admin/mail/mail_marketing.html: 目标状态下拉占位项', 1, now(), now()),
    ('admin.mail.marketing.contact_status.pick', 'en-US', '(choose a target status)', 200, 'admin', 'admin/mail/mail_marketing.html: 目标状态下拉占位项', 1, now(), now()),
    ('admin.mail.marketing.contact_status.target', 'zh-CN', '目标状态', 200, 'admin', 'admin/mail/mail_marketing.html: 「目标状态」字段标签', 1, now(), now()),
    ('admin.mail.marketing.contact_status.target', 'en-US', 'Target status', 200, 'admin', 'admin/mail/mail_marketing.html: 「目标状态」字段标签', 1, now(), now()),
    ('admin.menus.filter_empty', 'zh-CN', '没有匹配的菜单项。', 200, 'admin', 'admin/system/menus.html: 客户端筛选无结果提示（与 400 同值，见文件头）', 1, now(), now()),
    ('admin.menus.filter_empty', 'en-US', 'No menu items match.', 200, 'admin', 'admin/system/menus.html: 客户端筛选无结果提示（与 400 同值，见文件头）', 1, now(), now()),
    ('admin.navigations.add_to_menu_disabled', 'zh-CN', '本组没有可加入的内容：都还没有公开路径，先发布后再添加。', 200, 'admin', 'admin/navigation/navigations.html: 「加入菜单」禁用态说明', 1, now(), now()),
    ('admin.navigations.add_to_menu_disabled', 'en-US', 'Nothing in this group can be added yet: none of it has a public path. Publish first, then add.', 200, 'admin', 'admin/navigation/navigations.html: 「加入菜单」禁用态说明', 1, now(), now()),
    ('admin.navigations.field.parent', 'zh-CN', '层级', 200, 'admin', 'admin/navigation/navigations.html: 「层级」字段标签', 1, now(), now()),
    ('admin.navigations.field.parent', 'en-US', 'Level', 200, 'admin', 'admin/navigation/navigations.html: 「层级」字段标签', 1, now(), now()),
    ('admin.navigations.field.path', 'zh-CN', '链接', 200, 'admin', 'admin/navigation/navigations.html: 「链接」字段标签', 1, now(), now()),
    ('admin.navigations.field.path', 'en-US', 'Link', 200, 'admin', 'admin/navigation/navigations.html: 「链接」字段标签', 1, now(), now()),
    ('admin.navigations.field.target', 'zh-CN', '打开方式', 200, 'admin', 'admin/navigation/navigations.html: 「打开方式」字段标签', 1, now(), now()),
    ('admin.navigations.field.target', 'en-US', 'Open in', 200, 'admin', 'admin/navigation/navigations.html: 「打开方式」字段标签', 1, now(), now()),
    ('admin.navigations.field.title', 'zh-CN', '菜单文字', 200, 'admin', 'admin/navigation/navigations.html: 「菜单文字」字段标签', 1, now(), now()),
    ('admin.navigations.field.title', 'en-US', 'Menu label', 200, 'admin', 'admin/navigation/navigations.html: 「菜单文字」字段标签', 1, now(), now()),
    ('admin.pages.impact.count_tail', 'zh-CN', ' 个页面有更新未发布', 200, 'admin', 'admin/page/pages.html: 全站待重建计数（拼接段，中英均以空格开头）', 1, now(), now()),
    ('admin.pages.impact.count_tail', 'en-US', ' pages have updates that are not published', 200, 'admin', 'admin/page/pages.html: 全站待重建计数（拼接段，中英均以空格开头）', 1, now(), now()),
    ('admin.pages.impact.heading', 'zh-CN', '全站待重建', 200, 'admin', 'admin/page/pages.html: 全站待重建卡标题', 1, now(), now()),
    ('admin.pages.impact.heading', 'en-US', 'Site-wide rebuild', 200, 'admin', 'admin/page/pages.html: 全站待重建卡标题', 1, now(), now()),
    ('admin.pages.impact.more_lead', 'zh-CN', '（清单只列了前 ', 200, 'admin', 'admin/page/pages.html: 截断清单说明 ①（拼接段，中英均以空格结尾）', 1, now(), now()),
    ('admin.pages.impact.more_lead', 'en-US', '(the list shows only the first ', 200, 'admin', 'admin/page/pages.html: 截断清单说明 ①（拼接段，中英均以空格结尾）', 1, now(), now()),
    ('admin.pages.impact.more_tail', 'zh-CN', ' 条，其余只有计数；重新构建这些页面后新内容才会出现在访问面。）', 200, 'admin', 'admin/page/pages.html: 截断清单说明 ②（拼接段，中英均以空格开头）', 1, now(), now()),
    ('admin.pages.impact.more_tail', 'en-US', ' items; the rest are counted only. Rebuild these pages and the new content appears on the live site.)', 200, 'admin', 'admin/page/pages.html: 截断清单说明 ②（拼接段，中英均以空格开头）', 1, now(), now()),
    ('admin.pages.impact.never_published', 'zh-CN', '未上线', 200, 'admin', 'admin/page/pages.html: 全站待重建清单「未上线」标记', 1, now(), now()),
    ('admin.pages.impact.never_published', 'en-US', 'Never published', 200, 'admin', 'admin/page/pages.html: 全站待重建清单「未上线」标记', 1, now(), now()),
    ('admin.pages.impact.none', 'zh-CN', '当前没有待重建的页面：全部站点工程的页面产物都与最新内容一致。', 200, 'admin', 'admin/page/pages.html: 无待重建页面时空态', 1, now(), now()),
    ('admin.pages.impact.none', 'en-US', 'Nothing is pending a rebuild right now: page artifacts across every site project match the latest content.', 200, 'admin', 'admin/page/pages.html: 无待重建页面时空态', 1, now(), now()),
    ('admin.pages.impact.scope_note', 'zh-CN', '口径：全部站点工程。下方页面列表只显示当前聚焦的工程，两处的数不是同一个。', 200, 'admin', 'admin/page/pages.html: 全站待重建口径说明', 1, now(), now()),
    ('admin.pages.impact.scope_note', 'en-US', 'Scope: every site project. The page list below shows only the project in focus, so the two numbers are not the same.', 200, 'admin', 'admin/page/pages.html: 全站待重建口径说明', 1, now(), now()),
    ('admin.pages.impact.unavailable', 'zh-CN', '本次读不到全站待重建清单（已记日志），可刷新重试；下方页面列表不受影响。', 200, 'admin', 'admin/page/pages.html: 全站待重建读取失败时的降级说明', 1, now(), now()),
    ('admin.pages.impact.unavailable', 'en-US', 'The site-wide rebuild list could not be read this time (logged). Refresh to retry; the page list below is unaffected.', 200, 'admin', 'admin/page/pages.html: 全站待重建读取失败时的降级说明', 1, now(), now()),
    ('admin.pages.last_error', 'zh-CN', '上一次操作未完成：', 200, 'admin', 'admin/page/pages.html: 页顶错误前缀', 1, now(), now()),
    ('admin.pages.last_error', 'en-US', 'The last operation did not finish:', 200, 'admin', 'admin/page/pages.html: 页顶错误前缀', 1, now(), now()),
    ('admin.pages.list.delete_confirm_prefix', 'zh-CN', '确定删除页面「', 200, 'admin', 'admin/page/pages.html: 单页删除确认 ①（前缀）', 1, now(), now()),
    ('admin.pages.list.delete_confirm_prefix', 'en-US', 'Delete the page "', 200, 'admin', 'admin/page/pages.html: 单页删除确认 ①（前缀）', 1, now(), now()),
    ('admin.pages.list.delete_confirm_suffix', 'zh-CN', '」？已发布的路径会从线上下线（旧链接直接 404）。', 200, 'admin', 'admin/page/pages.html: 单页删除确认 ②（后缀）', 1, now(), now()),
    ('admin.pages.list.delete_confirm_suffix', 'en-US', '"? Its published paths go offline (old links return 404).', 200, 'admin', 'admin/page/pages.html: 单页删除确认 ②（后缀）', 1, now(), now()),
    ('admin.products.create.title', 'zh-CN', '基础信息', 200, 'admin', 'admin/product/products_new.html: 「基础信息」区标题', 1, now(), now()),
    ('admin.products.create.title', 'en-US', 'Basics', 200, 'admin', 'admin/product/products_new.html: 「基础信息」区标题', 1, now(), now()),
    ('admin.products.detailTemplate.title', 'zh-CN', '详情页模板', 200, 'admin', 'admin/product/products_new.html: 「详情页模板」区标题', 1, now(), now()),
    ('admin.products.detailTemplate.title', 'en-US', 'Detail page template', 200, 'admin', 'admin/product/products_new.html: 「详情页模板」区标题', 1, now(), now()),
    ('admin.settings.locales.mode.all_prefix', 'zh-CN', 'all_prefix — 所有语言一律加短码前缀（/zh/about）', 200, 'admin', 'admin/project/settings.html: 多语言路径方案选项（all_prefix）', 1, now(), now()),
    ('admin.settings.locales.mode.all_prefix', 'en-US', 'all_prefix — every language gets a short-code prefix (/zh/about)', 200, 'admin', 'admin/project/settings.html: 多语言路径方案选项（all_prefix）', 1, now(), now()),
    ('admin.settings.locales.mode.default_plain', 'zh-CN', 'default_plain — 默认语言无前缀（/about），其余语言加短码（/en/about）', 200, 'admin', 'admin/project/settings.html: 多语言路径方案选项（default_plain）', 1, now(), now()),
    ('admin.settings.locales.mode.default_plain', 'en-US', 'default_plain — the default language has no prefix (/about), other languages get a short code (/en/about)', 200, 'admin', 'admin/project/settings.html: 多语言路径方案选项（default_plain）', 1, now(), now()),
    ('admin.settings.locales.mode_label', 'zh-CN', '多语言访问路径方案', 200, 'admin', 'admin/project/settings.html: 「多语言访问路径方案」字段标签', 1, now(), now()),
    ('admin.settings.locales.mode_label', 'en-US', 'Multilingual URL scheme', 200, 'admin', 'admin/project/settings.html: 「多语言访问路径方案」字段标签', 1, now(), now()),
    ('admin.settings.locales.mode.off', 'zh-CN', 'off — 单语言：各语言共用同一路径', 200, 'admin', 'admin/project/settings.html: 多语言路径方案选项（off）', 1, now(), now()),
    ('admin.settings.locales.mode.off', 'en-US', 'off — single language: all languages share one path', 200, 'admin', 'admin/project/settings.html: 多语言路径方案选项（off）', 1, now(), now()),
    ('admin.site_slots.bind_panel.page', 'zh-CN', '要绑定的页面', 200, 'admin', 'admin/page/site_slots.html: 绑定面板「要绑定的页面」字段', 1, now(), now()),
    ('admin.site_slots.bind_panel.page', 'en-US', 'Page to bind', 200, 'admin', 'admin/page/site_slots.html: 绑定面板「要绑定的页面」字段', 1, now(), now()),
    ('admin.site_slots.load_failed.reload', 'zh-CN', '重新加载', 200, 'admin', 'admin/page/site_slots.html: 加载失败时「重新加载」按钮', 1, now(), now()),
    ('admin.site_slots.load_failed.reload', 'en-US', 'Reload', 200, 'admin', 'admin/page/site_slots.html: 加载失败时「重新加载」按钮', 1, now(), now()),
    ('admin.site_slots.no_project_list', 'zh-CN', '槽位是挂在站点工程下的：这个后台还没有工程，所以这里没有可列出的槽位。', 200, 'admin', 'admin/page/site_slots.html: 无站点工程时空态说明', 1, now(), now()),
    ('admin.site_slots.no_project_list', 'en-US', 'Slots hang off a site project, and this backend has no project yet — so there is nothing to list here.', 200, 'admin', 'admin/page/site_slots.html: 无站点工程时空态说明', 1, now(), now()),
    ('admin.site_slots.select_page_placeholder', 'zh-CN', '选择页面…', 200, 'admin', 'admin/page/site_slots.html: 页面选择器占位符', 1, now(), now()),
    ('admin.site_slots.select_page_placeholder', 'en-US', 'Choose a page…', 200, 'admin', 'admin/page/site_slots.html: 页面选择器占位符', 1, now(), now()),
    ('admin.theme.confirm_delete_note', 'zh-CN', '该主题的颜色 / 字体 / 页眉页脚设置会一并删除；挂在这套主题下的页面会自动改挂到当前激活主题。激活中的主题不能删除，请先切换到其它主题。', 200, 'admin', 'admin/project/theme.html: 删除主题确认说明', 1, now(), now()),
    ('admin.theme.confirm_delete_note', 'en-US', 'Deleting this theme also deletes its color / font / header-footer settings; pages pinned to this theme move to the active theme automatically. The active theme cannot be deleted — switch to another theme first.', 200, 'admin', 'admin/project/theme.html: 删除主题确认说明', 1, now(), now()),
    ('admin.theme.empty_desc', 'zh-CN', '主题决定站点前端的全局颜色、字体与页眉页脚；创建的第一个主题会自动激活。', 200, 'admin', 'admin/project/theme.html: 无主题空态说明', 1, now(), now()),
    ('admin.theme.empty_desc', 'en-US', 'A theme decides the global colors, fonts and header/footer of the site front end; the first theme you create becomes active automatically.', 200, 'admin', 'admin/project/theme.html: 无主题空态说明', 1, now(), now()),
    ('admin.theme.empty_title', 'zh-CN', '还没有主题', 200, 'admin', 'admin/project/theme.html: 无主题空态标题', 1, now(), now()),
    ('admin.theme.empty_title', 'en-US', 'No themes yet', 200, 'admin', 'admin/project/theme.html: 无主题空态标题', 1, now(), now()),
    ('admin.theme.no_project_action', 'zh-CN', '去页面管理', 200, 'admin', 'admin/project/theme.html: 无站点工程空态操作', 1, now(), now()),
    ('admin.theme.no_project_action', 'en-US', 'Go to pages', 200, 'admin', 'admin/project/theme.html: 无站点工程空态操作', 1, now(), now()),
    ('admin.theme.no_project_desc', 'zh-CN', '主题是站点工程的全局外观：颜色、字体与页眉页脚都挂在某个工程下，工程建好后再回来配置。', 200, 'admin', 'admin/project/theme.html: 无站点工程空态说明', 1, now(), now()),
    ('admin.theme.no_project_desc', 'en-US', 'A theme is the global look of a site project: colors, fonts and header/footer all hang off one project. Come back to configure it once the project exists.', 200, 'admin', 'admin/project/theme.html: 无站点工程空态说明', 1, now(), now()),
    ('admin.inventory.adjust.direction.adjust', 'zh-CN', '盘点（填目标绝对量）', 200, 'admin', 'admin/inventory/inventory.html: 库存调整方向下拉项（Go 侧 adjustDirectionOptions，inventory_page_handle.go）', 1, now(), now()),
    ('admin.inventory.adjust.direction.adjust', 'en-US', 'Stocktake (enter the target absolute quantity)', 200, 'admin', 'admin/inventory/inventory.html: 库存调整方向下拉项（Go 侧 adjustDirectionOptions，inventory_page_handle.go）', 1, now(), now()),
    ('admin.inventory.adjust.direction.out', 'zh-CN', '报损（填本次减少量）', 200, 'admin', 'admin/inventory/inventory.html: 库存调整方向下拉项（Go 侧 adjustDirectionOptions，inventory_page_handle.go）', 1, now(), now()),
    ('admin.inventory.adjust.direction.out', 'en-US', 'Write-off (enter the quantity lost this time)', 200, 'admin', 'admin/inventory/inventory.html: 库存调整方向下拉项（Go 侧 adjustDirectionOptions，inventory_page_handle.go）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
