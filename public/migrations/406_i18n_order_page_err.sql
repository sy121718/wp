-- ========================================
-- 406 — order 域三个后台列表页「装载失败」空态所需的词条
--
-- 背景：订单 / 优惠码 / 退货入库三个列表页原先在 `h.projects.List(ctx)` 失败时
--   `c.String(500, orderenums.ErrInternal)`。两件事一起错：
--     · 响应是一块纯文本 —— 侧栏、页头、筛选器、分页壳全部消失，用户既改不了也退不回去；
--     · 那句归口文案是**硬编码中文常量**（enums.ErrInternal = "操作失败，请稍后重试"），
--       英文界面照旧显示中文；并且它与本模块页面路径既有的归口文案
--       （shell.PageInternalText → MsgInternalError）是两种说法 —— 同一类故障在页面上有两套措辞。
--   本批把三处改成**降级渲染**：空列表 + 归口提示 + HTTP 200 + 页面结构完好
--   （判据与 project 域 theme_admin_pages.go 的 ThemeManage、admin 六页同一形状）。
--
-- 降级渲染带来一个必须由新词条解决的缺口：装载失败时工程列表为空，而三个模板的空态判据
-- 只看 `len(.Projects) == 0`，于是把「这一次没读出来」说成「还没有站点工程」/
-- 「这个工程还没有订单」—— 用户跑去建工程或改筛选，而真正的原因在页顶那条提示里。
-- 模板改用 handler 算好的 LoadFailed 分支承载这一档空态，它需要两句文案（三个页面共用：
-- 同一档语义、同一个位置，各写一份只会让措辞漂移）：
--   admin.common.list.loadFailedTitle —— 空态标题（「什么都没读到」而不是「没有数据」）
--   admin.common.list.loadFailedDesc  —— 空态说明（指向页顶提示 + 给出可行动作）
--
-- 归口提示本身（MsgInternalError）不新增、不修改：它的 zh-CN 行见 058，en-US 行由 405 补上
-- （本批 406 在其之后执行，ON CONFLICT DO NOTHING 不会回写它，也不会被它覆盖）。
--
-- 为什么必须走迁移而不是只改模板：**模板里的中文只是 t() 兜底，词条命中时显示的是库里的值** ——
-- 英文界面要看到英文，只能靠本文件这两行。
--
-- 幂等：INSERT ... ON CONFLICT (item_key, lang) DO NOTHING；本批**只新增、不修改任何既有词条**。
-- ========================================

INSERT INTO sys_i18n (item_key, lang, item_value, http_code, category, remark, status, create_time, update_time)
VALUES
('admin.common.list.loadFailedTitle', 'zh-CN', '这一页的数据没能读出来', 200, 'admin', 'admin/orders.html · admin/coupons.html · admin/returns.html: 列表装载失败时的空态标题（替代「还没有数据」那一档）', 1, now(), now()),
('admin.common.list.loadFailedTitle', 'en-US', 'This list could not be loaded', 200, 'admin', 'admin/orders.html · admin/coupons.html · admin/returns.html: 列表装载失败时的空态标题（替代「还没有数据」那一档）', 1, now(), now()),
('admin.common.list.loadFailedDesc', 'zh-CN', '页顶的提示说明了原因；稍后重试即可，也可以先切到别的菜单。', 200, 'admin', 'admin/orders.html · admin/coupons.html · admin/returns.html: 装载失败空态的说明句（指向页顶归口提示）', 1, now(), now()),
('admin.common.list.loadFailedDesc', 'en-US', 'See the notice at the top for the cause; retry shortly, or use another menu meanwhile.', 200, 'admin', 'admin/orders.html · admin/coupons.html · admin/returns.html: 装载失败空态的说明句（指向页顶归口提示）', 1, now(), now())
ON CONFLICT (item_key, lang) DO NOTHING;
