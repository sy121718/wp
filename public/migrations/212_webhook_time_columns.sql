-- 212 · webhook 两张表的时间列由 bigint 秒改为 timestamptz（DB-019 尾账）。
--
-- 205 把全库 created_at / updated_at 统一改名成 create_time / update_time，
-- 但改名不动类型：webhook_endpoints / webhook_deliveries 建表时（199）用的是
-- BIGINT 存 time.Now().Unix()，改名后成了全库**唯一**的 4 个 bigint 时间列
-- （其余 187 个时间列都是 timestamptz）。类型不统一有三个实际后果：
--   · 无法用 PG 的时间运算 / 区间索引（now() - interval、BRIN、date_trunc 分组都要先转换）；
--   · 与其它表的 join / 比较必须显式转换，写错就是隐式全表扫描；
--   · 秒级精度丢掉了亚秒，投递日志按时间排序在同秒内不稳定。
--
-- 转换用 to_timestamp（epoch 秒 → timestamptz），按列注释走显式 USING：
-- 隐式 cast 在 ALTER TYPE 上不成立，不写 USING 会直接报错。
-- 存量行数为 0（webhook 尚未接线），本迁移实际只改类型；USING 仍写全，保证有数据的库也能跑。
--
-- 幂等：判定按「四列都已是 timestamptz」；DO 块内再按列类型各判一次，
-- 避免「之一已改、重放时对 timestamptz 再调一次 to_timestamp」的报错。

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = current_schema() AND table_name = 'webhook_endpoints'
                 AND column_name = 'create_time' AND data_type = 'bigint') THEN
        ALTER TABLE webhook_endpoints
            ALTER COLUMN create_time TYPE timestamptz USING to_timestamp(create_time),
            ALTER COLUMN update_time TYPE timestamptz USING to_timestamp(update_time);
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = current_schema() AND table_name = 'webhook_deliveries'
                 AND column_name = 'create_time' AND data_type = 'bigint') THEN
        ALTER TABLE webhook_deliveries
            ALTER COLUMN create_time TYPE timestamptz USING to_timestamp(create_time),
            ALTER COLUMN update_time TYPE timestamptz USING to_timestamp(update_time);
    END IF;
END $$;
