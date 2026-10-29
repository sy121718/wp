-- 676 · blocks 并发版本与预设来源（业务区块解耦 B2）
--
-- 背景：B1 契约（public/docs/业务区块解耦-B1契约.md §3）要求 block 具备三样东西，
--   而 `\d blocks` 实测三样全无 —— 表里只有 project_id / name / kind / document /
--   create_time / update_time / category / reuse_mode / id。
--
-- 为什么要 version 列（乐观并发）：
--   UpdateDocument 现在是**覆盖式整树保存**（block_model.go）。两个管理员同时编辑同一
--   全局块时，后保存的静默覆盖前一个 —— 而全局块被多个页面引用，被覆盖的可能是
--   「已经发布过、正在线上跑」的那份结构。这类丢失不会报错，只在页面上表现为
--   「我改的东西又回去了」。
--   加 version 后保存必须带 expectedVersion，不匹配回 409 并把当前版本号给前端。
--
-- 为什么 version 不等于配置协议版本：
--   schemaVersion（文档根）表达「字段含义变没变」，未知版本要拒绝渲染；
--   version（本列）只防丢更新，与文档内容无关。拿 schemaVersion 当并发控制的话，
--   协议升一次版本会让全库文档同时"过期"，所有编辑器一起报冲突。
--
-- 为什么预设来源要单独两列而不是塞进 document：
--   document 是**用户可编辑的整树**，编辑器整树覆盖保存 —— 把"我来自哪个预设"写进
--   document 里，它迟早被编辑器覆盖掉。预设来源是**服务端记账**，不能被用户编辑触碰。
--
-- 回填：存量行一律 version = 1（默认值即完成回填，不需要单独的数据迁移）。
--   语义上「从这一刻起这份文档是第 1 版」是准确的：此前的编辑没有版本可追溯，
--   不能假装它们有。
--
-- 幂等：ADD COLUMN IF NOT EXISTS + 约束/索引各自判定（与 652 同形）。

ALTER TABLE blocks
    ADD COLUMN IF NOT EXISTS version bigint NOT NULL DEFAULT 1;

-- preset_key / preset_version 可空：手工创建的块没有预设来源。
-- 不给默认值 —— 默认空串会让「来自哪个预设」和「不是来自预设」无法区分。
ALTER TABLE blocks
    ADD COLUMN IF NOT EXISTS preset_key text;

ALTER TABLE blocks
    ADD COLUMN IF NOT EXISTS preset_version integer;

DO $$
BEGIN
    -- version 必须 >= 1：0 或负数会让「expectedVersion = 0 表示不校验」这类
    -- 约定有机可乘，而"不校验"正是这次要消灭的形态。
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = to_regclass('blocks') AND conname = 'ck_blocks_version_positive'
    ) THEN
        ALTER TABLE blocks ADD CONSTRAINT ck_blocks_version_positive
            CHECK (version >= 1);
    END IF;

    -- preset_key 与 preset_version 同时有或同时无：只有一半的来源信息是脏数据，
    -- 会让「要不要显示来源」这条判据在模板里分叉成两种情况。
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = to_regclass('blocks') AND conname = 'ck_blocks_preset_pair'
    ) THEN
        ALTER TABLE blocks ADD CONSTRAINT ck_blocks_preset_pair
            CHECK ((preset_key IS NULL) = (preset_version IS NULL));
    END IF;
END $$;

COMMENT ON COLUMN blocks.version IS
    '乐观并发版本（B2 迁移 676）：保存必须带 expectedVersion，不匹配回 409。与文档根的 schemaVersion 是两件事';
COMMENT ON COLUMN blocks.preset_key IS
    '本块由哪个内置预设复制而来（B2 迁移 676）。用户不可编辑：预设来源是服务端记账，不写进 document';
COMMENT ON COLUMN blocks.preset_version IS
    '预设的版本号（外观/默认结构变了才涨），与组件协议版本分开（B2 迁移 676）';
