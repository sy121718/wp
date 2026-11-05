-- 评论审核结果完整句：数量由具名参数承载；旧点分碎片来源退役。
WITH keys(old_key, new_key, retire) AS (VALUES
    ('admin.comment.done.approved', 'cmtDoneApprove', true),
    ('admin.comment.done.rejected', 'cmtDoneReject', true)
), guard AS MATERIALIZED (
    SELECT CASE WHEN COUNT(*) = 0 THEN 1
        ELSE ('评论审核句冲突: ' || string_agg(k.old_key || ' -> ' || k.new_key || '/' || s.lang, ', '))::integer END AS ok
    FROM keys k
    JOIN sys_i18n s ON s.item_key = k.old_key
    JOIN sys_i18n t ON t.item_key = k.new_key AND t.lang = s.lang
    WHERE ROW(t.item_value, t.http_code, t.category, t.remark, t.status)
        IS DISTINCT FROM ROW(s.item_value, s.http_code, s.category, s.remark, s.status)
), moved AS (
    UPDATE sys_i18n s SET item_key = k.new_key
    FROM keys k, guard g
    WHERE g.ok = 1 AND s.item_key = k.old_key
      AND NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = k.new_key AND t.lang = s.lang)
    RETURNING s.id
), retired AS (
    DELETE FROM sys_i18n s USING keys k, guard g
    WHERE g.ok = 1 AND s.item_key = k.old_key
      AND EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = k.new_key AND t.lang = s.lang)
    RETURNING s.id
)
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
SELECT d.item_key, d.lang, d.item_value, d.http_code, d.category, d.remark, d.status, d.create_time, d.update_time
FROM (VALUES
    ('cmtDoneApprove', 'zh-CN', '已通过 {count} 条评论。', 200, 'comment', 'admin/comment/comment_review_result.html', 1, now(), now()),
    ('cmtDoneApprove', 'en-US', 'Approved {count} comments.', 200, 'comment', 'admin/comment/comment_review_result.html', 1, now(), now()),
    ('cmtDoneReject', 'zh-CN', '已驳回 {count} 条评论。', 200, 'comment', 'admin/comment/comment_review_result.html', 1, now(), now()),
    ('cmtDoneReject', 'en-US', 'Rejected {count} comments.', 200, 'comment', 'admin/comment/comment_review_result.html', 1, now(), now())
) AS d(item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
LEFT JOIN keys k ON k.new_key = d.item_key CROSS JOIN guard g
WHERE g.ok = 1
  AND NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = d.item_key AND t.lang = d.lang)
  AND NOT EXISTS (SELECT 1 FROM sys_i18n s WHERE s.item_key = k.old_key AND s.lang = d.lang);
