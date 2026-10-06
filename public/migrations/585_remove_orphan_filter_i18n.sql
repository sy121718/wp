-- 585 · 仪表盘 / 销售概览接入统一时间筛选条后，清理五个失去引用的词条。
--
-- 背景：这五个 key 只在「各页自己写一份日期筛选条」的时期被引用 ——
--   · admin.analytics.range.from / .to / .submit：analytics 页原来自己画 label 与「查询」键；
--   · admin.analytics.range.last30：原来那颗「最近 30 天」按钮；
--   · admin.masterdata.action.filter：masterdata 页自己的「筛选」键。
-- 现在这几处文案统一由 admin/partials/date_filter.html 内部取词（admin.common.filter.*），
-- 页面上的提交键与「最近 30 天」也由组件渲染，所以这些 key 不再被任何模板引用。
--
-- 为什么光删 193 的 INSERT 行不够：已经应用过 193 的库里这十行是**已经存在的数据**，
-- 删 INSERT 只影响新库。所以这里补一条 DELETE，让存量库与全新库收敛到同一个状态。
--
-- 同时必须收窄 193 的 ConditionSQL（354 → 349）：那是「本批 354 个 key 都到位了吗」的
-- 门槛，少了五个就永远不满足，每次启动都会重跑 193，把这里删掉的十行又插回来 ——
-- 表现为「迁移写了、跑过了，界面上却还是旧状态」。193 的 INSERT 行已同步删除。
--
-- 保留不动：admin.analytics.range.title（analytics 页的卡片标题还在用）、
-- admin.masterdata.action.reset 之外的同批词条、admin.common.filter.*（组件自己的）。

DELETE FROM sys_i18n
 WHERE item_key IN (
    'admin.analytics.range.from',
    'admin.analytics.range.to',
    'admin.analytics.range.submit',
    'admin.analytics.range.last30',
    'admin.masterdata.action.filter'
 );
