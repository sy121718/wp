-- 433 · 代客建单整页的词条（admin.order_new.* 75 key + 列表页入口 admin.orders.action.create，共 76 key × 2 语言）
--
-- 背景（docs/02-W-admin-order-create.md）：
--   后台新增「代客建单」页（GET /admin/orders/new + POST /admin/orders/create，
--   模板 internal/templates/admin/order/order_new.html，handler
--   internal/module/order/inbound/http/order_create_page_handle.go）。页面把三条业务约定
--   写在页头 .help-pop 里：金额一律整数分、下单后停在待付款并立即占库存、默认不给客户开号。
--   这三段说明是拼接出来的（见下），页面上的字段标签与占位符数量也大。
--
--   本轮扫描发现：这一整页的词条**一条都没进库** —— 页面与词条 seed 没有同批落地，
--   于是英文界面下整页回落模板里的中文兜底（页头说明、每个字段标签、候选 SKU 检索区、
--   金额提示、开号提示，全部露中文）。本批把整页补齐，代价 S，不改任何既有词条。
--
--   注意扫描口径：本页两处用了 range 内取词（{{tr := .["t"]}} → {{tr("key", "兜底")}}），
--   只按 {{ .["t"]("key", …) }} 直调形式扫描会整批漏掉（本批 74 条模板 key 里有相当一部分
--   只以 tr() 形式出现）。
--
-- 分段词条与首尾空格（改这批词条前必读）：
--   页头说明、候选提示、金额提示都是**多段拼接**：普通段 + <strong> 粗体段交替，
--   中间还有 {{.CandidateCount}} / {{.MaxRows}} 这类数字插在段之间。段边界的空格必须落在值里，
--   否则中英都会粘成一片（「候选12个变体」/「At most10item rows」）。
--   · 中文段：照抄模板兜底——candidate.count_lead / items.row_limit_lead 以空格**结尾**，
--     count_tail / row_limit_tail 以空格**开头**（模板里就是这么写的）。
--   · 英文段：**中英断行规则不同**。中文靠标点分隔、段间不需要空格；英文是词与词相连，
--     凡「前段以字母结尾、后段以字母/数字开头」的边界都必须有且只有一个空格。
--     本批把空格放在**普通段那一侧**（不加在 <strong> 内部，避免粗体下划线多带一个空格）：
--     help.lead 结尾、help.mid 结尾、help.mid2 开头与结尾、help.mid3 开头、
--     amount.hint.lead 结尾、amount.hint.mid 结尾、
--     candidate.hint.lead 结尾、candidate.hint.mid 开头与结尾。
--     拼接后的英文读作：help = "Orders created here keep <b>the facts as of creation</b>: unit price,
--     … always stored in <b>cents</b> (the shipping field takes cents as well). After creation the
--     order stays <b>pending</b> and reserves stock immediately; …" —— 段间均为一格。
--
-- 幂等：INSERT ... ON CONFLICT (item_key, lang) DO NOTHING（sys_i18n 主键是 (item_key, lang)），
--   重复执行影响 0 行；本批只新增、不修改任何既有词条，故无 UPDATE。
--   判定写法：ConditionSQL 由迁移器 db.Raw 直接执行、没有参数替换，判定值只能写进 SQL 字面量。

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
    ('admin.order_new.heading', 'zh-CN', '代客建单', 200, 'admin', 'admin/order/order_new.html: 页头标题', 1, now(), now()),
    ('admin.order_new.heading', 'en-US', 'Create order on behalf', 200, 'admin', 'admin/order/order_new.html: 页头标题', 1, now(), now()),
    ('admin.order_new.help.label', 'zh-CN', '查看说明', 200, 'admin', 'admin/order/order_new.html: 说明按钮 aria-label', 1, now(), now()),
    ('admin.order_new.help.label', 'en-US', 'View help', 200, 'admin', 'admin/order/order_new.html: 说明按钮 aria-label', 1, now(), now()),
    ('admin.order_new.help.lead', 'zh-CN', '后台代客建单', 200, 'admin', 'admin/order/order_new.html: 页头 .help-pop 说明 ①（拼接段，英文以空格结尾）', 1, now(), now()),
    ('admin.order_new.help.lead', 'en-US', 'Orders created here keep ', 200, 'admin', 'admin/order/order_new.html: 页头 .help-pop 说明 ①（拼接段，英文以空格结尾）', 1, now(), now()),
    ('admin.order_new.help.strong_snapshot', 'zh-CN', '下单当时的事实快照', 200, 'admin', 'admin/order/order_new.html: 页头 .help-pop 说明 ②（粗体段）', 1, now(), now()),
    ('admin.order_new.help.strong_snapshot', 'en-US', 'the facts as of creation', 200, 'admin', 'admin/order/order_new.html: 页头 .help-pop 说明 ②（粗体段）', 1, now(), now()),
    ('admin.order_new.help.mid', 'zh-CN', '：单价、商品名与规格由服务端按当前商品读出并落库，页面上的候选价格只是选品线索。金额一律以', 200, 'admin', 'admin/order/order_new.html: 页头 .help-pop 说明 ③（拼接段，英文以空格结尾）', 1, now(), now()),
    ('admin.order_new.help.mid', 'en-US', ': unit price, product name and variant are read from the catalog by the server and written to the order; the candidate prices shown here are only a selection hint. Amounts are always stored in ', 200, 'admin', 'admin/order/order_new.html: 页头 .help-pop 说明 ③（拼接段，英文以空格结尾）', 1, now(), now()),
    ('admin.order_new.help.strong_cent', 'zh-CN', '分', 200, 'admin', 'admin/order/order_new.html: 页头 .help-pop 说明 ④（粗体段）', 1, now(), now()),
    ('admin.order_new.help.strong_cent', 'en-US', 'cents', 200, 'admin', 'admin/order/order_new.html: 页头 .help-pop 说明 ④（粗体段）', 1, now(), now()),
    ('admin.order_new.help.mid2', 'zh-CN', '存储（运费的输入框单位也是分）。建单后订单停在', 200, 'admin', 'admin/order/order_new.html: 页头 .help-pop 说明 ⑤（拼接段，英文首尾各一格）', 1, now(), now()),
    ('admin.order_new.help.mid2', 'en-US', ' (the shipping field takes cents as well). After creation the order stays ', 200, 'admin', 'admin/order/order_new.html: 页头 .help-pop 说明 ⑤（拼接段，英文首尾各一格）', 1, now(), now()),
    ('admin.order_new.help.strong_pending', 'zh-CN', '待付款', 200, 'admin', 'admin/order/order_new.html: 页头 .help-pop 说明 ⑥（粗体段）', 1, now(), now()),
    ('admin.order_new.help.strong_pending', 'en-US', 'pending', 200, 'admin', 'admin/order/order_new.html: 页头 .help-pop 说明 ⑥（粗体段）', 1, now(), now()),
    ('admin.order_new.help.mid3', 'zh-CN', '并立即占用库存，后续流转在订单详情里做。', 200, 'admin', 'admin/order/order_new.html: 页头 .help-pop 说明 ⑦（拼接段，英文以空格开头）', 1, now(), now()),
    ('admin.order_new.help.mid3', 'en-US', ' and reserves stock immediately; later transitions happen on the order detail.', 200, 'admin', 'admin/order/order_new.html: 页头 .help-pop 说明 ⑦（拼接段，英文以空格开头）', 1, now(), now()),
    ('admin.order_new.help.strong_account', 'zh-CN', '默认不给客户开号', 200, 'admin', 'admin/order/order_new.html: 页头 .help-pop 说明 ⑧（粗体段）', 1, now(), now()),
    ('admin.order_new.help.strong_account', 'en-US', 'No customer account is created by default', 200, 'admin', 'admin/order/order_new.html: 页头 .help-pop 说明 ⑧（粗体段）', 1, now(), now()),
    ('admin.order_new.help.tail', 'zh-CN', '：订单照常落库，客户不会收到任何邮件；需要开号请勾选表单底部的复选框，系统才会给该邮箱创建账号并把初始密码寄过去（邮箱已有账号时只关联、绝不改密码）。', 200, 'admin', 'admin/order/order_new.html: 页头 .help-pop 说明 ⑨（尾段）', 1, now(), now()),
    ('admin.order_new.help.tail', 'en-US', ': the order is saved as usual and no mail is sent. Tick the checkbox at the bottom of the form to create an account for this email and mail the initial password (if the email already has an account we only link it and never change the password).', 200, 'admin', 'admin/order/order_new.html: 页头 .help-pop 说明 ⑨（尾段）', 1, now(), now()),
    ('admin.order_new.err_prefix', 'zh-CN', '建单未完成：', 200, 'admin', 'admin/order/order_new.html: 提交失败时页顶错误前缀', 1, now(), now()),
    ('admin.order_new.err_prefix', 'en-US', 'Order not created:', 200, 'admin', 'admin/order/order_new.html: 提交失败时页顶错误前缀', 1, now(), now()),
    ('admin.order_new.field_error_hint', 'zh-CN', '标红的字段需要修正后再提交，其余内容已经保留。', 200, 'admin', 'admin/order/order_new.html: 提交失败时字段级错误提示', 1, now(), now()),
    ('admin.order_new.field_error_hint', 'en-US', 'Fix the fields marked in red and submit again — everything else has been kept.', 200, 'admin', 'admin/order/order_new.html: 提交失败时字段级错误提示', 1, now(), now()),
    ('admin.order_new.action.back', 'zh-CN', '返回订单列表', 200, 'admin', 'admin/order/order_new.html: 表单底部「返回」按钮', 1, now(), now()),
    ('admin.order_new.action.back', 'en-US', 'Back to orders', 200, 'admin', 'admin/order/order_new.html: 表单底部「返回」按钮', 1, now(), now()),
    ('admin.order_new.action.submit', 'zh-CN', '创建订单', 200, 'admin', 'admin/order/order_new.html: 表单底部「创建订单」提交按钮', 1, now(), now()),
    ('admin.order_new.action.submit', 'en-US', 'Create order', 200, 'admin', 'admin/order/order_new.html: 表单底部「创建订单」提交按钮', 1, now(), now()),
    ('admin.order_new.section.customer', 'zh-CN', '客户', 200, 'admin', 'admin/order/order_new.html: 表单区标题「客户」', 1, now(), now()),
    ('admin.order_new.section.customer', 'en-US', 'Customer', 200, 'admin', 'admin/order/order_new.html: 表单区标题「客户」', 1, now(), now()),
    ('admin.order_new.field.email', 'zh-CN', '客户邮箱（必填）', 200, 'admin', 'admin/order/order_new.html: 客户邮箱字段标签', 1, now(), now()),
    ('admin.order_new.field.email', 'en-US', 'Customer email (required)', 200, 'admin', 'admin/order/order_new.html: 客户邮箱字段标签', 1, now(), now()),
    ('admin.order_new.ph.email', 'zh-CN', '客户联系用邮箱，如 customer@example.com', 200, 'admin', 'admin/order/order_new.html: 客户邮箱输入框占位符', 1, now(), now()),
    ('admin.order_new.ph.email', 'en-US', 'email used to reach the customer, e.g. customer@example.com', 200, 'admin', 'admin/order/order_new.html: 客户邮箱输入框占位符', 1, now(), now()),
    ('admin.order_new.field.customer_name', 'zh-CN', '客户姓名', 200, 'admin', 'admin/order/order_new.html: 客户姓名字段标签', 1, now(), now()),
    ('admin.order_new.field.customer_name', 'en-US', 'Customer name', 200, 'admin', 'admin/order/order_new.html: 客户姓名字段标签', 1, now(), now()),
    ('admin.order_new.ph.customer_name', 'zh-CN', '电话单 / 线下单建议填上，列表的「客户」列才认得出人', 200, 'admin', 'admin/order/order_new.html: 客户姓名输入框占位符', 1, now(), now()),
    ('admin.order_new.ph.customer_name', 'en-US', 'recommended for phone/offline orders; the "Customer" column needs it to tell people apart', 200, 'admin', 'admin/order/order_new.html: 客户姓名输入框占位符', 1, now(), now()),
    ('admin.order_new.field.customer_phone', 'zh-CN', '客户电话', 200, 'admin', 'admin/order/order_new.html: 客户电话字段标签', 1, now(), now()),
    ('admin.order_new.field.customer_phone', 'en-US', 'Customer phone', 200, 'admin', 'admin/order/order_new.html: 客户电话字段标签', 1, now(), now()),
    ('admin.order_new.section.items', 'zh-CN', '商品明细', 200, 'admin', 'admin/order/order_new.html: 表单区标题「商品明细」', 1, now(), now()),
    ('admin.order_new.section.items', 'en-US', 'Items', 200, 'admin', 'admin/order/order_new.html: 表单区标题「商品明细」', 1, now(), now()),
    ('admin.order_new.col.variant', 'zh-CN', '变体（商品 · SKU）', 200, 'admin', 'admin/order/order_new.html: 明细表头「变体」列', 1, now(), now()),
    ('admin.order_new.col.variant', 'en-US', 'Variant (product · SKU)', 200, 'admin', 'admin/order/order_new.html: 明细表头「变体」列', 1, now(), now()),
    ('admin.order_new.col.quantity', 'zh-CN', '数量', 200, 'admin', 'admin/order/order_new.html: 明细表头「数量」列', 1, now(), now()),
    ('admin.order_new.col.quantity', 'en-US', 'Quantity', 200, 'admin', 'admin/order/order_new.html: 明细表头「数量」列', 1, now(), now()),
    ('admin.order_new.option.choose', 'zh-CN', '请选择 SKU', 200, 'admin', 'admin/order/order_new.html: 变体下拉的默认选项', 1, now(), now()),
    ('admin.order_new.option.choose', 'en-US', 'Select a SKU', 200, 'admin', 'admin/order/order_new.html: 变体下拉的默认选项', 1, now(), now()),
    ('admin.order_new.option.available', 'zh-CN', '可用 %s', 200, 'admin', 'admin/order/order_new.html: 变体下拉选项的可用量（Go 侧拼接，order_create_page_handle.go）', 1, now(), now()),
    ('admin.order_new.option.available', 'en-US', '%s available', 200, 'admin', 'admin/order/order_new.html: 变体下拉选项的可用量（Go 侧拼接，order_create_page_handle.go）', 1, now(), now()),
    ('admin.order_new.items.no_candidate', 'zh-CN', '当前关键词下没有可选的变体 —— 请在上面调整关键词，或先去商品管理启用变体。', 200, 'admin', 'admin/order/order_new.html: 明细区无候选变体空态', 1, now(), now()),
    ('admin.order_new.items.no_candidate', 'en-US', 'No selectable variant matches this keyword — adjust the keyword above, or enable variants in the product catalog first.', 200, 'admin', 'admin/order/order_new.html: 明细区无候选变体空态', 1, now(), now()),
    ('admin.order_new.items.add_row', 'zh-CN', '添加明细行', 200, 'admin', 'admin/order/order_new.html: 明细区「添加明细行」按钮', 1, now(), now()),
    ('admin.order_new.items.add_row', 'en-US', 'Add item row', 200, 'admin', 'admin/order/order_new.html: 明细区「添加明细行」按钮', 1, now(), now()),
    ('admin.order_new.items.row_limit_lead', 'zh-CN', '一次最多 ', 200, 'admin', 'admin/order/order_new.html: 明细行上限提示 ①（拼接段，中文以空格结尾）', 1, now(), now()),
    ('admin.order_new.items.row_limit_lead', 'en-US', 'At most ', 200, 'admin', 'admin/order/order_new.html: 明细行上限提示 ①（拼接段，中英均以空格结尾）', 1, now(), now()),
    ('admin.order_new.items.row_limit_tail', 'zh-CN', ' 行明细，需要更多请分单建立。', 200, 'admin', 'admin/order/order_new.html: 明细行上限提示 ②（拼接段，中英均以空格开头）', 1, now(), now()),
    ('admin.order_new.items.row_limit_tail', 'en-US', ' item rows per order; split into separate orders if you need more.', 200, 'admin', 'admin/order/order_new.html: 明细行上限提示 ②（拼接段，中英均以空格开头）', 1, now(), now()),
    ('admin.order_new.candidate.heading', 'zh-CN', '候选 SKU 检索', 200, 'admin', 'admin/order/order_new.html: 候选检索区标题', 1, now(), now()),
    ('admin.order_new.candidate.heading', 'en-US', 'Candidate SKU search', 200, 'admin', 'admin/order/order_new.html: 候选检索区标题', 1, now(), now()),
    ('admin.order_new.candidate.keyword', 'zh-CN', '关键词（商品名）', 200, 'admin', 'admin/order/order_new.html: 候选检索关键词字段标签', 1, now(), now()),
    ('admin.order_new.candidate.keyword', 'en-US', 'Keyword (product name)', 200, 'admin', 'admin/order/order_new.html: 候选检索关键词字段标签', 1, now(), now()),
    ('admin.order_new.candidate.ph', 'zh-CN', '留空 = 该工程全部启用变体', 200, 'admin', 'admin/order/order_new.html: 候选检索关键词输入框占位符', 1, now(), now()),
    ('admin.order_new.candidate.ph', 'en-US', 'empty = every enabled variant in this project', 200, 'admin', 'admin/order/order_new.html: 候选检索关键词输入框占位符', 1, now(), now()),
    ('admin.order_new.candidate.submit', 'zh-CN', '查询候选', 200, 'admin', 'admin/order/order_new.html: 候选检索提交按钮', 1, now(), now()),
    ('admin.order_new.candidate.submit', 'en-US', 'Search candidates', 200, 'admin', 'admin/order/order_new.html: 候选检索提交按钮', 1, now(), now()),
    ('admin.order_new.candidate.count_lead', 'zh-CN', '候选 ', 200, 'admin', 'admin/order/order_new.html: 候选计数 ①（拼接段，中文以空格结尾）', 1, now(), now()),
    ('admin.order_new.candidate.count_lead', 'en-US', 'Candidates: ', 200, 'admin', 'admin/order/order_new.html: 候选计数 ①（拼接段，中英均以空格结尾）', 1, now(), now()),
    ('admin.order_new.candidate.count_tail', 'zh-CN', ' 个变体', 200, 'admin', 'admin/order/order_new.html: 候选计数 ②（拼接段，中英均以空格开头）', 1, now(), now()),
    ('admin.order_new.candidate.count_tail', 'en-US', ' variants', 200, 'admin', 'admin/order/order_new.html: 候选计数 ②（拼接段，中英均以空格开头）', 1, now(), now()),
    ('admin.order_new.candidate.hint.lead', 'zh-CN', '关键词只匹配', 200, 'admin', 'admin/order/order_new.html: 候选说明 ①（拼接段，英文以空格结尾）', 1, now(), now()),
    ('admin.order_new.candidate.hint.lead', 'en-US', 'The keyword matches ', 200, 'admin', 'admin/order/order_new.html: 候选说明 ①（拼接段，英文以空格结尾）', 1, now(), now()),
    ('admin.order_new.candidate.hint.strong_match', 'zh-CN', '商品名称', 200, 'admin', 'admin/order/order_new.html: 候选说明 ②（粗体段）', 1, now(), now()),
    ('admin.order_new.candidate.hint.strong_match', 'en-US', 'product names', 200, 'admin', 'admin/order/order_new.html: 候选说明 ②（粗体段）', 1, now(), now()),
    ('admin.order_new.candidate.hint.mid', 'zh-CN', '（不匹配 SKU 编码与规格），候选是该工程下这些商品的全部启用变体、没有分页；重新查询会', 200, 'admin', 'admin/order/order_new.html: 候选说明 ③（拼接段，英文首尾各一格）', 1, now(), now()),
    ('admin.order_new.candidate.hint.mid', 'en-US', ' only (not SKU codes or variant labels); the candidates are every enabled variant of those products in this project, with no paging; re-running the search ', 200, 'admin', 'admin/order/order_new.html: 候选说明 ③（拼接段，英文首尾各一格）', 1, now(), now()),
    ('admin.order_new.candidate.hint.strong_reload', 'zh-CN', '重新加载本页', 200, 'admin', 'admin/order/order_new.html: 候选说明 ④（粗体段）', 1, now(), now()),
    ('admin.order_new.candidate.hint.strong_reload', 'en-US', 'reloads this page', 200, 'admin', 'admin/order/order_new.html: 候选说明 ④（粗体段）', 1, now(), now()),
    ('admin.order_new.candidate.hint.tail', 'zh-CN', '，请先确定候选范围再填写表单。', 200, 'admin', 'admin/order/order_new.html: 候选说明 ⑤（尾段）', 1, now(), now()),
    ('admin.order_new.candidate.hint.tail', 'en-US', ', so settle the candidate range before filling in the form.', 200, 'admin', 'admin/order/order_new.html: 候选说明 ⑤（尾段）', 1, now(), now()),
    ('admin.order_new.section.shipping', 'zh-CN', '收货地址', 200, 'admin', 'admin/order/order_new.html: 表单区标题「收货地址」', 1, now(), now()),
    ('admin.order_new.section.shipping', 'en-US', 'Shipping address', 200, 'admin', 'admin/order/order_new.html: 表单区标题「收货地址」', 1, now(), now()),
    ('admin.order_new.field.ship_name', 'zh-CN', '收货人', 200, 'admin', 'admin/order/order_new.html: 收货人字段标签', 1, now(), now()),
    ('admin.order_new.field.ship_name', 'en-US', 'Recipient', 200, 'admin', 'admin/order/order_new.html: 收货人字段标签', 1, now(), now()),
    ('admin.order_new.field.ship_phone', 'zh-CN', '收货人电话', 200, 'admin', 'admin/order/order_new.html: 收货人电话字段标签', 1, now(), now()),
    ('admin.order_new.field.ship_phone', 'en-US', 'Recipient phone', 200, 'admin', 'admin/order/order_new.html: 收货人电话字段标签', 1, now(), now()),
    ('admin.order_new.field.province', 'zh-CN', '省', 200, 'admin', 'admin/order/order_new.html: 省字段标签', 1, now(), now()),
    ('admin.order_new.field.province', 'en-US', 'Province', 200, 'admin', 'admin/order/order_new.html: 省字段标签', 1, now(), now()),
    ('admin.order_new.field.city', 'zh-CN', '市', 200, 'admin', 'admin/order/order_new.html: 市字段标签', 1, now(), now()),
    ('admin.order_new.field.city', 'en-US', 'City', 200, 'admin', 'admin/order/order_new.html: 市字段标签', 1, now(), now()),
    ('admin.order_new.field.district', 'zh-CN', '区 / 县', 200, 'admin', 'admin/order/order_new.html: 区县字段标签', 1, now(), now()),
    ('admin.order_new.field.district', 'en-US', 'District', 200, 'admin', 'admin/order/order_new.html: 区县字段标签', 1, now(), now()),
    ('admin.order_new.field.address', 'zh-CN', '详细地址', 200, 'admin', 'admin/order/order_new.html: 详细地址字段标签', 1, now(), now()),
    ('admin.order_new.field.address', 'en-US', 'Street address', 200, 'admin', 'admin/order/order_new.html: 详细地址字段标签', 1, now(), now()),
    ('admin.order_new.field.zip', 'zh-CN', '邮编', 200, 'admin', 'admin/order/order_new.html: 邮编字段标签', 1, now(), now()),
    ('admin.order_new.field.zip', 'en-US', 'Postal code', 200, 'admin', 'admin/order/order_new.html: 邮编字段标签', 1, now(), now()),
    ('admin.order_new.section.billing', 'zh-CN', '账单地址', 200, 'admin', 'admin/order/order_new.html: 表单区标题「账单地址」', 1, now(), now()),
    ('admin.order_new.section.billing', 'en-US', 'Billing address', 200, 'admin', 'admin/order/order_new.html: 表单区标题「账单地址」', 1, now(), now()),
    ('admin.order_new.field.same_billing', 'zh-CN', '同收货地址（勾选后下面的账单地址不适用，服务端按收货地址落库）', 200, 'admin', 'admin/order/order_new.html: 「同收货地址」复选框标签', 1, now(), now()),
    ('admin.order_new.field.same_billing', 'en-US', 'Same as shipping (when ticked the fields below are ignored; the server stores the shipping address)', 200, 'admin', 'admin/order/order_new.html: 「同收货地址」复选框标签', 1, now(), now()),
    ('admin.order_new.field.bill_name', 'zh-CN', '账单联系人', 200, 'admin', 'admin/order/order_new.html: 账单联系人字段标签', 1, now(), now()),
    ('admin.order_new.field.bill_name', 'en-US', 'Billing contact', 200, 'admin', 'admin/order/order_new.html: 账单联系人字段标签', 1, now(), now()),
    ('admin.order_new.field.bill_phone', 'zh-CN', '账单电话', 200, 'admin', 'admin/order/order_new.html: 账单电话字段标签', 1, now(), now()),
    ('admin.order_new.field.bill_phone', 'en-US', 'Billing phone', 200, 'admin', 'admin/order/order_new.html: 账单电话字段标签', 1, now(), now()),
    ('admin.order_new.section.amount', 'zh-CN', '金额与优惠', 200, 'admin', 'admin/order/order_new.html: 表单区标题「金额与优惠」', 1, now(), now()),
    ('admin.order_new.section.amount', 'en-US', 'Amount & discount', 200, 'admin', 'admin/order/order_new.html: 表单区标题「金额与优惠」', 1, now(), now()),
    ('admin.order_new.field.shipping_total', 'zh-CN', '运费（单位：分）', 200, 'admin', 'admin/order/order_new.html: 运费字段标签', 1, now(), now()),
    ('admin.order_new.field.shipping_total', 'en-US', 'Shipping fee (in cents)', 200, 'admin', 'admin/order/order_new.html: 运费字段标签', 1, now(), now()),
    ('admin.order_new.ph.shipping_total', 'zh-CN', '如 1500 表示 15.00 元；留空按 0', 200, 'admin', 'admin/order/order_new.html: 运费输入框占位符', 1, now(), now()),
    ('admin.order_new.ph.shipping_total', 'en-US', 'e.g. 1500 means 15.00; empty counts as 0', 200, 'admin', 'admin/order/order_new.html: 运费输入框占位符', 1, now(), now()),
    ('admin.order_new.field.coupon', 'zh-CN', '优惠码', 200, 'admin', 'admin/order/order_new.html: 优惠码字段标签', 1, now(), now()),
    ('admin.order_new.field.coupon', 'en-US', 'Coupon code', 200, 'admin', 'admin/order/order_new.html: 优惠码字段标签', 1, now(), now()),
    ('admin.order_new.ph.coupon', 'zh-CN', '可选；填了以服务端试算为准', 200, 'admin', 'admin/order/order_new.html: 优惠码输入框占位符', 1, now(), now()),
    ('admin.order_new.ph.coupon', 'en-US', 'optional; the server-side calculation wins', 200, 'admin', 'admin/order/order_new.html: 优惠码输入框占位符', 1, now(), now()),
    ('admin.order_new.amount.hint.lead', 'zh-CN', '小计与合计由服务端按商品价格算出，页面不提供输入；填了优惠码就以', 200, 'admin', 'admin/order/order_new.html: 金额说明 ①（拼接段，英文以空格结尾）', 1, now(), now()),
    ('admin.order_new.amount.hint.lead', 'en-US', 'Subtotal and total are computed by the server from product prices and cannot be typed here; when a coupon code is given the discount comes from ', 200, 'admin', 'admin/order/order_new.html: 金额说明 ①（拼接段，英文以空格结尾）', 1, now(), now()),
    ('admin.order_new.amount.hint.strong_coupon', 'zh-CN', '服务端试算', 200, 'admin', 'admin/order/order_new.html: 金额说明 ②（粗体段）', 1, now(), now()),
    ('admin.order_new.amount.hint.strong_coupon', 'en-US', 'the server-side calculation', 200, 'admin', 'admin/order/order_new.html: 金额说明 ②（粗体段）', 1, now(), now()),
    ('admin.order_new.amount.hint.mid', 'zh-CN', '的折扣为准，核销与建单同事务。', 200, 'admin', 'admin/order/order_new.html: 金额说明 ③（拼接段，英文以空格结尾）', 1, now(), now()),
    ('admin.order_new.amount.hint.mid', 'en-US', ', and redemption happens in the same transaction as order creation. ', 200, 'admin', 'admin/order/order_new.html: 金额说明 ③（拼接段，英文以空格结尾）', 1, now(), now()),
    ('admin.order_new.amount.hint.strong_discount', 'zh-CN', '没有人工折扣入口', 200, 'admin', 'admin/order/order_new.html: 金额说明 ④（粗体段）', 1, now(), now()),
    ('admin.order_new.amount.hint.strong_discount', 'en-US', 'There is no manual discount input', 200, 'admin', 'admin/order/order_new.html: 金额说明 ④（粗体段）', 1, now(), now()),
    ('admin.order_new.amount.hint.tail', 'zh-CN', '：能填折扣的收银台等于把成交价交给填表人。', 200, 'admin', 'admin/order/order_new.html: 金额说明 ⑤（尾段）', 1, now(), now()),
    ('admin.order_new.amount.hint.tail', 'en-US', ': a checkout that accepts a typed discount hands the final price to whoever fills the form.', 200, 'admin', 'admin/order/order_new.html: 金额说明 ⑤（尾段）', 1, now(), now()),
    ('admin.order_new.section.payment', 'zh-CN', '支付与备注', 200, 'admin', 'admin/order/order_new.html: 表单区标题「支付与备注」', 1, now(), now()),
    ('admin.order_new.section.payment', 'en-US', 'Payment & notes', 200, 'admin', 'admin/order/order_new.html: 表单区标题「支付与备注」', 1, now(), now()),
    ('admin.order_new.field.pay_method', 'zh-CN', '支付方式（码）', 200, 'admin', 'admin/order/order_new.html: 支付方式码字段标签', 1, now(), now()),
    ('admin.order_new.field.pay_method', 'en-US', 'Payment method (code)', 200, 'admin', 'admin/order/order_new.html: 支付方式码字段标签', 1, now(), now()),
    ('admin.order_new.ph.pay_method', 'zh-CN', '如 paypal', 200, 'admin', 'admin/order/order_new.html: 支付方式码输入框占位符', 1, now(), now()),
    ('admin.order_new.ph.pay_method', 'en-US', 'e.g. paypal', 200, 'admin', 'admin/order/order_new.html: 支付方式码输入框占位符', 1, now(), now()),
    ('admin.order_new.field.pay_title', 'zh-CN', '支付方式（展示名）', 200, 'admin', 'admin/order/order_new.html: 支付方式展示名字段标签', 1, now(), now()),
    ('admin.order_new.field.pay_title', 'en-US', 'Payment method (display name)', 200, 'admin', 'admin/order/order_new.html: 支付方式展示名字段标签', 1, now(), now()),
    ('admin.order_new.ph.pay_title', 'zh-CN', '如 PayPal（模拟）', 200, 'admin', 'admin/order/order_new.html: 支付方式展示名输入框占位符', 1, now(), now()),
    ('admin.order_new.ph.pay_title', 'en-US', 'e.g. PayPal (mock)', 200, 'admin', 'admin/order/order_new.html: 支付方式展示名输入框占位符', 1, now(), now()),
    ('admin.order_new.field.remark', 'zh-CN', '客户备注', 200, 'admin', 'admin/order/order_new.html: 客户备注字段标签', 1, now(), now()),
    ('admin.order_new.field.remark', 'en-US', 'Customer note', 200, 'admin', 'admin/order/order_new.html: 客户备注字段标签', 1, now(), now()),
    ('admin.order_new.ph.remark', 'zh-CN', '客户说的话（可选）', 200, 'admin', 'admin/order/order_new.html: 客户备注输入框占位符', 1, now(), now()),
    ('admin.order_new.ph.remark', 'en-US', 'what the customer said (optional)', 200, 'admin', 'admin/order/order_new.html: 客户备注输入框占位符', 1, now(), now()),
    ('admin.order_new.field.admin_note', 'zh-CN', '后台备注', 200, 'admin', 'admin/order/order_new.html: 后台备注字段标签', 1, now(), now()),
    ('admin.order_new.field.admin_note', 'en-US', 'Staff note', 200, 'admin', 'admin/order/order_new.html: 后台备注字段标签', 1, now(), now()),
    ('admin.order_new.ph.admin_note', 'zh-CN', '这单的来龙去脉（如「电话单，客户要求顺丰」）', 200, 'admin', 'admin/order/order_new.html: 后台备注输入框占位符', 1, now(), now()),
    ('admin.order_new.ph.admin_note', 'en-US', 'context for this order (e.g. "phone order, customer asked for SF Express")', 200, 'admin', 'admin/order/order_new.html: 后台备注输入框占位符', 1, now(), now()),
    ('admin.order_new.section.account', 'zh-CN', '客户账号', 200, 'admin', 'admin/order/order_new.html: 表单区标题「客户账号」', 1, now(), now()),
    ('admin.order_new.section.account', 'en-US', 'Customer account', 200, 'admin', 'admin/order/order_new.html: 表单区标题「客户账号」', 1, now(), now()),
    ('admin.order_new.field.provision', 'zh-CN', '同时给客户开号，并把初始密码发到这个邮箱', 200, 'admin', 'admin/order/order_new.html: 「同时开号」复选框标签', 1, now(), now()),
    ('admin.order_new.field.provision', 'en-US', 'Also create a customer account and mail the initial password to this address', 200, 'admin', 'admin/order/order_new.html: 「同时开号」复选框标签', 1, now(), now()),
    ('admin.order_new.provision.off_hint', 'zh-CN', '默认不开号：订单照常落库，客户不会收到任何邮件，也无法在账户中心看到这单（需要人工告知进度）。勾选后系统才会给该邮箱建号并发初始密码；邮箱已有账号时只把这单关联过去，绝不会改密码、也不会重复发信。', 200, 'admin', 'admin/order/order_new.html: 未勾选开号时的说明', 1, now(), now()),
    ('admin.order_new.provision.off_hint', 'en-US', 'No account is created by default: the order is saved as usual, the customer receives no mail and cannot see this order in the account center (progress must be reported manually). Tick the box above and the system creates an account for this email and mails the initial password; if the email already has an account we only link this order to it — the password is never changed and no mail is re-sent.', 200, 'admin', 'admin/order/order_new.html: 未勾选开号时的说明', 1, now(), now()),
    ('admin.orders.action.create', 'zh-CN', '代客建单', 200, 'admin', 'admin/order/orders.html: 列表页工具条「代客建单」入口', 1, now(), now()),
    ('admin.orders.action.create', 'en-US', 'Create order on behalf', 200, 'admin', 'admin/order/orders.html: 列表页工具条「代客建单」入口', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
