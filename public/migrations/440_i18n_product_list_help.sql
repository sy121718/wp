-- The detail page is read-only; edits to variants and ratings happen on the edit page.
-- Only the exact historical tail values inserted by 233 are replaced. Custom copy is untouched.
UPDATE sys_i18n
SET item_value = '页维护（点行的「编辑」进入）。评分是独立明细：平均值与条数由明细算出，改评分不改动商品字段；「没有评分」与「评分 0 分」是两回事。',
    update_time = now()
WHERE item_key = 'admin.products.hint.detailTail'
  AND lang = 'zh-CN'
  AND item_value = '里维护（点行的「详情」进入）。评分是独立明细：平均值与条数由明细算出，改评分不改动商品字段；「没有评分」与「评分 0 分」是两回事。';

UPDATE sys_i18n
SET item_value = ' page (open it via Edit in the row). Ratings live in their own detail table: the average and count are derived from it, and editing a rating never touches the product''s own fields. No ratings and rated 0 are different things.',
    update_time = now()
WHERE item_key = 'admin.products.hint.detailTail'
  AND lang = 'en-US'
  AND item_value = 'page (open it via Detail in the row). Ratings live in their own detail table: the average and count are derived from it, and editing a rating never touches the product''s own fields. No ratings and rated 0 are different things.';
