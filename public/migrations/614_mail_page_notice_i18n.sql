-- 614：mail 后台页面级受控提示的 9 条词条（「控制器不取词」这一批新增的 Notice* key）。
--
-- 这一批把 mail 页面 handler 自己写出的句子改成「key + 中文兜底」的事实，于是新出现了 9 个 key。
-- 它们此前只存在于 Go 侧（fallback 里就有中文），**没有任何词条行** —— 中文环境完全看不出问题，
-- 切到英文才露出中文句子。enums 对账判据只认 `a.msg.*` / `a.err.*` / `a.test.*` 三种形态，
-- 所以 `mail.bulk.*` 与 `mail.page.*` 那几条连红都不会红（public/test/enums/unit 的
-- TestEnumsKeysHaveSeedEntries 只抓到 mail.msg.automationTick 一条）。9 条一起补，不留半批。
--
-- 占位符顺序必须与 Go 侧的 WithArgs 逐字一致（中文与英文的顺序可以不同，但各自的 `%s`
-- 个数与语义位置要对得上）：
--   · mail.bulk.allDone      WithArgs(动作, 数量, 名词)
--   · mail.bulk.allSkipped   WithArgs(名词, 动作, 跳过数)
--   · mail.bulk.partial      WithArgs(动作, 数量, 名词, 跳过数)
--   · mail.msg.automationTick WithArgs(补投数)
-- 写错顺序不报错、也不会有异常日志 —— 页面上只是把名词和数字对调，这类错误只能靠
-- 逐条读一遍确认（本批就是这么做的）。
--
-- 列清单与其余 i18n seed 同形：(item_key, lang, item_value, http_code, category, remark,
-- status, create_time, update_time)。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('mail.bulk.allDone', 'zh-CN', '已%s %s 个%s。', 200, 'admin', '邮件批量写操作全部成功回执（mail_err.go mailBulkAllDoneMsg）', 1, now(), now()),
('mail.bulk.allDone', 'en-US', '%s %s %s.', 200, 'admin', 'Mail bulk write: all succeeded (mailBulkAllDoneMsg)', 1, now(), now()),
('mail.bulk.allSkipped', 'zh-CN', '0 个%s被%s：%s 个被跳过（不存在或被服务端拒绝）。', 200, 'admin', '邮件批量写操作全部跳过回执（mailBulkAllSkippedMsg）', 1, now(), now()),
('mail.bulk.allSkipped', 'en-US', '0 %s %s: %s skipped (missing or rejected by the server).', 200, 'admin', 'Mail bulk write: all skipped (mailBulkAllSkippedMsg)', 1, now(), now()),
('mail.bulk.partial', 'zh-CN', '已%s %s 个%s，另有 %s 个被跳过（不存在或被服务端拒绝）。', 200, 'admin', '邮件批量写操作部分成功回执（mailBulkPartialMsg）', 1, now(), now()),
('mail.bulk.partial', 'en-US', '%s %s %s; %s more were skipped (missing or rejected by the server).', 200, 'admin', 'Mail bulk write: partially succeeded (mailBulkPartialMsg)', 1, now(), now()),
('mail.msg.automationTick', 'zh-CN', '已补投 %s 个到点实例', 200, 'admin', '手工补投一轮延时实例的回执（mailAutomationTickMsg）', 1, now(), now()),
('mail.msg.automationTick', 'en-US', 'Re-queued %s due instances.', 200, 'admin', 'Manual tick of due automation runs (mailAutomationTickMsg)', 1, now(), now()),
('mail.page.templateListFailed', 'zh-CN', '读取模板列表失败，本次没有删除任何模板。', 200, 'admin', '模板列表取数失败：本次批量删除没有执行（mailTemplateListFailedMsg）', 1, now(), now()),
('mail.page.templateListFailed', 'en-US', 'Reading the template list failed; nothing was deleted.', 200, 'admin', 'Template list load failed; bulk delete did not run (mailTemplateListFailedMsg)', 1, now(), now()),
('mail.page.contactStatusBad', 'zh-CN', '目标状态不合法，本次没有处理任何联系人。', 200, 'admin', '批量改联系人状态：目标状态不在受控取值里（mailContactStatusBadMsg）', 1, now(), now()),
('mail.page.contactStatusBad', 'en-US', 'Invalid target status; no contacts were processed.', 200, 'admin', 'Bulk contact status: target status not allowed (mailContactStatusBadMsg)', 1, now(), now()),
('mail.page.campaignIDRequired', 'zh-CN', '请先从活动列表选择一条活动，再查看它的报表。', 200, 'admin', '缺 campaignId 的引导（mailCampaignIDRequiredMsg）', 1, now(), now()),
('mail.page.campaignIDRequired', 'en-US', 'Pick a campaign from the list first, then open its report.', 200, 'admin', 'Missing campaignId guidance (mailCampaignIDRequiredMsg)', 1, now(), now()),
('mail.page.runIDRequired', 'zh-CN', '请先从实例列表选择一条实例，再查看它的排障详情。', 200, 'admin', '缺 runId 的引导（mailRunIDRequiredMsg）', 1, now(), now()),
('mail.page.runIDRequired', 'en-US', 'Pick a run from the list first, then open its troubleshooting detail.', 200, 'admin', 'Missing runId guidance (mailRunIDRequiredMsg)', 1, now(), now()),
('mail.page.automationIDRequired', 'zh-CN', '请先从流程列表选择一个流程，再查看它的画布。', 200, 'admin', '缺 automationId 的引导（mailAutomationIDRequiredMsg）', 1, now(), now()),
('mail.page.automationIDRequired', 'en-US', 'Pick an automation from the list first, then open its canvas.', 200, 'admin', 'Missing automationId guidance (mailAutomationIDRequiredMsg)', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
