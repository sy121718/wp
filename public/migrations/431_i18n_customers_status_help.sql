-- 431 · 客户列表的词条（状态列解释 3 key + 筛选态空态 2 key，共 5 key × 2 语言）
--
-- 背景（docs/02-O-trade-site-audit.md §4 customers 任务 4，P2）：
--   customers.html 原先在**每个待激活 / 锁定 / 有失败次数的行下方**插一整行
--   `<tr><td colspan="7" class="hint">整句说明</td></tr>`（文案源 customer_view.go 的
--   PendingHint / LockHint，Go 侧硬编码中文）。两处问题：
--     ① 同一句话随行数重复 —— 多个待激活账号时页面被切成一段段正文，说明本该进 .help
--        （02-J §2.1「说明不属于正文」）；
--     ② 硬编码中文过不了 i18n：英文界面这两句原样露中文。
--
--   本批把说明移到**状态列表头的 .help** 里（一次渲染、不随行重复、不占正文），
--   文案随之 key 化 —— 模板写 t(key, 兜底)，库值存在时以库为准。
--   为什么挂表头而不是行内角标：`.table-scroll` 是 overflow 容器，会裁掉 td 内绝对定位的
--   悬浮层（表头在容器顶部，向下展开有空间）—— 这一点在模板注释里也写了一遍。
--
-- 第二组（`empty_filtered_*`）来自 docs/02-V-decision-brief.md §3「顺带发现」与 §8 结论表第 3 行：
--   筛选态空态用的这两个 key **在 sys_i18n 里根本不存在**（全仓 .sql / .go 也没有 seed）——
--   模板在用时取的是自己的中文兜底，于是**英文界面下筛选态空态回落中文**。
--   该简报给的方案 A 就是本条：仿 417 的形态补中英各一行，代价 S、无既有测试被破坏
--   （public/test/user/feature 的 TestCustomersEmptyStateActionsFollowFilter 断言的是
--   `class="empty-actions"` 与中文串，与这两个 key 的存在与否无关）。
--
-- 与模板兜底的关系：这 5 个 key 的 zh-CN 值与 internal/templates/admin/user/customers.html
--   里写的兜底**逐字一致**（empty_filtered_* 是唯一例外：模板那两处是同一句话，
--   SQL 里照抄）。库值一旦与兜底不同，页面显示的就永远是库值（库值覆盖兜底，
--   这正是本域 417 修掉的那类缺陷）—— 改文案时两边要一起改。
--
-- 幂等：新增走 INSERT ... ON CONFLICT (item_key, lang) DO NOTHING（sys_i18n 主键是
--   (item_key, lang)），重复执行影响 0 行。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.customers.status.help.pending', 'zh-CN', '待激活：客户还没完成邮箱验证，这类账号本来就登不上去 —— 验证完邮箱后状态会变成「正常」，那时再决定是否停用。', 200, 'admin', 'admin/user/customers.html: 状态列表头 .help —— 待激活', 1, now(), now()),
    ('admin.customers.status.help.pending', 'en-US', 'Pending activation: the customer has not verified their email yet — such accounts cannot sign in anyway; once the email is verified the status becomes Active, and you can decide then whether to disable it.', 200, 'admin', 'admin/user/customers.html: 状态列表头 .help —— 待激活', 1, now(), now()),
    ('admin.customers.status.help.locked', 'zh-CN', '已锁定：因连续登录失败被临时锁定，到点会自动解除；确认是本人操作时点「解除锁定」，不必等。', 200, 'admin', 'admin/user/customers.html: 状态列表头 .help —— 已锁定', 1, now(), now()),
    ('admin.customers.status.help.locked', 'en-US', 'Locked: temporarily locked after repeated failed sign-ins; it lifts by itself when the time is up. If you are sure it is the account owner, click Unlock so they do not have to wait.', 200, 'admin', 'admin/user/customers.html: 状态列表头 .help —— 已锁定', 1, now(), now()),
    ('admin.customers.status.help.failures', 'zh-CN', '失败次数未清零（状态仍是「正常」）：再失败几次就会进入锁定。', 200, 'admin', 'admin/user/customers.html: 状态列表头 .help —— 失败次数未清零', 1, now(), now()),
    ('admin.customers.status.help.failures', 'en-US', 'Failed attempts not reset (the status is still Active): a few more failures will lock the account.', 200, 'admin', 'admin/user/customers.html: 状态列表头 .help —— 失败次数未清零', 1, now(), now()),
    ('admin.customers.list.empty_filtered_heading', 'zh-CN', '该筛选条件下暂时没有账号', 200, 'admin', 'admin/user/customers.html: 筛选态空态标题（原先库中无此 key，英文界面回落中文兜底）', 1, now(), now()),
    ('admin.customers.list.empty_filtered_heading', 'en-US', 'Nothing here under the current filters', 200, 'admin', 'admin/user/customers.html: 筛选态空态标题（原先库中无此 key，英文界面回落中文兜底）', 1, now(), now()),
    ('admin.customers.list.empty_filtered_desc', 'zh-CN', '徽章上的数字是各口径的总数（不受筛选影响），点进来为空说明这个口径下确实没有账号。换个徽章，或点「重置」回到全部账号。', 200, 'admin', 'admin/user/customers.html: 筛选态空态说明（原先库中无此 key，英文界面回落中文兜底）', 1, now(), now()),
    ('admin.customers.list.empty_filtered_desc', 'en-US', 'The badge counters are per-dimension totals and are not affected by the filters below: if this list is empty, no account matches that dimension. Pick another badge, or click Reset to go back to every account.', 200, 'admin', 'admin/user/customers.html: 筛选态空态说明（原先库中无此 key，英文界面回落中文兜底）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;

