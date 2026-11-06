-- 当前词条回补源；历史原文 407_i18n_product_inventory_page_err.sql 保持归档，不再执行。
-- ========================================
-- 407 — 商品 / 库存域后台页失败出口的新增词条
--
-- 背景：三处页面 handler 的失败出口此前是
--   · product_detail_template_page.go 的预览入口：`c.String(400, "%s", errTemplateDepsMissing)`
--     —— 一行**硬编码中文**常量的纯文本（脱壳、不翻译、预览的新标签页里什么都没有）；
--   · inventory_source_page_handle.go / inventory_purchase_page_handle.go 的列表装载失败：
--     `c.String(500, shell.MsgInternalError)` —— 一块**裸归口 key** 的纯文本，
--     页面上显示的就是 `MsgInternalError` 这串英文，页壳（侧栏 / 页头 / 筛选栏）整块消失。
--
-- 本批把它们收成「回来源页 + ?err=提示」与「降级渲染」（空数据 + 归口提示 + HTTP 200，
-- 页壳保留；判据与 order 域订单页 / project 域主题页一致），文案随之走 i18n。
--
-- 本批新增 3 个 key（另补的既有英文 MsgInternalError 已退役，改由 747/738 统一处理）：
--   · admin.product_detail_template.depsMissing —— 详情页模板页「依赖未装配」这条提示
--     原先硬编码在 Go 里（errTemplateDepsMissing，五个写入口经 ?err= 回带），接上 i18n
--     之后英文站点不再显示中文；
--   · admin.common.list.loadFailedTitle / .loadFailedDesc —— 后台列表页「装载失败」的空态文案位。
--     order 域订单页 / 退货页的模板已在用它（模板里只有 t() 兜底中文），但从未登记词条 ——
--     页面上显示的一直是中文兜底；本批库存两页复用同一位文案，随批补齐中英。
-- 幂等：INSERT ... ON CONFLICT (item_key, lang) DO NOTHING；本批**只新增、不修改任何既有词条的值**。
-- ========================================

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)

SELECT d.item_key, d.lang, d.item_value, d.http_code, d.category, d.remark, d.status, d.create_time, d.update_time
FROM (VALUES
('tplDepsMiss', 'zh-CN', '详情页模板能力未装配（缺少内容模板或自动发布契约）', 200, 'admin', 'product/inbound/http: 详情页模板页依赖未装配（五个写入口经 ?err= 回带）', 1, now(), now()),
('tplDepsMiss', 'en-US', 'The detail-page template capability is not wired up (a content template or the auto-publish contract is missing).', 200, 'admin', 'product/inbound/http: 详情页模板页依赖未装配（五个写入口经 ?err= 回带）', 1, now(), now()),
('admin.common.list.loadFailedTitle', 'zh-CN', '这一页的数据没能读出来', 200, 'admin', '后台列表页装载失败的空态标题（order 域订单页 / 退货页在用；本批库存两页复用）', 1, now(), now()),
('admin.common.list.loadFailedTitle', 'en-US', 'This page could not load its data', 200, 'admin', '后台列表页装载失败的空态标题（order 域订单页 / 退货页在用；本批库存两页复用）', 1, now(), now()),
('admin.common.list.loadFailedDesc', 'zh-CN', '页顶的提示说明了原因；稍后重试即可，也可以先切到别的菜单。', 200, 'admin', '后台列表页装载失败的空态描述（与上面一条成对）', 1, now(), now()),
('admin.common.list.loadFailedDesc', 'en-US', 'The notice at the top of the page explains why; retry in a moment, or switch to another menu first.', 200, 'admin', '后台列表页装载失败的空态描述（与上面一条成对）', 1, now(), now())
) AS d(item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
WHERE NOT EXISTS (SELECT 1 FROM (VALUES ('tplDepsMiss', 'admin.product_detail_template.depsMissing')) AS renamed(new_key, old_key)
    JOIN sys_i18n source ON source.item_key = renamed.old_key AND source.lang = d.lang
    WHERE renamed.new_key = d.item_key)
ON CONFLICT (item_key, lang) DO NOTHING;
