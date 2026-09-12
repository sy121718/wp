-- 138 · 系统页面槽位表（BIZ-1 访问面骨架）。
--
-- 解决的是「引擎不知道该去哪找某个页面」：购物车片段里的「去结算」、下单成功的「查看订单」、
-- 登录页与注册页的互跳 —— 这些链接现在要么没有、要么硬编码路径，换个站就得改代码。
--
-- 绑的是**页面 id**（uuid，不可变）而不是 URL：页面改 URL 是常规操作（draft_path / active_path
-- 都是可变列），绑 id 之后链接自动跟着走，绑路径则一改就失效。
--
-- 槽位与页面的关系是「每工程每槽位至多一页」，**不限制一页被多个槽位引用** ——
-- 「个人中心页同时担任订单页」是合理用法（页面里同时放两个片段）。
--
-- 表隔离：page_id 只存 uuid 值、**不加外键**（跨模块表互不关联查询）；
-- 页面是否存在由 page 模块在服务层校验（同模块内），不靠数据库约束跨模块。

CREATE TABLE IF NOT EXISTS page_site_slots (
    id          uuid        PRIMARY KEY,
    project_id  uuid        NOT NULL,
    slot        text        NOT NULL,
    page_id     uuid        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    -- 白名单在服务层与 enums 里各有一份，DDL 这份是兜底：
    -- 键名写错的绑定不会报错，只会静默不生效（引擎按已知键查），那是最难查的一种。
    CONSTRAINT ck_page_site_slots_slot CHECK (slot IN (
        'shop', 'blog', 'cart', 'checkout', 'account', 'login', 'register', 'forgot', 'reset', 'orders'
    ))
);

-- 一个槽位只能有一个页面。
CREATE UNIQUE INDEX IF NOT EXISTS uq_page_site_slots_project_slot
    ON page_site_slots (project_id, slot);

-- 按工程取全部槽位（构建期与片段层每次都要用）。
CREATE INDEX IF NOT EXISTS idx_page_site_slots_project
    ON page_site_slots (project_id);
