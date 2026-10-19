-- 617 · 删除退货页批量「部分成功 / 全部跳过」的词条（整批语义落地后不再有这些回执）。
--
-- 背景：退货页的两个批量动作改为「整批一个事务，要么全成、要么全败」—— 前端不再按
-- outcome=update/skip/partial 选摘要，只显示服务端给的一句 message。605 里的六个 key
-- （bulkApprove/bulkReject × allUpdated/allSkipped/partial）因此没有任何消费者。
--
-- 605 的 seed SQL 里也已删掉这 12 行（新库不会再种）；本迁移负责把已有库里的存量行清掉。
-- 幂等：DELETE 天然幂等；只按 (item_key, lang) 精确点名，不用前缀。
DELETE FROM sys_i18n
 WHERE item_key IN (
    'admin.returns.bulkApprove.allUpdated',
    'admin.returns.bulkApprove.allSkipped',
    'admin.returns.bulkApprove.partial',
    'admin.returns.bulkReject.allUpdated',
    'admin.returns.bulkReject.allSkipped',
    'admin.returns.bulkReject.partial'
 )
   AND lang IN ('zh-CN', 'en-US');
