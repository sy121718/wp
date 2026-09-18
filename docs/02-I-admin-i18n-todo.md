# 后台改造新增文案位清单（i18n 对照）

> **状态：已全部补齐。** 本表最初是"待补英文词条"的清单，现已由迁移 232 / 233 全部 seed
> （中英成对，`ON CONFLICT DO NOTHING`）。表格保留下来作为**中文兜底与 key 的对照表** ——
> 改模板时对文案很有用，不用去翻模板。

**为什么这些 key 值得 seed**：模板里 `t(key, "中文兜底")` 的兜底**只在 catalog 缺该 key 时生效**。
一旦 catalog 里已有同名 key，页面显示的就是 catalog 的值 —— 两者不一致时，
同一个页面在中英两种语言下读起来会是两句话（本轮就被这个坑咬过一次：
`admin.inventory.list.delete` 的 catalog 值是「删除仓库」，把 fallback 的「删除」盖掉了）。

**新增文案位时的流程**：模板里写 `{{ .["t"]("新.key", "中文兜底") }}` → 同批补一条迁移 seed（中英成对）
→ 迁移的 `ConditionSQL` 取**本批自己的代表 key** 作门槛（不要用全库计数，存量库永远满足、补词条永远不会执行）。

下表为第一轮改造（230/231/232 覆盖的那批）的对照。

