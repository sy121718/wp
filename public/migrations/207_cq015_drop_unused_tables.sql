-- 207 · CQ-015：删除两张零消费方表。
--
-- 决策（YG 拍板）：删，不留。
--
--   presentation_artifact_objects：presentation 侧的内容对象闭包。page 侧的孪生表
--     （page_artifact_objects）有完整读写方——产物记录时写、GC 时查闭包；这张表
--     两样都没有，GC 的孤儿判定只查 page 侧。presentation 侧归档尚未落地，
--     等它落地时按 peer 表重建的成本远低于留一张无人维护、却会被误读成
--     「已经在跑」的表。
--   publication_events：发布事件审计。发布事实的真源是 publication_receipts
--     （有消费方、有 unique 约束），这张表从建起就没有写入方。
--
-- 迁移 204 给这两张表的外键补过 ON DELETE CASCADE，删表时一并消失；
-- 204 的注册判定已同步改成 to_regclass 形式，否则表删掉后它在下次启动求值时
-- 会因 'presentation_artifact_objects'::regclass 解析失败而报错。
--
-- 幂等：DROP TABLE IF EXISTS。

DROP TABLE IF EXISTS presentation_artifact_objects;
DROP TABLE IF EXISTS publication_events;
