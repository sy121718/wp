-- 147 · 页面浏览记录（BIZ-8 访问计数）。
--
-- 为什么计数只能落在这里：访问面是**静态产物由 http.Dir 直出**，Go 不在访客请求路径上
--（/site 是只读文件系统），服务端根本数不出任何一次访问 —— 每次浏览都由客户端打点
--（POST /analytics/collect）写进来，本表是那条打点流水的落地处。
--
-- 隐私口径（表结构与它同构）：
--   * 不存 IP 明文，只存带盐哈希（ip_hash）—— 计数不需要明文，存下来只增加泄漏责任；
--   * 会话 / 访客标识同样只存哈希（session_id / visitor_hash），原始 cookie 值不进库；
--   * UA 只存粗粒度分类（desktop / tablet / mobile / bot）。
-- 这些值都推不回任何个人身份，也满足「不必要就不存」的基本要求。
--
-- 幂等：建表与索引一律 IF NOT EXISTS。
CREATE TABLE IF NOT EXISTS page_views (
    -- 自增主键：流水没有业务主键（同一次浏览重复上报也算两次，它就是两次请求）。
    id            BIGSERIAL   PRIMARY KEY,
    -- 工程外键：打点里的工程 id 由构建期烘进产物，服务端仍按 uuid 校验后再落库，
    -- 外键再兜一层 —— 伪造的 id 到此为止，不会在表里堆成没有归属的行。
    project_id    UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    path          TEXT        NOT NULL,
    lang          TEXT        NOT NULL DEFAULT '',
    session_id    TEXT        NOT NULL DEFAULT '',
    visitor_hash  TEXT        NOT NULL DEFAULT '',
    referrer_host TEXT        NOT NULL DEFAULT '',
    ua_class      TEXT        NOT NULL DEFAULT '',
    ip_hash       TEXT        NOT NULL DEFAULT '',
    viewed_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    -- 形状约束：服务端已做归一化与截断，这里是第二道闸（写入路径将来变了也不会破口径）。
    CONSTRAINT chk_page_views_path_len  CHECK (char_length(path) > 0 AND char_length(path) <= 512),
    CONSTRAINT chk_page_views_path_root CHECK (left(path, 1) = '/'),
    CONSTRAINT chk_page_views_lang_len  CHECK (char_length(lang) <= 35),
    CONSTRAINT chk_page_views_sess_len  CHECK (char_length(session_id) <= 64),
    CONSTRAINT chk_page_views_hash_len  CHECK (char_length(visitor_hash) <= 64),
    CONSTRAINT chk_page_views_ip_len    CHECK (char_length(ip_hash) <= 64),
    CONSTRAINT chk_page_views_ref_len   CHECK (char_length(referrer_host) <= 255),
    CONSTRAINT chk_page_views_ua_class  CHECK (ua_class IN ('', 'desktop', 'tablet', 'mobile', 'bot'))
);

-- 按天聚合与总数查询走这条索引（工程 + 时间窗）。
CREATE INDEX IF NOT EXISTS idx_page_views_project_time
    ON page_views (project_id, viewed_at DESC);

-- 按路径聚合走这条（工程 + 路径 + 时间窗）。
CREATE INDEX IF NOT EXISTS idx_page_views_project_path_time
    ON page_views (project_id, path, viewed_at DESC);
