-- table: sys_menus（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.sys_menus_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.sys_menus (
    id bigint NOT NULL,
    permission_code character varying(100),
    title character varying(50) NOT NULL,
    parent_id bigint DEFAULT 0 NOT NULL,
    type smallint DEFAULT 2 NOT NULL,
    path character varying(100) DEFAULT ''::character varying NOT NULL,
    component character varying(255) DEFAULT ''::character varying NOT NULL,
    external_url character varying(300) DEFAULT ''::character varying NOT NULL,
    icon character varying(50) DEFAULT ''::character varying NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    is_hidden smallint DEFAULT 0 NOT NULL,
    is_public smallint DEFAULT 0 NOT NULL,
    is_system smallint DEFAULT 0 NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    remark character varying(200),
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone,
    update_by bigint DEFAULT 0 NOT NULL,
    update_time timestamp with time zone,
    deleted_at timestamp with time zone,
    title_key character varying(100)
);

ALTER SEQUENCE public.sys_menus_id_seq OWNED BY public.sys_menus.id;

ALTER TABLE ONLY public.sys_menus ALTER COLUMN id SET DEFAULT nextval('public.sys_menus_id_seq'::regclass);

CREATE TRIGGER trg_sys_menus_sync_menu_permission AFTER INSERT OR UPDATE OF permission_code ON public.sys_menus FOR EACH ROW EXECUTE FUNCTION public.sys_menus_sync_menu_permission();

-- @phase constr
ALTER TABLE ONLY public.sys_menus
    ADD CONSTRAINT sys_menus_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_sys_menus_parent_id ON public.sys_menus USING btree (parent_id);

-- @phase comment
COMMENT ON TABLE public.sys_menus IS '菜单表';

COMMENT ON COLUMN public.sys_menus.permission_code IS '关联权限编码';

COMMENT ON COLUMN public.sys_menus.type IS '类型：1目录 2菜单 3按钮 4iframe 5外链';

COMMENT ON COLUMN public.sys_menus.component IS '已退役（587）：soybean-admin / Vue SPA 时代的组件路径。后台改为 HTMX + Jet SSR 后页面路由只认 path，本列已无生产者与消费者，仅为历史 seed 的写入通道保留，可无视。';

COMMENT ON COLUMN public.sys_menus.status IS '状态：0禁用 1启用';

COMMENT ON COLUMN public.sys_menus.is_hidden IS '是否隐藏：0否 1是';

COMMENT ON COLUMN public.sys_menus.create_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.sys_menus.update_time IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.sys_menus.deleted_at IS '审计 DB-018：由 timestamp 统一为 timestamptz；历史值按会话时区解释';

COMMENT ON COLUMN public.sys_menus.title_key IS 'i18n 标题 key（routes 接口按语言翻译，未命中回退 title）';

