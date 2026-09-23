-- 412 · 库存域三个列表页的分页信息行词条
--
-- 背景：库存流水 / 货源 / 采购入库三页此前是「写死上限 + 没有分页条」
--（流水 50 / 货源 200 / 采购单 100，第 N+1 条静默消失）。本批接入项目既有的分页设施，
--「上一页 / 下一页」复用 059 已登记的 shell.pagination.*，本批只新增本域信息行的 3 个 key：
--   · admin.inventory.pagination.info —— 信息行的前半段（页码与每页条数）。
--   · admin.inventory.pagination.more —— 探测到下一页还有数据时的后缀。
--   · admin.inventory.pagination.end —— 末页的后缀（与上面一条互斥）。
-- 共 6 行（中英各一行 × 3）；不修改任何既有词条的值。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
--       注册见 register_inventory_pagination_i18n.go。
--
-- 2026-09（419 批）：这 3 个 key 随「降级分页条」一起退役 —— 库存三页换成真源分页后，
-- 信息行与翻页按钮统一走 shell.pagination.*，本域词条失去引用，由迁移 419 删除。
-- 本批的幂等条件同批从「3 个 key 的 en-US 行都在（>=3）」改成「3 个 key 一行都不剩（=0）」，
-- 于是本批不会再往库里插回已退役的词条，也不会因条件恒假每轮重跑
--（AGENTS.md「删能力要连 seed 的 SQL 与幂等条件一起收口」，122 号迁移的坑）。
--
-- 注：本文件曾在本轮并行作业中误删后按当时的读取记录重建（INSERT 行逐字一致；
--     头部注释的措辞为重建版本，与原文可能略有出入）。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.inventory.pagination.info', 'zh-CN', '第 %s 页 · 每页 %s 条', 200, 'admin', '库存流水 / 货源 / 采购入库三页的分页信息行（页码 · 每页条数；总数需要契约提供 CountXxx，本批不做）', 1, now(), now()),
('admin.inventory.pagination.info', 'en-US', 'Page %s · %s per page', 200, 'admin', '库存流水 / 货源 / 采购入库三页的分页信息行（页码 · 每页条数；总数需要契约提供 CountXxx，本批不做）', 1, now(), now()),
('admin.inventory.pagination.more', 'zh-CN', '后面还有记录', 200, 'admin', '库存三页分页信息行的后缀：实探到下一页还有数据（同条件 + page+1 + 只取 1 条）', 1, now(), now()),
('admin.inventory.pagination.more', 'en-US', 'more records available', 200, 'admin', '库存三页分页信息行的后缀：实探到下一页还有数据（同条件 + page+1 + 只取 1 条）', 1, now(), now()),
('admin.inventory.pagination.end', 'zh-CN', '已是最后一页', 200, 'admin', '库存三页分页信息行的后缀：后面没有更多数据', 1, now(), now()),
('admin.inventory.pagination.end', 'en-US', 'last page', 200, 'admin', '库存三页分页信息行的后缀：后面没有更多数据', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
