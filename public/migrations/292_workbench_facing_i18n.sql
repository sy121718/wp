-- 292 · 工作台「预览 / 编译 422」的可归因文案（workbench.err.*）
--
-- 背景：结构模板（页眉 / 页脚）的文档里若带了字段绑定（旧脏数据，或画布上拖入带绑定的组件后
-- 走**草稿预览** —— 保存期 contenttemplate 会拒绝绑定，草稿预览直接编译未保存文档、没有那道校验），
-- 预览与编译入口会以 HTTP 422 暴露。状态码刻意保留 422（那是作者能自己处理的配置问题，不是服务端
-- 故障），缺的是**可归因**：一句「预览编译失败」既不说错在哪，也不说下一步做什么。
--
-- 8 个 key × 中英 = 16 行，与 internal/module/workbench/enums/workbench_enums.go 的
-- workbench.err.* 常量、inbound/http/workbench_err.go 的中文兜底逐条对应（改一处必须三处同改：
-- 常量、兜底、本文件的中英词条）。
--
-- 另有 1 行：MsgCompileFailed 的 **en-US**（本批第 17 行）。它在 058 只 seed 了 zh-CN（「预览编译失败」），
-- 而本轮之后它出现在 422 的**响应路径**上（归口文案），英文界面不该回落中文 ——「改造前也是中文」
-- 不是理由：改造前它根本不在响应路径上、也没有词条 key。该行与 058 的 zh-CN 行保持同形
--（category / http_code 沿用该 key 的历史口径，不擅自改），只补缺失的语言。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING（seed 是默认值来源，后台是真相来源）。
-- 判定枚举本批**全部 8 个 key**（>=8）：用全库行数会被同期其它批次的行满足而静默跳过（226/277 踩过）。
--
-- 两条硬纪律（上一批因「多一个分号」整批迁移失败，50+ 测试包全红）：
--   · 值里不得出现 ASCII 半角分号与未配对的单引号 —— 语句边界由 SplitStatements 按字面量判定，
--     一个游离的分号会把 INSERT 切成两半；
--   · 本文件末尾**不写**分号：最后一条语句由 SplitStatements 收尾的 appendCurrent 落下。
--
-- 注册：public/migrations/register_workbench_facing_i18n.go（由 register.go 的 init 调用）。

