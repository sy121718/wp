-- 048_media_variant.sql — 媒体图片变体（thumb / medium / webp）
--
-- sys_media_variant 记录每张图片附件的衍生版本：
--   thumb  = imaging.Fit 320x320 Lanczos → webp（缩略图）
--   medium = imaging.Fit 1280x1280 Lanczos → webp（调节尺寸版）
--   webp   = 原图尺寸重编码 webp（全尺寸 webp 版）
-- 原图恒保留在 sys_attachment，变体只增不改原图。
-- status 取值：pending / processing / ready / failed（varchar，服务端状态机）。
-- UNIQUE(attachment_id, variant_type)：同附件同类型仅一条，重新生成时先删后插。
--
-- 同迁移内补 media 下载/变体接口的权限点 seed（照 030_business_permissions.sql 写法），
-- 缺失时非超管访问 /api/media/download* 会被 Casbin 拒绝（403）。
--
-- 幂等：DDL 全部 IF NOT EXISTS；权限点 INSERT 带 NOT EXISTS 守卫，重复执行不报错。

-- 1. 变体表
CREATE TABLE IF NOT EXISTS sys_media_variant (
    id            BIGSERIAL    PRIMARY KEY,
    attachment_id BIGINT       NOT NULL,
    variant_type  VARCHAR(20)  NOT NULL,
    file_path     VARCHAR(500) NOT NULL,
    width         INT,
    height        INT,
    file_size     BIGINT       NOT NULL DEFAULT 0,
    mime_type     VARCHAR(100),
    status        VARCHAR(20)  NOT NULL DEFAULT 'pending',
    create_time   TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP,
    update_time   TIMESTAMP,
    CONSTRAINT fk_media_variant_attachment FOREIGN KEY (attachment_id) REFERENCES sys_attachment(id) ON UPDATE CASCADE ON DELETE CASCADE,
    CONSTRAINT uq_media_variant_attachment_type UNIQUE (attachment_id, variant_type)
);
CREATE INDEX IF NOT EXISTS idx_mva_attachment ON sys_media_variant(attachment_id);
CREATE INDEX IF NOT EXISTS idx_mva_status ON sys_media_variant(status);

COMMENT ON TABLE sys_media_variant IS '媒体图片变体表（缩略图/调节尺寸版/全尺寸webp）';
COMMENT ON COLUMN sys_media_variant.attachment_id IS '所属附件ID（物理级联删除）';
COMMENT ON COLUMN sys_media_variant.variant_type IS '变体类型：thumb/medium/webp';
COMMENT ON COLUMN sys_media_variant.file_path IS '变体文件存储相对路径（local storage key）';
COMMENT ON COLUMN sys_media_variant.width IS '变体宽度（像素）';
COMMENT ON COLUMN sys_media_variant.height IS '变体高度（像素）';
COMMENT ON COLUMN sys_media_variant.file_size IS '变体文件大小（字节）';
COMMENT ON COLUMN sys_media_variant.mime_type IS '变体 MIME 类型';
COMMENT ON COLUMN sys_media_variant.status IS '生成状态：pending/processing/ready/failed';
COMMENT ON COLUMN sys_media_variant.create_time IS '创建时间';
COMMENT ON COLUMN sys_media_variant.update_time IS '更新时间';

-- 2. 权限点 seed（media 下载 / 变体接口；照 030 的 NOT EXISTS 守卫写法）
INSERT INTO sys_permission (permission_code, permission_name, module, api_path, api_method, status, create_by, create_time, update_by, update_time)
SELECT v.code, v.name, v.module, v.path, v.method, 1, 0, NOW(), 0, NOW()
FROM (VALUES
    ('media:download',          '下载媒体资源包',   'media', '/api/media/download',          'GET'),
    ('media:download_batch',    '批量下载媒体',     'media', '/api/media/download/batch',    'GET'),
    ('media:variants_generate', '重新生成媒体变体', 'media', '/api/media/variants/generate', 'POST')
) AS v(code, name, module, path, method)
WHERE NOT EXISTS (SELECT 1 FROM sys_permission x WHERE x.permission_code = v.code);
