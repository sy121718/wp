-- 417 · 修正交易域后台页的词条（库值覆盖模板兜底的旧文案 + 缺失的动词 / 状态说明）
--
-- 背景（docs/02-O-trade-site-audit.md 的 orders T1 / customers T2 / customer_detail T1 / returns T2）：
-- 后台页面的文案真源是 **sys_i18n**，模板里的中文只是 t(key, 兜底) 的 fallback ——
-- 词条命中时永远显示库里的值。所以「只改模板」= 页面还是旧文案（02-I §5.2 #1 的教训）。
-- 而 seed 一律是 INSERT ... ON CONFLICT DO NOTHING（seed 是默认值来源、后台是真相来源），
-- 改历史 seed 的 SQL 对存量库是 no-op，必须由新迁移显式 UPDATE。
--
-- 本迁移做三件事：
--
-- 一、修正 3 个 key 的库值（与模板兜底对齐，且不再与事实矛盾）
--   1) admin.orders.list.empty_tail —— 原文「台前下单后（**或后台代客建单**）就会出现在这里」，
--      但 /admin/orders 的路由表只有 GET + cancel / status / refund / note / bulk-*，
--      **没有任何建单端点**：运营读到「后台可以代客建单」后翻遍页头 / 列表 / 行操作都找不到入口，
--      会转而怀疑权限或版本。文案提到的控件必须真的存在（02-O 任务 1，P0）。
--   2) admin.customers.list.empty_heading —— 原文「没有符合条件的客户」在**无筛选**时是错的
--      （没有条件可"符合"），且这一句在两种场景下都显示。改为与模板兜底一致的「还没有客户」；
--      有筛选时走的是另一个 key（empty_filtered_heading），不受影响。
--   3) admin.customers.list.empty_desc —— 22 种口径的旧句「站点还没有访客注册，或者上面的筛选
--      条件太窄了 —— 先点「重置」看一眼全部账号」把**信息丢了**：模板里那句「后台不能直接新建」
--      回答了用户唯一的疑问（我能不能在这儿建一个客户？），被换成了一句「点重置」——
--      而重置按钮在无筛选时本来就不该出现（同一批修的 T1）。恢复为模板兜底那句。
--   4) admin.customers.field.registered_at / registered_to —— 两个独立 date 的 label 分别是
--      「注册时间」与「至」，脱离上下文看不懂（02-O 任务 5）。与同域 analytics 的
--      「起始日期 / 结束日期」成对写法对齐，改成「注册时间（起）/ 注册时间（止）」。
--
-- 二、新增 8 个 key × 2 语言（动词 / 状态说明，原先硬编码在 Go 里）
--   · admin.customers.action.disable / enable —— 状态按钮的**动词**。详情页的按钮是
--     「<动词> + account_suffix」拼出来的，而后缀早已中英成对（zh「这个账号」/ en「 this account」）——
--     动词留在 Go 里当硬编码中文时，英文界面就是「Disable这个账号」式中英混排
--     （02-O customer_detail 任务 1）。列表页按钮与详情页共用这一份。
--   · admin.returns.note.*（6 条）—— 退货单**当前状态**的操作说明（「这一单现在该做什么 /
--     为什么没有按钮」）。它原先是 Go 侧硬编码中文（return_page_query.go 的 returnStatusNote），
--     既有中英混排问题，又平铺在「操作」标题下方当正文说明。本批把它移进 .help 悬浮，
--     文案随之 key 化（模板用 t(key, 兜底) 取）。
--
-- 为什么用 UPDATE 而不是重写 INSERT ... DO NOTHING：
--   DO NOTHING 对已存在的行是 no-op，写成 INSERT 等于什么都没做（正是这些缺陷的成因）。
--   本条要的是「修正一个已知的错误默认值」，且必须**不覆盖运营在后台改过的值** ——
--   所以 UPDATE 带 WHERE item_value = <旧值> 前置条件：只有库里还是那条旧值时才改；
--   运营手工改过（值已不同）就保持不动。新增的 8 个 key 没有历史值，用 ON CONFLICT DO NOTHING。
--
-- 幂等：UPDATE 条件命中旧值才更新，重复执行时旧值已不存在、影响 0 行；
--       INSERT 带 ON CONFLICT (item_key, lang) DO NOTHING。两者都可重复执行。

