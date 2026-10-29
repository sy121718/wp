-- 商品定价与变体整批写入的提示；新键避免历史种子恢复旧语义。
-- 幂等补齐缺失语言，保留运营已编辑的文案。
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.product_pricing.apply.atomicHint', 'zh-CN', '规则参数只取当前规则类型用得上的那一格：成本乘倍数填 multiplier，成本加价与统一售价填 amount，目标毛利率填 margin（0.3 表示 30%）。筛选集范围必须指定工程（页面顶部切换），空筛选不会全表改价；单批命中商品数有上限，超过请收紧条件。任何变体缺少成本价或算价越界时，整批应用都会被拒绝；请先在预览中查看并修正这些条目。', 200, 'admin', 'product atomic writes', 1, now(), now()),
    ('admin.product_pricing.apply.atomicHint', 'en-US', 'Only the field used by the selected rule is read: multiplier for cost multiplied by a factor, amount for cost plus markup or a fixed price, and margin for target gross margin (0.3 means 30%). Filter scope requires a project and at least one condition. Narrow the filters if the batch exceeds the product limit. If any variant lacks a required cost or produces an out-of-range price, the entire application is rejected. Review and fix those items in the preview first.', 200, 'admin', 'product atomic writes', 1, now(), now()),
    ('ErrPricingCostMissing', 'zh-CN', '未填成本价，按成本类规则无法计算', 409, 'product', 'product atomic writes', 1, now(), now()),
    ('ErrPricingCostMissing', 'en-US', 'A cost price is required for this pricing rule.', 409, 'product', 'product atomic writes', 1, now(), now()),
    ('ErrPricingOutOfRange', 'zh-CN', '计算结果为负或超过 9999999999.99', 409, 'product', 'product atomic writes', 1, now(), now()),
    ('ErrPricingOutOfRange', 'en-US', 'The calculated price is negative or exceeds 9999999999.99.', 409, 'product', 'product atomic writes', 1, now(), now()),
    ('ErrPricingTargetChanged', 'zh-CN', '定价范围已发生变化，请重新预览后再应用', 409, 'product', 'product atomic writes', 1, now(), now()),
    ('ErrPricingTargetChanged', 'en-US', 'The pricing scope has changed. Preview the current selection again before applying prices.', 409, 'product', 'product atomic writes', 1, now(), now()),
    ('ErrVariantListDuplicated', 'zh-CN', '变体清单包含重复规格，请移除重复行后重新保存', 409, 'product', 'product atomic writes', 1, now(), now()),
    ('ErrVariantListDuplicated', 'en-US', 'The variant list contains duplicate specifications. Remove the duplicate rows and save again.', 409, 'product', 'product atomic writes', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
