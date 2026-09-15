-- 172_time_columns_to_timestamptz.sql
--
-- 审计 DB-018：全库时间列类型五五对开（93 列 timestamp without time zone 对 91 列 timestamptz）。
-- 无时区列存的是**墙钟**，「这是几点」取决于读它的会话时区 —— 跨表比较、
-- 换服务器时区、导出到别的系统，都可能解读出不同的时刻。统一到 timestamptz 之后，
-- 存的是绝对时刻，怎么读都不会变。
--
-- 历史数据的时区解释（这是本迁移唯一有风险的地方）：
--   实测本库现存的时间值都是「SQL 写入的本地墙钟」—— 种子与迁移里的 now() / CURRENT_TIMESTAMP
--   按会话时区（Asia/Shanghai）求值，写入无时区列后就是本地墙钟。
--   因此统一按**会话时区**解释：USING <列> AT TIME ZONE current_setting('TimeZone')。
--   用 current_setting 而不是写死 'Asia/Shanghai'：写死会让「换部署时区」这件事本身出错，
--   而这里要表达的正是「原值是按当时那个会话时区写下的墙钟」。
--
-- 用 DO 块遍历而不是逐列罗列：93 列分布在 36 张表，手写清单漏一列不会有任何提示，
-- 而漏掉的那列会一直保持无时区 —— 正是这条审计要消除的状态。
-- 幂等：ALTER COLUMN TYPE 对已是 timestamptz 的列是空操作；迁移器另有 CheckSQL 兜底。

DO $$
DECLARE
    r RECORD;
BEGIN
    FOR r IN
        SELECT table_name, column_name
        FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND data_type = 'timestamp without time zone'
        ORDER BY table_name, column_name
    LOOP
        EXECUTE format(
            'ALTER TABLE %I ALTER COLUMN %I TYPE timestamptz USING %I AT TIME ZONE current_setting(''TimeZone'')',
            r.table_name, r.column_name, r.column_name);
        EXECUTE format(
            'COMMENT ON COLUMN %I.%I IS ''审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释''',
            r.table_name, r.column_name);
    END LOOP;
END $$;
