-- 715 · media 三条错误词条与状态码对齐
--
-- 背景：`internal/module/media/enums/media_enums.go` 的 StatusOf 现在把存储 / IO 失败
--   声明成 500（用户改输入改不了，是「我们这边出错」）。但词库里 ErrUploadFailed /
--   ErrDownloadFailed 的 http_code 还停在 400 —— 那是 058 / 430 批随手填的，不是判据。
--   声明与数据不能长期分叉：状态码的**事实源**是 Go 声明，词库这份是可观测副本，
--   这里把它们校正到 500。只改 http_code，**不动任何文案**（运营改过的词条不能被覆盖）。
--
-- 另补一条从未有过词条的 key：ErrReplaceStaleNotifyFailed。
--   它在 media.go 的两条路径上都会外泄 —— `:564`（上传内容与原文件相同、跳过换图后
--   通知引用方）与 `:629`（换图成功之后通知引用方）。两处都是「换图已生效、但失效
--   通知没发出去」，属补偿留痕失败：调用方要重试的是重建，不是重传文件，所以是 500。
--   注意它**不是**「只在日志里」——notifyRefsStale 返回 firstErr，service 直接把它当
--   失败返回给 handler。缺词条时页面只能看到归口文案，运维看不出是哪一步没成。
--
-- 幂等：UPDATE 带旧值条件（运营若已改过 http_code 则不再覆盖）；
--        INSERT ... ON CONFLICT (item_key, lang) DO NOTHING。
-- 判据见 register_media_error_codes_i18n.go。

UPDATE sys_i18n
SET http_code = 500, update_time = now()
WHERE item_key IN ('ErrUploadFailed', 'ErrDownloadFailed')
  AND lang IN ('zh-CN', 'en-US')
  AND http_code = 400;

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('ErrReplaceStaleNotifyFailed', 'zh-CN', '换图已生效，但通知引用方重建失败，请手动重建引用了该附件的页面', 500, 'media', 'internal/module/media/enums/media_enums.go: ErrReplaceStaleNotifyFailed 此前无任何语言的词条；会在换图成功后通知失败路径返回（media.go:564 / :629）', 1, now(), now()),
('ErrReplaceStaleNotifyFailed', 'en-US', 'The file was replaced, but notifying the referencing pages failed. Rebuild the pages that reference this file.', 500, 'media', 'internal/module/media/enums/media_enums.go: ErrReplaceStaleNotifyFailed had no i18n entry in any language; returned after a successful replace when notifyRefsStale fails (media.go:564 / :629)', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
