-- 305 · DB-02：产物表上与 manifest 同字节的重复 manifest 列收敛。
--
-- 判定（读 build 链路的写入与消费点，两边逐字一致）：
--   1) build_input_manifest 与 manifest 收到的是**同一份字节**：
--      presentation 侧 recordArtifactTx 用同一个 manifestJSON 平铺写两列；
--      page 侧经 artifact.Record / EnsureRecord，两列同取 req.Manifest
--      （page_publish_artifact.go 传的就是 json.Marshal(art.Manifest)）。
--   2) 那两份字节是 pipeline.Manifest —— **输出清单**：含 canonicalPath / files /
--      dependencies / diagnostics，就是 NewArtifact 写进产物目录 manifest.json
--      并参与产物 hash 的那一份。
--   3) 一个真正的「输入清单」在代码里并不存在：pipeline.BuildInput 是内存结构
--      （PageID/Lang/Path/DocJSON/Usage/Diagnostics），从未序列化落库；
--      输入侧的事实由 source_document（page）/ document_snapshots.document
--      （presentation）/ source_hash / build_input_hash 各自承载。
--   4) 消费点盘点（全仓 grep）：manifest 被读 —— artifact.toResp 取 canonicalPath、
--      page.persistDependenciesFromManifest 反序列化依赖；build_input_manifest
--      **零读点**（Go 与 SQL 都没有 SELECT 它的地方）。
-- 结论：两份确为同义 → 合并为一个真源，保留 manifest（唯一有读者的输出清单）。
--
-- 本迁移做两件事，粒度不同、理由同一：
--   A. presentation_artifacts：**删列**。该表引用这一列的文件都在本票文件域内
--      （model / service / public/test/presentation），可以一次收口。
--   B. page_artifacts：**保留列、只放开 NOT NULL**。页侧同一冗余确实存在，但删列要同步
--      5 个 raw INSERT 的列清单，其中 3 个（public/test/page、public/test/block、
--      public/test/rls）不在本票文件域；且页侧 source_document + manifest 是回滚与
--      缺文件重建的恢复边界，审计落地方案已把 page_artifacts 归入「必要不可变快照，保留」。
--      放开 NOT NULL 后写入侧不再写这一列（新行 NULL），存量行的字节原样保留
--      —— 不删历史数据、不回填、不重建任何产物。
--
-- 为什么不是「两个列都留着只改注释」：同字节的第二份 JSONB 随行数线性放大存储与
-- TOAST，而它没有任何读取者 —— 留着的唯一效果是让每个产物行多付一份字节。
--
-- 不改 Manifest 本身：产物 hash = f(manifestJSON + indexHTML)，本次只动存储侧的
-- 重复列与列表查询的列投影，Manifest 的 JSON 内容与序列化结果逐字节不变。
--
-- 幂等：DROP COLUMN IF EXISTS；DROP NOT NULL 本身对已 nullable 的列是空操作；
-- COMMENT 只在本表列必定存在的库上执行（init_builder_schema 建列，无任何迁移删它）。

ALTER TABLE presentation_artifacts DROP COLUMN IF EXISTS build_input_manifest;

ALTER TABLE page_artifacts ALTER COLUMN build_input_manifest DROP NOT NULL;

COMMENT ON COLUMN page_artifacts.build_input_manifest IS
    'DB-02 遗留列：历史上被写入与 manifest 同字节的输出清单，零读取者。305 起写入侧不再写它（新行 NULL），存量字节保留待后续迁移删除；读取清单请用 manifest 列。';