| key | 中文兜底 |
|---|---|
| `admin.analytics.help.label` | 查看说明 |
| `admin.article.list.emptyDesc` | 用右上角「新建文章」建第一篇；正文、摘要、封面与 SEO 字段都在编辑页里填。 |
| `admin.article.list.emptyTitle` | 还没有文章 |
| `admin.article.translations.empty.desc` | 先在「文章」页写正文与标题，再回来填译文。 |
| `admin.article.translations.empty.title` | 没有可翻译的文章内容 |
| `admin.article.translations.lang_label` | 语言 |
| `admin.blocks.col_kind` | 类型 |
| `admin.blocks.col_reuse` | 复用方式 |
| `admin.blocks.no_project.title` | 还没有站点工程 |
| `admin.common.action.pages` | 页面管理 |
| `admin.common.action.preview` | 预览 |
| `admin.common.col.actions` | 操作 |
| `admin.common.counter.chars` | {n} / {max} 字符 |
| `admin.common.field.keyword` | 关键词 |
| `admin.common.help.label` | 查看说明 |
| `admin.content.templates.empty.title` | 还没有内容模板 |
| `admin.coupons.action.redemptions` | 核销记录 |
| `admin.coupons.detail.ph.discount_value` | 折扣值 |
| `admin.coupons.help.label` | 查看说明 |
| `admin.coupons.list.deleteConfirm` | 删除该优惠码？有核销记录的券会被拒绝，请改用停用。 |
| `admin.coupons.list.empty_desc` | 建一张券后它会出现在这里；已经建过券时，检查一下筛选条件是不是过窄了。 |
| `admin.coupons.ph.keyword_short` | 券码 / 名称 |
| `admin.customer_detail.help.label` | 查看说明 |
| `admin.customer_detail.no_detail.title` | 客户信息读不出来 |
| `admin.customer_detail.orders.no_projects.title` | 还没有站点工程 |
| `admin.customers.list.empty_desc` | 站点还没有访客注册，或者上面的筛选条件太窄了 —— 先点「重置」看一眼全部账号。 |
| `admin.customers.ph.keyword_short` | 邮箱 / 用户名 / 昵称 / 展示名 |
| `admin.datarules.help.label` | 查看说明 |
| `admin.inventory.action.delete` | 删除 |
| `admin.inventory.change.actionHint` | 入库 / 出库 / 调整：选 SKU、方向与数量，原因从变动原因字典里选 |
| `admin.inventory.change.actionLabel` | 入库 / 出库 / 调整 |
| `admin.inventory.change.labelQuantity` | 数量 |
| `admin.inventory.change.labelReason` | 变动原因 |
| `admin.inventory.change.labelRemark` | 备注 |
| `admin.inventory.change.labelSku` | SKU |
| `admin.inventory.change.labelSourceRef` | 来源引用 |
| `admin.inventory.change.labelWarehouse` | 仓库 |
| `admin.inventory.col.code` | 仓库短码 |
| `admin.inventory.col.name` | 仓库名称 |
| `admin.inventory.col.status` | 状态 |
| `admin.inventory.field.project` | 站点工程 |
| `admin.inventory.filter.allDirections` | （全部） |
| `admin.inventory.filter.allReasons` | （全部） |
| `admin.inventory.filter.allWarehouses` | （全部） |
| `admin.inventory.filter.reset` | 重置 |
| `admin.inventory.filter.submit` | 筛选 |
| `admin.inventory.help.label` | 查看说明 |
| `admin.inventory.list.emptyHeading` | 还没有仓库 |
| `admin.inventory.moves.emptyHeading` | 没有符合条件的库存流水 |
| `admin.inventory_purchases.col.code` | 采购单号 |
| `admin.inventory_purchases.col.orderedAt` | 下单时间 |
| `admin.inventory_purchases.col.progress` | 收货进度 |
| `admin.inventory_purchases.col.status` | 单据状态 |
| `admin.inventory_purchases.filter.labelKeyword` | 关键词 |
| `admin.inventory_purchases.filter.reset` | 重置 |
| `admin.inventory_purchases.help.label` | 查看说明 |
| `admin.inventory_purchases.list.emptyHeading` | 没有符合条件的采购单 |
| `admin.inventory_purchases.noInternal.heading` | 还没有内部货源 |
| `admin.inventory_purchases.noSources.heading` | 还没有启用中的货源 |
| `admin.inventory_purchases.production.labelCost` | 成本价 |
| `admin.inventory_purchases.production.labelQty` | 入库数量 |
| `admin.inventory_purchases.receipt.drawerTitle` | 登记入库： |
| `admin.inventory_purchases.receipt.noLines` | 这张采购单没有采购行。 |
| `admin.inventory_purchases.receipt.open` | 登记入库 |
| `admin.inventory.reasons.emptyHeading` | 还没有变动原因 |
| `admin.inventory.reasons.labelCode` | 原因 code |
| `admin.inventory.reasons.labelName` | 原因名称 |
| `admin.inventory.reasons.title` | 变动原因字典 |
| `admin.inventory_sources.col.code` | 编码 |
| `admin.inventory_sources.col.name` | 名称 |
| `admin.inventory_sources.col.settlePrice` | 结算价 |
| `admin.inventory_sources.col.status` | 状态 |
| `admin.inventory_sources.filter.labelKeyword` | 关键词 |
| `admin.inventory_sources.filter.labelRelated` | 关联方 |
| `admin.inventory_sources.filter.labelStatus` | 状态 |
| `admin.inventory_sources.filter.labelType` | 类型 |
| `admin.inventory_sources.filter.reset` | 重置 |
| `admin.inventory_sources.help.label` | 查看说明 |
| `admin.inventory_sources.list.emptyHeading` | 没有符合条件的货源 |
| `admin.inventory.warehouses.title` | 仓库管理 |
| `admin.mail.account_form.new` | 新建发信账号 |
| `admin.mail.accounts.action.edit` | 编辑 |
| `admin.mail.accounts.action.edit_full` | 编辑发信账号： |
| `admin.mail.accounts.confirm.delete` | 删除该发信账号？正在用它发信的邮件会失败。 |
| `admin.mail.accounts.empty.title` | 还没有发信账号 |
| `admin.mail.automation_canvas.help.label` | 查看说明 |
| `admin.mail.automation_edit.help.label` | 查看说明 |
| `admin.mail.automation_edit.types.dash` |  —  |
| `admin.mail.automation.empty.title` | 还没有流程 |
| `admin.mail.automation.help.label` | 查看说明 |
| `admin.mail.automation_run.help.label` | 查看说明 |
| `admin.mail.automation.runs.empty.title` | 还没有实例 |
| `admin.mail.automation.runs.help.label` | 查看说明 |
| `admin.mail.automation_run.timeline.empty.title` | 还没有执行记录 |
| `admin.mail.campaign.col.rate` | 比率 |
| `admin.mail.campaign.estimate_short` | 估算 |
| `admin.mail.campaign.help.colon` | ： |
| `admin.mail.campaign.help.label` | 查看说明 |
| `admin.mail.campaign.links_empty.title` | 还没有点击数据 |
| `admin.mail.campaign.recipients_empty.title` | 还没有投递记录 |
| `admin.mail.help.label` | 查看说明 |
| `admin.mail.marketing.campaigns.confirm.delete` | 删除该活动？投递记录与报表会一并消失。 |
| `admin.mail.marketing.campaigns.confirm.start` | 启动该活动？会按目标标签把已订阅的联系人分批入队发送。 |
| `admin.mail.marketing.campaigns.empty.title` | 还没有活动 |
| `admin.mail.marketing.contacts.empty.title` | 没有匹配的联系人 |
| `admin.mail.marketing.contact_status.note_ph` | 例如：本人电话要求停止发送 |
| `admin.mail.marketing.filter.keyword` | 关键词 |
| `admin.mail.marketing.filter.status` | 状态 |
| `admin.mail.marketing.help.label` | 查看说明 |
| `admin.mail.marketing.import.help.label` | 查看说明 |
| `admin.mail.marketing.start_hint.label` | 查看说明 |
| `admin.mail.marketing.status.hint` | 待确认 = 没有同意证据，不会被群发；已订阅 = 可以发；已退订 / 硬退信 / 投诉 = 已进抑制名单。 |
| `admin.mail.template_form.new` | 新建 / 覆盖模板 |
| `admin.mail.templates.action.edit_full` | 编辑模板： |
| `admin.mail.templates.confirm.delete` | 删除该模板？用到它的邮件会发不出去。 |
| `admin.mail.templates.empty.title` | 还没有邮件模板 |
| `admin.masterdata.entities.aria` | 按实体汇总 |
| `admin.masterdata.entities.emptyTitle` | 没有任何变更记录 |
| `admin.masterdata.help.label` | 查看说明 |
| `admin.masterdata.label.entity_id` | 实体 id |
| `admin.masterdata.label.keyword` | 实体名 |
| `admin.masterdata.label.range` | 时间区间 |
| `admin.masterdata.ph.until` | 结束日期 |
| `admin.masterdata.rows.aria` | 字段级变更记录 |
| `admin.masterdata.rows.emptyTitle` | 没有符合条件的变更记录 |
| `admin.masterdata.view.entities` | 按实体汇总 |
| `admin.masterdata.view.label` | 视图 |
| `admin.masterdata.view.records` | 逐条记录 |
| `admin.navigations.col.position` | 导航位置 |
| `admin.navigations.no_project.title` | 还没有站点工程 |
| `admin.navigation_translations.empty.desc` | 先在「导航菜单」页给这个位置添加菜单项，再回来填译文。 |
| `admin.navigation_translations.empty.title` | 没有可翻译的菜单文字 |
| `admin.navigation_translations.lang_label` | 语言 |
| `admin.orders.field.project` | 站点工程 |
| `admin.orders.help.label` | 查看说明 |
| `admin.orders.no_project.heading` | 还没有站点工程 |
| `admin.orders.ph.keyword_short` | 订单号 / 客户邮箱 / 客户姓名 |
| `admin.orders.ph.payment_short` | 如 paypal |
| `admin.pages.col_project` | 所属工程 |
| `admin.pages.empty.title` | 还没有页面 |
| `admin.pages.help.intro` | 页面是手工搭建的产物：在画布（工作台）里编辑草稿，构建后发布到访问面。列表里的「多语言」进入该页的译文工作台；「已暂存待发布」表示草稿已经构建过但还没激活到线上。 |
| `admin.page_translations.empty.title` | 没有可翻译的行 |
| `admin.plugins.help.label` | 查看说明 |
| `admin.product_attributes.col.actions` | 操作 |
| `admin.product_attributes.col.key` | 标识 |
| `admin.product_attributes.col.name` | 属性组 |
| `admin.product_attributes.col.values` | 属性值 |
| `admin.product_attributes.col.variation` | 变体 |
| `admin.product_attributes.help.label` | 查看说明 |
| `admin.product_attributes.list.emptyTitle` | 还没有属性组 |
| `admin.product_attributes.list.valuesAction` | 属性值 |
| `admin.product_bundle.cfg.tableAria` | 捆绑选项 |
| `admin.product_bundle.detailFailedTitle` | 配置读取失败 |
| `admin.product_bundle.help.label` | 查看说明 |
| `admin.product_detail_template.help.label` | 查看说明 |
| `admin.product_detail_template.named.col.actions` | 操作 |
| `admin.product_detail_template.named.emptyTitle` | 还没有模板 |
| `admin.product_detail_template.named.label.name` | 新模板名 |
| `admin.product_detail_template.notReadyTitle` | 详情页模板能力未装配 |
| `admin.product_detail_template.previewHint` | 预览在浏览器新标签页里打开渲染结果 —— 看到的就是发布会产出的字节。 |
| `admin.product_pricing.history.emptyTitle` | 还没有调价记录 |
| `admin.product_pricing.preview.emptyTitle` | 没有可试算的变体 |
| `admin.products.attrs.title` | 属性引用 |
| `admin.products.col.actions` | 操作 |
| `admin.products.col.name` | 商品 |
| `admin.products.col.slug` | URL 段 |
| `admin.products.col.status` | 状态 |
| `admin.products.col.variants` | 变体 |
| `admin.products.combo.generate` | 生成组合 |
| `admin.products.help.label` | 查看说明 |
| `admin.products.list.emptyTitle` | 还没有商品 |
| `admin.products.rating.deleteConfirm` | 删除该条评分？商品的平均分与条数会随之重算。 |
| `admin.products.rating.help` | 评分是独立明细：平均值与条数由明细算出，改评分不改动商品字段；「没有评分」与「评分 0 分」是两回事，它不参与最低评分筛选。 |
| `admin.products.ratings.countTail` |  条 |
| `admin.products.ratings.empty` | 这个商品还没有评分。 |
| `admin.products.ratings.scoreSlash` |  分 /  |
| `admin.products.seo.title` | SEO 检查 |
| `admin.products.tags.autoCountLead` |  · 自动  |
| `admin.products.tags.manualCountLead` | 手工  |
| `admin.products.variant.create` | 新建变体 |
| `admin.products.variant.deleteConfirm` | 删除该变体？它的库存记录会一并删除。 |
| `admin.products.variant.disabled` | 停用 |
| `admin.products.variant.enabled` | 启用 |
| `admin.products.variants.empty` | 这个商品还没有变体。 |
| `admin.products.variants.title` | 变体 |
| `admin.product_tags.col.actions` | 操作 |
| `admin.product_tags.col.hits` | 命中 |
| `admin.product_tags.col.kind` | 类型 |
| `admin.product_tags.col.name` | 标签 |
| `admin.product_tags.col.recalcAt` | 重算时间 |
| `admin.product_tags.col.rule` | 规则 |
| `admin.product_tags.col.tag` | 标签 |
| `admin.product_tags.help.label` | 查看说明 |
| `admin.product_tags.list.emptyTitle` | 还没有标签 |
| `admin.product_tags.list.hitCountTail` |  个商品 |
| `admin.product_tags.rules.note` | 自动标签可选的内置规则与参数（只读） |
| `admin.product_translations.emptyTitle` | 没有可翻译文本 |
| `admin.product_translations.help.label` | 查看说明 |
| `admin.redirect.empty.title` | 还没有重定向 |
| `admin.returns.auto_receive.short` | 同意并立即完成入库 + 退款 |
| `admin.returns.ph.keyword_short` | 退货单号 / 订单号 / 客户邮箱 |
| `admin.seo.help.label` | 查看说明 |
| `admin.seo.paths.empty_desc` | 页面被访问后，这里按浏览量列出前 30 天的路径。 |
| `admin.seo.pending.heading` | 尚未接入的能力 |
| `admin.settings.empty.action` | 去页面管理 |
| `admin.settings.field.project` | 站点工程 |
| `admin.settings.field.site_name_hint` | 显示在浏览器标签与搜索结果标题里的名字；留空则回退用工程名。 |
| `admin.settings.help.label` | 查看说明 |
| `admin.settings.locales.rules_label` | 清单规则 |
| `admin.settings.locales.summary_note` | 站点语言清单与访问路径方案 |
| `admin.settings.roadmap.summary_note` | 尚未在后端实现的站点级能力 |
| `admin.settings.section.advanced` | 高级 |
| `admin.settings.section.advanced_note` | 自定义 404 页 · 详情页 URL 结构 |
| `admin.settings.section.integrations` | 搜索引擎与统计集成 |
| `admin.settings.section.site` | 站点信息 |
| `admin.site_slots.confirm.unbind` | 解绑这个槽位？指向它的链接将不再生成（页面本身不受影响）。 |
| `admin.theme.help.label` | 查看说明 |
| `admin.theme.name_label` | 主题名称 |
| `admin.theme_settings.help.label` | 查看说明 |
| `admin.theme.site_slots_hint` | 系统页面槽位（结算页 / 购物车页等绑定哪个页面）已并入本页： |
| `admin.theme.site_slots_link` | 系统页面槽位 |
| `shell.nav.close` | 关闭导航 |
| `shell.nav.open` | 打开导航 |