-- @phase data
INSERT INTO public.sys_menus VALUES
	(12, 'project:create', '新建项目', 6, 3, '', '', '', '', 1, 0, 0, 1, 10, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'project.create'),
	(2, NULL, '库存', 0, 1, '', '', '', 'warehouse', 1, 0, 0, 1, 4, '库存模块一级目录：库存流水 / 仓库 / 货源 / 采购 / 变动原因', 0, '2026-11-15 20:36:54.626655+08', 0, '2026-11-15 20:36:54.628945+08', NULL, NULL),
	(3, 'inventory:warehouse_list', '仓库管理', 2, 2, '/admin/inventory/warehouses', '', '', '', 1, 0, 0, 1, 2, '仓库实体：短码 / 默认仓 / 状态', 0, '2026-11-15 20:36:54.626655+08', 0, '2026-11-15 20:36:54.628945+08', NULL, NULL),
	(4, 'inventory:reason_list', '变动原因字典', 2, 2, '/admin/inventory/reasons', '', '', '', 1, 0, 0, 1, 5, '库存变动原因的取值集合（内置 + 自定义）', 0, '2026-11-15 20:36:54.626655+08', 0, '2026-11-15 20:36:54.628945+08', NULL, NULL),
	(13, 'project:update', '更新项目', 6, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'project.update'),
	(14, 'project:detail', '项目详情', 6, 3, '', '', '', '', 1, 0, 0, 1, 12, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'project.detail'),
	(15, 'page:create', '新建页面', 7, 3, '', '', '', '', 1, 0, 0, 1, 10, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'page.create'),
	(16, 'page:detail', '页面详情', 7, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'page.detail'),
	(17, 'page:draft_save', '保存草稿', 7, 3, '', '', '', '', 1, 0, 0, 1, 12, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'page.draft_save'),
	(18, 'page:revision_list', '修订记录', 7, 3, '', '', '', '', 1, 0, 0, 1, 13, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'page.revision_list'),
	(19, 'page:build', '构建页面', 7, 3, '', '', '', '', 1, 0, 0, 1, 14, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'page.build'),
	(20, 'page:publish', '发布页面', 7, 3, '', '', '', '', 1, 0, 0, 1, 15, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'page.publish'),
	(21, 'page:rollback', '回滚页面', 7, 3, '', '', '', '', 1, 0, 0, 1, 16, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'page.rollback'),
	(22, 'page:url_update', '更新页面URL', 7, 3, '', '', '', '', 1, 0, 0, 1, 17, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'page.url_update'),
	(23, 'block:create', '新建区块', 8, 3, '', '', '', '', 1, 0, 0, 1, 10, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'block.create'),
	(24, 'block:detail', '区块详情', 8, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'block.detail'),
	(25, 'block:update', '更新区块', 8, 3, '', '', '', '', 1, 0, 0, 1, 12, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'block.update'),
	(5, NULL, '内容', 0, 1, '', '', '', 'file-text', 1, 0, 0, 1, 2, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(94, NULL, '管理', 0, 1, '', '', '', 'shield-check', 1, 0, 0, 1, 1, NULL, 0, '2026-11-15 20:36:55.534492+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(95, NULL, '商品与库存', 0, 1, '', '', '', 'boxes', 1, 0, 0, 1, 3, NULL, 0, '2026-11-15 20:36:55.534492+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(96, NULL, '订单', 0, 1, '', '', '', 'shopping-cart', 1, 0, 0, 1, 4, NULL, 0, '2026-11-15 20:36:55.534492+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(97, NULL, '站点', 0, 1, '', '', '', 'globe', 1, 0, 0, 1, 6, NULL, 0, '2026-11-15 20:36:55.534492+08', 0, '2026-11-15 20:36:56.003241+08', NULL, NULL),
	(98, NULL, '系统', 0, 1, '', '', '', 'settings', 1, 0, 0, 1, 7, NULL, 0, '2026-11-15 20:36:55.534492+08', 0, '2026-11-15 20:36:56.003241+08', NULL, NULL),
	(54, 'product:tag_recalc', '重算商品标签', 50, 3, '', '', '', '', 1, 0, 0, 1, 43, NULL, 0, '2026-11-15 20:36:55.205987+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'product.tag_recalc'),
	(56, 'product:pricing_preview', '预览定价', 55, 3, '', '', '', '', 1, 0, 0, 1, 50, NULL, 0, '2026-11-15 20:36:55.212196+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'product.pricing_preview'),
	(57, 'product:pricing_apply', '应用定价', 55, 3, '', '', '', '', 1, 0, 0, 1, 51, NULL, 0, '2026-11-15 20:36:55.212196+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'product.pricing_apply'),
	(59, 'inventory:warehouse_create', '新建仓库', 58, 3, '', '', '', '', 1, 0, 0, 1, 60, NULL, 0, '2026-11-15 20:36:55.218377+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'inventory.warehouse_create'),
	(60, 'inventory:warehouse_update', '修改仓库', 58, 3, '', '', '', '', 1, 0, 0, 1, 61, NULL, 0, '2026-11-15 20:36:55.218377+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'inventory.warehouse_update'),
	(61, 'inventory:warehouse_delete', '删除仓库', 58, 3, '', '', '', '', 1, 0, 0, 1, 62, NULL, 0, '2026-11-15 20:36:55.218377+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'inventory.warehouse_delete'),
	(63, 'inventory:source_create', '货源管理', 62, 3, '', '', '', '', 1, 0, 0, 1, 70, NULL, 0, '2026-11-15 20:36:55.227349+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'inventory.source_create'),
	(64, 'inventory:source_update', '货源管理', 62, 3, '', '', '', '', 1, 0, 0, 1, 71, NULL, 0, '2026-11-15 20:36:55.227349+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'inventory.source_update'),
	(65, 'inventory:source_delete', '货源管理', 62, 3, '', '', '', '', 1, 0, 0, 1, 72, NULL, 0, '2026-11-15 20:36:55.227349+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'inventory.source_delete'),
	(67, 'inventory:purchase_create', '采购入库', 66, 3, '', '', '', '', 1, 0, 0, 1, 80, NULL, 0, '2026-11-15 20:36:55.231493+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'inventory.purchase_create'),
	(68, 'inventory:purchase_receipt', '采购入库', 66, 3, '', '', '', '', 1, 0, 0, 1, 81, NULL, 0, '2026-11-15 20:36:55.231493+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'inventory.purchase_receipt'),
	(69, 'inventory:purchase_production', '采购入库', 66, 3, '', '', '', '', 1, 0, 0, 1, 82, NULL, 0, '2026-11-15 20:36:55.231493+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'inventory.purchase_production'),
	(72, 'mail:account_save', '新建 / 编辑账号', 71, 3, '', '', '', '', 1, 0, 0, 1, 60, NULL, 0, '2026-11-15 20:36:55.2406+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'mail.account_save'),
	(73, 'mail:account_delete', '删除账号', 71, 3, '', '', '', '', 1, 0, 0, 1, 61, NULL, 0, '2026-11-15 20:36:55.2406+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'mail.account_delete'),
	(74, 'mail:account_default', '设默认账号', 71, 3, '', '', '', '', 1, 0, 0, 1, 62, NULL, 0, '2026-11-15 20:36:55.2406+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'mail.account_default'),
	(75, 'mail:account_test', '测试发送', 71, 3, '', '', '', '', 1, 0, 0, 1, 63, NULL, 0, '2026-11-15 20:36:55.2406+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'mail.account_test'),
	(92, 'content:list', '文章', 5, 2, '/admin/articles', 'view.articles', '', '', 1, 0, 0, 1, 1, NULL, 0, '2026-11-15 20:36:55.282645+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(7, 'page:list', '页面管理', 5, 2, '/admin/pages', 'view.page', '', '', 1, 0, 0, 1, 2, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(8, 'block:list', '区块管理', 5, 2, '/admin/blocks', 'view.block', '', '', 1, 0, 0, 1, 4, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(9, 'media:list', '媒体管理', 5, 2, '/admin/media', 'view.media', '', '', 1, 0, 0, 1, 6, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(1, 'i18n:manage', '文案词条', 5, 2, '/admin/i18n', 'view.i18n', '', '', 1, 0, 0, 1, 8, NULL, 0, '2026-11-15 20:36:54.432004+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(36, 'product:list', '商品管理', 95, 2, '/admin/products', 'view.product', '', '', 1, 0, 0, 1, 1, NULL, 0, '2026-11-15 20:36:55.194827+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(37, 'product:attribute_list', '商品属性', 95, 2, '/admin/product-attributes', 'view.product.attributes', '', '', 1, 0, 0, 1, 2, NULL, 0, '2026-11-15 20:36:55.198605+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(42, 'product:category_list', '商品分类', 95, 2, '/admin/product-categories', 'view.product.categories', '', '', 1, 0, 0, 1, 3, NULL, 0, '2026-11-15 20:36:55.202308+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(43, 'product:brand_list', '商品品牌', 95, 2, '/admin/product-brands', 'view.product.brands', '', '', 1, 0, 0, 1, 4, NULL, 0, '2026-11-15 20:36:55.202308+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(50, 'product:tag_list', '商品标签', 95, 2, '/admin/product-tags', 'view.product.tags', '', '', 1, 0, 0, 1, 5, NULL, 0, '2026-11-15 20:36:55.205987+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(70, 'masterdata:change_list', '变更记录', 95, 2, '/admin/masterdata/changes', 'view.masterdata.changes', '', '', 1, 0, 0, 1, 12, NULL, 0, '2026-11-15 20:36:55.238929+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(89, 'order:list', '订单管理', 96, 2, '/admin/orders', 'view.order', '', '', 1, 0, 0, 1, 1, NULL, 0, '2026-11-15 20:36:55.270502+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(90, 'order:coupon_list', '优惠码', 96, 2, '/admin/coupons', 'view.order.coupon', '', '', 1, 0, 0, 1, 3, NULL, 0, '2026-11-15 20:36:55.270502+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(91, 'analytics:view', '访问统计', 97, 2, '/admin/analytics', 'view.analytics', '', '', 1, 0, 0, 1, 4, NULL, 0, '2026-11-15 20:36:55.281225+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(93, 'page:redirect_view', '重定向管理', 97, 2, '/api/page/redirect', 'view.page.redirects', '', '', 1, 0, 0, 1, 13, NULL, 0, '2026-11-15 20:36:55.520069+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(71, 'mail:account_list', '邮箱管理', 98, 2, '/admin/mail', 'view.mail', '', '', 1, 0, 0, 1, 1, NULL, 0, '2026-11-15 20:36:55.2406+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(99, 'admin:list', '管理员', 94, 2, '/admin/administrators', '', '', '', 1, 0, 0, 1, 1, NULL, 0, '2026-11-15 20:36:55.534492+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(100, 'role:list', '角色管理', 94, 2, '/admin/roles', '', '', '', 1, 0, 0, 1, 2, NULL, 0, '2026-11-15 20:36:55.534492+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(101, 'menu:list', '菜单管理', 94, 2, '/admin/menus', '', '', '', 1, 0, 0, 1, 3, NULL, 0, '2026-11-15 20:36:55.534492+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(102, 'permission:list', '权限资源', 94, 2, '/admin/permissions', '', '', '', 1, 0, 0, 1, 4, NULL, 0, '2026-11-15 20:36:55.534492+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(103, 'dept:list', '部门管理', 94, 2, '/admin/departments', '', '', '', 1, 0, 0, 1, 5, NULL, 0, '2026-11-15 20:36:55.534492+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(104, 'datarule:list', '数据权限', 94, 2, '/admin/datarules', '', '', '', 1, 0, 0, 1, 6, NULL, 0, '2026-11-15 20:36:55.534492+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(105, 'contenttemplate:list', '内容模板', 5, 2, '/admin/content-templates', '', '', '', 1, 0, 0, 1, 5, NULL, 0, '2026-11-15 20:36:55.534492+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(106, 'navigation:list', '导航菜单', 5, 2, '/admin/navigations', '', '', '', 1, 0, 0, 1, 7, NULL, 0, '2026-11-15 20:36:55.534492+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(108, 'order:return_list', '退货入库', 96, 2, '/admin/returns', '', '', '', 1, 0, 0, 1, 2, NULL, 0, '2026-11-15 20:36:55.534492+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(109, 'project:theme_list', '主题管理', 97, 2, '/admin/themes', '', '', '', 1, 0, 0, 1, 1, NULL, 0, '2026-11-15 20:36:55.534492+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(110, 'project:detail', '站点设置', 97, 2, '/admin/settings', '', '', '', 1, 0, 0, 1, 2, NULL, 0, '2026-11-15 20:36:55.534492+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(111, 'seo:audit', 'SEO 控制台', 97, 2, '/admin/seo', '', '', '', 1, 0, 0, 1, 3, NULL, 0, '2026-11-15 20:36:55.534492+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(55, 'product:pricing_rules', '定价工具', 95, 2, '/admin/product-pricing', 'view.product.pricing', '', '', 0, 0, 0, 1, 6, NULL, 0, '2026-11-15 20:36:55.212196+08', 0, '2026-11-15 20:36:55.599084+08', NULL, NULL),
	(107, 'product:get', '商品详情模板', 5, 2, '/admin/products/template', '', '', '', 1, 0, 0, 1, 9, NULL, 0, '2026-11-15 20:36:55.534492+08', 0, '2026-11-15 20:36:55.599084+08', NULL, NULL),
	(112, 'mail:campaign_list', '群发活动', 122, 2, '/admin/mail/campaigns', '', '', 'send', 1, 0, 0, 1, 2, NULL, 0, '2026-11-15 20:36:55.534492+08', 0, '2026-11-15 20:36:56.003241+08', NULL, NULL),
	(113, 'mail:campaign_list', '邮件活动', 98, 2, '/admin/mail/campaign', '', '', '', 1, 1, 0, 1, 3, NULL, 0, '2026-11-15 20:36:55.534492+08', 0, '2026-11-15 20:36:56.003241+08', NULL, NULL),
	(58, 'inventory:warehouse_list', '库存管理', 2, 2, '/admin/inventory', 'view.inventory', '', '', 1, 0, 0, 1, 1, NULL, 0, '2026-11-15 20:36:55.218377+08', 0, '2026-11-15 20:36:56.217957+08', NULL, NULL),
	(62, 'inventory:source_list', '货源管理', 2, 2, '/admin/inventory/sources', 'view.inventory.sources', '', '', 1, 0, 0, 1, 3, NULL, 0, '2026-11-15 20:36:55.227349+08', 0, '2026-11-15 20:36:56.217957+08', NULL, NULL),
	(66, 'inventory:purchase_list', '采购入库', 2, 2, '/admin/inventory/purchases', 'view.inventory.purchases', '', '', 1, 0, 0, 1, 4, NULL, 0, '2026-11-15 20:36:55.231493+08', 0, '2026-11-15 20:36:56.217957+08', NULL, NULL),
	(115, NULL, '仪表盘', 0, 2, '/admin', '', '', 'layout-dashboard', 1, 0, 1, 1, 0, NULL, 0, '2026-11-15 20:36:55.534492+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(35, 'plugin:list', '插件管理', 98, 2, '/admin/plugins', 'plugin/list', '', 'puzzle', 1, 0, 0, 1, 60, NULL, 0, '2026-11-15 20:36:55.162361+08', 0, '2026-11-15 20:36:55.534492+08', NULL, NULL),
	(118, 'comment:list', '评论审核', 5, 2, '/admin/comments', 'view.comments', '', '', 1, 0, 0, 1, 30, NULL, 0, '2026-11-15 20:36:55.90286+08', 0, '2026-11-15 20:36:55.90286+08', NULL, 'admin.comment.menu'),
	(119, 'sysconfig:get', '系统设置', 97, 2, '/admin/system', '', '', 'settings', 1, 0, 0, 1, 5, NULL, 0, '2026-11-15 20:36:55.980196+08', 0, '2026-11-15 20:36:55.980196+08', NULL, NULL),
	(122, NULL, '营销', 0, 1, '', '', '', 'megaphone', 1, 0, 0, 1, 5, '营销模块一级目录：联系人 / 群发活动 / 邮件模板 / 自动化任务', 0, '2026-11-15 20:36:56.003241+08', 0, '2026-11-15 20:36:56.003241+08', NULL, NULL),
	(114, 'mail:automation_list', '自动化任务', 122, 2, '/admin/mail/automation', '', '', 'workflow', 1, 0, 0, 1, 4, NULL, 0, '2026-11-15 20:36:55.534492+08', 0, '2026-11-15 20:36:56.003241+08', NULL, NULL),
	(123, 'mail:contact_list', '联系人', 122, 2, '/admin/mail/contacts', '', '', 'users', 1, 0, 0, 1, 1, '联系人名单：筛选 / 改状态 / 导入', 0, '2026-11-15 20:36:56.003241+08', 0, '2026-11-15 20:36:56.003241+08', NULL, NULL),
	(124, 'mail:template_list', '邮件模板', 122, 2, '/admin/mail/templates', '', '', 'mail', 1, 0, 0, 1, 3, '邮件模板：标识 / 语言 / 主题 / 变量', 0, '2026-11-15 20:36:56.003241+08', 0, '2026-11-15 20:36:56.003241+08', NULL, NULL),
	(121, 'ai:session_list', 'AI 会话', 97, 2, '/admin/ai/sessions', '', '', 'chat', 1, 0, 0, 1, 10, NULL, 0, '2026-11-15 20:36:55.996319+08', 0, '2026-11-15 20:36:56.033054+08', '2026-11-15 20:36:56.033054+08', NULL),
	(120, 'ai:provider_list', '大模型管理', 97, 2, '/admin/ai/sessions', '', '', 'sparkles', 1, 0, 0, 1, 9, NULL, 0, '2026-11-15 20:36:55.996319+08', 0, '2026-11-15 20:36:56.033054+08', NULL, 'admin.ai.menu.title'),
	(128, 'ai:token_list', 'MCP 与外部访问', 97, 2, '/admin/ai/mcp', '', '', '', 1, 0, 0, 1, 10, NULL, 0, '2026-11-15 20:36:56.048414+08', 0, '2026-11-15 20:36:56.048414+08', NULL, NULL),
	(130, 'user:customer_list', '客户列表', 129, 2, '/admin/customers', '', '', 'users', 1, 0, 0, 1, 2, NULL, 0, '2026-11-15 20:36:56.077078+08', 0, '2026-11-15 20:36:56.077078+08', NULL, NULL),
	(131, 'user:customer_list', '客户概览', 129, 2, '/admin/customers/overview', '', '', '', 1, 0, 0, 1, 1, NULL, 0, '2026-11-15 20:36:56.077078+08', 0, '2026-11-15 20:36:56.077078+08', NULL, NULL),
	(132, 'user:customer_list', 'RFM 分析', 129, 2, '/admin/customers/rfm', '', '', '', 1, 0, 0, 1, 3, NULL, 0, '2026-11-15 20:36:56.082292+08', 0, '2026-11-15 20:36:56.082292+08', NULL, NULL),
	(133, 'user:customer_list', '群组留存', 129, 2, '/admin/customers/cohort', '', '', '', 1, 0, 0, 1, 4, NULL, 0, '2026-11-15 20:36:56.084836+08', 0, '2026-11-15 20:36:56.084836+08', NULL, NULL),
	(134, 'order:overview', '销售概览', 96, 2, '/admin/orders/overview', 'view.order.sales', '', '', 1, 0, 0, 1, 3, NULL, 0, '2026-11-15 20:36:56.11387+08', 0, '2026-11-15 20:36:56.11387+08', NULL, NULL),
	(6, 'project:list', '项目管理', 5, 2, '', 'view.project', '', '', 1, 1, 0, 1, 1, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.121487+08', NULL, NULL),
	(10, 'artifact:detail', '构建产物', 5, 2, '', 'view.artifact', '', '', 1, 1, 0, 1, 5, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.121487+08', NULL, NULL),
	(11, 'publication:receipts_pending', '发布管理', 5, 2, '', 'view.publication', '', '', 1, 1, 0, 1, 6, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.121487+08', NULL, NULL),
	(26, 'block:delete', '删除区块', 8, 3, '', '', '', '', 1, 0, 0, 1, 13, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'block.delete'),
	(27, 'media:upload', '上传媒体', 9, 3, '', '', '', '', 1, 0, 0, 1, 10, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'media.upload'),
	(28, 'media:detail', '媒体详情', 9, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'media.detail'),
	(29, 'media:update', '更新媒体', 9, 3, '', '', '', '', 1, 0, 0, 1, 12, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'media.update'),
	(30, 'media:delete', '删除媒体', 9, 3, '', '', '', '', 1, 0, 0, 1, 13, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'media.delete'),
	(31, 'media:category_tree', '媒体分类树', 9, 3, '', '', '', '', 1, 0, 0, 1, 14, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'media.category_tree'),
	(32, 'media:category_create', '新建分类', 9, 3, '', '', '', '', 1, 0, 0, 1, 15, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'media.category_create') ON CONFLICT DO NOTHING;
INSERT INTO public.sys_menus VALUES
	(33, 'media:category_update', '更新分类', 9, 3, '', '', '', '', 1, 0, 0, 1, 16, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'media.category_update'),
	(34, 'media:category_delete', '删除分类', 9, 3, '', '', '', '', 1, 0, 0, 1, 17, NULL, 0, '2026-11-15 20:36:55.150548+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'media.category_delete'),
	(38, 'product:attribute_create', '新建属性组', 37, 3, '', '', '', '', 1, 0, 0, 1, 10, NULL, 0, '2026-11-15 20:36:55.198605+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'product.attribute_create'),
	(39, 'product:attribute_update', '更新属性组', 37, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:55.198605+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'product.attribute_update'),
	(40, 'product:attribute_delete', '删除属性组', 37, 3, '', '', '', '', 1, 0, 0, 1, 12, NULL, 0, '2026-11-15 20:36:55.198605+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'product.attribute_delete'),
	(41, 'product:attribute_set_values', '保存属性值', 37, 3, '', '', '', '', 1, 0, 0, 1, 13, NULL, 0, '2026-11-15 20:36:55.198605+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'product.attribute_set_values'),
	(44, 'product:category_create', '新建商品分类', 42, 3, '', '', '', '', 1, 0, 0, 1, 20, NULL, 0, '2026-11-15 20:36:55.202308+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'product.category_create'),
	(125, 'mail:contact_save', '保存联系人', 123, 3, '', '', '', '', 1, 0, 0, 1, 68, NULL, 0, '2026-11-15 20:36:56.018249+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'mail.contact_save'),
	(126, 'mail:contact_delete', '删除联系人', 123, 3, '', '', '', '', 1, 0, 0, 1, 69, NULL, 0, '2026-11-15 20:36:56.018249+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'mail.contact_delete'),
	(127, 'mail:contact_tag', '批量打标签', 123, 3, '', '', '', '', 1, 0, 0, 1, 70, NULL, 0, '2026-11-15 20:36:56.018249+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'mail.contact_tag'),
	(116, 'membership:tier_list', '会员等级与权益', 129, 2, '/admin/membership', 'view.membership', '', '', 1, 0, 0, 1, 5, NULL, 0, '2026-11-15 20:36:55.882563+08', 0, '2026-11-15 20:36:56.217957+08', NULL, 'admin.membership.menu.tiers'),
	(117, 'membership:assign_list', '会员归属', 129, 2, '/admin/membership/assignments', 'view.membership_assignments', '', '', 1, 0, 0, 1, 6, NULL, 0, '2026-11-15 20:36:55.882563+08', 0, '2026-11-15 20:36:56.217957+08', NULL, 'admin.membership.menu.assignments'),
	(129, NULL, '客户', 0, 1, '', '', '', 'users', 1, 0, 0, 1, 9, NULL, 0, '2026-11-15 20:36:56.077078+08', 0, '2026-11-15 20:36:56.217957+08', NULL, NULL),
	(45, 'product:category_update', '更新商品分类', 42, 3, '', '', '', '', 1, 0, 0, 1, 21, NULL, 0, '2026-11-15 20:36:55.202308+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'product.category_update'),
	(46, 'product:category_delete', '删除商品分类', 42, 3, '', '', '', '', 1, 0, 0, 1, 22, NULL, 0, '2026-11-15 20:36:55.202308+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'product.category_delete'),
	(47, 'product:brand_create', '新建商品品牌', 43, 3, '', '', '', '', 1, 0, 0, 1, 30, NULL, 0, '2026-11-15 20:36:55.202308+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'product.brand_create'),
	(48, 'product:brand_update', '更新商品品牌', 43, 3, '', '', '', '', 1, 0, 0, 1, 31, NULL, 0, '2026-11-15 20:36:55.202308+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'product.brand_update'),
	(49, 'product:brand_delete', '删除商品品牌', 43, 3, '', '', '', '', 1, 0, 0, 1, 32, NULL, 0, '2026-11-15 20:36:55.202308+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'product.brand_delete'),
	(51, 'product:tag_create', '新建商品标签', 50, 3, '', '', '', '', 1, 0, 0, 1, 40, NULL, 0, '2026-11-15 20:36:55.205987+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'product.tag_create'),
	(52, 'product:tag_update', '更新商品标签', 50, 3, '', '', '', '', 1, 0, 0, 1, 41, NULL, 0, '2026-11-15 20:36:55.205987+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'product.tag_update'),
	(53, 'product:tag_delete', '删除商品标签', 50, 3, '', '', '', '', 1, 0, 0, 1, 42, NULL, 0, '2026-11-15 20:36:55.205987+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'product.tag_delete'),
	(76, 'mail:template_save', '编辑模板', 71, 3, '', '', '', '', 1, 0, 0, 1, 64, NULL, 0, '2026-11-15 20:36:55.2406+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'mail.template_save'),
	(77, 'mail:template_delete', '删除模板', 71, 3, '', '', '', '', 1, 0, 0, 1, 65, NULL, 0, '2026-11-15 20:36:55.2406+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'mail.template_delete'),
	(78, 'mail:contact_import', '导入联系人', 71, 3, '', '', '', '', 1, 0, 0, 1, 66, NULL, 0, '2026-11-15 20:36:55.2406+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'mail.contact_import'),
	(79, 'mail:contact_status', '修改联系人状态', 71, 3, '', '', '', '', 1, 0, 0, 1, 67, NULL, 0, '2026-11-15 20:36:55.2406+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'mail.contact_status'),
	(80, 'mail:campaign_save', '新建 / 编辑活动', 71, 3, '', '', '', '', 1, 0, 0, 1, 70, NULL, 0, '2026-11-15 20:36:55.244177+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'mail.campaign_save'),
	(81, 'mail:campaign_delete', '删除活动', 71, 3, '', '', '', '', 1, 0, 0, 1, 71, NULL, 0, '2026-11-15 20:36:55.244177+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'mail.campaign_delete'),
	(82, 'mail:campaign_start', '启动群发', 71, 3, '', '', '', '', 1, 0, 0, 1, 72, NULL, 0, '2026-11-15 20:36:55.244177+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'mail.campaign_start'),
	(83, 'mail:automation_save', '保存流程', 71, 3, '', '', '', '', 1, 0, 0, 1, 80, NULL, 0, '2026-11-15 20:36:55.248878+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'mail.automation_save'),
	(84, 'mail:automation_status', '启停流程', 71, 3, '', '', '', '', 1, 0, 0, 1, 81, NULL, 0, '2026-11-15 20:36:55.248878+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'mail.automation_status'),
	(85, 'mail:automation_delete', '删除流程', 71, 3, '', '', '', '', 1, 0, 0, 1, 82, NULL, 0, '2026-11-15 20:36:55.248878+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'mail.automation_delete'),
	(86, 'mail:automation_start', '加入流程', 71, 3, '', '', '', '', 1, 0, 0, 1, 83, NULL, 0, '2026-11-15 20:36:55.248878+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'mail.automation_start'),
	(87, 'mail:automation_tick', '补投延时实例', 71, 3, '', '', '', '', 1, 0, 0, 1, 84, NULL, 0, '2026-11-15 20:36:55.248878+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'mail.automation_tick'),
	(88, 'mail:automation_layout', '邮箱管理', 71, 3, '', '', '', '', 1, 0, 0, 1, 85, NULL, 0, '2026-11-15 20:36:55.251869+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'mail.automation_layout'),
	(135, 'admin:create', '新建管理员', 99, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'admin.create'),
	(136, 'admin:delete', '删除管理员', 99, 3, '', '', '', '', 1, 0, 0, 1, 12, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'admin.delete'),
	(137, 'admin:edit', '编辑管理员', 99, 3, '', '', '', '', 1, 0, 0, 1, 13, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'admin.edit'),
	(138, 'admin:menu_list', '管理员菜单查看', 99, 3, '', '', '', '', 1, 0, 0, 1, 14, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'admin.menu_list'),
	(139, 'admin:role_list', '管理员角色查看', 99, 3, '', '', '', '', 1, 0, 0, 1, 15, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'admin.role_list'),
	(140, 'ai:chat', 'ai:chat', 120, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'ai.chat'),
	(141, 'ai:provider_delete', 'ai:provider_delete', 120, 3, '', '', '', '', 1, 0, 0, 1, 12, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'ai.provider_delete'),
	(142, 'ai:provider_models_fetch', 'ai:provider_models_fetch', 120, 3, '', '', '', '', 1, 0, 0, 1, 13, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'ai.provider_models_fetch'),
	(143, 'ai:provider_models_restore', 'ai:provider_models_restore', 120, 3, '', '', '', '', 1, 0, 0, 1, 14, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'ai.provider_models_restore'),
	(144, 'ai:provider_models_save', 'ai:provider_models_save', 120, 3, '', '', '', '', 1, 0, 0, 1, 15, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'ai.provider_models_save'),
	(145, 'ai:provider_save', 'ai:provider_save', 120, 3, '', '', '', '', 1, 0, 0, 1, 16, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'ai.provider_save'),
	(146, 'ai:provider_status', 'ai:provider_status', 120, 3, '', '', '', '', 1, 0, 0, 1, 17, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'ai.provider_status'),
	(147, 'ai:session_append', 'ai:session_append', 120, 3, '', '', '', '', 1, 0, 0, 1, 18, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'ai.session_append'),
	(148, 'ai:session_archive', 'ai:session_archive', 120, 3, '', '', '', '', 1, 0, 0, 1, 19, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'ai.session_archive'),
	(149, 'ai:session_fold', 'ai:session_fold', 120, 3, '', '', '', '', 1, 0, 0, 1, 20, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'ai.session_fold'),
	(150, 'ai:session_rename', 'ai:session_rename', 120, 3, '', '', '', '', 1, 0, 0, 1, 21, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'ai.session_rename'),
	(151, 'comment:review', 'comment:review', 118, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'comment.review'),
	(152, 'content:create', '内容创建', 92, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'content.create'),
	(153, 'contenttemplate:create', '模板创建', 105, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'contenttemplate.create'),
	(154, 'datarule:create', '新建数据规则', 104, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'datarule.create'),
	(155, 'datarule:delete', '删除数据规则', 104, 3, '', '', '', '', 1, 0, 0, 1, 12, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'datarule.delete'),
	(156, 'datarule:update', '更新数据规则', 104, 3, '', '', '', '', 1, 0, 0, 1, 13, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'datarule.update'),
	(157, 'dept:create', '新建部门', 103, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'dept.create'),
	(158, 'dept:delete', '删除部门', 103, 3, '', '', '', '', 1, 0, 0, 1, 12, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'dept.delete'),
	(159, 'dept:update', '更新部门', 103, 3, '', '', '', '', 1, 0, 0, 1, 13, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'dept.update'),
	(160, 'i18n:manage', '文案词条管理', 0, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'i18n.manage'),
	(161, 'inventory:reason_create', '新建变动原因', 4, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'inventory.reason_create'),
	(162, 'inventory:reason_update', '修改变动原因', 4, 3, '', '', '', '', 1, 0, 0, 1, 12, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'inventory.reason_update'),
	(163, 'membership:assign_set', 'membership:assign_set', 117, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'membership.assign_set'),
	(164, 'membership:assign_unlock', 'membership:assign_unlock', 117, 3, '', '', '', '', 1, 0, 0, 1, 12, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'membership.assign_unlock'),
	(165, 'membership:tier_create', 'membership:tier_create', 116, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'membership.tier_create'),
	(166, 'membership:tier_delete', 'membership:tier_delete', 116, 3, '', '', '', '', 1, 0, 0, 1, 12, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'membership.tier_delete'),
	(167, 'membership:tier_update', 'membership:tier_update', 116, 3, '', '', '', '', 1, 0, 0, 1, 13, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'membership.tier_update'),
	(168, 'menu:create', '新建菜单', 101, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'menu.create'),
	(169, 'menu:delete', '删除菜单', 101, 3, '', '', '', '', 1, 0, 0, 1, 12, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'menu.delete'),
	(170, 'menu:update', '更新菜单', 101, 3, '', '', '', '', 1, 0, 0, 1, 13, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'menu.update'),
	(171, 'navigation:delete', '导航删除', 106, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'navigation.delete'),
	(172, 'navigation:update', '导航更新', 106, 3, '', '', '', '', 1, 0, 0, 1, 12, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'navigation.update'),
	(173, 'order:coupon_create', '新建优惠码', 90, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'order.coupon_create'),
	(174, 'order:coupon_update', '修改优惠码', 90, 3, '', '', '', '', 1, 0, 0, 1, 12, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'order.coupon_update'),
	(175, 'order:create', '新建订单', 89, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'order.create'),
	(176, 'page:delete', '删除页面', 7, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'page.delete'),
	(177, 'permission:create', '新建权限点', 102, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'permission.create'),
	(178, 'permission:delete', '删除权限点', 102, 3, '', '', '', '', 1, 0, 0, 1, 12, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'permission.delete'),
	(179, 'permission:update', '更新权限点', 102, 3, '', '', '', '', 1, 0, 0, 1, 13, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'permission.update'),
	(180, 'product:create', '商品创建', 36, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'product.create'),
	(181, 'product:delete', '商品删除', 36, 3, '', '', '', '', 1, 0, 0, 1, 12, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'product.delete'),
	(182, 'product:update', '商品更新', 36, 3, '', '', '', '', 1, 0, 0, 1, 13, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'product.update'),
	(183, 'product:variant_create', '变体创建', 36, 3, '', '', '', '', 1, 0, 0, 1, 14, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'product.variant_create'),
	(184, 'product:variant_generate', '变体组合生成', 36, 3, '', '', '', '', 1, 0, 0, 1, 15, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'product.variant_generate'),
	(185, 'project:theme_create', '新建主题', 109, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'project.theme_create'),
	(186, 'role:create', '新建角色', 100, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'role.create'),
	(187, 'role:delete', '删除角色', 100, 3, '', '', '', '', 1, 0, 0, 1, 12, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'role.delete'),
	(188, 'role:menu_list', '角色菜单查看', 100, 3, '', '', '', '', 1, 0, 0, 1, 13, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'role.menu_list'),
	(189, 'role:update', '更新角色', 100, 3, '', '', '', '', 1, 0, 0, 1, 14, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'role.update'),
	(190, 'seo:audit', '产物 SEO 体检', 111, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:56.123831+08', 0, '2026-11-15 20:36:56.123831+08', NULL, 'seo.audit'),
	(191, 'inventory:stock_list', '仓库商品 SKU', 58, 2, '/admin/inventory/skus', 'view.inventory.stock_skus', '', '', 1, 0, 0, 1, 6, NULL, 0, '2026-11-15 20:36:56.242741+08', 0, '2026-11-15 20:36:56.242741+08', NULL, 'admin.inventory.stock_skus.title'),
	(192, 'inventory:stock_ensure', '新建仓库 SKU', 191, 3, '', '', '', '', 1, 0, 0, 1, 70, NULL, 0, '2026-11-15 20:36:56.242741+08', 0, '2026-11-15 20:36:56.242741+08', NULL, 'inventory.stock_create'),
	(193, 'inventory:stock_change', '修改仓库 SKU', 191, 3, '', '', '', '', 1, 0, 0, 1, 71, NULL, 0, '2026-11-15 20:36:56.242741+08', 0, '2026-11-15 20:36:56.242741+08', NULL, 'inventory.stock_update'),
	(194, 'inventory:movement_list', '库存流水', 58, 2, '/admin/inventory/movements', 'view.inventory.movements', '', '', 1, 0, 0, 1, 7, NULL, 0, '2026-11-15 20:36:56.249797+08', 0, '2026-11-15 20:36:56.249797+08', NULL, 'admin.inventory.movements.title'),
	(195, 'product:list', '发布记录', 95, 2, '/admin/products/publish-records', 'view.products.publish_records', '', '', 1, 0, 0, 1, 7, NULL, 0, '2026-11-15 20:36:56.283713+08', 0, '2026-11-15 20:36:56.283713+08', NULL, 'admin.products.publish.title'),
	(196, 'product:publish_attempts', '发布历史', 195, 3, '', '', '', '', 1, 0, 0, 1, 80, NULL, 0, '2026-11-15 20:36:56.299034+08', 0, '2026-11-15 20:36:56.299034+08', NULL, 'product.publish_attempts'),
	(197, 'ai:provider_models_append', 'ai:provider_models_append', 120, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:56.356724+08', 0, '2026-11-15 20:36:56.356724+08', NULL, 'ai.provider_models_append'),
	(198, 'ai:provider_models_candidates', 'ai:provider_models_candidates', 120, 3, '', '', '', '', 1, 0, 0, 1, 12, NULL, 0, '2026-11-15 20:36:56.356724+08', 0, '2026-11-15 20:36:56.356724+08', NULL, 'ai.provider_models_candidates'),
	(199, 'ai:session_send', 'ai:session_send', 120, 3, '', '', '', '', 1, 0, 0, 1, 13, NULL, 0, '2026-11-15 20:36:56.356724+08', 0, '2026-11-15 20:36:56.356724+08', NULL, 'ai.session_send'),
	(200, 'ai:token_toggle', 'ai:token_toggle', 128, 3, '', '', '', '', 1, 0, 0, 1, 11, NULL, 0, '2026-11-15 20:36:56.356724+08', 0, '2026-11-15 20:36:56.356724+08', NULL, 'ai.token_toggle') ON CONFLICT DO NOTHING;
INSERT INTO public.sys_menus VALUES
	(201, 'product:seo_score', '商品 SEO 评分', 36, 3, '', '', '', '', 1, 0, 0, 1, 60, NULL, 0, '2026-11-15 20:36:56.662101+08', 0, '2026-11-15 20:36:56.662101+08', NULL, 'product.seo_score'),
	(202, 'product:brand_seo_score', '品牌 SEO 评分', 43, 3, '', '', '', '', 1, 0, 0, 1, 61, NULL, 0, '2026-11-15 20:36:56.662101+08', 0, '2026-11-15 20:36:56.662101+08', NULL, 'product.brand_seo_score'),
	(203, 'product:category_seo_score', '分类 SEO 评分', 42, 3, '', '', '', '', 1, 0, 0, 1, 62, NULL, 0, '2026-11-15 20:36:56.662101+08', 0, '2026-11-15 20:36:56.662101+08', NULL, 'product.category_seo_score'),
	(204, 'product:rating_add', '新增商品评分', 36, 3, '', '', '', '', 1, 0, 0, 1, 63, NULL, 0, '2026-11-15 20:36:56.662101+08', 0, '2026-11-15 20:36:56.662101+08', NULL, 'product.rating_add'),
	(205, 'product:rating_delete', '删除商品评分', 36, 3, '', '', '', '', 1, 0, 0, 1, 64, NULL, 0, '2026-11-15 20:36:56.662101+08', 0, '2026-11-15 20:36:56.662101+08', NULL, 'product.rating_delete') ON CONFLICT DO NOTHING;

SELECT pg_catalog.setval('public.sys_menus_id_seq', 205, true);
