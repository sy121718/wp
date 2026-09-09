-- 067 · 媒体中心（docs/02-B）在既有表上落地四个能力
--
-- 背景：sys_attachment / sys_file_category / sys_media_variant 是媒体库的真实持久化表；
-- init_schema.sql 里的 media_asset / media_asset_variant / media_reference 三张空表
-- 无任何 Go 代码读写，本轮**不启用、不改动**（另行决策）。
--
-- 本迁移只做 sys_attachment 的结构增量，对应 02-B 四能力：
--   ① 稳定引用：命名 <id>.<ext> 属 service 层行为，不加列（存量路径不动）
--   ② 上传去重：md5 + file_type 查重，补部分索引加速
--   ③ generation：换图代数 +1（供依赖记录/构建期重建判定）
--   ④ 引用缓存：extra_info 升级 JSONB，refs 数组 + GIN 索引（删除前引用保护）
--
-- 幂等：全部语句可重复执行；register.go 用「generation 列是否存在」判定，
-- 避免 sys_attachment 已存在导致默认「表存在即跳过」误跳过。

-- ---------------------------------------------------------------------------
-- 1) extra_info: json → jsonb
--
-- 转换安全性（三类兜底）：
--   · NULL：CASE 显式分支，NULL 保持 NULL，不写 '{}'，不污染存量语义；
--   · 非法 JSON 文本：json 类型在写入时已由 PG 校验语法，正常不存在；仍先跑
--     一段 DO 块逐行试转（子事务捕获），任何转不动/空串的行就地置 NULL，
--     避免一条脏数据让整个 ALTER 失败并卡住全部后续迁移；
--   · 空串/纯空白：json 列不可能出现，防御跨库导入或历史工具直写残留。
-- 列已是 jsonb 时本段为 no-op（同类型 cast 合法，DO 块按 data_type 提前返回）。
DO $$
DECLARE
    r record;
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'sys_attachment'
          AND column_name = 'extra_info'
    ) THEN
        RETURN;
    END IF;

    -- json 为生产形态；text/varchar 覆盖「历史库被误建为文本列」的导入场景，
    -- 两者都需要先清洗空串与非法 JSON 文本。
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'sys_attachment'
          AND column_name = 'extra_info'
          AND data_type IN ('json', 'text', 'character varying')
    ) THEN
        -- 空串/纯空白先归零（jsonb 无法解析空串）。
        UPDATE sys_attachment
           SET extra_info = NULL
         WHERE extra_info IS NOT NULL
           AND btrim(extra_info::text) = '';

        -- 逐行试转，转不动的行置 NULL（历史脏数据不阻塞迁移）。
        FOR r IN
            SELECT id, extra_info::text AS txt
              FROM sys_attachment
             WHERE extra_info IS NOT NULL
        LOOP
            BEGIN
                PERFORM r.txt::jsonb;
            EXCEPTION WHEN others THEN
                UPDATE sys_attachment SET extra_info = NULL WHERE id = r.id;
            END;
        END LOOP;
    END IF;
END $$;

ALTER TABLE sys_attachment
    ALTER COLUMN extra_info TYPE jsonb
    USING CASE
        WHEN extra_info IS NULL THEN NULL
        WHEN btrim(extra_info::text) = '' THEN NULL
        ELSE extra_info::jsonb
    END;

-- 2) refs 查询索引（引用保护反向查询：哪些附件被某页面引用）
--    默认 jsonb_ops 而非 jsonb_path_ops：同时支持 @>（包含）与 ?（键存在），
--    refs 是对象数组，@> 走索引即可满足「删除前查引用」与「构建期全量替换」。
CREATE INDEX IF NOT EXISTS idx_att_extra_info_gin
    ON sys_attachment USING GIN (extra_info);

-- 3) generation：换图代数。换图 +1，供依赖记录/构建期判定「该资产产物需重建」。
--    DEFAULT 1 让存量行一次性回填，NOT NULL 保证读取端无需判空。
ALTER TABLE sys_attachment
    ADD COLUMN IF NOT EXISTS generation INTEGER NOT NULL DEFAULT 1;

-- 4) 上传去重查询索引（md5 + file_type，仅启用行）
--    去重只认「启用中的附件」，软删记录不参与复用（避免复用已下架资源）。
CREATE INDEX IF NOT EXISTS idx_att_md5_type
    ON sys_attachment (md5, file_type)
    WHERE md5 IS NOT NULL AND status = 1;

COMMENT ON COLUMN sys_attachment.extra_info IS '额外信息（JSONB）：alt/title/description + refs 引用缓存数组';
COMMENT ON COLUMN sys_attachment.md5 IS '文件MD5（上传去重键，与 file_type 联合查重）';
COMMENT ON COLUMN sys_attachment.generation IS '换图代数，初始 1，每次换图 +1（触发依赖重建）';
COMMENT ON INDEX sys_attachment_pkey IS '附件主键（稳定引用标识，URL 形如 /storage/<id>.<ext>）';
