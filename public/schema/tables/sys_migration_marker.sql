-- table: sys_migration_marker（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase ddl
CREATE TABLE public.sys_migration_marker (
    version character varying(120) NOT NULL,
    component character varying(80) NOT NULL,
    note character varying(500),
    completed_at timestamp with time zone DEFAULT now() NOT NULL,
    completed_by bigint DEFAULT 0 NOT NULL
);

-- @phase constr
ALTER TABLE ONLY public.sys_migration_marker
    ADD CONSTRAINT sys_migration_marker_pkey PRIMARY KEY (version);

-- @phase comment
COMMENT ON TABLE public.sys_migration_marker IS '一次性数据迁移的执行台账：version 与注册项 Version 逐字一致，行存在即代表该动作已完成、永不重放';

COMMENT ON COLUMN public.sys_migration_marker.version IS '迁移版本号（主键，与 public/migrations 注册项 Version 一致）';

COMMENT ON COLUMN public.sys_migration_marker.component IS '归属主题（如 permission）';

COMMENT ON COLUMN public.sys_migration_marker.note IS '这次动作做了什么、为什么只做一次（判据写在这里，供日后审计）';

COMMENT ON COLUMN public.sys_migration_marker.completed_at IS '标记写入时间（= 一次性动作完成时间）';

COMMENT ON COLUMN public.sys_migration_marker.completed_by IS '执行者账号 id；迁移由启动过程执行，固定为 0';

-- @phase data
INSERT INTO public.sys_migration_marker VALUES
	('597-block-bulk-delete-grant-copy', 'permission', '把历史 block:delete 主体的删除能力一次性复制到 block:bulk_delete（不随后续授权变动重放；源按 v3 识别，含 canonical 与 bulk 两条历史路径）', '2026-11-15 20:36:54.853367+08', 0) ON CONFLICT DO NOTHING;