-- ── 一、修正既有值（zh-CN / en-US 各一条，带旧值前置条件）──

-- orders 空态：删掉「（或后台代客建单）」—— 页面上没有这个入口。
UPDATE sys_i18n
SET item_value = '台前下单后就会出现在这里。若已经下过单，检查上面的筛选条件是不是过窄了。',
    update_time = now()
WHERE item_key = 'admin.orders.list.empty_tail'
  AND lang = 'zh-CN'
  AND item_value = '台前下单后（或后台代客建单）就会出现在这里。若已经下过单，检查上面的筛选条件是不是过窄了。';

UPDATE sys_i18n
SET item_value = 'orders appear here once customers check out. If orders do exist, check whether the filters above are too narrow.',
    update_time = now()
WHERE item_key = 'admin.orders.list.empty_tail'
  AND lang = 'en-US'
  AND item_value = 'orders appear here once customers check out (or an admin places one on their behalf). If orders do exist, check whether the filters above are too narrow.';

-- customers 空态标题：无筛选时「没有符合条件的客户」是自相矛盾的（没有条件可"符合"）。
UPDATE sys_i18n
SET item_value = '还没有客户',
    update_time = now()
WHERE item_key = 'admin.customers.list.empty_heading'
  AND lang = 'zh-CN'
  AND item_value = '没有符合条件的客户';

UPDATE sys_i18n
SET item_value = 'No customers yet',
    update_time = now()
WHERE item_key = 'admin.customers.list.empty_heading'
  AND lang = 'en-US'
  AND item_value = 'No matching customers';

-- customers 空态说明：把「后台不能直接新建」这条唯一有价值的约束还回来。
UPDATE sys_i18n
SET item_value = '客户是访客在站点上自己注册出来的，后台不能直接新建 —— 完成注册后会出现在这里。',
    update_time = now()
WHERE item_key = 'admin.customers.list.empty_desc'
  AND lang = 'zh-CN'
  AND item_value = '站点还没有访客注册，或者上面的筛选条件太窄了 —— 先点「重置」看一眼全部账号。';

UPDATE sys_i18n
SET item_value = 'Customers sign themselves up on the site — the backend cannot create one for them. They appear here once registration is complete.',
    update_time = now()
WHERE item_key = 'admin.customers.list.empty_desc'
  AND lang = 'en-US'
  AND item_value = 'No visitor has signed up yet, or the filters are too narrow - hit Reset to see every account.';

-- 注册时间范围的两个 label：原先是「注册时间」+「至」—— 第二个 label 脱离第一个就不成立。
UPDATE sys_i18n
SET item_value = '注册时间（起）',
    update_time = now()
WHERE item_key = 'admin.customers.field.registered_at'
  AND lang = 'zh-CN'
  AND item_value = '注册时间';

UPDATE sys_i18n
SET item_value = 'Registered from',
    update_time = now()
WHERE item_key = 'admin.customers.field.registered_at'
  AND lang = 'en-US'
  AND item_value = 'Registered at';

UPDATE sys_i18n
SET item_value = '注册时间（止）',
    update_time = now()
WHERE item_key = 'admin.customers.field.registered_to'
  AND lang = 'zh-CN'
  AND item_value = '至';

UPDATE sys_i18n
SET item_value = 'Registered until',
    update_time = now()
WHERE item_key = 'admin.customers.field.registered_to'
  AND lang = 'en-US'
  AND item_value = 'to';

