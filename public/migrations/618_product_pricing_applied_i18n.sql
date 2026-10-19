-- 618 —— 调价回执合并成一条带计数的整句（替换 191 里拆成 lead / tail 的两条）。
--
-- 背景：应用调价的回执此前是「前缀 + 计数 + 后缀」三段拼在 Go 里（ProductPricingAppliedLead +
-- strconv + ProductPricingAppliedTail）。取词收归响应出口后，业务层只交一条事实（key + 计数参数），
-- 因此改成一句 {0} 占位整句 —— 语序归词条，不再由 Go 决定中文语序。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING；执行门槛由 register_product_pricing_applied_i18n.go
-- 逐条枚举本批自己的 item_key（上界封闭）。旧的两条 key 不再有消费点，同批删除。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.product_pricing.applied', 'zh-CN', '已应用调价：本次改动 %s 个变体，明细见下方留痕台账。', 200, 'admin', 'admin/product_pricing.html', 1, now(), now()),
    ('admin.product_pricing.applied', 'en-US', 'Prices applied: %s variant(s) changed this run; see the ledger below for details.', 200, 'admin', 'admin/product_pricing.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;

DELETE FROM sys_i18n
 WHERE item_key IN ('admin.product_pricing.applied.lead', 'admin.product_pricing.applied.tail')
   AND lang IN ('zh-CN', 'en-US');
