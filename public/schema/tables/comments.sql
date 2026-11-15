-- table: comments（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.comments_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.comments (
    id bigint NOT NULL,
    project_id uuid NOT NULL,
    entity_type character varying(40) NOT NULL,
    entity_id character varying(64) NOT NULL,
    user_id bigint NOT NULL,
    parent_id bigint,
    body text NOT NULL,
    status character varying(16) DEFAULT 'pending'::character varying NOT NULL,
    author_ip_hash character varying(64) DEFAULT ''::character varying NOT NULL,
    reviewed_by bigint,
    reviewed_at timestamp with time zone,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT ck_comments_body_len CHECK (((char_length(body) >= 1) AND (char_length(body) <= 2000))),
    CONSTRAINT ck_comments_status CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'approved'::character varying, 'rejected'::character varying, 'spam'::character varying])::text[])))
);

ALTER TABLE ONLY public.comments FORCE ROW LEVEL SECURITY;

ALTER SEQUENCE public.comments_id_seq OWNED BY public.comments.id;

ALTER TABLE ONLY public.comments ALTER COLUMN id SET DEFAULT nextval('public.comments_id_seq'::regclass);

ALTER TABLE public.comments ENABLE ROW LEVEL SECURITY;

CREATE POLICY p_comments_project ON public.comments USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));

-- @phase constr
ALTER TABLE ONLY public.comments
    ADD CONSTRAINT comments_pkey PRIMARY KEY (id);

-- @phase idx
CREATE INDEX idx_comments_author_recent ON public.comments USING btree (project_id, user_id, create_time DESC) WHERE (deleted_at IS NULL);

CREATE INDEX idx_comments_entity_list ON public.comments USING btree (project_id, entity_type, entity_id, status, create_time DESC) WHERE (deleted_at IS NULL);

CREATE INDEX idx_comments_parent ON public.comments USING btree (parent_id) WHERE (deleted_at IS NULL);

CREATE INDEX idx_comments_review_queue ON public.comments USING btree (status, create_time DESC) WHERE (deleted_at IS NULL);

-- @phase comment
COMMENT ON TABLE public.comments IS '评论（BIZ-5）：多态挂载（entity_type + entity_id），先审后发（列表只出 approved），支持一级回复；project_id 受 RLS 隔离';

COMMENT ON COLUMN public.comments.id IS '纯内部流水主键（不对外暴露：列表与提交接口都不需要它；将来若把 id 摆进 URL 必须改 uuid 并加归属校验）';

COMMENT ON COLUMN public.comments.project_id IS '工程归属：评论按工程隔离，列表 / 审核 / 提交都必须给工程';

COMMENT ON COLUMN public.comments.entity_type IS '被评论实体类型：取值白名单由拥有该实体的模块声明（comment 模块不认识具体实体），未注册的类型在写入入口被拒绝';

COMMENT ON COLUMN public.comments.entity_id IS '被评论实体 id：字符串存储（商品是 uuid、将来可能有大整数，统一字符串避免 uuid 列装不下别的实体 id）';

COMMENT ON COLUMN public.comments.user_id IS '评论者 = 访客账号 id（users.id）；匿名不可评，故恒非空；刻意不建外键（评论不该因账号注销而消失）';

COMMENT ON COLUMN public.comments.parent_id IS '回复目标（顶层评论 id）；NULL = 顶层。只支持一级回复：回复的回复由 service 归一到顶层评论之下';

COMMENT ON COLUMN public.comments.body IS '评论正文（纯文本，1..2000 字符，与 Go 侧上限一致）；展示层由 Jet 默认转义，不做 HTML 渲染';

COMMENT ON COLUMN public.comments.status IS '审核状态：pending=待审（新评论默认，不进任何公开面）/ approved=已通过（唯一可见）/ rejected=驳回 / spam=垃圾';

COMMENT ON COLUMN public.comments.author_ip_hash IS '来源 IP 的带盐哈希（不存明文，同 analytics 口径）；空串 = 本次请求拿不到 IP；用途只有防刷排查';

COMMENT ON COLUMN public.comments.reviewed_by IS '审核人 = sys_admin.id；未审核为 NULL。审核队列的追责依据';

COMMENT ON COLUMN public.comments.reviewed_at IS '审核时刻；未审核为 NULL';

-- @phase data
SELECT pg_catalog.setval('public.comments_id_seq', 1, false);