INSERT INTO sys_i18n (item_key, lang, item_value, category, remark, status, http_code, create_time, update_time)
VALUES
	('workbench.err.structureTemplateFieldBinding', 'zh-CN', '这份页眉 / 页脚里有「字段绑定」：结构模板是站点级结构，不是某个内容实体的实例，同一份页眉在商品页与文章页会拿到不同的值，所以构建时不解析绑定。请到画布里选中带绑定的组件（标题 / 正文 / 图片 / 商品卡等），清空它的绑定槽位改用静态内容；要让某段内容随实体变化，请把那个组件放进对应的内容模板。', 'error', '', 1, 422, now(), now()),
	('workbench.err.structureTemplateFieldBinding', 'en-US', 'This header or footer contains a field binding. Structure templates are site-wide structure rather than instances of a content entity: the same header would show different values on a product page and on an article page, so bindings are not resolved at build time. In the canvas, select the bound component (heading, text, image, product card), clear its binding slot and use static content instead. To make a piece of content follow the entity, move that component into the matching content template.', 'error', '', 1, 422, now(), now()),
	('workbench.err.previewFieldBindingUnsupported', 'zh-CN', '这份文档里有「字段绑定」，但页面与全局块不是内容实体实例，编译时不解析绑定。请到画布里选中带绑定的组件，清空绑定槽位改用静态内容（或删掉该组件）；需要按实体字段显示的内容，请放进对应的内容模板。', 'error', '', 1, 422, now(), now()),
	('workbench.err.previewFieldBindingUnsupported', 'en-US', 'This document contains a field binding, but pages and global blocks are not content entity instances and bindings are not resolved when compiling. In the canvas, clear the binding slot on that component (or delete it) and use static content instead. Content that must follow entity fields belongs in the matching content template.', 'error', '', 1, 422, now(), now()),
	('workbench.err.previewDocumentInvalid', 'zh-CN', '画布文档没通过校验：多半是某个组件的配置不完整（组件必需的槽位留空 —— 例如轮播没有 slide、表单缺提交按钮、图片缺地址），或组件树嵌套超过上限。请按画布与检查器里的提示逐项补齐、把结构拍平，再重新预览。', 'error', '', 1, 422, now(), now()),
	('workbench.err.previewDocumentInvalid', 'en-US', 'The canvas document did not pass validation: usually a component is incomplete (a required slot is empty, for example a carousel with no slide, a form without a submit button, an image without a URL), or the component tree is nested too deep. Fill in what the canvas and inspector flag, flatten the structure, then preview again.', 'error', '', 1, 422, now(), now()),
	('workbench.err.templateEntityTypeMismatch', 'zh-CN', '这套模板是给另一种内容类型用的（例如拿商品模板渲染文章），不能这样预览 —— 模板的数据源与当前实体的字段对不上。请换成与当前类型一致的模板；要新建就到「内容模板」页按当前类型建一套。', 'error', '', 1, 422, now(), now()),
	('workbench.err.templateEntityTypeMismatch', 'en-US', 'This template is built for another content type (for example a product template rendering an article), so it cannot be previewed here: the template data source and the current entity fields do not match. Pick a template matching the current type, or create one from the content templates page for this type.', 'error', '', 1, 422, now(), now()),
	('workbench.err.templateFieldBindingInvalid', 'zh-CN', '模板里的字段绑定越界：绑定的字段不属于当前内容类型的数据源，或字段名已被改名 / 删除。请回到工作台检查带绑定的组件，改用本类型数据源里确实存在的字段（白名单由实体类型决定）。', 'error', '', 1, 422, now(), now()),
	('workbench.err.templateFieldBindingInvalid', 'en-US', 'A field binding in this template is out of range: the bound field does not belong to the data source of the current content type, or the field has been renamed or removed. Check the bound components in the workbench and use fields that really exist in this type data source (the whitelist comes from the entity type).', 'error', '', 1, 422, now(), now()),
	('workbench.err.templateProjectScope', 'zh-CN', '预览的工程作用域没定下来：站点里有多个工程时必须显式指定。请从对应工程的入口（页面列表 / 商品详情）重新打开工作台，让地址里带上 projectId。', 'error', '', 1, 422, now(), now()),
	('workbench.err.templateProjectScope', 'en-US', 'The project scope for this preview is unresolved: on a site with several projects it must be given explicitly. Reopen the workbench from the entry of the project you want (page list or product detail) so the URL carries projectId.', 'error', '', 1, 422, now(), now()),
	('workbench.err.instanceNoDocument', 'zh-CN', '这个实例还没有可编辑的文档：它既没有发布过快照，也没有自己的覆盖文档。请先到它的详情页点「重新套用预设」生成初始文档，再回到工作台编辑。', 'error', '', 1, 422, now(), now()),
	('workbench.err.instanceNoDocument', 'en-US', 'This instance has no editable document yet: it has never been published and has no override document of its own. Open its detail page, click Reapply preset to create the initial document, then come back to the workbench.', 'error', '', 1, 422, now(), now()),
	('workbench.err.instanceSaveRejected', 'zh-CN', '保存没通过：这份文档没有编译成功（组件必需槽位留空 / 字段绑定越界 / 引用的模板或块不可用都会走到这里）。你的改动没有写入，线上内容保持不变 —— 请按画布与检查器里的提示修正后重试；想回到跟随模板的状态，用详情页的「重新套用预设」。', 'error', '', 1, 422, now(), now()),
	('workbench.err.instanceSaveRejected', 'en-US', 'The save was rejected: this document did not compile (an empty required slot, an out-of-range field binding, or an unavailable template or block all land here). Your changes were not written and the live site is unchanged. Fix what the canvas and inspector flag and retry, or use Reapply preset on the detail page to go back to following the template.', 'error', '', 1, 422, now(), now()),
	('MsgCompileFailed', 'en-US', 'Preview compile failed', 'ui', 'internal/module/workbench/enums/workbench_enums.go', 1, 200, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING
