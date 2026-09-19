-- 278 · 页面列表页的「发布回执收敛状态」词条（中英各一行）。
--
-- 背景：page / presentation 两条发布路径都有 ConvergePendingReceipts（ticker 1 分钟 +
-- 写路径快通道，用 ClaimPendingReceipts 以 FOR UPDATE SKIP LOCKED 认领），失败的回执留在
-- pending 等下一轮。这套机制此前只有结构化日志与 /readyz 的只读字段可见，
-- 而后台页面 —— 运维每天的落点 —— 完全看不到有积压。
--
-- 本轮把状态条加到 /admin/pages（列表页是页面发布这件事的入口），文案落在这里：
-- 模板侧写 {{ .["t"]("admin.pages.receipts.<语义>", "中文兜底") }}，
-- 真文案由响应层 pkg/i18n.Translate 按请求语言查表，缺 key 时回落到模板内的中文原文。
--
-- 数值与时长不在这里：条数是计数、最老一条的等待时长由 Go 侧格式化成 SI 记法（3m20s），
-- 中英文都读得懂，也避开复数与语序问题。
--
-- 幂等：ON CONFLICT (item_key, lang) DO NOTHING —— seed 是默认值来源，后台是真相来源。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.pages.receipts.pending', 'zh-CN', '待收敛发布回执', 200, 'admin', 'admin/pages.html', 1, now(), now()),
    ('admin.pages.receipts.pending', 'en-US', 'Pending publication receipts', 200, 'admin', 'admin/pages.html', 1, now(), now()),
    ('admin.pages.receipts.oldest', 'zh-CN', '最老一条已等待', 200, 'admin', 'admin/pages.html', 1, now(), now()),
    ('admin.pages.receipts.oldest', 'en-US', 'Oldest waiting for', 200, 'admin', 'admin/pages.html', 1, now(), now()),
    ('admin.pages.receipts.last_converge', 'zh-CN', '最近一次收敛', 200, 'admin', 'admin/pages.html', 1, now(), now()),
    ('admin.pages.receipts.last_converge', 'en-US', 'Last convergence', 200, 'admin', 'admin/pages.html', 1, now(), now()),
    ('admin.pages.receipts.action', 'zh-CN', '收敛例程每分钟兜底一次；持续积压请查服务日志（scene=publication）与线上路径一致性，必要时人工核对后再处理。', 200, 'admin', 'admin/pages.html', 1, now(), now()),
    ('admin.pages.receipts.action', 'en-US', 'The converger sweeps once a minute; if the backlog persists, check the service log (scene=publication) and the consistency of live paths before acting.', 200, 'admin', 'admin/pages.html', 1, now(), now()),
    ('admin.pages.receipts.ok', 'zh-CN', '发布回执收敛正常：没有待收敛的回执。', 200, 'admin', 'admin/pages.html', 1, now(), now()),
    ('admin.pages.receipts.ok', 'en-US', 'Publication receipts converged: nothing is pending.', 200, 'admin', 'admin/pages.html', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
