-- 280 · 库存后台页成功回执的受控文案（admin.inventory.actionDone，中英各一行）。
--
-- 背景：库存 / 仓库 / 原因字典 / 货源 / 采购五个页面的写入口成功时统一回带 ?ok=1 / ?done=1，
-- 而模板是**直接渲染**这个值的 —— 运营看到的是一条裸「1」。本批把成功态回显收敛成
-- 「固定 token → 翻译后的固定文案」（读侧出口见
-- internal/module/product/inventory/inbound/http/inventory_page_handle.go 的 inventoryNoticeSuccess）：
-- 命中 token 用本词条，其余取值过读侧白名单、未命中落空串 —— 不再有任意字符串进模板的通道。
--
-- 为什么不是新的 enums 常量：它不是任何接口的错误 / 成功消息，只是页面回执的展示文案，
-- 与 shell.MsgInternalError / shell.err.bulkIdsTooMany 同属「壳 / 页面级文案」，因此值带模块前缀
--（sys_i18n 主键是 (item_key, lang)，裸 key 会与别的模块撞车）。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.inventory.actionDone', 'zh-CN', '操作已完成', 200, 'inventory', 'internal/module/product/inventory/inbound/http/inventory_page_handle.go', 1, now(), now()),
    ('admin.inventory.actionDone', 'en-US', 'Done.', 200, 'inventory', 'internal/module/product/inventory/inbound/http/inventory_page_handle.go', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
