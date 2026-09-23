-- 420 · 文章列表页分页落地后修正 footHint 词条（去掉「一次最多列出 50 篇」的过时说明）
--
-- 背景（审计 02-L §2 P1-14）：/admin/articles 原先一次取 50 条、没有任何分页参数 ——
-- 第 50 篇之后的文章在页面上根本不存在，而页头说明还写着「一次最多列出 50 篇
-- （超过这个量级再谈分页）」。本批把列表改成真源分页（总数来自内容契约的 Count，
-- 页码与每页条数走 shell.PageParams），50 条上限不复存在，说明必须同步改。
--
-- 判据是双向的：文案提到的控件必须真的存在（02-O 任务 1，P0），反过来「行为变了、
-- 文案还写着旧行为」同样是错误信息 —— 运营会照着那句「50 条上限」去判断自己有没有丢数据。
--
-- 后台页面文案的真源是 sys_i18n，模板里的中文只是 t(key, 兜底) 的 fallback（02-I §5.2 #1）：
-- 只改模板 = 页面还是旧文案，必须由新迁移显式 UPDATE 库值。本批模板兜底也同批改了
-- （internal/templates/admin/articles.html 的 footHint），两者逐字一致。
--
-- 为什么用 UPDATE 而不是 INSERT ... ON CONFLICT DO NOTHING：
-- DO NOTHING 对已存在的行是 no-op（188 早已插入这两行），写成 INSERT 等于什么都没做。
-- 且必须**不覆盖运营在后台改过的值** —— 故 UPDATE 带 WHERE item_value = <旧值> 前置条件：
-- 只有库里还是那条旧值时才改；运营手工改过（值已不同）就保持不动。
--
-- 幂等：条件命中旧值才更新，重复执行时旧值已不存在、影响 0 行。

-- zh-CN：删掉「一次最多列出 50 篇（超过这个量级再谈分页）」，换上分页后的真实行为。
UPDATE sys_i18n
SET item_value = '按更新时间倒序排列，每页 20 篇，用分页条看更早的文章。「未发布」表示这篇文章还没有线上详情页 —— 不一定是问题（草稿就该是未发布），编辑页的「发布」区块里能看到它当前缺什么。',
    update_time = now()
WHERE item_key = 'admin.article.list.footHint'
  AND lang = 'zh-CN'
  AND item_value = '按更新时间倒序排列，一次最多列出 50 篇（超过这个量级再谈分页）。「未发布」表示这篇文章还没有线上详情页 —— 不一定是问题（草稿就该是未发布），编辑页的「发布」区块里能看到它当前缺什么。';

-- en-US：同一处修正（英文界面此前同样宣称「at most 50」）。
UPDATE sys_i18n
SET item_value = 'Sorted by update time, newest first; 20 per page — use the pager to reach older posts. "Unpublished" means the article has no public detail page yet — that is not necessarily a problem (a draft should be unpublished), and the publishing section of the edit page shows what it is still missing.',
    update_time = now()
WHERE item_key = 'admin.article.list.footHint'
  AND lang = 'en-US'
  AND item_value = 'Sorted by update time, newest first; at most 50 are listed at a time (pagination is a conversation for a larger scale). "Unpublished" means the article has no public detail page yet — that is not necessarily a problem (a draft should be unpublished), and the publishing section of the edit page shows what it is still missing.';
