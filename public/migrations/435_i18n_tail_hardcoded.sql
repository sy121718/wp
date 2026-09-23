-- 435 · i18n 收尾批次：后台模板硬编码中文清零（脚本 scripts/check-i18n-coverage.sh 基线 19 → 0）
--
-- 本批把门禁清单里剩下的 19 行全部 key 化，落在三个模板上，新增 14 个 key
-- （每个 key 两行：zh-CN + en-US，共 28 行）：
--   · admin/partials/locale_rows.html —— 跨模块共用片段（settings 页 include + HTMX 行片段
--     两个入口），按约定挂 admin.common.locale.* 这一支：语言码 placeholder / 启用 / 空态；
--   · admin/project/theme_settings.html —— 折叠卡 summary 的「N 项」计数单位，以及两条固定注记；
--   · admin/product/product_attribute_rows.html —— 属性值编辑行（表头 3 列 + 两个 placeholder
--     + 行内勾选文案 + 添加按钮），挂 admin.product_attributes.values.* 新支。
--
-- 另有 4 个既有词条被**复用**（模板兜底与库值逐字一致，故本批不重复插入）：
--   admin.common.default（默认）、admin.common.action.delete（删除，两处复用）、
--   admin.product_attributes.col.key（标识）、admin.common.col.actions（操作）。
--   复用而不是新造同值词条：同页同义的词只有一份真源，改文案时不会只改一边。
--
-- 关于 product_attribute_rows.html 的取词形态：该片段的渲染数据是 **struct**（attrRowsCtx），
--   Jet 在 struct 上不支持 `.["t"]`（渲染时报 can't use t as field name in struct type），
--   所以模板里用的是数据类自带的 Tr 取词函数（{{tr := .Tr}} → tr("key", "兜底")），
--   与 attrGroupFormOpts.Tr 同一形态。key 与兜底形态与其它模板完全一致。
--
-- 幂等：INSERT ... ON CONFLICT (item_key, lang) DO NOTHING（sys_i18n 主键是 (item_key, lang)），
--   重复执行影响 0 行。
--
-- zh-CN 的值与模板里的兜底**逐字一致**（库值一旦存在就覆盖兜底，两边不一致 = 顺手改了文案）。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.common.locale.ph.lang', 'zh-CN', '语言码，如 zh-CN', 200, 'admin', 'admin/partials/locale_rows.html: 语言码输入框 placeholder', 1, now(), now()),
    ('admin.common.locale.ph.lang', 'en-US', 'Language code, e.g. zh-CN', 200, 'admin', 'admin/partials/locale_rows.html: 语言码输入框 placeholder', 1, now(), now()),
    ('admin.common.locale.enabled', 'zh-CN', '启用', 200, 'admin', 'admin/partials/locale_rows.html: 行内「启用」勾选框文案', 1, now(), now()),
    ('admin.common.locale.enabled', 'en-US', 'Enabled', 200, 'admin', 'admin/partials/locale_rows.html: 行内「启用」勾选框文案', 1, now(), now()),
    ('admin.common.locale.empty', 'zh-CN', '还没有语言清单。至少保留一种语言，且默认语言必须启用。', 200, 'admin', 'admin/partials/locale_rows.html: 语言清单空态提示', 1, now(), now()),
    ('admin.common.locale.empty', 'en-US', 'No languages yet. Keep at least one language, and the default one must stay enabled.', 200, 'admin', 'admin/partials/locale_rows.html: 语言清单空态提示', 1, now(), now()),
    ('admin.theme_settings.group_items', 'zh-CN', '项', 200, 'admin', 'admin/project/theme_settings.html: 字段组折叠卡 summary 的计数单位（N 项）', 1, now(), now()),
    ('admin.theme_settings.group_items', 'en-US', 'items', 200, 'admin', 'admin/project/theme_settings.html: 字段组折叠卡 summary 的计数单位（N 项）', 1, now(), now()),
    ('admin.theme_settings.global_blocks_note', 'zh-CN', '3 项 —— 构建期内联进产物', 200, 'admin', 'admin/project/theme_settings.html: 全局页眉 / 页脚块折叠卡注记', 1, now(), now()),
    ('admin.theme_settings.global_blocks_note', 'en-US', '3 items — inlined into the artifacts at build time', 200, 'admin', 'admin/project/theme_settings.html: 全局页眉 / 页脚块折叠卡注记', 1, now(), now()),
    ('admin.theme_settings.structure_templates_note', 'zh-CN', '2 项 —— 构建期优先于块', 200, 'admin', 'admin/project/theme_settings.html: 结构模板折叠卡注记', 1, now(), now()),
    ('admin.theme_settings.structure_templates_note', 'en-US', '2 items — build-time templates take precedence over blocks', 200, 'admin', 'admin/project/theme_settings.html: 结构模板折叠卡注记', 1, now(), now()),
    ('admin.product_attributes.values.empty', 'zh-CN', '还没有属性值。点下面的「+ 添加值」开始。', 200, 'admin', 'admin/product/product_attribute_rows.html: 属性值编辑行空态提示', 1, now(), now()),
    ('admin.product_attributes.values.empty', 'en-US', 'No attribute values yet. Click "Add value" below to start.', 200, 'admin', 'admin/product/product_attribute_rows.html: 属性值编辑行空态提示', 1, now(), now()),
    ('admin.product_attributes.values.col.label', 'zh-CN', '显示名', 200, 'admin', 'admin/product/product_attribute_rows.html: 表头「显示名」列', 1, now(), now()),
    ('admin.product_attributes.values.col.label', 'en-US', 'Label', 200, 'admin', 'admin/product/product_attribute_rows.html: 表头「显示名」列', 1, now(), now()),
    ('admin.product_attributes.values.col.sort', 'zh-CN', '排序', 200, 'admin', 'admin/product/product_attribute_rows.html: 表头「排序」列', 1, now(), now()),
    ('admin.product_attributes.values.col.sort', 'en-US', 'Sort', 200, 'admin', 'admin/product/product_attribute_rows.html: 表头「排序」列', 1, now(), now()),
    ('admin.product_attributes.values.col.status', 'zh-CN', '状态', 200, 'admin', 'admin/product/product_attribute_rows.html: 表头「状态」列', 1, now(), now()),
    ('admin.product_attributes.values.col.status', 'en-US', 'Status', 200, 'admin', 'admin/product/product_attribute_rows.html: 表头「状态」列', 1, now(), now()),
    ('admin.product_attributes.values.ph.key', 'zh-CN', '留空自动生成', 200, 'admin', 'admin/product/product_attribute_rows.html: 标识输入框 placeholder', 1, now(), now()),
    ('admin.product_attributes.values.ph.key', 'en-US', 'Leave blank to generate', 200, 'admin', 'admin/product/product_attribute_rows.html: 标识输入框 placeholder', 1, now(), now()),
    ('admin.product_attributes.values.ph.label', 'zh-CN', '如 红色 / S', 200, 'admin', 'admin/product/product_attribute_rows.html: 显示名输入框 placeholder', 1, now(), now()),
    ('admin.product_attributes.values.ph.label', 'en-US', 'e.g. Red / S', 200, 'admin', 'admin/product/product_attribute_rows.html: 显示名输入框 placeholder', 1, now(), now()),
    ('admin.product_attributes.values.enabled', 'zh-CN', '启用', 200, 'admin', 'admin/product/product_attribute_rows.html: 行内「启用」勾选框文案', 1, now(), now()),
    ('admin.product_attributes.values.enabled', 'en-US', 'Enabled', 200, 'admin', 'admin/product/product_attribute_rows.html: 行内「启用」勾选框文案', 1, now(), now()),
    ('admin.product_attributes.values.add', 'zh-CN', '添加值', 200, 'admin', 'admin/product/product_attribute_rows.html: 「添加值」按钮（前面的 + 是符号，不入词条）', 1, now(), now()),
    ('admin.product_attributes.values.add', 'en-US', 'Add value', 200, 'admin', 'admin/product/product_attribute_rows.html: 「添加值」按钮（前面的 + 是符号，不入词条）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
