-- 204 · 内容对象闭包外键补 ON DELETE CASCADE（修「孤儿内容对象永远删不掉」）。
--
-- 现象：内容对象 GC 判定某条 content_objects 只被 payload_state='deleted' 的产物引用 → 选中它，
-- 但 page_artifact_objects 里的闭包行**还在**，而外键没有级联 → DELETE 被 23503 挡下；
-- GC 把失败记进统计而不是返回 error，于是内容对象表在生产上只增不减（静默泄漏）。
--
-- 语义：内容对象是共享的内容字节，闭包行只是「某个产物引用它」的投影。内容对象一旦被判定回收，
-- 指向它的闭包行就没有意义（引用它的产物都已回收）—— 级联删除正是这个语义。
-- 反向的 artifact_id 外键保持原样：产物 GC 只把行标成 payload_state='deleted'，不删行。
--
-- 幂等：先 DROP IF EXISTS 再 ADD，可重复执行。
ALTER TABLE page_artifact_objects DROP CONSTRAINT IF EXISTS page_artifact_objects_content_hash_fkey;
ALTER TABLE page_artifact_objects
    ADD CONSTRAINT page_artifact_objects_content_hash_fkey
    FOREIGN KEY (content_hash) REFERENCES content_objects(content_hash) ON DELETE CASCADE;

ALTER TABLE presentation_artifact_objects DROP CONSTRAINT IF EXISTS presentation_artifact_objects_content_hash_fkey;
ALTER TABLE presentation_artifact_objects
    ADD CONSTRAINT presentation_artifact_objects_content_hash_fkey
    FOREIGN KEY (content_hash) REFERENCES content_objects(content_hash) ON DELETE CASCADE;
