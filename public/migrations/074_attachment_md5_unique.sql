-- ========================================
-- go_wp — 媒体上传去重补数据库层唯一约束
--
-- 背景（全项目审查发现）：上传去重是 check-then-act —— 先 GetByMD5AndType 查
-- 是否已有同内容附件，没有再 Create。sys_attachment 此前只有主键索引，约束不在
-- 数据库层，并发上传同一份文件时两次请求都能通过查重判断，各写一行。
--
-- 补部分唯一索引（只约束「启用中」的行）：软删（status=0）后同名文件可重新上传。
-- 注册：public/migrations/register.go（Migration 074-attachment-md5-unique）。
-- ========================================
CREATE UNIQUE INDEX IF NOT EXISTS uq_attachment_md5_type_active
    ON sys_attachment (md5, file_type)
    WHERE status = 1 AND md5 IS NOT NULL;
