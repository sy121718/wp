-- 277 · 批量 id 超限的受控提示词条（shell.err.bulkIdsTooMany，中英各一行）。
--
-- 背景：shell.BulkIDs 的超限错误此前是 fmt.Errorf 拼出来的字符串 ——「这条错误是受控的」
-- 只能写在注释里。于是 admin / navigation / inventory 三处直传 err.Error() 只能靠门禁豁免放行，
-- 而 mail / order / user 三处各自用 shell.MaxBulkIDs 重组了同一句话（副作用是丢掉「当前 N 项」：
-- 去重后的条数只有 shell 知道，在模块里重算就是第二份真相）。
--
-- 现在错误是**带 sentinel 的类型**（shell.ErrBulkIDsTooMany / *shell.BulkIDsError，值域只有
-- Count/Max 两个整数），对外文案统一走 shell.BulkIDsFacingText —— 本词条就是那个出口的文案：
-- 两个 %s 依次是「上限」与「本次条数」，与 pkg/i18n.HasStringPlaceholdersOnly 的约定一致
--（只允许 %s，数字在 Go 侧先转字符串）。
--
-- http_code 用 400：超限是**请求自身的问题**（参数校验），不是服务端故障。
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('shell.err.bulkIdsTooMany', 'zh-CN', '一次最多操作 %s 项，当前 %s 项，请分批进行', 400, 'error', 'internal/web/shell/bulk.go', 1, now(), now()),
    ('shell.err.bulkIdsTooMany', 'en-US', 'At most %s items can be operated at once; %s selected, please split into batches.', 400, 'error', 'internal/web/shell/bulk.go', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
