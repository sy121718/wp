-- 270 · 入库入口的 SKU 编码校验词条：ErrStockSKURequired（中英成对）。
--
-- 与本项目所有 enums 常量一致：常量值就是 i18n key，真文案在这张表里。缺词条的后果是
-- 接口原样返回 key、页面原样显示 key（既不中文也不是话），所以新增常量必须同批 seed。
--
-- 背景（本批主任务）：入库新建库存行时 sku_code 的口径缺口 ——
--   · 仓库里的 SKU **永远是裸码**（DRAWERSMOKE_001），不带仓码前缀（迁移 262 已把存量剥干净）；
--   · 但入库不是商品侧的调用路径（RegisterReceipt / RegisterProductionInbound 按请求里的编码建行），
--     编码为空串或带前缀时会直接破坏不变量。现在入库入口统一归一 + 校验，空串一律拒绝 ——
--     被拒绝时给的就是本词条：文案必须**可行动**（告诉调用方传裸码），而不是「参数错误」。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('ErrStockSKURequired', 'zh-CN', '缺少仓库侧 SKU 编码：请传裸码（不带仓码前缀，如 DRAWERSMOKE_001）', 400, 'inventory', 'internal/module/product/inventory/enums', 1, now(), now()),
    ('ErrStockSKURequired', 'en-US', 'Warehouse-side SKU code is required: pass the bare code without a warehouse prefix (e.g. DRAWERSMOKE_001)', 400, 'inventory', 'internal/module/product/inventory/enums', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