-- ── 二、新增词条（动词 + 退货单状态说明，8 key × 2 语言）──

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.customers.action.disable', 'zh-CN', '停用', 200, 'admin', 'admin/customers.html + admin/customer_detail.html: 状态按钮动词', 1, now(), now()),
    ('admin.customers.action.disable', 'en-US', 'Disable', 200, 'admin', 'admin/customers.html + admin/customer_detail.html: 状态按钮动词', 1, now(), now()),
    ('admin.customers.action.enable', 'zh-CN', '启用', 200, 'admin', 'admin/customers.html + admin/customer_detail.html: 状态按钮动词', 1, now(), now()),
    ('admin.customers.action.enable', 'en-US', 'Enable', 200, 'admin', 'admin/customers.html + admin/customer_detail.html: 状态按钮动词', 1, now(), now()),
    ('admin.returns.note.requested', 'zh-CN', '客户已提交，等待审核：同意后可以勾「立即完成入库 + 退款」（货已经在手上时），也可以等货到仓库再点确认收货。', 200, 'admin', 'admin/returns.html: 当前状态说明（.help）', 1, now(), now()),
    ('admin.returns.note.requested', 'en-US', 'The customer has submitted this request and it is waiting for review. If you already have the goods, tick "Approve and complete restock + refund now"; otherwise wait for the goods to reach the warehouse and then confirm receipt.', 200, 'admin', 'admin/returns.html: 当前状态说明（.help）', 1, now(), now()),
    ('admin.returns.note.approved', 'zh-CN', '已同意，等待收货：货到仓库后点下面的「确认收货并退货」—— 它会先入库、入库成功后立刻退款。', 200, 'admin', 'admin/returns.html: 当前状态说明（.help）', 1, now(), now()),
    ('admin.returns.note.approved', 'en-US', 'Approved and waiting for the goods. Once they reach the warehouse, click "Confirm receipt and restock" below — it restocks first and refunds right after the restock succeeds.', 200, 'admin', 'admin/returns.html: 当前状态说明（.help）', 1, now(), now()),
    ('admin.returns.note.received', 'zh-CN', '货已入库，但退款没做完：点下面的「补退款」重试退款即可（入库那一步已完成，不会重复加库存）。', 200, 'admin', 'admin/returns.html: 当前状态说明（.help）', 1, now(), now()),
    ('admin.returns.note.received', 'en-US', 'The goods are restocked but the refund did not finish. Click "Retry refund" below (the restock step is already done — stock is never added twice).', 200, 'admin', 'admin/returns.html: 当前状态说明（.help）', 1, now(), now()),
    ('admin.returns.note.completed', 'zh-CN', '这单已完成：货已入库、款已退。没有可执行的操作。', 200, 'admin', 'admin/returns.html: 当前状态说明（.help）', 1, now(), now()),
    ('admin.returns.note.completed', 'en-US', 'This request is complete: the goods were restocked and the refund was paid out. There is nothing left to do.', 200, 'admin', 'admin/returns.html: 当前状态说明（.help）', 1, now(), now()),
    ('admin.returns.note.rejected', 'zh-CN', '这单已被拒绝（终态）。客户如有异议，需要他重新提交退货申请。', 200, 'admin', 'admin/returns.html: 当前状态说明（.help）', 1, now(), now()),
    ('admin.returns.note.rejected', 'en-US', 'This request was rejected (a terminal state). If the customer disagrees, they have to submit a new return request.', 200, 'admin', 'admin/returns.html: 当前状态说明（.help）', 1, now(), now()),
    ('admin.returns.note.cancelled', 'zh-CN', '客户已撤销这单申请（终态）。没有可执行的操作。', 200, 'admin', 'admin/returns.html: 当前状态说明（.help）', 1, now(), now()),
    ('admin.returns.note.cancelled', 'en-US', 'The customer cancelled this request (a terminal state). There is nothing left to do.', 200, 'admin', 'admin/returns.html: 当前状态说明（.help）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
