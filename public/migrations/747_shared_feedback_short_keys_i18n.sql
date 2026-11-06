-- 747 · 共享反馈六组词条 旧 key → 新 key 的安全搬迁（含 brandNoneSel 默认补种）
--
-- 唯一执行路线：
--   · 本 SQL 同时被「正式迁移 747」与「现行 seed 738」复用；
--   · 完整 Run→RunSeeds 时，747 跑在全部 Seeds 之前，upgrade 先把旧数据原位搬完；
--   · 单独调用 RunSeeds 时，前序 seed 的现行 SQL 都不复制、不默认播种这六组，
--     旧值原样存续到 738，再由同一段安全 SQL 原位更名（保留原 id 与全部元数据），
--     因此不存在「前序 seed 先复制出目标行、738 再冲突」的部分变更路径。
--
-- 安全边界：
--   1) 只按 keys 里的六组旧→新键搬迁，不碰其它 key；
--   2) 旧行原位改名（UPDATE item_key），保留 id、全部语言、运营译文、状态与时间等元数据；
--   3) 目标 (item_key, lang) 已存在时先整批检查完整元数据是否完全一致：
--        - 完全一致：删除旧重复行；
--        - 任一处不同：整条语句报可定位的 `old -> new/lang` 冲突并回滚，绝不覆盖/加后缀/静默选择；
--      比较 item_value/http_code/category/remark/status 与 create_time/update_time，
--      只排除 id（两个来源天然不同，不代表语义冲突）；时间不同同样拒绝，避免去重时丢掉历史。
--   4) 只有「旧、新都不存在」的目标语言才补默认，且放在搬迁之后，避免先种默认制造冲突。
WITH keys(old_key, new_key) AS (VALUES
    ('MsgInternalError', 'uiInternalErr'),
    ('ErrInvalidParams', 'sysErrBadParam'),
    ('ErrPermissionDenied', 'sysErrNoPerm'),
    ('ErrRequestEntityTooLarge', 'sysErrTooLarge'),
    ('msg_operation_success', 'uiOpSuccess'),
    ('msg_delete_confirm', 'uiDelConfirm')
), guard AS MATERIALIZED (
    SELECT CASE WHEN COUNT(*) = 0 THEN 1
        ELSE ('共享反馈词条冲突: ' || string_agg(
            k.old_key || ' -> ' || k.new_key || '/' || s.lang, ', ' ORDER BY k.old_key, s.lang
        ))::integer END AS ok
    FROM keys k
    JOIN sys_i18n s ON s.item_key = k.old_key
    JOIN sys_i18n t ON t.item_key = k.new_key AND t.lang = s.lang
    WHERE ROW(t.item_value, t.http_code, t.category, t.remark, t.status, t.create_time, t.update_time)
        IS DISTINCT FROM ROW(s.item_value, s.http_code, s.category, s.remark, s.status, s.create_time, s.update_time)
), moved AS (
    UPDATE sys_i18n s SET item_key = k.new_key
    FROM keys k, guard g
    WHERE g.ok = 1 AND s.item_key = k.old_key
      AND NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = k.new_key AND t.lang = s.lang)
    RETURNING s.id
), deduped AS (
    DELETE FROM sys_i18n s USING keys k, guard g
    WHERE g.ok = 1 AND s.item_key = k.old_key
      AND EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = k.new_key AND t.lang = s.lang)
    RETURNING s.id
)
INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
SELECT d.item_key, d.lang, d.item_value, d.http_code, d.category, d.remark, d.status, now(), now()
FROM (VALUES
    ('uiInternalErr', 'zh-CN', '系统内部错误，请稍后重试', 200, 'ui', 'internal/module/block/enums/block_enums.go', 1),
    ('uiInternalErr', 'en-US', 'Internal error, please try again later', 200, 'ui', 'shell.PageInternalText 的页面级归口文案', 1),
    ('sysErrBadParam', 'zh-CN', '请求参数错误', 400, 'error', 'pkg/enums/errors.go: ErrInvalidParams', 1),
    ('sysErrBadParam', 'en-US', 'Invalid request parameters', 400, 'error', 'pkg/enums/errors.go: ErrInvalidParams', 1),
    ('sysErrNoPerm', 'zh-CN', '无权限访问', 403, 'error', 'pkg/enums/errors.go: ErrPermissionDenied', 1),
    ('sysErrNoPerm', 'en-US', 'Permission denied', 403, 'error', 'pkg/enums/errors.go: ErrPermissionDenied', 1),
    ('sysErrTooLarge', 'zh-CN', '请求体过大', 413, 'error', 'pkg/enums/errors.go: ErrRequestEntityTooLarge', 1),
    ('sysErrTooLarge', 'en-US', 'Request entity too large', 413, 'error', 'pkg/enums/errors.go: ErrRequestEntityTooLarge', 1),
    ('uiOpSuccess', 'zh-CN', '操作成功', 200, 'ui', 'admin/partials/write_success.html', 1),
    ('uiOpSuccess', 'en-US', 'Operation successful', 200, 'ui', 'admin/partials/write_success.html', 1),
    ('uiDelConfirm', 'zh-CN', '确定要删除吗？', 200, 'ui', 'admin/system/i18n.html', 1),
    ('uiDelConfirm', 'en-US', 'Are you sure to delete?', 200, 'ui', 'admin/system/i18n.html', 1),
    ('brandNoneSel', 'zh-CN', '没有勾选任何品牌，列表未改动。', 400, 'product', 'admin/product/product_brands.html: 批量条空选提示', 1),
    ('brandNoneSel', 'en-US', 'No brands selected; the list is unchanged.', 400, 'product', 'admin/product/product_brands.html: 批量条空选提示', 1)
) AS d(item_key, lang, item_value, http_code, category, remark, status)
CROSS JOIN guard g
WHERE g.ok = 1
  AND NOT EXISTS (SELECT 1 FROM sys_i18n t WHERE t.item_key = d.item_key AND t.lang = d.lang)
  AND NOT EXISTS (
      SELECT 1 FROM keys k JOIN sys_i18n s ON s.item_key = k.old_key AND s.lang = d.lang
      WHERE k.new_key = d.item_key
  )
ON CONFLICT (item_key, lang) DO NOTHING;
