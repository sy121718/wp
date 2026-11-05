-- 709 · AI 视图块空态词条（四类块共用一个 key）。
--
-- 背景：ui_blocks.html 的 stat / table / list / accordion 四种块，空数据时原本靠
-- 数据源往 TableData.Empty / ListData.Empty / StatData.Empty / AccordionData.Empty
-- 里塞一句成品中文。那个字段没有任何生产构造者（工具只交数据与表头），
-- 读侧等于永远为空 —— 也就是「空态什么都不显示」。
--
-- 现在空态归模板：模板用 tr("admin.ai.blocks.empty", "暂无数据") 取词。
-- 四类块的语义完全一致（这块没有数据），共用一个 key，不按类型拆四条。
--
-- category ui：它是界面文案，不是错误声明；不进任何 enums。
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.ai.blocks.empty', 'zh-CN', '暂无数据', 200, 'ui', 'internal/templates/admin/ai/ui_blocks.html: 四类视图块空态', 1, now(), now()),
    ('admin.ai.blocks.empty', 'en-US', 'No data yet.', 200, 'ui', 'internal/templates/admin/ai/ui_blocks.html: empty state for the four block kinds', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
