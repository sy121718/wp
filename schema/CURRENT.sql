-- 当前数据库结构快照（由 scripts/schema-snapshot.sh 生成，请勿手改）。
-- 
-- 这份文件回答「现在 schema 长什么样」；public/migrations 回答「它是怎么变成这样的」。
-- 两者必须并存：把 488 个迁移文件从头读一遍才能知道当前形态，是不可行的。
-- 
-- 更新方式：bash scripts/schema-snapshot.sh
-- 漂移检测：bash scripts/schema-snapshot.sh --check（CI 里跑）
-- 迁移台账版本：597-block-bulk-delete-grant-copy
-- 生成时间：2026-10-23T11:37:16+08:00

--
-- PostgreSQL database dump
--



SET statement_timeout = 0;
SET lock_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: ext_shared; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA ext_shared;


--
-- Name: pg_trgm; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA ext_shared;


--
-- Name: fn_inventory_purchase_order_status_guard(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.fn_inventory_purchase_order_status_guard() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    v_status text;
BEGIN
    SELECT CASE
               WHEN COUNT(*) = 0 THEN 'pending'
               WHEN COUNT(*) FILTER (WHERE received_quantity < quantity) = 0 THEN 'received'
               WHEN COUNT(*) FILTER (WHERE received_quantity > 0) > 0 THEN 'partial'
               ELSE 'pending'
           END
      INTO v_status
      FROM inventory_purchase_order_lines
     WHERE order_id = NEW.id;

    IF NEW.status IS DISTINCT FROM v_status THEN
        NEW.status := v_status;
    END IF;
    RETURN NEW;
END;
$$;


--
-- Name: fn_inventory_purchase_order_status_sync(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.fn_inventory_purchase_order_status_sync() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    v_order_id uuid;
    v_status   text;
BEGIN
    -- INSERT 时有 NEW、DELETE 时有 OLD；另一侧为 NULL，COALESCE 取到的那一侧就是本单。
    v_order_id := COALESCE(NEW.order_id, OLD.order_id);
    IF v_order_id IS NULL THEN
        RETURN NULL;
    END IF;

    SELECT CASE
               WHEN COUNT(*) = 0 THEN 'pending'
               WHEN COUNT(*) FILTER (WHERE received_quantity < quantity) = 0 THEN 'received'
               WHEN COUNT(*) FILTER (WHERE received_quantity > 0) > 0 THEN 'partial'
               ELSE 'pending'
           END
      INTO v_status
      FROM inventory_purchase_order_lines
     WHERE order_id = v_order_id;

    -- 只在真的不一致时写：同一个值反复写会让 update_time 抖动，
    -- 也让每次收货都被记成一次「采购单本身被修改」。
    UPDATE inventory_purchase_orders
       SET status = v_status, update_time = now()
     WHERE id = v_order_id AND status IS DISTINCT FROM v_status;

    RETURN NULL;
END;
$$;


--
-- Name: master_data_changes_append_only(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.master_data_changes_append_only() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    RAISE EXCEPTION 'master_data_changes is append-only: % is rejected', TG_OP;
END;
$$;


--
-- Name: sys_menus_sync_menu_permission(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.sys_menus_sync_menu_permission() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    IF NEW.permission_code IS NOT NULL AND NEW.permission_code <> '' THEN
        INSERT INTO sys_menu_permission (menu_id, permission_code, create_time, update_time)
        VALUES (NEW.id, NEW.permission_code, now(), now())
        ON CONFLICT (menu_id, permission_code) DO NOTHING;
    END IF;
    RETURN NEW;
END;
$$;


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: ai_access_token; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ai_access_token (
    id bigint NOT NULL,
    name character varying(80) DEFAULT ''::character varying NOT NULL,
    user_id bigint DEFAULT 0 NOT NULL,
    token_prefix character varying(16) DEFAULT ''::character varying NOT NULL,
    token_hash character varying(64) DEFAULT ''::character varying NOT NULL,
    scopes jsonb DEFAULT '[]'::jsonb NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    expires_at timestamp with time zone,
    last_used_time timestamp with time zone,
    revoked_time timestamp with time zone,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: ai_access_token_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.ai_access_token_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: ai_access_token_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.ai_access_token_id_seq OWNED BY public.ai_access_token.id;


--
-- Name: ai_call_log; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ai_call_log (
    id bigint NOT NULL,
    session_id bigint DEFAULT 0 NOT NULL,
    user_id bigint DEFAULT 0 NOT NULL,
    provider_key character varying(50) DEFAULT ''::character varying NOT NULL,
    model_id character varying(120) DEFAULT ''::character varying NOT NULL,
    protocol character varying(50) DEFAULT ''::character varying NOT NULL,
    input_tokens bigint DEFAULT 0 NOT NULL,
    output_tokens bigint DEFAULT 0 NOT NULL,
    total_tokens bigint DEFAULT 0 NOT NULL,
    usage_reported boolean DEFAULT false NOT NULL,
    latency_ms bigint DEFAULT 0 NOT NULL,
    status character varying(20) DEFAULT ''::character varying NOT NULL,
    error_key character varying(80) DEFAULT ''::character varying NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    cached_tokens bigint DEFAULT 0 NOT NULL,
    cached_reported boolean DEFAULT false NOT NULL
);


--
-- Name: ai_call_log_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.ai_call_log_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: ai_call_log_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.ai_call_log_id_seq OWNED BY public.ai_call_log.id;


--
-- Name: ai_event; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ai_event (
    id bigint NOT NULL,
    session_id bigint NOT NULL,
    seq bigint NOT NULL,
    kind character varying(30) NOT NULL,
    surface_op character varying(20) DEFAULT 'append'::character varying NOT NULL,
    replace_from_seq bigint DEFAULT 0 NOT NULL,
    replace_to_seq bigint DEFAULT 0 NOT NULL,
    content text DEFAULT ''::text NOT NULL,
    content_tokens bigint DEFAULT 0 NOT NULL,
    meta jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    provider_key character varying(50) DEFAULT ''::character varying NOT NULL,
    model_id character varying(120) DEFAULT ''::character varying NOT NULL,
    user_id bigint DEFAULT 0 NOT NULL
);


--
-- Name: ai_event_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.ai_event_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: ai_event_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.ai_event_id_seq OWNED BY public.ai_event.id;


--
-- Name: ai_provider; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ai_provider (
    id bigint NOT NULL,
    provider_key character varying(50) NOT NULL,
    display_name character varying(100) NOT NULL,
    base_url character varying(500),
    protocol character varying(50) DEFAULT 'openai_chat_completions'::character varying NOT NULL,
    api_key_cipher text,
    status smallint DEFAULT 1 NOT NULL,
    sort integer DEFAULT 0 NOT NULL,
    config_data jsonb DEFAULT '{}'::jsonb NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_by bigint DEFAULT 0 NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: ai_provider_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.ai_provider_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: ai_provider_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.ai_provider_id_seq OWNED BY public.ai_provider.id;


--
-- Name: ai_session; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ai_session (
    id bigint NOT NULL,
    session_key character varying(120) NOT NULL,
    title character varying(200) DEFAULT ''::character varying NOT NULL,
    provider_key character varying(50) DEFAULT ''::character varying NOT NULL,
    model_id character varying(120) DEFAULT ''::character varying NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    next_seq bigint DEFAULT 1 NOT NULL,
    head_seq bigint DEFAULT 0 NOT NULL,
    compact_count integer DEFAULT 0 NOT NULL,
    context_tokens bigint DEFAULT 0 NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_by bigint DEFAULT 0 NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: ai_session_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.ai_session_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: ai_session_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.ai_session_id_seq OWNED BY public.ai_session.id;


--
-- Name: ai_tool_call_log; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ai_tool_call_log (
    id bigint NOT NULL,
    session_id bigint DEFAULT 0 NOT NULL,
    user_id bigint DEFAULT 0 NOT NULL,
    tool_name character varying(80) DEFAULT ''::character varying NOT NULL,
    arguments_summary character varying(500) DEFAULT ''::character varying NOT NULL,
    result_summary character varying(500) DEFAULT ''::character varying NOT NULL,
    result_len bigint DEFAULT 0 NOT NULL,
    truncated boolean DEFAULT false NOT NULL,
    status character varying(20) DEFAULT ''::character varying NOT NULL,
    error_key character varying(80) DEFAULT ''::character varying NOT NULL,
    latency_ms bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: ai_tool_call_log_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.ai_tool_call_log_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: ai_tool_call_log_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.ai_tool_call_log_id_seq OWNED BY public.ai_tool_call_log.id;


--
-- Name: ai_tool_idempotency; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.ai_tool_idempotency (
    id bigint NOT NULL,
    tool_name text NOT NULL,
    idem_key text NOT NULL,
    result_text text DEFAULT ''::text NOT NULL,
    result_data jsonb,
    create_time timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: ai_tool_idempotency_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.ai_tool_idempotency_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: ai_tool_idempotency_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.ai_tool_idempotency_id_seq OWNED BY public.ai_tool_idempotency.id;


--
-- Name: blocks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.blocks (
    project_id uuid NOT NULL,
    name text NOT NULL,
    kind text DEFAULT 'block'::text NOT NULL,
    document jsonb DEFAULT '{"root": [], "settings": {"layout": {"mode": "full"}}}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT blocks_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT blocks_updated_at_not_null NOT NULL,
    category text DEFAULT 'general'::text NOT NULL,
    reuse_mode text DEFAULT 'global'::text NOT NULL,
    id uuid DEFAULT gen_random_uuid() CONSTRAINT blocks_id_uuid_not_null NOT NULL,
    CONSTRAINT blocks_kind_check CHECK ((kind = ANY (ARRAY['block'::text, 'header'::text, 'footer'::text, 'announcement'::text, 'sidebar'::text, 'breadcrumb'::text, 'drawer'::text, 'search'::text, 'cta'::text, 'trust'::text, 'brands'::text, 'contact'::text, 'about'::text, 'banner'::text, 'grid'::text, 'snippet'::text]))),
    CONSTRAINT blocks_reuse_mode_check CHECK ((reuse_mode = ANY (ARRAY['global'::text, 'template'::text])))
);

ALTER TABLE ONLY public.blocks FORCE ROW LEVEL SECURITY;


--
-- Name: blueprint_versions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.blueprint_versions (
    id uuid NOT NULL,
    blueprint_id uuid NOT NULL,
    version bigint NOT NULL,
    document jsonb NOT NULL,
    create_time timestamp with time zone CONSTRAINT blueprint_versions_created_at_not_null NOT NULL
);


--
-- Name: blueprints; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.blueprints (
    id uuid NOT NULL,
    name text NOT NULL,
    kind text NOT NULL,
    draft_document jsonb NOT NULL,
    draft_version bigint DEFAULT 1 NOT NULL,
    create_time timestamp with time zone CONSTRAINT blueprints_created_at_not_null NOT NULL,
    update_time timestamp with time zone CONSTRAINT blueprints_updated_at_not_null NOT NULL
);


--
-- Name: build_jobs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.build_jobs (
    source_type text NOT NULL,
    source_id uuid NOT NULL,
    draft_version bigint NOT NULL,
    build_input_hash text NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    artifact_id uuid,
    error_message text,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT build_jobs_created_at_not_null NOT NULL,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    id bigint CONSTRAINT build_jobs_id_new_not_null NOT NULL,
    project_id uuid,
    lang text DEFAULT ''::text NOT NULL,
    intent text DEFAULT 'dependency'::text NOT NULL,
    lease_token uuid,
    lease_expires_time timestamp with time zone,
    attempt integer DEFAULT 0 NOT NULL,
    CONSTRAINT build_jobs_intent_check CHECK ((intent = ANY (ARRAY['manual'::text, 'dependency'::text]))),
    CONSTRAINT build_jobs_source_type_check CHECK ((source_type = ANY (ARRAY['page'::text, 'presentation'::text]))),
    CONSTRAINT build_jobs_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'running'::text, 'superseded'::text, 'failed'::text, 'succeeded'::text])))
);


--
-- Name: build_jobs_id_new_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.build_jobs ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.build_jobs_id_new_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: comments; Type: TABLE; Schema: public; Owner: -
--

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


--
-- Name: comments_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.comments_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: comments_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.comments_id_seq OWNED BY public.comments.id;


--
-- Name: content_objects; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.content_objects (
    content_hash text NOT NULL,
    provider text NOT NULL,
    object_key text NOT NULL,
    byte_size bigint NOT NULL,
    create_time timestamp with time zone CONSTRAINT content_objects_created_at_not_null NOT NULL,
    deleted_at timestamp with time zone,
    CONSTRAINT content_objects_byte_size_check CHECK ((byte_size >= 0))
);


--
-- Name: content_template_component_pins; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.content_template_component_pins (
    template_id uuid NOT NULL,
    component_id uuid NOT NULL,
    pinned_version_id uuid NOT NULL
);


--
-- Name: content_template_versions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.content_template_versions (
    id uuid NOT NULL,
    template_id uuid NOT NULL,
    version bigint NOT NULL,
    document jsonb NOT NULL,
    source_hash text NOT NULL,
    created_by uuid NOT NULL,
    create_time timestamp with time zone CONSTRAINT content_template_versions_created_at_not_null NOT NULL
);


--
-- Name: content_templates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.content_templates (
    id uuid NOT NULL,
    project_id uuid NOT NULL,
    name text NOT NULL,
    entity_type text NOT NULL,
    draft_document jsonb NOT NULL,
    draft_version bigint DEFAULT 1 NOT NULL,
    current_version_id uuid,
    create_time timestamp with time zone CONSTRAINT content_templates_created_at_not_null NOT NULL,
    update_time timestamp with time zone CONSTRAINT content_templates_updated_at_not_null NOT NULL,
    is_default boolean DEFAULT false NOT NULL,
    template_role text DEFAULT 'detail'::text NOT NULL
);

ALTER TABLE ONLY public.content_templates FORCE ROW LEVEL SECURITY;


--
-- Name: contents; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.contents (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    entity_type text NOT NULL,
    slug text NOT NULL,
    revision bigint DEFAULT 1 NOT NULL,
    data jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT contents_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT contents_updated_at_not_null NOT NULL,
    title text GENERATED ALWAYS AS ((data ->> 'title'::text)) STORED,
    CONSTRAINT contents_entity_type_check CHECK ((entity_type = 'article'::text))
);


--
-- Name: coupon_redemptions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.coupon_redemptions (
    id bigint NOT NULL,
    coupon_id bigint NOT NULL,
    project_id uuid NOT NULL,
    code character varying(64) NOT NULL,
    order_id bigint NOT NULL,
    order_no character varying(40) DEFAULT ''::character varying NOT NULL,
    discount_amount bigint DEFAULT 0 NOT NULL,
    user_id bigint,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_coupon_redemptions_amount CHECK ((discount_amount >= 0))
);

ALTER TABLE ONLY public.coupon_redemptions FORCE ROW LEVEL SECURITY;


--
-- Name: coupon_redemptions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.coupon_redemptions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: coupon_redemptions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.coupon_redemptions_id_seq OWNED BY public.coupon_redemptions.id;


--
-- Name: coupons; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.coupons (
    id bigint NOT NULL,
    project_id uuid NOT NULL,
    code character varying(64) NOT NULL,
    name character varying(120) DEFAULT ''::character varying NOT NULL,
    discount_type character varying(16) NOT NULL,
    discount_value bigint NOT NULL,
    min_subtotal bigint DEFAULT 0 NOT NULL,
    max_uses integer DEFAULT 0 NOT NULL,
    used_count integer DEFAULT 0 NOT NULL,
    per_user_limit integer DEFAULT 0 NOT NULL,
    starts_at timestamp with time zone,
    ends_at timestamp with time zone,
    status smallint DEFAULT 1 NOT NULL,
    remark character varying(255) DEFAULT ''::character varying NOT NULL,
    create_by bigint DEFAULT 0 NOT NULL,
    update_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_coupons_discount_type CHECK (((discount_type)::text = ANY ((ARRAY['percent'::character varying, 'fixed'::character varying])::text[]))),
    CONSTRAINT ck_coupons_discount_value CHECK (((((discount_type)::text = 'percent'::text) AND ((discount_value >= 1) AND (discount_value <= 100))) OR (((discount_type)::text = 'fixed'::text) AND (discount_value > 0)))),
    CONSTRAINT ck_coupons_limits CHECK (((max_uses >= 0) AND (per_user_limit >= 0) AND (used_count >= 0))),
    CONSTRAINT ck_coupons_min_subtotal CHECK ((min_subtotal >= 0)),
    CONSTRAINT ck_coupons_status CHECK ((status = ANY (ARRAY[0, 1]))),
    CONSTRAINT ck_coupons_window CHECK (((starts_at IS NULL) OR (ends_at IS NULL) OR (ends_at > starts_at)))
);

ALTER TABLE ONLY public.coupons FORCE ROW LEVEL SECURITY;


--
-- Name: coupons_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.coupons_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: coupons_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.coupons_id_seq OWNED BY public.coupons.id;


--
-- Name: document_snapshots; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.document_snapshots (
    id uuid NOT NULL,
    presentation_instance_id uuid NOT NULL,
    source_template_version_id uuid NOT NULL,
    source_entity_revision_id uuid NOT NULL,
    document jsonb NOT NULL,
    create_time timestamp with time zone CONSTRAINT document_snapshots_created_at_not_null NOT NULL
);


--
-- Name: inventory_bom_items; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.inventory_bom_items (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    parent_variant_id uuid NOT NULL,
    parent_sku_code text DEFAULT ''::text NOT NULL,
    component_variant_id uuid NOT NULL,
    component_sku_code text DEFAULT ''::text NOT NULL,
    quantity integer NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_bom_items_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_bom_items_updated_at_not_null NOT NULL,
    CONSTRAINT inventory_bom_items_quantity_check CHECK ((quantity > 0)),
    CONSTRAINT inventory_bom_items_self_check CHECK ((parent_variant_id <> component_variant_id))
);

ALTER TABLE ONLY public.inventory_bom_items FORCE ROW LEVEL SECURITY;


--
-- Name: inventory_change_reasons; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.inventory_change_reasons (
    project_id uuid,
    code text NOT NULL,
    name text NOT NULL,
    direction text NOT NULL,
    is_builtin boolean DEFAULT false NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    sort integer DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_change_reasons_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_change_reasons_updated_at_not_null NOT NULL,
    id bigint CONSTRAINT inventory_change_reasons_id_new_not_null NOT NULL,
    CONSTRAINT inventory_change_reasons_direction_check CHECK ((direction = ANY (ARRAY['in'::text, 'out'::text, 'adjust'::text]))),
    CONSTRAINT inventory_change_reasons_status_check CHECK ((status = ANY (ARRAY['active'::text, 'disabled'::text])))
);

ALTER TABLE ONLY public.inventory_change_reasons FORCE ROW LEVEL SECURITY;


--
-- Name: inventory_change_reasons_id_new_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.inventory_change_reasons ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.inventory_change_reasons_id_new_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: inventory_purchase_order_lines; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.inventory_purchase_order_lines (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    order_id uuid NOT NULL,
    project_id uuid NOT NULL,
    product_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    sku_code text DEFAULT ''::text NOT NULL,
    quantity integer NOT NULL,
    received_quantity integer DEFAULT 0 NOT NULL,
    unit_price numeric(12,2) NOT NULL,
    sort integer DEFAULT 0 NOT NULL,
    remark text DEFAULT ''::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_purchase_order_lines_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_purchase_order_lines_updated_at_not_null NOT NULL,
    CONSTRAINT inventory_purchase_order_lines_price_check CHECK ((unit_price >= (0)::numeric)),
    CONSTRAINT inventory_purchase_order_lines_qty_check CHECK ((quantity > 0)),
    CONSTRAINT inventory_purchase_order_lines_received_check CHECK (((received_quantity >= 0) AND (received_quantity <= quantity)))
);

ALTER TABLE ONLY public.inventory_purchase_order_lines FORCE ROW LEVEL SECURITY;


--
-- Name: inventory_purchase_orders; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.inventory_purchase_orders (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    code text NOT NULL,
    source_id uuid NOT NULL,
    warehouse_id uuid NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    ordered_at timestamp with time zone DEFAULT now() NOT NULL,
    expected_at timestamp with time zone,
    remark text DEFAULT ''::text NOT NULL,
    operator_id text DEFAULT ''::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_purchase_orders_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_purchase_orders_updated_at_not_null NOT NULL,
    CONSTRAINT inventory_purchase_orders_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'partial'::text, 'received'::text])))
);

ALTER TABLE ONLY public.inventory_purchase_orders FORCE ROW LEVEL SECURITY;


--
-- Name: inventory_purchase_receipt_items; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.inventory_purchase_receipt_items (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    receipt_id uuid NOT NULL,
    project_id uuid NOT NULL,
    line_id uuid,
    product_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    sku_code text DEFAULT ''::text NOT NULL,
    quantity integer NOT NULL,
    unit_price numeric(12,2) NOT NULL,
    cost_updated boolean DEFAULT false NOT NULL,
    cost_error text DEFAULT ''::text NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_purchase_receipt_items_created_at_not_null NOT NULL,
    CONSTRAINT inventory_purchase_receipt_items_price_check CHECK ((unit_price >= (0)::numeric)),
    CONSTRAINT inventory_purchase_receipt_items_qty_check CHECK ((quantity > 0))
);

ALTER TABLE ONLY public.inventory_purchase_receipt_items FORCE ROW LEVEL SECURITY;


--
-- Name: inventory_purchase_receipts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.inventory_purchase_receipts (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    code text NOT NULL,
    kind text DEFAULT 'purchase'::text NOT NULL,
    order_id uuid,
    source_id uuid NOT NULL,
    warehouse_id uuid NOT NULL,
    request_id text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'posted'::text NOT NULL,
    movement_batch_id text DEFAULT ''::text NOT NULL,
    remark text DEFAULT ''::text NOT NULL,
    operator_id text DEFAULT ''::text NOT NULL,
    received_at timestamp with time zone DEFAULT now() NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_purchase_receipts_created_at_not_null NOT NULL,
    CONSTRAINT inventory_purchase_receipts_kind_check CHECK ((kind = ANY (ARRAY['purchase'::text, 'production'::text]))),
    CONSTRAINT inventory_purchase_receipts_order_kind_check CHECK (((kind = 'purchase'::text) = (order_id IS NOT NULL))),
    CONSTRAINT inventory_purchase_receipts_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'posted'::text])))
);

ALTER TABLE ONLY public.inventory_purchase_receipts FORCE ROW LEVEL SECURITY;


--
-- Name: inventory_sources; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.inventory_sources (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    code text NOT NULL,
    name text NOT NULL,
    type text DEFAULT 'external'::text NOT NULL,
    related_party boolean DEFAULT false NOT NULL,
    settle_price numeric(12,2),
    status text DEFAULT 'active'::text NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    sort integer DEFAULT 0 NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_sources_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_sources_updated_at_not_null NOT NULL,
    CONSTRAINT inventory_sources_config_object_check CHECK ((jsonb_typeof(config) = 'object'::text)),
    CONSTRAINT inventory_sources_internal_related_check CHECK (((type = 'external'::text) OR related_party)),
    CONSTRAINT inventory_sources_settle_price_check CHECK (((settle_price IS NULL) OR ((type = 'internal'::text) AND (settle_price >= (0)::numeric)))),
    CONSTRAINT inventory_sources_status_check CHECK ((status = ANY (ARRAY['active'::text, 'disabled'::text]))),
    CONSTRAINT inventory_sources_type_check CHECK ((type = ANY (ARRAY['external'::text, 'internal'::text])))
);

ALTER TABLE ONLY public.inventory_sources FORCE ROW LEVEL SECURITY;


--
-- Name: inventory_stock_movements; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.inventory_stock_movements (
    id uuid DEFAULT gen_random_uuid() CONSTRAINT inventory_stock_movements_id_not_null1 NOT NULL,
    project_id uuid CONSTRAINT inventory_stock_movements_project_id_not_null1 NOT NULL,
    warehouse_id uuid CONSTRAINT inventory_stock_movements_warehouse_id_not_null1 NOT NULL,
    product_id uuid CONSTRAINT inventory_stock_movements_product_id_not_null1 NOT NULL,
    variant_id uuid CONSTRAINT inventory_stock_movements_variant_id_not_null1 NOT NULL,
    sku_code text DEFAULT ''::text CONSTRAINT inventory_stock_movements_sku_code_not_null1 NOT NULL,
    direction text CONSTRAINT inventory_stock_movements_direction_not_null1 NOT NULL,
    quantity integer CONSTRAINT inventory_stock_movements_quantity_not_null1 NOT NULL,
    delta integer CONSTRAINT inventory_stock_movements_delta_not_null1 NOT NULL,
    quantity_before integer CONSTRAINT inventory_stock_movements_quantity_before_not_null1 NOT NULL,
    quantity_after integer CONSTRAINT inventory_stock_movements_quantity_after_not_null1 NOT NULL,
    reason_id bigint,
    reason_code text CONSTRAINT inventory_stock_movements_reason_code_not_null1 NOT NULL,
    parent_variant_id uuid,
    source_type text DEFAULT ''::text CONSTRAINT inventory_stock_movements_source_type_not_null1 NOT NULL,
    source_ref text DEFAULT ''::text CONSTRAINT inventory_stock_movements_source_ref_not_null1 NOT NULL,
    remark text DEFAULT ''::text CONSTRAINT inventory_stock_movements_remark_not_null1 NOT NULL,
    operator_id text DEFAULT ''::text CONSTRAINT inventory_stock_movements_operator_id_not_null1 NOT NULL,
    batch_id uuid CONSTRAINT inventory_stock_movements_batch_id_not_null1 NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_stock_movements_created_at_not_null1 NOT NULL,
    unit_cost numeric(12,2),
    CONSTRAINT inventory_stock_movements_after_check CHECK ((quantity_after >= 0)),
    CONSTRAINT inventory_stock_movements_direction_check CHECK ((direction = ANY (ARRAY['in'::text, 'out'::text, 'adjust'::text]))),
    CONSTRAINT inventory_stock_movements_quantity_check CHECK ((quantity > 0))
)
PARTITION BY RANGE (create_time);

ALTER TABLE ONLY public.inventory_stock_movements FORCE ROW LEVEL SECURITY;


--
-- Name: inventory_stock_movements_2026_09; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.inventory_stock_movements_2026_09 (
    id uuid DEFAULT gen_random_uuid() CONSTRAINT inventory_stock_movements_id_not_null1 NOT NULL,
    project_id uuid CONSTRAINT inventory_stock_movements_project_id_not_null1 NOT NULL,
    warehouse_id uuid CONSTRAINT inventory_stock_movements_warehouse_id_not_null1 NOT NULL,
    product_id uuid CONSTRAINT inventory_stock_movements_product_id_not_null1 NOT NULL,
    variant_id uuid CONSTRAINT inventory_stock_movements_variant_id_not_null1 NOT NULL,
    sku_code text DEFAULT ''::text CONSTRAINT inventory_stock_movements_sku_code_not_null1 NOT NULL,
    direction text CONSTRAINT inventory_stock_movements_direction_not_null1 NOT NULL,
    quantity integer CONSTRAINT inventory_stock_movements_quantity_not_null1 NOT NULL,
    delta integer CONSTRAINT inventory_stock_movements_delta_not_null1 NOT NULL,
    quantity_before integer CONSTRAINT inventory_stock_movements_quantity_before_not_null1 NOT NULL,
    quantity_after integer CONSTRAINT inventory_stock_movements_quantity_after_not_null1 NOT NULL,
    reason_id bigint,
    reason_code text CONSTRAINT inventory_stock_movements_reason_code_not_null1 NOT NULL,
    parent_variant_id uuid,
    source_type text DEFAULT ''::text CONSTRAINT inventory_stock_movements_source_type_not_null1 NOT NULL,
    source_ref text DEFAULT ''::text CONSTRAINT inventory_stock_movements_source_ref_not_null1 NOT NULL,
    remark text DEFAULT ''::text CONSTRAINT inventory_stock_movements_remark_not_null1 NOT NULL,
    operator_id text DEFAULT ''::text CONSTRAINT inventory_stock_movements_operator_id_not_null1 NOT NULL,
    batch_id uuid CONSTRAINT inventory_stock_movements_batch_id_not_null1 NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_stock_movements_created_at_not_null1 NOT NULL,
    unit_cost numeric(12,2),
    CONSTRAINT inventory_stock_movements_after_check CHECK ((quantity_after >= 0)),
    CONSTRAINT inventory_stock_movements_direction_check CHECK ((direction = ANY (ARRAY['in'::text, 'out'::text, 'adjust'::text]))),
    CONSTRAINT inventory_stock_movements_quantity_check CHECK ((quantity > 0))
);

ALTER TABLE ONLY public.inventory_stock_movements_2026_09 FORCE ROW LEVEL SECURITY;


--
-- Name: inventory_stock_movements_2026_10; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.inventory_stock_movements_2026_10 (
    id uuid DEFAULT gen_random_uuid() CONSTRAINT inventory_stock_movements_id_not_null1 NOT NULL,
    project_id uuid CONSTRAINT inventory_stock_movements_project_id_not_null1 NOT NULL,
    warehouse_id uuid CONSTRAINT inventory_stock_movements_warehouse_id_not_null1 NOT NULL,
    product_id uuid CONSTRAINT inventory_stock_movements_product_id_not_null1 NOT NULL,
    variant_id uuid CONSTRAINT inventory_stock_movements_variant_id_not_null1 NOT NULL,
    sku_code text DEFAULT ''::text CONSTRAINT inventory_stock_movements_sku_code_not_null1 NOT NULL,
    direction text CONSTRAINT inventory_stock_movements_direction_not_null1 NOT NULL,
    quantity integer CONSTRAINT inventory_stock_movements_quantity_not_null1 NOT NULL,
    delta integer CONSTRAINT inventory_stock_movements_delta_not_null1 NOT NULL,
    quantity_before integer CONSTRAINT inventory_stock_movements_quantity_before_not_null1 NOT NULL,
    quantity_after integer CONSTRAINT inventory_stock_movements_quantity_after_not_null1 NOT NULL,
    reason_id bigint,
    reason_code text CONSTRAINT inventory_stock_movements_reason_code_not_null1 NOT NULL,
    parent_variant_id uuid,
    source_type text DEFAULT ''::text CONSTRAINT inventory_stock_movements_source_type_not_null1 NOT NULL,
    source_ref text DEFAULT ''::text CONSTRAINT inventory_stock_movements_source_ref_not_null1 NOT NULL,
    remark text DEFAULT ''::text CONSTRAINT inventory_stock_movements_remark_not_null1 NOT NULL,
    operator_id text DEFAULT ''::text CONSTRAINT inventory_stock_movements_operator_id_not_null1 NOT NULL,
    batch_id uuid CONSTRAINT inventory_stock_movements_batch_id_not_null1 NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_stock_movements_created_at_not_null1 NOT NULL,
    unit_cost numeric(12,2),
    CONSTRAINT inventory_stock_movements_after_check CHECK ((quantity_after >= 0)),
    CONSTRAINT inventory_stock_movements_direction_check CHECK ((direction = ANY (ARRAY['in'::text, 'out'::text, 'adjust'::text]))),
    CONSTRAINT inventory_stock_movements_quantity_check CHECK ((quantity > 0))
);

ALTER TABLE ONLY public.inventory_stock_movements_2026_10 FORCE ROW LEVEL SECURITY;


--
-- Name: inventory_stock_movements_2026_11; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.inventory_stock_movements_2026_11 (
    id uuid DEFAULT gen_random_uuid() CONSTRAINT inventory_stock_movements_id_not_null1 NOT NULL,
    project_id uuid CONSTRAINT inventory_stock_movements_project_id_not_null1 NOT NULL,
    warehouse_id uuid CONSTRAINT inventory_stock_movements_warehouse_id_not_null1 NOT NULL,
    product_id uuid CONSTRAINT inventory_stock_movements_product_id_not_null1 NOT NULL,
    variant_id uuid CONSTRAINT inventory_stock_movements_variant_id_not_null1 NOT NULL,
    sku_code text DEFAULT ''::text CONSTRAINT inventory_stock_movements_sku_code_not_null1 NOT NULL,
    direction text CONSTRAINT inventory_stock_movements_direction_not_null1 NOT NULL,
    quantity integer CONSTRAINT inventory_stock_movements_quantity_not_null1 NOT NULL,
    delta integer CONSTRAINT inventory_stock_movements_delta_not_null1 NOT NULL,
    quantity_before integer CONSTRAINT inventory_stock_movements_quantity_before_not_null1 NOT NULL,
    quantity_after integer CONSTRAINT inventory_stock_movements_quantity_after_not_null1 NOT NULL,
    reason_id bigint,
    reason_code text CONSTRAINT inventory_stock_movements_reason_code_not_null1 NOT NULL,
    parent_variant_id uuid,
    source_type text DEFAULT ''::text CONSTRAINT inventory_stock_movements_source_type_not_null1 NOT NULL,
    source_ref text DEFAULT ''::text CONSTRAINT inventory_stock_movements_source_ref_not_null1 NOT NULL,
    remark text DEFAULT ''::text CONSTRAINT inventory_stock_movements_remark_not_null1 NOT NULL,
    operator_id text DEFAULT ''::text CONSTRAINT inventory_stock_movements_operator_id_not_null1 NOT NULL,
    batch_id uuid CONSTRAINT inventory_stock_movements_batch_id_not_null1 NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_stock_movements_created_at_not_null1 NOT NULL,
    unit_cost numeric(12,2),
    CONSTRAINT inventory_stock_movements_after_check CHECK ((quantity_after >= 0)),
    CONSTRAINT inventory_stock_movements_direction_check CHECK ((direction = ANY (ARRAY['in'::text, 'out'::text, 'adjust'::text]))),
    CONSTRAINT inventory_stock_movements_quantity_check CHECK ((quantity > 0))
);

ALTER TABLE ONLY public.inventory_stock_movements_2026_11 FORCE ROW LEVEL SECURITY;


--
-- Name: inventory_stock_movements_2026_12; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.inventory_stock_movements_2026_12 (
    id uuid DEFAULT gen_random_uuid() CONSTRAINT inventory_stock_movements_id_not_null1 NOT NULL,
    project_id uuid CONSTRAINT inventory_stock_movements_project_id_not_null1 NOT NULL,
    warehouse_id uuid CONSTRAINT inventory_stock_movements_warehouse_id_not_null1 NOT NULL,
    product_id uuid CONSTRAINT inventory_stock_movements_product_id_not_null1 NOT NULL,
    variant_id uuid CONSTRAINT inventory_stock_movements_variant_id_not_null1 NOT NULL,
    sku_code text DEFAULT ''::text CONSTRAINT inventory_stock_movements_sku_code_not_null1 NOT NULL,
    direction text CONSTRAINT inventory_stock_movements_direction_not_null1 NOT NULL,
    quantity integer CONSTRAINT inventory_stock_movements_quantity_not_null1 NOT NULL,
    delta integer CONSTRAINT inventory_stock_movements_delta_not_null1 NOT NULL,
    quantity_before integer CONSTRAINT inventory_stock_movements_quantity_before_not_null1 NOT NULL,
    quantity_after integer CONSTRAINT inventory_stock_movements_quantity_after_not_null1 NOT NULL,
    reason_id bigint,
    reason_code text CONSTRAINT inventory_stock_movements_reason_code_not_null1 NOT NULL,
    parent_variant_id uuid,
    source_type text DEFAULT ''::text CONSTRAINT inventory_stock_movements_source_type_not_null1 NOT NULL,
    source_ref text DEFAULT ''::text CONSTRAINT inventory_stock_movements_source_ref_not_null1 NOT NULL,
    remark text DEFAULT ''::text CONSTRAINT inventory_stock_movements_remark_not_null1 NOT NULL,
    operator_id text DEFAULT ''::text CONSTRAINT inventory_stock_movements_operator_id_not_null1 NOT NULL,
    batch_id uuid CONSTRAINT inventory_stock_movements_batch_id_not_null1 NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_stock_movements_created_at_not_null1 NOT NULL,
    unit_cost numeric(12,2),
    CONSTRAINT inventory_stock_movements_after_check CHECK ((quantity_after >= 0)),
    CONSTRAINT inventory_stock_movements_direction_check CHECK ((direction = ANY (ARRAY['in'::text, 'out'::text, 'adjust'::text]))),
    CONSTRAINT inventory_stock_movements_quantity_check CHECK ((quantity > 0))
);

ALTER TABLE ONLY public.inventory_stock_movements_2026_12 FORCE ROW LEVEL SECURITY;


--
-- Name: inventory_stock_movements_2027_01; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.inventory_stock_movements_2027_01 (
    id uuid DEFAULT gen_random_uuid() CONSTRAINT inventory_stock_movements_id_not_null1 NOT NULL,
    project_id uuid CONSTRAINT inventory_stock_movements_project_id_not_null1 NOT NULL,
    warehouse_id uuid CONSTRAINT inventory_stock_movements_warehouse_id_not_null1 NOT NULL,
    product_id uuid CONSTRAINT inventory_stock_movements_product_id_not_null1 NOT NULL,
    variant_id uuid CONSTRAINT inventory_stock_movements_variant_id_not_null1 NOT NULL,
    sku_code text DEFAULT ''::text CONSTRAINT inventory_stock_movements_sku_code_not_null1 NOT NULL,
    direction text CONSTRAINT inventory_stock_movements_direction_not_null1 NOT NULL,
    quantity integer CONSTRAINT inventory_stock_movements_quantity_not_null1 NOT NULL,
    delta integer CONSTRAINT inventory_stock_movements_delta_not_null1 NOT NULL,
    quantity_before integer CONSTRAINT inventory_stock_movements_quantity_before_not_null1 NOT NULL,
    quantity_after integer CONSTRAINT inventory_stock_movements_quantity_after_not_null1 NOT NULL,
    reason_id bigint,
    reason_code text CONSTRAINT inventory_stock_movements_reason_code_not_null1 NOT NULL,
    parent_variant_id uuid,
    source_type text DEFAULT ''::text CONSTRAINT inventory_stock_movements_source_type_not_null1 NOT NULL,
    source_ref text DEFAULT ''::text CONSTRAINT inventory_stock_movements_source_ref_not_null1 NOT NULL,
    remark text DEFAULT ''::text CONSTRAINT inventory_stock_movements_remark_not_null1 NOT NULL,
    operator_id text DEFAULT ''::text CONSTRAINT inventory_stock_movements_operator_id_not_null1 NOT NULL,
    batch_id uuid CONSTRAINT inventory_stock_movements_batch_id_not_null1 NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_stock_movements_created_at_not_null1 NOT NULL,
    unit_cost numeric(12,2),
    CONSTRAINT inventory_stock_movements_after_check CHECK ((quantity_after >= 0)),
    CONSTRAINT inventory_stock_movements_direction_check CHECK ((direction = ANY (ARRAY['in'::text, 'out'::text, 'adjust'::text]))),
    CONSTRAINT inventory_stock_movements_quantity_check CHECK ((quantity > 0))
);

ALTER TABLE ONLY public.inventory_stock_movements_2027_01 FORCE ROW LEVEL SECURITY;


--
-- Name: inventory_stock_movements_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.inventory_stock_movements_default (
    id uuid DEFAULT gen_random_uuid() CONSTRAINT inventory_stock_movements_id_not_null1 NOT NULL,
    project_id uuid CONSTRAINT inventory_stock_movements_project_id_not_null1 NOT NULL,
    warehouse_id uuid CONSTRAINT inventory_stock_movements_warehouse_id_not_null1 NOT NULL,
    product_id uuid CONSTRAINT inventory_stock_movements_product_id_not_null1 NOT NULL,
    variant_id uuid CONSTRAINT inventory_stock_movements_variant_id_not_null1 NOT NULL,
    sku_code text DEFAULT ''::text CONSTRAINT inventory_stock_movements_sku_code_not_null1 NOT NULL,
    direction text CONSTRAINT inventory_stock_movements_direction_not_null1 NOT NULL,
    quantity integer CONSTRAINT inventory_stock_movements_quantity_not_null1 NOT NULL,
    delta integer CONSTRAINT inventory_stock_movements_delta_not_null1 NOT NULL,
    quantity_before integer CONSTRAINT inventory_stock_movements_quantity_before_not_null1 NOT NULL,
    quantity_after integer CONSTRAINT inventory_stock_movements_quantity_after_not_null1 NOT NULL,
    reason_id bigint,
    reason_code text CONSTRAINT inventory_stock_movements_reason_code_not_null1 NOT NULL,
    parent_variant_id uuid,
    source_type text DEFAULT ''::text CONSTRAINT inventory_stock_movements_source_type_not_null1 NOT NULL,
    source_ref text DEFAULT ''::text CONSTRAINT inventory_stock_movements_source_ref_not_null1 NOT NULL,
    remark text DEFAULT ''::text CONSTRAINT inventory_stock_movements_remark_not_null1 NOT NULL,
    operator_id text DEFAULT ''::text CONSTRAINT inventory_stock_movements_operator_id_not_null1 NOT NULL,
    batch_id uuid CONSTRAINT inventory_stock_movements_batch_id_not_null1 NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_stock_movements_created_at_not_null1 NOT NULL,
    unit_cost numeric(12,2),
    CONSTRAINT inventory_stock_movements_after_check CHECK ((quantity_after >= 0)),
    CONSTRAINT inventory_stock_movements_direction_check CHECK ((direction = ANY (ARRAY['in'::text, 'out'::text, 'adjust'::text]))),
    CONSTRAINT inventory_stock_movements_quantity_check CHECK ((quantity > 0))
);

ALTER TABLE ONLY public.inventory_stock_movements_default FORCE ROW LEVEL SECURITY;


--
-- Name: inventory_stocks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.inventory_stocks (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    warehouse_id uuid NOT NULL,
    product_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    sku_code text DEFAULT ''::text NOT NULL,
    quantity integer DEFAULT 0 NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_stocks_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_stocks_updated_at_not_null NOT NULL,
    cost_price numeric(12,2),
    external_sku text DEFAULT ''::text NOT NULL,
    track_quantity boolean DEFAULT false NOT NULL,
    CONSTRAINT inventory_stocks_track_quantity_check CHECK ((track_quantity OR (quantity = 0)))
);

ALTER TABLE ONLY public.inventory_stocks FORCE ROW LEVEL SECURITY;


--
-- Name: inventory_warehouses; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.inventory_warehouses (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    code text NOT NULL,
    name text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    is_default boolean DEFAULT false NOT NULL,
    sort integer DEFAULT 0 NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_warehouses_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT inventory_warehouses_updated_at_not_null NOT NULL,
    type text DEFAULT 'self'::text NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    CONSTRAINT inventory_warehouses_status_check CHECK ((status = ANY (ARRAY['active'::text, 'disabled'::text]))),
    CONSTRAINT inventory_warehouses_type_check CHECK ((type = ANY (ARRAY['self'::text, 'third_party'::text, 'virtual'::text])))
);

ALTER TABLE ONLY public.inventory_warehouses FORCE ROW LEVEL SECURITY;


--
-- Name: mail_accounts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.mail_accounts (
    id bigint NOT NULL,
    name character varying(64) NOT NULL,
    purpose character varying(16) DEFAULT 'transactional'::character varying NOT NULL,
    is_default boolean DEFAULT false NOT NULL,
    from_name character varying(100),
    from_email character varying(254) NOT NULL,
    reply_to character varying(254),
    provider character varying(32) DEFAULT 'smtp'::character varying NOT NULL,
    host character varying(255),
    port integer,
    username character varying(255),
    password_cipher text,
    encryption character varying(16) DEFAULT 'starttls'::character varying NOT NULL,
    rate_per_hour integer DEFAULT 0 NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    last_check_at timestamp with time zone,
    last_check_error text,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone
);


--
-- Name: mail_accounts_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.mail_accounts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: mail_accounts_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.mail_accounts_id_seq OWNED BY public.mail_accounts.id;


--
-- Name: mail_automation_node_logs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.mail_automation_node_logs (
    id bigint NOT NULL,
    run_id bigint NOT NULL,
    node_key character varying(64) NOT NULL,
    node_type character varying(32) NOT NULL,
    status character varying(16) NOT NULL,
    detail text,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: mail_automation_node_logs_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.mail_automation_node_logs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: mail_automation_node_logs_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.mail_automation_node_logs_id_seq OWNED BY public.mail_automation_node_logs.id;


--
-- Name: mail_automation_runs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.mail_automation_runs (
    id bigint NOT NULL,
    automation_id bigint NOT NULL,
    automation_version integer DEFAULT 1 NOT NULL,
    contact_id bigint NOT NULL,
    status character varying(16) DEFAULT 'running'::character varying NOT NULL,
    current_node character varying(64),
    next_run_at timestamp with time zone,
    error_message text,
    trigger_event character varying(32),
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone
);


--
-- Name: mail_automation_runs_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.mail_automation_runs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: mail_automation_runs_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.mail_automation_runs_id_seq OWNED BY public.mail_automation_runs.id;


--
-- Name: mail_automations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.mail_automations (
    id bigint NOT NULL,
    name character varying(150) NOT NULL,
    description character varying(500),
    trigger_type character varying(32) DEFAULT 'manual'::character varying NOT NULL,
    trigger_params jsonb,
    definition jsonb NOT NULL,
    status character varying(16) DEFAULT 'draft'::character varying NOT NULL,
    version integer DEFAULT 1 NOT NULL,
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone
);


--
-- Name: mail_automations_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.mail_automations_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: mail_automations_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.mail_automations_id_seq OWNED BY public.mail_automations.id;


--
-- Name: mail_campaign_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.mail_campaign_events (
    id bigint NOT NULL,
    campaign_id bigint NOT NULL,
    contact_id bigint NOT NULL,
    event_type character varying(16) NOT NULL,
    url character varying(1000),
    ip character varying(50),
    user_agent character varying(255),
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: mail_campaign_events_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.mail_campaign_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: mail_campaign_events_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.mail_campaign_events_id_seq OWNED BY public.mail_campaign_events.id;


--
-- Name: mail_campaigns; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.mail_campaigns (
    id bigint NOT NULL,
    name character varying(150) NOT NULL,
    account_id bigint NOT NULL,
    template_id bigint NOT NULL,
    target_tags text[] DEFAULT '{}'::text[] NOT NULL,
    subject character varying(255) NOT NULL,
    variables jsonb,
    status character varying(16) DEFAULT 'draft'::character varying NOT NULL,
    scheduled_at timestamp with time zone,
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    total_count integer DEFAULT 0 NOT NULL,
    sent_count integer DEFAULT 0 NOT NULL,
    failed_count integer DEFAULT 0 NOT NULL,
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone,
    open_count integer DEFAULT 0 NOT NULL,
    click_count integer DEFAULT 0 NOT NULL
);


--
-- Name: mail_campaigns_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.mail_campaigns_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: mail_campaigns_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.mail_campaigns_id_seq OWNED BY public.mail_campaigns.id;


--
-- Name: mail_contacts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.mail_contacts (
    id bigint NOT NULL,
    email character varying(254) NOT NULL,
    name character varying(128),
    user_id bigint,
    source character varying(16) DEFAULT 'manual'::character varying NOT NULL,
    status character varying(16) DEFAULT 'pending'::character varying NOT NULL,
    subscribed_at timestamp with time zone,
    consent_source character varying(64),
    tags text[] DEFAULT '{}'::text[] NOT NULL,
    attributes jsonb,
    last_activity_at timestamp with time zone,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone
);


--
-- Name: mail_contacts_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.mail_contacts_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: mail_contacts_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.mail_contacts_id_seq OWNED BY public.mail_contacts.id;


--
-- Name: mail_logs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.mail_logs (
    id bigint NOT NULL,
    account_id bigint,
    template_key character varying(64),
    to_email character varying(254) NOT NULL,
    subject character varying(255),
    status character varying(16) DEFAULT 'pending'::character varying NOT NULL,
    provider character varying(32),
    provider_msg_id character varying(191),
    error_kind character varying(16),
    error_message text,
    retry_count integer DEFAULT 0 NOT NULL,
    sent_at timestamp with time zone,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    campaign_id bigint,
    contact_id bigint
);


--
-- Name: mail_logs_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.mail_logs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: mail_logs_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.mail_logs_id_seq OWNED BY public.mail_logs.id;


--
-- Name: mail_suppressions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.mail_suppressions (
    id bigint NOT NULL,
    email character varying(254) NOT NULL,
    reason character varying(16) NOT NULL,
    source character varying(64),
    note character varying(255),
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: mail_suppressions_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.mail_suppressions_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: mail_suppressions_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.mail_suppressions_id_seq OWNED BY public.mail_suppressions.id;


--
-- Name: mail_templates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.mail_templates (
    id bigint NOT NULL,
    template_key character varying(64) NOT NULL,
    locale character varying(16) DEFAULT ''::character varying NOT NULL,
    name character varying(100) NOT NULL,
    subject character varying(255) NOT NULL,
    body_html text DEFAULT ''::text NOT NULL,
    body_text text DEFAULT ''::text NOT NULL,
    variables text[] DEFAULT '{}'::text[] NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone
);


--
-- Name: mail_templates_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.mail_templates_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: mail_templates_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.mail_templates_id_seq OWNED BY public.mail_templates.id;


--
-- Name: master_data_changes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.master_data_changes (
    id uuid DEFAULT gen_random_uuid() CONSTRAINT master_data_changes_id_not_null1 NOT NULL,
    project_id uuid CONSTRAINT master_data_changes_project_id_not_null1 NOT NULL,
    entity_type text CONSTRAINT master_data_changes_entity_type_not_null1 NOT NULL,
    entity_id uuid CONSTRAINT master_data_changes_entity_id_not_null1 NOT NULL,
    entity_label text DEFAULT ''::text CONSTRAINT master_data_changes_entity_label_not_null1 NOT NULL,
    action text CONSTRAINT master_data_changes_action_not_null1 NOT NULL,
    field text CONSTRAINT master_data_changes_field_not_null1 NOT NULL,
    old_value text DEFAULT ''::text CONSTRAINT master_data_changes_old_value_not_null1 NOT NULL,
    new_value text DEFAULT ''::text CONSTRAINT master_data_changes_new_value_not_null1 NOT NULL,
    origin text DEFAULT ''::text CONSTRAINT master_data_changes_origin_not_null1 NOT NULL,
    operator_id text DEFAULT ''::text CONSTRAINT master_data_changes_operator_id_not_null1 NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT master_data_changes_created_at_not_null1 NOT NULL,
    CONSTRAINT master_data_changes_action_check CHECK ((action = ANY (ARRAY['create'::text, 'update'::text, 'delete'::text]))),
    CONSTRAINT master_data_changes_entity_type_check CHECK ((entity_type = ANY (ARRAY['product'::text, 'product_variant'::text, 'inventory_source'::text]))),
    CONSTRAINT master_data_changes_field_check CHECK ((field <> ''::text))
)
PARTITION BY RANGE (create_time);

ALTER TABLE ONLY public.master_data_changes FORCE ROW LEVEL SECURITY;


--
-- Name: master_data_changes_2026_09; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.master_data_changes_2026_09 (
    id uuid DEFAULT gen_random_uuid() CONSTRAINT master_data_changes_id_not_null1 NOT NULL,
    project_id uuid CONSTRAINT master_data_changes_project_id_not_null1 NOT NULL,
    entity_type text CONSTRAINT master_data_changes_entity_type_not_null1 NOT NULL,
    entity_id uuid CONSTRAINT master_data_changes_entity_id_not_null1 NOT NULL,
    entity_label text DEFAULT ''::text CONSTRAINT master_data_changes_entity_label_not_null1 NOT NULL,
    action text CONSTRAINT master_data_changes_action_not_null1 NOT NULL,
    field text CONSTRAINT master_data_changes_field_not_null1 NOT NULL,
    old_value text DEFAULT ''::text CONSTRAINT master_data_changes_old_value_not_null1 NOT NULL,
    new_value text DEFAULT ''::text CONSTRAINT master_data_changes_new_value_not_null1 NOT NULL,
    origin text DEFAULT ''::text CONSTRAINT master_data_changes_origin_not_null1 NOT NULL,
    operator_id text DEFAULT ''::text CONSTRAINT master_data_changes_operator_id_not_null1 NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT master_data_changes_created_at_not_null1 NOT NULL,
    CONSTRAINT master_data_changes_action_check CHECK ((action = ANY (ARRAY['create'::text, 'update'::text, 'delete'::text]))),
    CONSTRAINT master_data_changes_entity_type_check CHECK ((entity_type = ANY (ARRAY['product'::text, 'product_variant'::text, 'inventory_source'::text]))),
    CONSTRAINT master_data_changes_field_check CHECK ((field <> ''::text))
);

ALTER TABLE ONLY public.master_data_changes_2026_09 FORCE ROW LEVEL SECURITY;


--
-- Name: master_data_changes_2026_10; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.master_data_changes_2026_10 (
    id uuid DEFAULT gen_random_uuid() CONSTRAINT master_data_changes_id_not_null1 NOT NULL,
    project_id uuid CONSTRAINT master_data_changes_project_id_not_null1 NOT NULL,
    entity_type text CONSTRAINT master_data_changes_entity_type_not_null1 NOT NULL,
    entity_id uuid CONSTRAINT master_data_changes_entity_id_not_null1 NOT NULL,
    entity_label text DEFAULT ''::text CONSTRAINT master_data_changes_entity_label_not_null1 NOT NULL,
    action text CONSTRAINT master_data_changes_action_not_null1 NOT NULL,
    field text CONSTRAINT master_data_changes_field_not_null1 NOT NULL,
    old_value text DEFAULT ''::text CONSTRAINT master_data_changes_old_value_not_null1 NOT NULL,
    new_value text DEFAULT ''::text CONSTRAINT master_data_changes_new_value_not_null1 NOT NULL,
    origin text DEFAULT ''::text CONSTRAINT master_data_changes_origin_not_null1 NOT NULL,
    operator_id text DEFAULT ''::text CONSTRAINT master_data_changes_operator_id_not_null1 NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT master_data_changes_created_at_not_null1 NOT NULL,
    CONSTRAINT master_data_changes_action_check CHECK ((action = ANY (ARRAY['create'::text, 'update'::text, 'delete'::text]))),
    CONSTRAINT master_data_changes_entity_type_check CHECK ((entity_type = ANY (ARRAY['product'::text, 'product_variant'::text, 'inventory_source'::text]))),
    CONSTRAINT master_data_changes_field_check CHECK ((field <> ''::text))
);

ALTER TABLE ONLY public.master_data_changes_2026_10 FORCE ROW LEVEL SECURITY;


--
-- Name: master_data_changes_2026_11; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.master_data_changes_2026_11 (
    id uuid DEFAULT gen_random_uuid() CONSTRAINT master_data_changes_id_not_null1 NOT NULL,
    project_id uuid CONSTRAINT master_data_changes_project_id_not_null1 NOT NULL,
    entity_type text CONSTRAINT master_data_changes_entity_type_not_null1 NOT NULL,
    entity_id uuid CONSTRAINT master_data_changes_entity_id_not_null1 NOT NULL,
    entity_label text DEFAULT ''::text CONSTRAINT master_data_changes_entity_label_not_null1 NOT NULL,
    action text CONSTRAINT master_data_changes_action_not_null1 NOT NULL,
    field text CONSTRAINT master_data_changes_field_not_null1 NOT NULL,
    old_value text DEFAULT ''::text CONSTRAINT master_data_changes_old_value_not_null1 NOT NULL,
    new_value text DEFAULT ''::text CONSTRAINT master_data_changes_new_value_not_null1 NOT NULL,
    origin text DEFAULT ''::text CONSTRAINT master_data_changes_origin_not_null1 NOT NULL,
    operator_id text DEFAULT ''::text CONSTRAINT master_data_changes_operator_id_not_null1 NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT master_data_changes_created_at_not_null1 NOT NULL,
    CONSTRAINT master_data_changes_action_check CHECK ((action = ANY (ARRAY['create'::text, 'update'::text, 'delete'::text]))),
    CONSTRAINT master_data_changes_entity_type_check CHECK ((entity_type = ANY (ARRAY['product'::text, 'product_variant'::text, 'inventory_source'::text]))),
    CONSTRAINT master_data_changes_field_check CHECK ((field <> ''::text))
);

ALTER TABLE ONLY public.master_data_changes_2026_11 FORCE ROW LEVEL SECURITY;


--
-- Name: master_data_changes_2026_12; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.master_data_changes_2026_12 (
    id uuid DEFAULT gen_random_uuid() CONSTRAINT master_data_changes_id_not_null1 NOT NULL,
    project_id uuid CONSTRAINT master_data_changes_project_id_not_null1 NOT NULL,
    entity_type text CONSTRAINT master_data_changes_entity_type_not_null1 NOT NULL,
    entity_id uuid CONSTRAINT master_data_changes_entity_id_not_null1 NOT NULL,
    entity_label text DEFAULT ''::text CONSTRAINT master_data_changes_entity_label_not_null1 NOT NULL,
    action text CONSTRAINT master_data_changes_action_not_null1 NOT NULL,
    field text CONSTRAINT master_data_changes_field_not_null1 NOT NULL,
    old_value text DEFAULT ''::text CONSTRAINT master_data_changes_old_value_not_null1 NOT NULL,
    new_value text DEFAULT ''::text CONSTRAINT master_data_changes_new_value_not_null1 NOT NULL,
    origin text DEFAULT ''::text CONSTRAINT master_data_changes_origin_not_null1 NOT NULL,
    operator_id text DEFAULT ''::text CONSTRAINT master_data_changes_operator_id_not_null1 NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT master_data_changes_created_at_not_null1 NOT NULL,
    CONSTRAINT master_data_changes_action_check CHECK ((action = ANY (ARRAY['create'::text, 'update'::text, 'delete'::text]))),
    CONSTRAINT master_data_changes_entity_type_check CHECK ((entity_type = ANY (ARRAY['product'::text, 'product_variant'::text, 'inventory_source'::text]))),
    CONSTRAINT master_data_changes_field_check CHECK ((field <> ''::text))
);

ALTER TABLE ONLY public.master_data_changes_2026_12 FORCE ROW LEVEL SECURITY;


--
-- Name: master_data_changes_2027_01; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.master_data_changes_2027_01 (
    id uuid DEFAULT gen_random_uuid() CONSTRAINT master_data_changes_id_not_null1 NOT NULL,
    project_id uuid CONSTRAINT master_data_changes_project_id_not_null1 NOT NULL,
    entity_type text CONSTRAINT master_data_changes_entity_type_not_null1 NOT NULL,
    entity_id uuid CONSTRAINT master_data_changes_entity_id_not_null1 NOT NULL,
    entity_label text DEFAULT ''::text CONSTRAINT master_data_changes_entity_label_not_null1 NOT NULL,
    action text CONSTRAINT master_data_changes_action_not_null1 NOT NULL,
    field text CONSTRAINT master_data_changes_field_not_null1 NOT NULL,
    old_value text DEFAULT ''::text CONSTRAINT master_data_changes_old_value_not_null1 NOT NULL,
    new_value text DEFAULT ''::text CONSTRAINT master_data_changes_new_value_not_null1 NOT NULL,
    origin text DEFAULT ''::text CONSTRAINT master_data_changes_origin_not_null1 NOT NULL,
    operator_id text DEFAULT ''::text CONSTRAINT master_data_changes_operator_id_not_null1 NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT master_data_changes_created_at_not_null1 NOT NULL,
    CONSTRAINT master_data_changes_action_check CHECK ((action = ANY (ARRAY['create'::text, 'update'::text, 'delete'::text]))),
    CONSTRAINT master_data_changes_entity_type_check CHECK ((entity_type = ANY (ARRAY['product'::text, 'product_variant'::text, 'inventory_source'::text]))),
    CONSTRAINT master_data_changes_field_check CHECK ((field <> ''::text))
);

ALTER TABLE ONLY public.master_data_changes_2027_01 FORCE ROW LEVEL SECURITY;


--
-- Name: master_data_changes_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.master_data_changes_default (
    id uuid DEFAULT gen_random_uuid() CONSTRAINT master_data_changes_id_not_null1 NOT NULL,
    project_id uuid CONSTRAINT master_data_changes_project_id_not_null1 NOT NULL,
    entity_type text CONSTRAINT master_data_changes_entity_type_not_null1 NOT NULL,
    entity_id uuid CONSTRAINT master_data_changes_entity_id_not_null1 NOT NULL,
    entity_label text DEFAULT ''::text CONSTRAINT master_data_changes_entity_label_not_null1 NOT NULL,
    action text CONSTRAINT master_data_changes_action_not_null1 NOT NULL,
    field text CONSTRAINT master_data_changes_field_not_null1 NOT NULL,
    old_value text DEFAULT ''::text CONSTRAINT master_data_changes_old_value_not_null1 NOT NULL,
    new_value text DEFAULT ''::text CONSTRAINT master_data_changes_new_value_not_null1 NOT NULL,
    origin text DEFAULT ''::text CONSTRAINT master_data_changes_origin_not_null1 NOT NULL,
    operator_id text DEFAULT ''::text CONSTRAINT master_data_changes_operator_id_not_null1 NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT master_data_changes_created_at_not_null1 NOT NULL,
    CONSTRAINT master_data_changes_action_check CHECK ((action = ANY (ARRAY['create'::text, 'update'::text, 'delete'::text]))),
    CONSTRAINT master_data_changes_entity_type_check CHECK ((entity_type = ANY (ARRAY['product'::text, 'product_variant'::text, 'inventory_source'::text]))),
    CONSTRAINT master_data_changes_field_check CHECK ((field <> ''::text))
);

ALTER TABLE ONLY public.master_data_changes_default FORCE ROW LEVEL SECURITY;


--
-- Name: membership_assignments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.membership_assignments (
    id bigint NOT NULL,
    project_id uuid NOT NULL,
    user_id bigint NOT NULL,
    tier_id bigint NOT NULL,
    source character varying(16) NOT NULL,
    assigned_at timestamp with time zone DEFAULT now() NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_membership_assignments_source CHECK (((source)::text = ANY ((ARRAY['auto'::character varying, 'manual'::character varying])::text[])))
);

ALTER TABLE ONLY public.membership_assignments FORCE ROW LEVEL SECURITY;


--
-- Name: membership_assignments_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.membership_assignments_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: membership_assignments_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.membership_assignments_id_seq OWNED BY public.membership_assignments.id;


--
-- Name: membership_entitlements; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.membership_entitlements (
    id bigint NOT NULL,
    tier_id bigint NOT NULL,
    kind character varying(32) NOT NULL,
    value_int bigint NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_membership_entitlements_value CHECK (((((kind)::text = 'free_shipping'::text) AND (value_int = ANY (ARRAY[(0)::bigint, (1)::bigint]))) OR (((kind)::text = 'discount'::text) AND ((value_int >= 1) AND (value_int <= 100)))))
);


--
-- Name: membership_entitlements_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.membership_entitlements_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: membership_entitlements_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.membership_entitlements_id_seq OWNED BY public.membership_entitlements.id;


--
-- Name: membership_tiers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.membership_tiers (
    id bigint NOT NULL,
    project_id uuid NOT NULL,
    name character varying(60) NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    threshold_amount bigint DEFAULT 0 NOT NULL,
    is_default boolean DEFAULT false NOT NULL,
    remark character varying(200) DEFAULT ''::character varying NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);

ALTER TABLE ONLY public.membership_tiers FORCE ROW LEVEL SECURITY;


--
-- Name: membership_tiers_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.membership_tiers_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: membership_tiers_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.membership_tiers_id_seq OWNED BY public.membership_tiers.id;


--
-- Name: navigations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.navigations (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    title text NOT NULL,
    path text NOT NULL,
    kind text DEFAULT 'header'::text NOT NULL,
    parent_id uuid,
    sort_order integer DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT navigations_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT navigations_updated_at_not_null NOT NULL,
    source_type text DEFAULT 'custom'::text NOT NULL,
    source_id uuid,
    target text DEFAULT 'self'::text NOT NULL,
    panel_block_id uuid,
    panel_width text DEFAULT 'auto'::text NOT NULL,
    CONSTRAINT navigations_kind_check CHECK ((kind = ANY (ARRAY['header'::text, 'header_mobile'::text, 'footer'::text, 'footer_mobile'::text]))),
    CONSTRAINT navigations_panel_width_check CHECK ((panel_width = ANY (ARRAY['auto'::text, 'full'::text]))),
    CONSTRAINT navigations_source_type_check CHECK ((source_type = ANY (ARRAY['custom'::text, 'page'::text, 'article'::text, 'product'::text, 'category'::text, 'block'::text]))),
    CONSTRAINT navigations_target_check CHECK ((target = ANY (ARRAY['self'::text, 'blank'::text])))
);

ALTER TABLE ONLY public.navigations FORCE ROW LEVEL SECURITY;


--
-- Name: order_items; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.order_items (
    id bigint NOT NULL,
    order_id bigint NOT NULL,
    product_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    product_name character varying(200) DEFAULT ''::character varying NOT NULL,
    variant_label character varying(200) DEFAULT ''::character varying NOT NULL,
    sku character varying(80) DEFAULT ''::character varying NOT NULL,
    unit_price bigint DEFAULT 0 NOT NULL,
    quantity integer DEFAULT 0 NOT NULL,
    line_subtotal bigint DEFAULT 0 NOT NULL,
    line_discount bigint DEFAULT 0 NOT NULL,
    line_tax bigint DEFAULT 0 NOT NULL,
    line_total bigint DEFAULT 0 NOT NULL,
    cost_price bigint,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: order_items_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.order_items_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: order_items_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.order_items_id_seq OWNED BY public.order_items.id;


--
-- Name: order_return_items; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.order_return_items (
    id bigint NOT NULL,
    return_id bigint NOT NULL,
    order_item_id bigint NOT NULL,
    product_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    product_name character varying(200) DEFAULT ''::character varying NOT NULL,
    variant_label character varying(200) DEFAULT ''::character varying NOT NULL,
    sku character varying(80) DEFAULT ''::character varying NOT NULL,
    unit_price bigint DEFAULT 0 NOT NULL,
    quantity integer NOT NULL,
    received_quantity integer DEFAULT 0 NOT NULL,
    refund_amount bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_order_return_items_amount CHECK (((refund_amount >= 0) AND (unit_price >= 0))),
    CONSTRAINT ck_order_return_items_qty CHECK (((quantity > 0) AND (received_quantity >= 0) AND (received_quantity <= quantity)))
);


--
-- Name: order_return_items_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.order_return_items_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: order_return_items_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.order_return_items_id_seq OWNED BY public.order_return_items.id;


--
-- Name: order_returns; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.order_returns (
    id bigint NOT NULL,
    project_id uuid NOT NULL,
    order_id bigint NOT NULL,
    order_no character varying(40) DEFAULT ''::character varying NOT NULL,
    return_no character varying(40) NOT NULL,
    status character varying(20) DEFAULT 'requested'::character varying NOT NULL,
    reason character varying(255) DEFAULT ''::character varying NOT NULL,
    refund_amount bigint DEFAULT 0 NOT NULL,
    user_id bigint,
    customer_email character varying(120) DEFAULT ''::character varying NOT NULL,
    customer_name character varying(60) DEFAULT ''::character varying NOT NULL,
    admin_note character varying(500) DEFAULT ''::character varying NOT NULL,
    reviewer_id bigint DEFAULT 0 NOT NULL,
    reviewer_name character varying(60) DEFAULT ''::character varying NOT NULL,
    reviewed_at timestamp with time zone,
    received_at timestamp with time zone,
    refunded_at timestamp with time zone,
    transaction_id character varying(120) DEFAULT ''::character varying NOT NULL,
    request_id character varying(64) DEFAULT ''::character varying NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_order_returns_amount CHECK ((refund_amount >= 0)),
    CONSTRAINT ck_order_returns_status CHECK (((status)::text = ANY ((ARRAY['requested'::character varying, 'approved'::character varying, 'received'::character varying, 'completed'::character varying, 'rejected'::character varying, 'cancelled'::character varying])::text[])))
);

ALTER TABLE ONLY public.order_returns FORCE ROW LEVEL SECURITY;


--
-- Name: order_returns_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.order_returns_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: order_returns_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.order_returns_id_seq OWNED BY public.order_returns.id;


--
-- Name: order_status_logs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.order_status_logs (
    id bigint NOT NULL,
    order_id bigint NOT NULL,
    from_status character varying(20) DEFAULT ''::character varying NOT NULL,
    to_status character varying(20) DEFAULT ''::character varying NOT NULL,
    operator_type character varying(20) DEFAULT 'system'::character varying NOT NULL,
    operator_id bigint DEFAULT 0 NOT NULL,
    operator_name character varying(60) DEFAULT ''::character varying NOT NULL,
    remark character varying(255) DEFAULT ''::character varying NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: order_status_logs_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.order_status_logs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: order_status_logs_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.order_status_logs_id_seq OWNED BY public.order_status_logs.id;


--
-- Name: orders; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.orders (
    id bigint NOT NULL,
    project_id uuid NOT NULL,
    order_no character varying(40) NOT NULL,
    status character varying(20) DEFAULT 'pending'::character varying NOT NULL,
    user_id bigint,
    customer_email character varying(120) DEFAULT ''::character varying NOT NULL,
    customer_name character varying(60) DEFAULT ''::character varying NOT NULL,
    customer_phone character varying(40) DEFAULT ''::character varying NOT NULL,
    currency character varying(8) DEFAULT 'CNY'::character varying NOT NULL,
    subtotal bigint DEFAULT 0 NOT NULL,
    discount_total bigint DEFAULT 0 NOT NULL,
    shipping_total bigint DEFAULT 0 NOT NULL,
    tax_total bigint DEFAULT 0 NOT NULL,
    total bigint DEFAULT 0 NOT NULL,
    ship_name character varying(60) DEFAULT ''::character varying NOT NULL,
    ship_phone character varying(40) DEFAULT ''::character varying NOT NULL,
    ship_province character varying(40) DEFAULT ''::character varying NOT NULL,
    ship_city character varying(40) DEFAULT ''::character varying NOT NULL,
    ship_district character varying(40) DEFAULT ''::character varying NOT NULL,
    ship_address character varying(255) DEFAULT ''::character varying NOT NULL,
    ship_zip character varying(20) DEFAULT ''::character varying NOT NULL,
    bill_name character varying(60) DEFAULT ''::character varying NOT NULL,
    bill_phone character varying(40) DEFAULT ''::character varying NOT NULL,
    bill_province character varying(40) DEFAULT ''::character varying NOT NULL,
    bill_city character varying(40) DEFAULT ''::character varying NOT NULL,
    bill_district character varying(40) DEFAULT ''::character varying NOT NULL,
    bill_address character varying(255) DEFAULT ''::character varying NOT NULL,
    bill_zip character varying(20) DEFAULT ''::character varying NOT NULL,
    payment_method character varying(40) DEFAULT ''::character varying NOT NULL,
    payment_method_title character varying(60) DEFAULT ''::character varying NOT NULL,
    transaction_id character varying(120) DEFAULT ''::character varying NOT NULL,
    paid_at timestamp with time zone,
    completed_at timestamp with time zone,
    created_via character varying(20) DEFAULT 'checkout'::character varying NOT NULL,
    ip_address character varying(50) DEFAULT ''::character varying NOT NULL,
    user_agent character varying(255) DEFAULT ''::character varying NOT NULL,
    attribution jsonb DEFAULT '{}'::jsonb NOT NULL,
    admin_note character varying(500) DEFAULT ''::character varying NOT NULL,
    request_id character varying(64) DEFAULT ''::character varying NOT NULL,
    remark character varying(255) DEFAULT ''::character varying NOT NULL,
    cancel_reason character varying(255) DEFAULT ''::character varying NOT NULL,
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    membership_discount_total bigint DEFAULT 0 NOT NULL,
    ship_country character varying(2) DEFAULT ''::character varying NOT NULL,
    bill_country character varying(2) DEFAULT ''::character varying NOT NULL,
    CONSTRAINT orders_status_check CHECK (((status)::text = ANY ((ARRAY['pending'::character varying, 'paid'::character varying, 'shipped'::character varying, 'completed'::character varying, 'cancelled'::character varying, 'refunded'::character varying])::text[])))
);

ALTER TABLE ONLY public.orders FORCE ROW LEVEL SECURITY;


--
-- Name: orders_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.orders_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: orders_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.orders_id_seq OWNED BY public.orders.id;


--
-- Name: page_artifact_objects; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.page_artifact_objects (
    artifact_id uuid NOT NULL,
    content_hash text NOT NULL
);


--
-- Name: page_artifacts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.page_artifacts (
    id uuid NOT NULL,
    page_id uuid NOT NULL,
    version bigint NOT NULL,
    source_document jsonb NOT NULL,
    page_document_schema_version integer NOT NULL,
    source_hash text NOT NULL,
    build_input_manifest jsonb,
    build_input_hash text NOT NULL,
    artifact_provider text NOT NULL,
    artifact_key text NOT NULL,
    artifact_hash text NOT NULL,
    compiler_version text NOT NULL,
    registry_version text NOT NULL,
    manifest jsonb NOT NULL,
    payload_state text DEFAULT 'available'::text NOT NULL,
    payload_deleted_at timestamp with time zone,
    note text DEFAULT ''::text NOT NULL,
    created_by uuid NOT NULL,
    create_time timestamp with time zone CONSTRAINT page_artifacts_created_at_not_null NOT NULL,
    lang text DEFAULT 'zh-CN'::text NOT NULL,
    CONSTRAINT page_artifacts_payload_state_check CHECK ((payload_state = ANY (ARRAY['available'::text, 'gc_pending'::text, 'deleted'::text])))
);


--
-- Name: page_component_pins; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.page_component_pins (
    page_id uuid NOT NULL,
    component_id uuid NOT NULL,
    pinned_version_id uuid NOT NULL
);


--
-- Name: page_dependencies; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.page_dependencies (
    page_id uuid NOT NULL,
    artifact_id uuid NOT NULL,
    dependency_kind text NOT NULL,
    dependency_key text NOT NULL,
    revision text,
    last_checked timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT page_dependencies_dependency_kind_check CHECK ((dependency_kind = ANY (ARRAY['direct_content'::text, 'content_collection'::text, 'content_template'::text, 'menu'::text, 'media'::text, 'global_component'::text, 'site_setting'::text, 'runtime'::text, 'i18n'::text, 'block'::text, 'site_slot'::text, 'navigation'::text])))
);


--
-- Name: page_publication_plans; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.page_publication_plans (
    page_id uuid NOT NULL,
    lang text NOT NULL,
    plan jsonb NOT NULL,
    plan_hash text DEFAULT ''::text NOT NULL,
    draft_version bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: page_publications; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.page_publications (
    page_id uuid NOT NULL,
    lang text NOT NULL,
    active_path text NOT NULL,
    artifact_id uuid,
    artifact_hash text DEFAULT ''::text NOT NULL,
    published_at timestamp with time zone NOT NULL,
    update_time timestamp with time zone CONSTRAINT page_publications_updated_at_not_null NOT NULL
);


--
-- Name: page_revisions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.page_revisions (
    id uuid NOT NULL,
    page_id uuid NOT NULL,
    version bigint NOT NULL,
    draft_path text NOT NULL,
    draft_document jsonb NOT NULL,
    source_hash text NOT NULL,
    create_time timestamp with time zone CONSTRAINT page_revisions_created_at_not_null NOT NULL
);


--
-- Name: page_routes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.page_routes (
    project_id uuid NOT NULL,
    path text NOT NULL,
    page_id uuid,
    presentation_id uuid,
    route_kind text NOT NULL,
    artifact_id uuid,
    update_time timestamp with time zone CONSTRAINT page_routes_updated_at_not_null NOT NULL,
    CONSTRAINT page_routes_check CHECK ((((page_id IS NOT NULL) AND (presentation_id IS NULL)) OR ((page_id IS NULL) AND (presentation_id IS NOT NULL)))),
    CONSTRAINT page_routes_route_kind_check CHECK ((route_kind = ANY (ARRAY['reserved'::text, 'active'::text, 'redirect'::text])))
);

ALTER TABLE ONLY public.page_routes FORCE ROW LEVEL SECURITY;


--
-- Name: page_schedules; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.page_schedules (
    id bigint NOT NULL,
    page_id uuid NOT NULL,
    lang text NOT NULL,
    action text NOT NULL,
    scheduled_at timestamp with time zone NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    artifact_id uuid,
    draft_version bigint DEFAULT 0 NOT NULL,
    redirect_path text,
    lease_token uuid,
    lease_expires_time timestamp with time zone,
    attempts integer DEFAULT 0 NOT NULL,
    last_error text,
    create_by bigint,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT page_schedules_action_check CHECK ((action = ANY (ARRAY['publish'::text, 'offline'::text]))),
    CONSTRAINT page_schedules_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'running'::text, 'done'::text, 'failed'::text, 'canceled'::text])))
);


--
-- Name: page_schedules_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.page_schedules ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.page_schedules_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: page_site_slots; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.page_site_slots (
    project_id uuid NOT NULL,
    slot text NOT NULL,
    page_id uuid NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT page_site_slots_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT page_site_slots_updated_at_not_null NOT NULL,
    id bigint CONSTRAINT page_site_slots_id_new_not_null NOT NULL,
    CONSTRAINT ck_page_site_slots_slot CHECK ((slot = ANY (ARRAY['shop'::text, 'blog'::text, 'cart'::text, 'checkout'::text, 'account'::text, 'login'::text, 'register'::text, 'forgot'::text, 'reset'::text, 'orders'::text])))
);

ALTER TABLE ONLY public.page_site_slots FORCE ROW LEVEL SECURITY;


--
-- Name: page_site_slots_id_new_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.page_site_slots ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.page_site_slots_id_new_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: page_stagings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.page_stagings (
    page_id uuid NOT NULL,
    lang text NOT NULL,
    artifact_id uuid NOT NULL,
    artifact_hash text DEFAULT ''::text NOT NULL,
    draft_version bigint DEFAULT 0 NOT NULL,
    update_time timestamp with time zone CONSTRAINT page_stagings_updated_at_not_null NOT NULL
);


--
-- Name: page_views; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.page_views (
    id bigint CONSTRAINT page_views_id_not_null1 NOT NULL,
    project_id uuid CONSTRAINT page_views_project_id_not_null1 NOT NULL,
    path text CONSTRAINT page_views_path_not_null1 NOT NULL,
    lang text DEFAULT ''::text CONSTRAINT page_views_lang_not_null1 NOT NULL,
    session_id text DEFAULT ''::text CONSTRAINT page_views_session_id_not_null1 NOT NULL,
    visitor_hash text DEFAULT ''::text CONSTRAINT page_views_visitor_hash_not_null1 NOT NULL,
    referrer_host text DEFAULT ''::text CONSTRAINT page_views_referrer_host_not_null1 NOT NULL,
    ua_class text DEFAULT ''::text CONSTRAINT page_views_ua_class_not_null1 NOT NULL,
    ip_hash text DEFAULT ''::text CONSTRAINT page_views_ip_hash_not_null1 NOT NULL,
    viewed_at timestamp with time zone DEFAULT now() CONSTRAINT page_views_viewed_at_not_null1 NOT NULL,
    CONSTRAINT chk_page_views_hash_len CHECK ((char_length(visitor_hash) <= 64)),
    CONSTRAINT chk_page_views_ip_len CHECK ((char_length(ip_hash) <= 64)),
    CONSTRAINT chk_page_views_lang_len CHECK ((char_length(lang) <= 35)),
    CONSTRAINT chk_page_views_path_len CHECK (((char_length(path) > 0) AND (char_length(path) <= 512))),
    CONSTRAINT chk_page_views_path_root CHECK (("left"(path, 1) = '/'::text)),
    CONSTRAINT chk_page_views_ref_len CHECK ((char_length(referrer_host) <= 255)),
    CONSTRAINT chk_page_views_sess_len CHECK ((char_length(session_id) <= 64)),
    CONSTRAINT chk_page_views_ua_class CHECK ((ua_class = ANY (ARRAY[''::text, 'desktop'::text, 'tablet'::text, 'mobile'::text, 'bot'::text])))
)
PARTITION BY RANGE (viewed_at);

ALTER TABLE ONLY public.page_views FORCE ROW LEVEL SECURITY;


--
-- Name: page_views_id_seq1; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.page_views_id_seq1
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: page_views_id_seq1; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.page_views_id_seq1 OWNED BY public.page_views.id;


--
-- Name: page_views_2026_09; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.page_views_2026_09 (
    id bigint DEFAULT nextval('public.page_views_id_seq1'::regclass) CONSTRAINT page_views_id_not_null1 NOT NULL,
    project_id uuid CONSTRAINT page_views_project_id_not_null1 NOT NULL,
    path text CONSTRAINT page_views_path_not_null1 NOT NULL,
    lang text DEFAULT ''::text CONSTRAINT page_views_lang_not_null1 NOT NULL,
    session_id text DEFAULT ''::text CONSTRAINT page_views_session_id_not_null1 NOT NULL,
    visitor_hash text DEFAULT ''::text CONSTRAINT page_views_visitor_hash_not_null1 NOT NULL,
    referrer_host text DEFAULT ''::text CONSTRAINT page_views_referrer_host_not_null1 NOT NULL,
    ua_class text DEFAULT ''::text CONSTRAINT page_views_ua_class_not_null1 NOT NULL,
    ip_hash text DEFAULT ''::text CONSTRAINT page_views_ip_hash_not_null1 NOT NULL,
    viewed_at timestamp with time zone DEFAULT now() CONSTRAINT page_views_viewed_at_not_null1 NOT NULL,
    CONSTRAINT chk_page_views_hash_len CHECK ((char_length(visitor_hash) <= 64)),
    CONSTRAINT chk_page_views_ip_len CHECK ((char_length(ip_hash) <= 64)),
    CONSTRAINT chk_page_views_lang_len CHECK ((char_length(lang) <= 35)),
    CONSTRAINT chk_page_views_path_len CHECK (((char_length(path) > 0) AND (char_length(path) <= 512))),
    CONSTRAINT chk_page_views_path_root CHECK (("left"(path, 1) = '/'::text)),
    CONSTRAINT chk_page_views_ref_len CHECK ((char_length(referrer_host) <= 255)),
    CONSTRAINT chk_page_views_sess_len CHECK ((char_length(session_id) <= 64)),
    CONSTRAINT chk_page_views_ua_class CHECK ((ua_class = ANY (ARRAY[''::text, 'desktop'::text, 'tablet'::text, 'mobile'::text, 'bot'::text])))
);

ALTER TABLE ONLY public.page_views_2026_09 FORCE ROW LEVEL SECURITY;


--
-- Name: page_views_2026_10; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.page_views_2026_10 (
    id bigint DEFAULT nextval('public.page_views_id_seq1'::regclass) CONSTRAINT page_views_id_not_null1 NOT NULL,
    project_id uuid CONSTRAINT page_views_project_id_not_null1 NOT NULL,
    path text CONSTRAINT page_views_path_not_null1 NOT NULL,
    lang text DEFAULT ''::text CONSTRAINT page_views_lang_not_null1 NOT NULL,
    session_id text DEFAULT ''::text CONSTRAINT page_views_session_id_not_null1 NOT NULL,
    visitor_hash text DEFAULT ''::text CONSTRAINT page_views_visitor_hash_not_null1 NOT NULL,
    referrer_host text DEFAULT ''::text CONSTRAINT page_views_referrer_host_not_null1 NOT NULL,
    ua_class text DEFAULT ''::text CONSTRAINT page_views_ua_class_not_null1 NOT NULL,
    ip_hash text DEFAULT ''::text CONSTRAINT page_views_ip_hash_not_null1 NOT NULL,
    viewed_at timestamp with time zone DEFAULT now() CONSTRAINT page_views_viewed_at_not_null1 NOT NULL,
    CONSTRAINT chk_page_views_hash_len CHECK ((char_length(visitor_hash) <= 64)),
    CONSTRAINT chk_page_views_ip_len CHECK ((char_length(ip_hash) <= 64)),
    CONSTRAINT chk_page_views_lang_len CHECK ((char_length(lang) <= 35)),
    CONSTRAINT chk_page_views_path_len CHECK (((char_length(path) > 0) AND (char_length(path) <= 512))),
    CONSTRAINT chk_page_views_path_root CHECK (("left"(path, 1) = '/'::text)),
    CONSTRAINT chk_page_views_ref_len CHECK ((char_length(referrer_host) <= 255)),
    CONSTRAINT chk_page_views_sess_len CHECK ((char_length(session_id) <= 64)),
    CONSTRAINT chk_page_views_ua_class CHECK ((ua_class = ANY (ARRAY[''::text, 'desktop'::text, 'tablet'::text, 'mobile'::text, 'bot'::text])))
);

ALTER TABLE ONLY public.page_views_2026_10 FORCE ROW LEVEL SECURITY;


--
-- Name: page_views_2026_11; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.page_views_2026_11 (
    id bigint DEFAULT nextval('public.page_views_id_seq1'::regclass) CONSTRAINT page_views_id_not_null1 NOT NULL,
    project_id uuid CONSTRAINT page_views_project_id_not_null1 NOT NULL,
    path text CONSTRAINT page_views_path_not_null1 NOT NULL,
    lang text DEFAULT ''::text CONSTRAINT page_views_lang_not_null1 NOT NULL,
    session_id text DEFAULT ''::text CONSTRAINT page_views_session_id_not_null1 NOT NULL,
    visitor_hash text DEFAULT ''::text CONSTRAINT page_views_visitor_hash_not_null1 NOT NULL,
    referrer_host text DEFAULT ''::text CONSTRAINT page_views_referrer_host_not_null1 NOT NULL,
    ua_class text DEFAULT ''::text CONSTRAINT page_views_ua_class_not_null1 NOT NULL,
    ip_hash text DEFAULT ''::text CONSTRAINT page_views_ip_hash_not_null1 NOT NULL,
    viewed_at timestamp with time zone DEFAULT now() CONSTRAINT page_views_viewed_at_not_null1 NOT NULL,
    CONSTRAINT chk_page_views_hash_len CHECK ((char_length(visitor_hash) <= 64)),
    CONSTRAINT chk_page_views_ip_len CHECK ((char_length(ip_hash) <= 64)),
    CONSTRAINT chk_page_views_lang_len CHECK ((char_length(lang) <= 35)),
    CONSTRAINT chk_page_views_path_len CHECK (((char_length(path) > 0) AND (char_length(path) <= 512))),
    CONSTRAINT chk_page_views_path_root CHECK (("left"(path, 1) = '/'::text)),
    CONSTRAINT chk_page_views_ref_len CHECK ((char_length(referrer_host) <= 255)),
    CONSTRAINT chk_page_views_sess_len CHECK ((char_length(session_id) <= 64)),
    CONSTRAINT chk_page_views_ua_class CHECK ((ua_class = ANY (ARRAY[''::text, 'desktop'::text, 'tablet'::text, 'mobile'::text, 'bot'::text])))
);

ALTER TABLE ONLY public.page_views_2026_11 FORCE ROW LEVEL SECURITY;


--
-- Name: page_views_2026_12; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.page_views_2026_12 (
    id bigint DEFAULT nextval('public.page_views_id_seq1'::regclass) CONSTRAINT page_views_id_not_null1 NOT NULL,
    project_id uuid CONSTRAINT page_views_project_id_not_null1 NOT NULL,
    path text CONSTRAINT page_views_path_not_null1 NOT NULL,
    lang text DEFAULT ''::text CONSTRAINT page_views_lang_not_null1 NOT NULL,
    session_id text DEFAULT ''::text CONSTRAINT page_views_session_id_not_null1 NOT NULL,
    visitor_hash text DEFAULT ''::text CONSTRAINT page_views_visitor_hash_not_null1 NOT NULL,
    referrer_host text DEFAULT ''::text CONSTRAINT page_views_referrer_host_not_null1 NOT NULL,
    ua_class text DEFAULT ''::text CONSTRAINT page_views_ua_class_not_null1 NOT NULL,
    ip_hash text DEFAULT ''::text CONSTRAINT page_views_ip_hash_not_null1 NOT NULL,
    viewed_at timestamp with time zone DEFAULT now() CONSTRAINT page_views_viewed_at_not_null1 NOT NULL,
    CONSTRAINT chk_page_views_hash_len CHECK ((char_length(visitor_hash) <= 64)),
    CONSTRAINT chk_page_views_ip_len CHECK ((char_length(ip_hash) <= 64)),
    CONSTRAINT chk_page_views_lang_len CHECK ((char_length(lang) <= 35)),
    CONSTRAINT chk_page_views_path_len CHECK (((char_length(path) > 0) AND (char_length(path) <= 512))),
    CONSTRAINT chk_page_views_path_root CHECK (("left"(path, 1) = '/'::text)),
    CONSTRAINT chk_page_views_ref_len CHECK ((char_length(referrer_host) <= 255)),
    CONSTRAINT chk_page_views_sess_len CHECK ((char_length(session_id) <= 64)),
    CONSTRAINT chk_page_views_ua_class CHECK ((ua_class = ANY (ARRAY[''::text, 'desktop'::text, 'tablet'::text, 'mobile'::text, 'bot'::text])))
);

ALTER TABLE ONLY public.page_views_2026_12 FORCE ROW LEVEL SECURITY;


--
-- Name: page_views_2027_01; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.page_views_2027_01 (
    id bigint DEFAULT nextval('public.page_views_id_seq1'::regclass) CONSTRAINT page_views_id_not_null1 NOT NULL,
    project_id uuid CONSTRAINT page_views_project_id_not_null1 NOT NULL,
    path text CONSTRAINT page_views_path_not_null1 NOT NULL,
    lang text DEFAULT ''::text CONSTRAINT page_views_lang_not_null1 NOT NULL,
    session_id text DEFAULT ''::text CONSTRAINT page_views_session_id_not_null1 NOT NULL,
    visitor_hash text DEFAULT ''::text CONSTRAINT page_views_visitor_hash_not_null1 NOT NULL,
    referrer_host text DEFAULT ''::text CONSTRAINT page_views_referrer_host_not_null1 NOT NULL,
    ua_class text DEFAULT ''::text CONSTRAINT page_views_ua_class_not_null1 NOT NULL,
    ip_hash text DEFAULT ''::text CONSTRAINT page_views_ip_hash_not_null1 NOT NULL,
    viewed_at timestamp with time zone DEFAULT now() CONSTRAINT page_views_viewed_at_not_null1 NOT NULL,
    CONSTRAINT chk_page_views_hash_len CHECK ((char_length(visitor_hash) <= 64)),
    CONSTRAINT chk_page_views_ip_len CHECK ((char_length(ip_hash) <= 64)),
    CONSTRAINT chk_page_views_lang_len CHECK ((char_length(lang) <= 35)),
    CONSTRAINT chk_page_views_path_len CHECK (((char_length(path) > 0) AND (char_length(path) <= 512))),
    CONSTRAINT chk_page_views_path_root CHECK (("left"(path, 1) = '/'::text)),
    CONSTRAINT chk_page_views_ref_len CHECK ((char_length(referrer_host) <= 255)),
    CONSTRAINT chk_page_views_sess_len CHECK ((char_length(session_id) <= 64)),
    CONSTRAINT chk_page_views_ua_class CHECK ((ua_class = ANY (ARRAY[''::text, 'desktop'::text, 'tablet'::text, 'mobile'::text, 'bot'::text])))
);

ALTER TABLE ONLY public.page_views_2027_01 FORCE ROW LEVEL SECURITY;


--
-- Name: page_views_daily; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.page_views_daily (
    project_id uuid NOT NULL,
    day date NOT NULL,
    path text NOT NULL,
    views bigint DEFAULT 0 NOT NULL,
    visitors bigint DEFAULT 0 NOT NULL,
    scope text DEFAULT 'path'::text NOT NULL,
    rolled_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT page_views_daily_scope_check CHECK ((scope = ANY (ARRAY['all'::text, 'path'::text])))
);

ALTER TABLE ONLY public.page_views_daily FORCE ROW LEVEL SECURITY;


--
-- Name: page_views_default; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.page_views_default (
    id bigint DEFAULT nextval('public.page_views_id_seq1'::regclass) CONSTRAINT page_views_id_not_null1 NOT NULL,
    project_id uuid CONSTRAINT page_views_project_id_not_null1 NOT NULL,
    path text CONSTRAINT page_views_path_not_null1 NOT NULL,
    lang text DEFAULT ''::text CONSTRAINT page_views_lang_not_null1 NOT NULL,
    session_id text DEFAULT ''::text CONSTRAINT page_views_session_id_not_null1 NOT NULL,
    visitor_hash text DEFAULT ''::text CONSTRAINT page_views_visitor_hash_not_null1 NOT NULL,
    referrer_host text DEFAULT ''::text CONSTRAINT page_views_referrer_host_not_null1 NOT NULL,
    ua_class text DEFAULT ''::text CONSTRAINT page_views_ua_class_not_null1 NOT NULL,
    ip_hash text DEFAULT ''::text CONSTRAINT page_views_ip_hash_not_null1 NOT NULL,
    viewed_at timestamp with time zone DEFAULT now() CONSTRAINT page_views_viewed_at_not_null1 NOT NULL,
    CONSTRAINT chk_page_views_hash_len CHECK ((char_length(visitor_hash) <= 64)),
    CONSTRAINT chk_page_views_ip_len CHECK ((char_length(ip_hash) <= 64)),
    CONSTRAINT chk_page_views_lang_len CHECK ((char_length(lang) <= 35)),
    CONSTRAINT chk_page_views_path_len CHECK (((char_length(path) > 0) AND (char_length(path) <= 512))),
    CONSTRAINT chk_page_views_path_root CHECK (("left"(path, 1) = '/'::text)),
    CONSTRAINT chk_page_views_ref_len CHECK ((char_length(referrer_host) <= 255)),
    CONSTRAINT chk_page_views_sess_len CHECK ((char_length(session_id) <= 64)),
    CONSTRAINT chk_page_views_ua_class CHECK ((ua_class = ANY (ARRAY[''::text, 'desktop'::text, 'tablet'::text, 'mobile'::text, 'bot'::text])))
);

ALTER TABLE ONLY public.page_views_default FORCE ROW LEVEL SECURITY;


--
-- Name: pages; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.pages (
    id uuid NOT NULL,
    project_id uuid NOT NULL,
    kind text NOT NULL,
    content_target_type text NOT NULL,
    content_target_id uuid,
    source_blueprint_version_id uuid,
    draft_path text NOT NULL,
    active_path text,
    draft_document jsonb NOT NULL,
    draft_version bigint DEFAULT 1 NOT NULL,
    staged_artifact_id uuid,
    active_artifact_id uuid,
    stale boolean DEFAULT true NOT NULL,
    deleted_at timestamp with time zone,
    published_at timestamp with time zone,
    create_time timestamp with time zone CONSTRAINT pages_created_at_not_null NOT NULL,
    update_time timestamp with time zone CONSTRAINT pages_updated_at_not_null NOT NULL,
    theme_id uuid,
    rebuild_failed_at timestamp with time zone,
    rebuild_failed_stage character varying(16),
    excluded_langs text[] DEFAULT '{}'::text[] NOT NULL,
    CONSTRAINT pages_content_contract_check CHECK ((((kind = ANY (ARRAY['home'::text, 'archive'::text, 'search'::text, 'notFound'::text])) AND (content_target_type = 'none'::text) AND (content_target_id IS NULL)) OR ((kind = 'page'::text) AND (content_target_type = 'page'::text) AND (content_target_id IS NOT NULL)) OR ((kind = 'article'::text) AND (content_target_type = 'article'::text) AND (content_target_id IS NOT NULL)) OR ((kind = 'tag'::text) AND (content_target_type = 'tag'::text) AND (content_target_id IS NOT NULL))))
);

ALTER TABLE ONLY public.pages FORCE ROW LEVEL SECURITY;


--
-- Name: plugin_registry; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.plugin_registry (
    plugin_id text NOT NULL,
    name text NOT NULL,
    version text NOT NULL,
    schema_version integer DEFAULT 0 NOT NULL,
    enabled boolean DEFAULT false NOT NULL,
    manifest jsonb NOT NULL,
    storage_path text DEFAULT ''::text NOT NULL,
    installed_at timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT plugin_registry_updated_at_not_null NOT NULL
);


--
-- Name: presentation_artifacts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.presentation_artifacts (
    id uuid NOT NULL,
    presentation_instance_id uuid NOT NULL,
    snapshot_id uuid NOT NULL,
    version bigint NOT NULL,
    source_hash text NOT NULL,
    build_input_hash text NOT NULL,
    artifact_provider text NOT NULL,
    artifact_key text NOT NULL,
    artifact_hash text NOT NULL,
    compiler_version text NOT NULL,
    registry_version text NOT NULL,
    manifest jsonb NOT NULL,
    payload_state text DEFAULT 'available'::text NOT NULL,
    payload_deleted_at timestamp with time zone,
    note text DEFAULT ''::text NOT NULL,
    created_by uuid NOT NULL,
    create_time timestamp with time zone CONSTRAINT presentation_artifacts_created_at_not_null NOT NULL,
    lang text DEFAULT 'zh-CN'::text NOT NULL,
    CONSTRAINT presentation_artifacts_payload_state_check CHECK ((payload_state = ANY (ARRAY['available'::text, 'gc_pending'::text, 'deleted'::text])))
);


--
-- Name: presentation_dependencies; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.presentation_dependencies (
    presentation_id uuid NOT NULL,
    artifact_id uuid NOT NULL,
    dependency_kind text NOT NULL,
    dependency_key text NOT NULL,
    revision text,
    last_checked timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT presentation_dependencies_dependency_kind_check CHECK ((dependency_kind = ANY (ARRAY['direct_content'::text, 'content_collection'::text, 'content_template'::text, 'menu'::text, 'media'::text, 'global_component'::text, 'site_setting'::text, 'runtime'::text, 'i18n'::text, 'block'::text, 'site_slot'::text, 'navigation'::text])))
);


--
-- Name: presentation_instances; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.presentation_instances (
    id uuid NOT NULL,
    project_id uuid NOT NULL,
    entity_type text NOT NULL,
    entity_id uuid NOT NULL,
    url_path text NOT NULL,
    template_id uuid NOT NULL,
    current_snapshot_id uuid,
    staged_snapshot_id uuid,
    staged_artifact_id uuid,
    active_artifact_id uuid,
    stale boolean DEFAULT true NOT NULL,
    deleted_at timestamp with time zone,
    published_at timestamp with time zone,
    create_time timestamp with time zone CONSTRAINT presentation_instances_created_at_not_null NOT NULL,
    update_time timestamp with time zone CONSTRAINT presentation_instances_updated_at_not_null NOT NULL,
    instance_role text DEFAULT 'detail'::text NOT NULL,
    override_document jsonb,
    render_mode text DEFAULT 'template'::text NOT NULL,
    CONSTRAINT ck_presentation_instances_render_mode CHECK ((render_mode = ANY (ARRAY['template'::text, 'document'::text])))
);

ALTER TABLE ONLY public.presentation_instances FORCE ROW LEVEL SECURITY;


--
-- Name: presentation_publications; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.presentation_publications (
    presentation_id uuid NOT NULL,
    lang text NOT NULL,
    active_path text NOT NULL,
    artifact_id uuid,
    artifact_hash text DEFAULT ''::text NOT NULL,
    published_at timestamp with time zone NOT NULL,
    update_time timestamp with time zone CONSTRAINT presentation_publications_updated_at_not_null NOT NULL
);


--
-- Name: product_attributes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.product_attributes (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    key text NOT NULL,
    name text NOT NULL,
    is_variation boolean DEFAULT true NOT NULL,
    sort integer DEFAULT 0 NOT NULL,
    "values" jsonb DEFAULT '[]'::jsonb NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT product_attributes_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT product_attributes_updated_at_not_null NOT NULL,
    CONSTRAINT product_attributes_is_variation_check CHECK ((is_variation = ANY (ARRAY[true, false])))
);

ALTER TABLE ONLY public.product_attributes FORCE ROW LEVEL SECURITY;


--
-- Name: product_brands; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.product_brands (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    name text NOT NULL,
    slug text NOT NULL,
    logo text DEFAULT ''::text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    seo_title text DEFAULT ''::text NOT NULL,
    seo_description text DEFAULT ''::text NOT NULL,
    sort integer DEFAULT 0 NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT product_brands_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT product_brands_updated_at_not_null NOT NULL
);

ALTER TABLE ONLY public.product_brands FORCE ROW LEVEL SECURITY;


--
-- Name: product_categories; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.product_categories (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    parent_id uuid,
    name text NOT NULL,
    slug text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    image text DEFAULT ''::text NOT NULL,
    seo_title text DEFAULT ''::text NOT NULL,
    seo_description text DEFAULT ''::text NOT NULL,
    sort integer DEFAULT 0 NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT product_categories_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT product_categories_updated_at_not_null NOT NULL
);

ALTER TABLE ONLY public.product_categories FORCE ROW LEVEL SECURITY;


--
-- Name: product_outbox_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.product_outbox_events (
    id bigint NOT NULL,
    project_id uuid NOT NULL,
    entity_type text NOT NULL,
    entity_id text NOT NULL,
    entity_revision bigint NOT NULL,
    dependency_kind text NOT NULL,
    dependency_key text NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    claimed_time timestamp with time zone,
    processed_time timestamp with time zone,
    attempts integer DEFAULT 0 NOT NULL,
    last_error text DEFAULT ''::text NOT NULL,
    CONSTRAINT product_outbox_events_dependency_kind_check CHECK ((dependency_kind = ANY (ARRAY['direct_content'::text, 'content_collection'::text])))
);


--
-- Name: product_outbox_events_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.product_outbox_events ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.product_outbox_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: product_price_adjustment_items; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.product_price_adjustment_items (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    adjustment_id uuid NOT NULL,
    product_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    sku_code text DEFAULT ''::text NOT NULL,
    old_price numeric(12,2) NOT NULL,
    new_price numeric(12,2) NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT product_price_adjustment_items_created_at_not_null NOT NULL
);


--
-- Name: product_price_adjustments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.product_price_adjustments (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    rule_type text NOT NULL,
    rule_params jsonb DEFAULT '{}'::jsonb NOT NULL,
    rounding text DEFAULT 'none'::text NOT NULL,
    scope text NOT NULL,
    target_id uuid,
    filter jsonb DEFAULT '{}'::jsonb NOT NULL,
    variant_count integer DEFAULT 0 NOT NULL,
    changed_count integer DEFAULT 0 NOT NULL,
    note text DEFAULT ''::text NOT NULL,
    operator_id text DEFAULT ''::text NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT product_price_adjustments_created_at_not_null NOT NULL
);

ALTER TABLE ONLY public.product_price_adjustments FORCE ROW LEVEL SECURITY;


--
-- Name: product_ratings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.product_ratings (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    product_id uuid NOT NULL,
    score numeric(3,2) NOT NULL,
    source text DEFAULT 'manual'::text NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT product_ratings_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT product_ratings_updated_at_not_null NOT NULL,
    CONSTRAINT product_ratings_score_check CHECK (((score >= (0)::numeric) AND (score <= (5)::numeric)))
);

ALTER TABLE ONLY public.product_ratings FORCE ROW LEVEL SECURITY;


--
-- Name: product_tags; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.product_tags (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    name text NOT NULL,
    slug text NOT NULL,
    kind text DEFAULT 'manual'::text NOT NULL,
    rule_type text DEFAULT ''::text NOT NULL,
    rule_params jsonb DEFAULT '{}'::jsonb NOT NULL,
    sort integer DEFAULT 0 NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT product_tags_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT product_tags_updated_at_not_null NOT NULL,
    recalc_at timestamp with time zone,
    CONSTRAINT ck_product_tags_rule_shape CHECK ((((kind = 'manual'::text) AND (rule_type = ''::text)) OR ((kind = 'rule'::text) AND (rule_type <> ''::text)))),
    CONSTRAINT product_tags_kind_check CHECK ((kind = ANY (ARRAY['manual'::text, 'rule'::text])))
);

ALTER TABLE ONLY public.product_tags FORCE ROW LEVEL SECURITY;


--
-- Name: product_variants; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.product_variants (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    product_id uuid NOT NULL,
    sku_code text NOT NULL,
    barcode text DEFAULT ''::text NOT NULL,
    price numeric(12,2) DEFAULT 0 NOT NULL,
    compare_price numeric(12,2),
    cost_price numeric(12,2),
    image text DEFAULT ''::text NOT NULL,
    option_values jsonb DEFAULT '{}'::jsonb NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    sort integer DEFAULT 0 NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT product_variants_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT product_variants_updated_at_not_null NOT NULL
);


--
-- Name: products; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.products (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    name text NOT NULL,
    subtitle text DEFAULT ''::text NOT NULL,
    description jsonb DEFAULT '{}'::jsonb NOT NULL,
    slug text NOT NULL,
    status text DEFAULT 'draft'::text NOT NULL,
    sort integer DEFAULT 0 NOT NULL,
    unit text DEFAULT ''::text NOT NULL,
    weight numeric(12,3),
    seo_title text DEFAULT ''::text NOT NULL,
    seo_description text DEFAULT ''::text NOT NULL,
    images jsonb DEFAULT '[]'::jsonb NOT NULL,
    category_ids jsonb DEFAULT '[]'::jsonb NOT NULL,
    tag_ids jsonb DEFAULT '[]'::jsonb NOT NULL,
    related_ids jsonb DEFAULT '[]'::jsonb NOT NULL,
    bundle_items jsonb DEFAULT '{"options": [], "maxOptions": 20, "maxTotalQty": 0, "minTotalQty": 0}'::jsonb NOT NULL,
    brand_id uuid,
    default_price numeric(12,2),
    default_compare_price numeric(12,2),
    default_cost_price numeric(12,2),
    default_image text DEFAULT ''::text NOT NULL,
    metadata jsonb DEFAULT '{}'::jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT products_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT products_updated_at_not_null NOT NULL,
    attribute_ids jsonb DEFAULT '[]'::jsonb NOT NULL,
    primary_category_id uuid,
    published_at timestamp with time zone,
    images_alt jsonb DEFAULT '[]'::jsonb NOT NULL,
    type text DEFAULT 'variant'::text NOT NULL,
    sku_code text DEFAULT ''::text NOT NULL,
    CONSTRAINT ck_products_bundle_price_positive CHECK (((type <> 'bundle'::text) OR ((default_price IS NOT NULL) AND (default_price > (0)::numeric)))),
    CONSTRAINT products_bundle_items_shape_check CHECK (((jsonb_typeof(bundle_items) = 'object'::text) AND (jsonb_typeof((bundle_items -> 'options'::text)) = 'array'::text))),
    CONSTRAINT products_status_check CHECK ((status = ANY (ARRAY['draft'::text, 'published'::text, 'archived'::text]))),
    CONSTRAINT products_type_check CHECK ((type = ANY (ARRAY['variant'::text, 'bundle'::text])))
);

ALTER TABLE ONLY public.products FORCE ROW LEVEL SECURITY;


--
-- Name: project_locales; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.project_locales (
    project_id uuid NOT NULL,
    lang text NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    is_default boolean DEFAULT false NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    create_time timestamp with time zone CONSTRAINT project_locales_created_at_not_null NOT NULL,
    update_time timestamp with time zone CONSTRAINT project_locales_updated_at_not_null NOT NULL
);

ALTER TABLE ONLY public.project_locales FORCE ROW LEVEL SECURITY;


--
-- Name: projects; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.projects (
    id uuid NOT NULL,
    name text NOT NULL,
    settings jsonb NOT NULL,
    create_time timestamp with time zone CONSTRAINT projects_created_at_not_null NOT NULL,
    update_time timestamp with time zone CONSTRAINT projects_updated_at_not_null NOT NULL,
    analytics_retention_days integer DEFAULT 90 NOT NULL,
    masterdata_retention_days integer DEFAULT 0 NOT NULL
);


--
-- Name: publication_receipts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.publication_receipts (
    source_type text NOT NULL,
    source_id uuid NOT NULL,
    action text NOT NULL,
    path text NOT NULL,
    from_artifact_id uuid,
    to_artifact_id uuid,
    receipt_state text NOT NULL,
    receipt_data jsonb NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT publication_receipts_created_at_not_null NOT NULL,
    completed_at timestamp with time zone,
    id bigint CONSTRAINT publication_receipts_id_new_not_null NOT NULL,
    CONSTRAINT publication_receipts_receipt_state_check CHECK ((receipt_state = ANY (ARRAY['pending'::text, 'committed'::text, 'rolled_back'::text]))),
    CONSTRAINT publication_receipts_source_type_check CHECK ((source_type = ANY (ARRAY['page'::text, 'presentation'::text])))
);


--
-- Name: publication_receipts_id_new_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.publication_receipts ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.publication_receipts_id_new_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: sys_admin; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sys_admin (
    id bigint NOT NULL,
    dept_id bigint DEFAULT 0 NOT NULL,
    username character varying(50) NOT NULL,
    password character varying(100) NOT NULL,
    name character varying(50),
    avatar character varying(255),
    email character varying(100),
    phone character varying(20),
    status smallint DEFAULT 1 NOT NULL,
    is_admin smallint DEFAULT 0 NOT NULL,
    login_failure_count integer DEFAULT 0 NOT NULL,
    locked_until_time timestamp with time zone,
    metadata jsonb,
    last_failure_time timestamp with time zone,
    register_ip character varying(50),
    register_location character varying(100),
    last_login_ip character varying(50),
    last_login_location character varying(100),
    last_login_isp character varying(50),
    last_login_time timestamp with time zone,
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone,
    update_by bigint DEFAULT 0 NOT NULL,
    update_time timestamp with time zone,
    remark character varying(255)
);


--
-- Name: sys_admin_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.sys_admin_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: sys_admin_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.sys_admin_id_seq OWNED BY public.sys_admin.id;


--
-- Name: sys_area; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sys_area (
    code character varying(16) NOT NULL,
    kind character varying(16) NOT NULL,
    level smallint NOT NULL,
    parent_code character varying(16),
    name_zh character varying(128) NOT NULL,
    name_en character varying(128),
    iso_code character varying(16),
    alpha3 character varying(3),
    numeric_code character varying(8),
    dial_code character varying(8),
    default_currency character varying(8),
    default_lang character varying(16),
    source character varying(32) DEFAULT 'manual'::character varying NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_sys_area_kind CHECK (((kind)::text = ANY ((ARRAY['continent'::character varying, 'country'::character varying, 'division'::character varying])::text[]))),
    CONSTRAINT ck_sys_area_level CHECK (((level >= 1) AND (level <= 5)))
);


--
-- Name: sys_attachment; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sys_attachment (
    id bigint NOT NULL,
    category_id bigint,
    file_name character varying(255) NOT NULL,
    file_path character varying(500) NOT NULL,
    file_size bigint NOT NULL,
    file_type character varying(50) NOT NULL,
    mime_type character varying(100),
    storage_type character varying(50) DEFAULT 'local'::character varying NOT NULL,
    storage_path character varying(500),
    url character varying(500),
    md5 character varying(32),
    extra_info jsonb,
    status smallint DEFAULT 1 NOT NULL,
    create_by bigint,
    update_by bigint,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone,
    generation integer DEFAULT 1 NOT NULL
);


--
-- Name: sys_attachment_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.sys_attachment_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: sys_attachment_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.sys_attachment_id_seq OWNED BY public.sys_attachment.id;


--
-- Name: sys_casbin_rule; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sys_casbin_rule (
    id bigint NOT NULL,
    ptype character varying(100),
    v0 character varying(100),
    v1 character varying(100),
    v2 character varying(100),
    v3 character varying(100),
    v4 character varying(100),
    v5 character varying(100)
);


--
-- Name: sys_casbin_rule_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.sys_casbin_rule_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: sys_casbin_rule_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.sys_casbin_rule_id_seq OWNED BY public.sys_casbin_rule.id;


--
-- Name: sys_config; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sys_config (
    id bigint NOT NULL,
    group_key character varying(50) NOT NULL,
    group_name character varying(100) NOT NULL,
    config_data jsonb NOT NULL,
    remark character varying(500),
    status smallint DEFAULT 1 NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_by bigint DEFAULT 0 NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: sys_config_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.sys_config_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: sys_config_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.sys_config_id_seq OWNED BY public.sys_config.id;


--
-- Name: sys_dept; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sys_dept (
    id bigint NOT NULL,
    parent_id bigint DEFAULT 0 NOT NULL,
    ancestors character varying(500) DEFAULT ''::character varying NOT NULL,
    dept_name character varying(100) NOT NULL,
    dept_code character varying(50) NOT NULL,
    leader_id bigint,
    sort_order integer DEFAULT 0 NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    remark character varying(200),
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone,
    update_by bigint DEFAULT 0 NOT NULL,
    update_time timestamp with time zone
);


--
-- Name: sys_dept_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.sys_dept_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: sys_dept_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.sys_dept_id_seq OWNED BY public.sys_dept.id;


--
-- Name: sys_dict; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sys_dict (
    type character varying(16) NOT NULL,
    code character varying(16) NOT NULL,
    url_code character varying(16),
    iso639_1 character varying(8),
    iso639_2 character varying(8),
    ui_available boolean DEFAULT false NOT NULL,
    symbol character varying(8),
    minor_unit smallint,
    numeric_code character varying(8),
    enabled boolean DEFAULT true NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_sys_dict_currency CHECK ((((type)::text <> 'currency'::text) OR (minor_unit IS NOT NULL))),
    CONSTRAINT ck_sys_dict_language CHECK ((((type)::text <> 'language'::text) OR (url_code IS NOT NULL))),
    CONSTRAINT ck_sys_dict_type CHECK (((type)::text = ANY ((ARRAY['language'::character varying, 'currency'::character varying])::text[])))
);


--
-- Name: sys_file_category; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sys_file_category (
    id bigint NOT NULL,
    category_name character varying(100) NOT NULL,
    category_code character varying(50) NOT NULL,
    parent_id bigint DEFAULT 0 NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    icon character varying(50),
    status smallint DEFAULT 1 NOT NULL,
    create_by bigint,
    update_by bigint,
    create_time timestamp with time zone,
    update_time timestamp with time zone
);


--
-- Name: sys_file_category_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.sys_file_category_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: sys_file_category_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.sys_file_category_id_seq OWNED BY public.sys_file_category.id;


--
-- Name: sys_i18n; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sys_i18n (
    id bigint NOT NULL,
    item_key character varying(100) NOT NULL,
    lang character varying(10) NOT NULL,
    item_value text NOT NULL,
    http_code integer DEFAULT 0,
    status smallint DEFAULT 1 NOT NULL,
    create_time timestamp with time zone,
    update_time timestamp with time zone,
    category character varying(20),
    remark character varying(200)
);


--
-- Name: sys_i18n_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.sys_i18n_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: sys_i18n_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.sys_i18n_id_seq OWNED BY public.sys_i18n.id;


--
-- Name: sys_i18n_revision; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sys_i18n_revision (
    id smallint DEFAULT 1 NOT NULL,
    revision bigint DEFAULT 0 NOT NULL,
    update_time timestamp with time zone,
    CONSTRAINT ck_sys_i18n_revision_single CHECK ((id = 1))
);


--
-- Name: sys_media_variant; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sys_media_variant (
    id bigint NOT NULL,
    attachment_id bigint NOT NULL,
    variant_type character varying(20) NOT NULL,
    file_path character varying(500) NOT NULL,
    width integer,
    height integer,
    file_size bigint DEFAULT 0 NOT NULL,
    mime_type character varying(100),
    status character varying(20) DEFAULT 'pending'::character varying NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone
);


--
-- Name: sys_media_variant_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.sys_media_variant_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: sys_media_variant_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.sys_media_variant_id_seq OWNED BY public.sys_media_variant.id;


--
-- Name: sys_menu_permission; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sys_menu_permission (
    id bigint NOT NULL,
    menu_id bigint NOT NULL,
    permission_code character varying(100) NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: sys_menu_permission_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.sys_menu_permission_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: sys_menu_permission_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.sys_menu_permission_id_seq OWNED BY public.sys_menu_permission.id;


--
-- Name: sys_menus; Type: TABLE; Schema: public; Owner: -
--

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


--
-- Name: sys_menus_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.sys_menus_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: sys_menus_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.sys_menus_id_seq OWNED BY public.sys_menus.id;


--
-- Name: sys_permission; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sys_permission (
    id bigint NOT NULL,
    permission_code character varying(100) NOT NULL,
    permission_name character varying(100) NOT NULL,
    module character varying(50) NOT NULL,
    api_path character varying(200) NOT NULL,
    api_method character varying(10) DEFAULT 'GET'::character varying NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    remark character varying(200),
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone,
    update_by bigint DEFAULT 0 NOT NULL,
    update_time timestamp with time zone
);


--
-- Name: sys_permission_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.sys_permission_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: sys_permission_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.sys_permission_id_seq OWNED BY public.sys_permission.id;


--
-- Name: sys_role; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sys_role (
    id bigint NOT NULL,
    role_code character varying(50) NOT NULL,
    role_name character varying(100) NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    is_system smallint DEFAULT 0 NOT NULL,
    sort_order integer DEFAULT 0 NOT NULL,
    remark character varying(200),
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone,
    update_by bigint DEFAULT 0 NOT NULL,
    update_time timestamp with time zone
);


--
-- Name: sys_role_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.sys_role_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: sys_role_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.sys_role_id_seq OWNED BY public.sys_role.id;


--
-- Name: sys_rule; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sys_rule (
    id bigint NOT NULL,
    rule_name character varying(100) NOT NULL,
    domain character varying(50) NOT NULL,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    remark character varying(200),
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone,
    update_by bigint DEFAULT 0 NOT NULL,
    update_time timestamp with time zone
);


--
-- Name: sys_rule_assignment; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sys_rule_assignment (
    id bigint NOT NULL,
    rule_id bigint NOT NULL,
    target_type smallint NOT NULL,
    target_id bigint NOT NULL,
    target_scope smallint DEFAULT 0 NOT NULL,
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone
);


--
-- Name: sys_rule_assignment_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.sys_rule_assignment_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: sys_rule_assignment_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.sys_rule_assignment_id_seq OWNED BY public.sys_rule_assignment.id;


--
-- Name: sys_rule_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.sys_rule_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: sys_rule_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.sys_rule_id_seq OWNED BY public.sys_rule.id;


--
-- Name: sys_translation; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sys_translation (
    source_hash text NOT NULL,
    context text NOT NULL,
    lang text NOT NULL,
    source_text text NOT NULL,
    target_text text NOT NULL,
    engine text DEFAULT 'manual'::text NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT sys_translation_updated_at_not_null NOT NULL,
    project_id uuid,
    CONSTRAINT ck_sys_translation_engine CHECK ((engine = ANY (ARRAY['manual'::text, 'ai'::text, 'po'::text]))),
    CONSTRAINT ck_sys_translation_hash CHECK ((source_hash ~ '^[0-9a-f]{64}$'::text)),
    CONSTRAINT ck_sys_translation_target CHECK ((target_text <> ''::text))
);

ALTER TABLE ONLY public.sys_translation FORCE ROW LEVEL SECURITY;


--
-- Name: themes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.themes (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    project_id uuid NOT NULL,
    name text NOT NULL,
    settings jsonb DEFAULT '{}'::jsonb NOT NULL,
    is_active boolean DEFAULT false NOT NULL,
    create_time timestamp with time zone DEFAULT now() CONSTRAINT themes_created_at_not_null NOT NULL,
    update_time timestamp with time zone DEFAULT now() CONSTRAINT themes_updated_at_not_null NOT NULL
);

ALTER TABLE ONLY public.themes FORCE ROW LEVEL SECURITY;


--
-- Name: user_preferences; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_preferences (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    theme character varying(64),
    locale character varying(16),
    timezone character varying(64),
    page_size smallint DEFAULT 20 NOT NULL,
    email_notify boolean DEFAULT true NOT NULL,
    sms_notify boolean DEFAULT false NOT NULL,
    profile_visibility character varying(16) DEFAULT 'public'::character varying NOT NULL,
    show_online boolean DEFAULT true NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone
);


--
-- Name: user_preferences_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.user_preferences_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: user_preferences_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.user_preferences_id_seq OWNED BY public.user_preferences.id;


--
-- Name: user_profiles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_profiles (
    id bigint NOT NULL,
    user_id bigint NOT NULL,
    first_name character varying(60),
    last_name character varying(60),
    gender smallint DEFAULT 0 NOT NULL,
    birthday date,
    bio text,
    website character varying(255),
    locale character varying(16),
    timezone character varying(64),
    country character varying(64),
    province character varying(64),
    city character varying(64),
    address character varying(255),
    postcode character varying(20),
    phone character varying(20),
    company character varying(100),
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone
);


--
-- Name: user_profiles_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.user_profiles_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: user_profiles_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.user_profiles_id_seq OWNED BY public.user_profiles.id;


--
-- Name: users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.users (
    id bigint NOT NULL,
    username character varying(60) NOT NULL,
    password character varying(100) DEFAULT ''::character varying NOT NULL,
    email character varying(100) DEFAULT ''::character varying NOT NULL,
    email_verified_at timestamp with time zone,
    status smallint DEFAULT 1 NOT NULL,
    nickname character varying(60),
    display_name character varying(250),
    avatar character varying(255),
    activation_key character varying(64),
    activation_expires_at timestamp with time zone,
    login_failure_count integer DEFAULT 0 NOT NULL,
    locked_until_time timestamp with time zone,
    last_failure_time timestamp with time zone,
    register_ip character varying(50),
    register_location character varying(100),
    last_login_ip character varying(50),
    last_login_location character varying(100),
    registered_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    last_login_time timestamp with time zone,
    last_active_at timestamp with time zone,
    metadata jsonb,
    create_by bigint DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    update_time timestamp with time zone,
    deleted_at timestamp with time zone,
    search_text text GENERATED ALWAYS AS (lower((((((((COALESCE(username, ''::character varying))::text || ' '::text) || (COALESCE(email, ''::character varying))::text) || ' '::text) || (COALESCE(nickname, ''::character varying))::text) || ' '::text) || (COALESCE(display_name, ''::character varying))::text))) STORED
);


--
-- Name: users_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.users_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: users_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.users_id_seq OWNED BY public.users.id;


--
-- Name: webhook_deliveries; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.webhook_deliveries (
    id bigint NOT NULL,
    endpoint_id bigint NOT NULL,
    event_type character varying(128) NOT NULL,
    payload text NOT NULL,
    status character varying(16) DEFAULT 'pending'::character varying NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    response_status integer,
    last_error character varying(1024) DEFAULT ''::character varying NOT NULL,
    create_time timestamp with time zone CONSTRAINT webhook_deliveries_created_at_not_null NOT NULL,
    update_time timestamp with time zone CONSTRAINT webhook_deliveries_updated_at_not_null NOT NULL
);


--
-- Name: webhook_deliveries_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.webhook_deliveries_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: webhook_deliveries_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.webhook_deliveries_id_seq OWNED BY public.webhook_deliveries.id;


--
-- Name: webhook_endpoints; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.webhook_endpoints (
    id bigint NOT NULL,
    event_type character varying(128) NOT NULL,
    target_url character varying(1024) NOT NULL,
    secret_cipher character varying(512) NOT NULL,
    description character varying(512) DEFAULT ''::character varying NOT NULL,
    status smallint DEFAULT 1 NOT NULL,
    create_time timestamp with time zone CONSTRAINT webhook_endpoints_created_at_not_null NOT NULL,
    update_time timestamp with time zone CONSTRAINT webhook_endpoints_updated_at_not_null NOT NULL
);


--
-- Name: webhook_endpoints_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.webhook_endpoints_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: webhook_endpoints_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.webhook_endpoints_id_seq OWNED BY public.webhook_endpoints.id;


--
-- Name: inventory_stock_movements_2026_09; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stock_movements ATTACH PARTITION public.inventory_stock_movements_2026_09 FOR VALUES FROM ('2026-09-01 00:00:00+08') TO ('2026-10-01 00:00:00+08');


--
-- Name: inventory_stock_movements_2026_10; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stock_movements ATTACH PARTITION public.inventory_stock_movements_2026_10 FOR VALUES FROM ('2026-10-01 00:00:00+08') TO ('2026-11-01 00:00:00+08');


--
-- Name: inventory_stock_movements_2026_11; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stock_movements ATTACH PARTITION public.inventory_stock_movements_2026_11 FOR VALUES FROM ('2026-11-01 00:00:00+08') TO ('2026-12-01 00:00:00+08');


--
-- Name: inventory_stock_movements_2026_12; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stock_movements ATTACH PARTITION public.inventory_stock_movements_2026_12 FOR VALUES FROM ('2026-12-01 00:00:00+08') TO ('2027-01-01 00:00:00+08');


--
-- Name: inventory_stock_movements_2027_01; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stock_movements ATTACH PARTITION public.inventory_stock_movements_2027_01 FOR VALUES FROM ('2027-01-01 00:00:00+08') TO ('2027-02-01 00:00:00+08');


--
-- Name: inventory_stock_movements_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stock_movements ATTACH PARTITION public.inventory_stock_movements_default DEFAULT;


--
-- Name: master_data_changes_2026_09; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.master_data_changes ATTACH PARTITION public.master_data_changes_2026_09 FOR VALUES FROM ('2026-09-01 00:00:00+08') TO ('2026-10-01 00:00:00+08');


--
-- Name: master_data_changes_2026_10; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.master_data_changes ATTACH PARTITION public.master_data_changes_2026_10 FOR VALUES FROM ('2026-10-01 00:00:00+08') TO ('2026-11-01 00:00:00+08');


--
-- Name: master_data_changes_2026_11; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.master_data_changes ATTACH PARTITION public.master_data_changes_2026_11 FOR VALUES FROM ('2026-11-01 00:00:00+08') TO ('2026-12-01 00:00:00+08');


--
-- Name: master_data_changes_2026_12; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.master_data_changes ATTACH PARTITION public.master_data_changes_2026_12 FOR VALUES FROM ('2026-12-01 00:00:00+08') TO ('2027-01-01 00:00:00+08');


--
-- Name: master_data_changes_2027_01; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.master_data_changes ATTACH PARTITION public.master_data_changes_2027_01 FOR VALUES FROM ('2027-01-01 00:00:00+08') TO ('2027-02-01 00:00:00+08');


--
-- Name: master_data_changes_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.master_data_changes ATTACH PARTITION public.master_data_changes_default DEFAULT;


--
-- Name: page_views_2026_09; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_views ATTACH PARTITION public.page_views_2026_09 FOR VALUES FROM ('2026-09-01 00:00:00+08') TO ('2026-10-01 00:00:00+08');


--
-- Name: page_views_2026_10; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_views ATTACH PARTITION public.page_views_2026_10 FOR VALUES FROM ('2026-10-01 00:00:00+08') TO ('2026-11-01 00:00:00+08');


--
-- Name: page_views_2026_11; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_views ATTACH PARTITION public.page_views_2026_11 FOR VALUES FROM ('2026-11-01 00:00:00+08') TO ('2026-12-01 00:00:00+08');


--
-- Name: page_views_2026_12; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_views ATTACH PARTITION public.page_views_2026_12 FOR VALUES FROM ('2026-12-01 00:00:00+08') TO ('2027-01-01 00:00:00+08');


--
-- Name: page_views_2027_01; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_views ATTACH PARTITION public.page_views_2027_01 FOR VALUES FROM ('2027-01-01 00:00:00+08') TO ('2027-02-01 00:00:00+08');


--
-- Name: page_views_default; Type: TABLE ATTACH; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_views ATTACH PARTITION public.page_views_default DEFAULT;


--
-- Name: ai_access_token id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_access_token ALTER COLUMN id SET DEFAULT nextval('public.ai_access_token_id_seq'::regclass);


--
-- Name: ai_call_log id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_call_log ALTER COLUMN id SET DEFAULT nextval('public.ai_call_log_id_seq'::regclass);


--
-- Name: ai_event id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_event ALTER COLUMN id SET DEFAULT nextval('public.ai_event_id_seq'::regclass);


--
-- Name: ai_provider id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_provider ALTER COLUMN id SET DEFAULT nextval('public.ai_provider_id_seq'::regclass);


--
-- Name: ai_session id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_session ALTER COLUMN id SET DEFAULT nextval('public.ai_session_id_seq'::regclass);


--
-- Name: ai_tool_call_log id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_tool_call_log ALTER COLUMN id SET DEFAULT nextval('public.ai_tool_call_log_id_seq'::regclass);


--
-- Name: ai_tool_idempotency id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_tool_idempotency ALTER COLUMN id SET DEFAULT nextval('public.ai_tool_idempotency_id_seq'::regclass);


--
-- Name: comments id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.comments ALTER COLUMN id SET DEFAULT nextval('public.comments_id_seq'::regclass);


--
-- Name: coupon_redemptions id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.coupon_redemptions ALTER COLUMN id SET DEFAULT nextval('public.coupon_redemptions_id_seq'::regclass);


--
-- Name: coupons id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.coupons ALTER COLUMN id SET DEFAULT nextval('public.coupons_id_seq'::regclass);


--
-- Name: mail_accounts id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mail_accounts ALTER COLUMN id SET DEFAULT nextval('public.mail_accounts_id_seq'::regclass);


--
-- Name: mail_automation_node_logs id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mail_automation_node_logs ALTER COLUMN id SET DEFAULT nextval('public.mail_automation_node_logs_id_seq'::regclass);


--
-- Name: mail_automation_runs id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mail_automation_runs ALTER COLUMN id SET DEFAULT nextval('public.mail_automation_runs_id_seq'::regclass);


--
-- Name: mail_automations id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mail_automations ALTER COLUMN id SET DEFAULT nextval('public.mail_automations_id_seq'::regclass);


--
-- Name: mail_campaign_events id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mail_campaign_events ALTER COLUMN id SET DEFAULT nextval('public.mail_campaign_events_id_seq'::regclass);


--
-- Name: mail_campaigns id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mail_campaigns ALTER COLUMN id SET DEFAULT nextval('public.mail_campaigns_id_seq'::regclass);


--
-- Name: mail_contacts id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mail_contacts ALTER COLUMN id SET DEFAULT nextval('public.mail_contacts_id_seq'::regclass);


--
-- Name: mail_logs id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mail_logs ALTER COLUMN id SET DEFAULT nextval('public.mail_logs_id_seq'::regclass);


--
-- Name: mail_suppressions id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mail_suppressions ALTER COLUMN id SET DEFAULT nextval('public.mail_suppressions_id_seq'::regclass);


--
-- Name: mail_templates id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mail_templates ALTER COLUMN id SET DEFAULT nextval('public.mail_templates_id_seq'::regclass);


--
-- Name: membership_assignments id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.membership_assignments ALTER COLUMN id SET DEFAULT nextval('public.membership_assignments_id_seq'::regclass);


--
-- Name: membership_entitlements id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.membership_entitlements ALTER COLUMN id SET DEFAULT nextval('public.membership_entitlements_id_seq'::regclass);


--
-- Name: membership_tiers id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.membership_tiers ALTER COLUMN id SET DEFAULT nextval('public.membership_tiers_id_seq'::regclass);


--
-- Name: order_items id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.order_items ALTER COLUMN id SET DEFAULT nextval('public.order_items_id_seq'::regclass);


--
-- Name: order_return_items id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.order_return_items ALTER COLUMN id SET DEFAULT nextval('public.order_return_items_id_seq'::regclass);


--
-- Name: order_returns id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.order_returns ALTER COLUMN id SET DEFAULT nextval('public.order_returns_id_seq'::regclass);


--
-- Name: order_status_logs id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.order_status_logs ALTER COLUMN id SET DEFAULT nextval('public.order_status_logs_id_seq'::regclass);


--
-- Name: orders id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.orders ALTER COLUMN id SET DEFAULT nextval('public.orders_id_seq'::regclass);


--
-- Name: page_views id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_views ALTER COLUMN id SET DEFAULT nextval('public.page_views_id_seq1'::regclass);


--
-- Name: sys_admin id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_admin ALTER COLUMN id SET DEFAULT nextval('public.sys_admin_id_seq'::regclass);


--
-- Name: sys_attachment id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_attachment ALTER COLUMN id SET DEFAULT nextval('public.sys_attachment_id_seq'::regclass);


--
-- Name: sys_casbin_rule id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_casbin_rule ALTER COLUMN id SET DEFAULT nextval('public.sys_casbin_rule_id_seq'::regclass);


--
-- Name: sys_config id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_config ALTER COLUMN id SET DEFAULT nextval('public.sys_config_id_seq'::regclass);


--
-- Name: sys_dept id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_dept ALTER COLUMN id SET DEFAULT nextval('public.sys_dept_id_seq'::regclass);


--
-- Name: sys_file_category id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_file_category ALTER COLUMN id SET DEFAULT nextval('public.sys_file_category_id_seq'::regclass);


--
-- Name: sys_i18n id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_i18n ALTER COLUMN id SET DEFAULT nextval('public.sys_i18n_id_seq'::regclass);


--
-- Name: sys_media_variant id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_media_variant ALTER COLUMN id SET DEFAULT nextval('public.sys_media_variant_id_seq'::regclass);


--
-- Name: sys_menu_permission id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_menu_permission ALTER COLUMN id SET DEFAULT nextval('public.sys_menu_permission_id_seq'::regclass);


--
-- Name: sys_menus id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_menus ALTER COLUMN id SET DEFAULT nextval('public.sys_menus_id_seq'::regclass);


--
-- Name: sys_permission id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_permission ALTER COLUMN id SET DEFAULT nextval('public.sys_permission_id_seq'::regclass);


--
-- Name: sys_role id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_role ALTER COLUMN id SET DEFAULT nextval('public.sys_role_id_seq'::regclass);


--
-- Name: sys_rule id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_rule ALTER COLUMN id SET DEFAULT nextval('public.sys_rule_id_seq'::regclass);


--
-- Name: sys_rule_assignment id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_rule_assignment ALTER COLUMN id SET DEFAULT nextval('public.sys_rule_assignment_id_seq'::regclass);


--
-- Name: user_preferences id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_preferences ALTER COLUMN id SET DEFAULT nextval('public.user_preferences_id_seq'::regclass);


--
-- Name: user_profiles id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_profiles ALTER COLUMN id SET DEFAULT nextval('public.user_profiles_id_seq'::regclass);


--
-- Name: users id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users ALTER COLUMN id SET DEFAULT nextval('public.users_id_seq'::regclass);


--
-- Name: webhook_deliveries id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webhook_deliveries ALTER COLUMN id SET DEFAULT nextval('public.webhook_deliveries_id_seq'::regclass);


--
-- Name: webhook_endpoints id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webhook_endpoints ALTER COLUMN id SET DEFAULT nextval('public.webhook_endpoints_id_seq'::regclass);


--
-- Name: ai_access_token ai_access_token_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_access_token
    ADD CONSTRAINT ai_access_token_pkey PRIMARY KEY (id);


--
-- Name: ai_call_log ai_call_log_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_call_log
    ADD CONSTRAINT ai_call_log_pkey PRIMARY KEY (id);


--
-- Name: ai_event ai_event_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_event
    ADD CONSTRAINT ai_event_pkey PRIMARY KEY (id);


--
-- Name: ai_provider ai_provider_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_provider
    ADD CONSTRAINT ai_provider_pkey PRIMARY KEY (id);


--
-- Name: ai_session ai_session_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_session
    ADD CONSTRAINT ai_session_pkey PRIMARY KEY (id);


--
-- Name: ai_tool_call_log ai_tool_call_log_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_tool_call_log
    ADD CONSTRAINT ai_tool_call_log_pkey PRIMARY KEY (id);


--
-- Name: ai_tool_idempotency ai_tool_idempotency_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_tool_idempotency
    ADD CONSTRAINT ai_tool_idempotency_pkey PRIMARY KEY (id);


--
-- Name: blocks blocks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.blocks
    ADD CONSTRAINT blocks_pkey PRIMARY KEY (id);


--
-- Name: blueprint_versions blueprint_versions_blueprint_id_version_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.blueprint_versions
    ADD CONSTRAINT blueprint_versions_blueprint_id_version_key UNIQUE (blueprint_id, version);


--
-- Name: blueprint_versions blueprint_versions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.blueprint_versions
    ADD CONSTRAINT blueprint_versions_pkey PRIMARY KEY (id);


--
-- Name: blueprints blueprints_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.blueprints
    ADD CONSTRAINT blueprints_pkey PRIMARY KEY (id);


--
-- Name: build_jobs build_jobs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.build_jobs
    ADD CONSTRAINT build_jobs_pkey PRIMARY KEY (id);


--
-- Name: comments comments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.comments
    ADD CONSTRAINT comments_pkey PRIMARY KEY (id);


--
-- Name: content_objects content_objects_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.content_objects
    ADD CONSTRAINT content_objects_pkey PRIMARY KEY (content_hash);


--
-- Name: content_objects content_objects_provider_object_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.content_objects
    ADD CONSTRAINT content_objects_provider_object_key_key UNIQUE (provider, object_key);


--
-- Name: content_template_component_pins content_template_component_pins_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.content_template_component_pins
    ADD CONSTRAINT content_template_component_pins_pkey PRIMARY KEY (template_id, component_id);


--
-- Name: content_template_versions content_template_versions_id_template_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.content_template_versions
    ADD CONSTRAINT content_template_versions_id_template_id_key UNIQUE (id, template_id);


--
-- Name: content_template_versions content_template_versions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.content_template_versions
    ADD CONSTRAINT content_template_versions_pkey PRIMARY KEY (id);


--
-- Name: content_template_versions content_template_versions_template_id_version_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.content_template_versions
    ADD CONSTRAINT content_template_versions_template_id_version_key UNIQUE (template_id, version);


--
-- Name: content_templates content_templates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.content_templates
    ADD CONSTRAINT content_templates_pkey PRIMARY KEY (id);


--
-- Name: contents contents_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.contents
    ADD CONSTRAINT contents_pkey PRIMARY KEY (id);


--
-- Name: coupon_redemptions coupon_redemptions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.coupon_redemptions
    ADD CONSTRAINT coupon_redemptions_pkey PRIMARY KEY (id);


--
-- Name: coupons coupons_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.coupons
    ADD CONSTRAINT coupons_pkey PRIMARY KEY (id);


--
-- Name: document_snapshots document_snapshots_id_presentation_instance_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.document_snapshots
    ADD CONSTRAINT document_snapshots_id_presentation_instance_id_key UNIQUE (id, presentation_instance_id);


--
-- Name: document_snapshots document_snapshots_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.document_snapshots
    ADD CONSTRAINT document_snapshots_pkey PRIMARY KEY (id);


--
-- Name: inventory_bom_items inventory_bom_items_parent_variant_id_component_variant_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_bom_items
    ADD CONSTRAINT inventory_bom_items_parent_variant_id_component_variant_id_key UNIQUE (parent_variant_id, component_variant_id);


--
-- Name: inventory_bom_items inventory_bom_items_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_bom_items
    ADD CONSTRAINT inventory_bom_items_pkey PRIMARY KEY (id);


--
-- Name: inventory_change_reasons inventory_change_reasons_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_change_reasons
    ADD CONSTRAINT inventory_change_reasons_pkey PRIMARY KEY (id);


--
-- Name: inventory_purchase_order_lines inventory_purchase_order_lines_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_purchase_order_lines
    ADD CONSTRAINT inventory_purchase_order_lines_pkey PRIMARY KEY (id);


--
-- Name: inventory_purchase_orders inventory_purchase_orders_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_purchase_orders
    ADD CONSTRAINT inventory_purchase_orders_pkey PRIMARY KEY (id);


--
-- Name: inventory_purchase_receipt_items inventory_purchase_receipt_items_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_purchase_receipt_items
    ADD CONSTRAINT inventory_purchase_receipt_items_pkey PRIMARY KEY (id);


--
-- Name: inventory_purchase_receipts inventory_purchase_receipts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_purchase_receipts
    ADD CONSTRAINT inventory_purchase_receipts_pkey PRIMARY KEY (id);


--
-- Name: inventory_sources inventory_sources_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_sources
    ADD CONSTRAINT inventory_sources_pkey PRIMARY KEY (id);


--
-- Name: inventory_stock_movements inventory_stock_movements_pkey1; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stock_movements
    ADD CONSTRAINT inventory_stock_movements_pkey1 PRIMARY KEY (id, create_time);


--
-- Name: inventory_stock_movements_2026_09 inventory_stock_movements_2026_09_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stock_movements_2026_09
    ADD CONSTRAINT inventory_stock_movements_2026_09_pkey PRIMARY KEY (id, create_time);


--
-- Name: inventory_stock_movements_2026_10 inventory_stock_movements_2026_10_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stock_movements_2026_10
    ADD CONSTRAINT inventory_stock_movements_2026_10_pkey PRIMARY KEY (id, create_time);


--
-- Name: inventory_stock_movements_2026_11 inventory_stock_movements_2026_11_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stock_movements_2026_11
    ADD CONSTRAINT inventory_stock_movements_2026_11_pkey PRIMARY KEY (id, create_time);


--
-- Name: inventory_stock_movements_2026_12 inventory_stock_movements_2026_12_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stock_movements_2026_12
    ADD CONSTRAINT inventory_stock_movements_2026_12_pkey PRIMARY KEY (id, create_time);


--
-- Name: inventory_stock_movements_2027_01 inventory_stock_movements_2027_01_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stock_movements_2027_01
    ADD CONSTRAINT inventory_stock_movements_2027_01_pkey PRIMARY KEY (id, create_time);


--
-- Name: inventory_stock_movements_default inventory_stock_movements_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stock_movements_default
    ADD CONSTRAINT inventory_stock_movements_default_pkey PRIMARY KEY (id, create_time);


--
-- Name: inventory_stocks inventory_stocks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stocks
    ADD CONSTRAINT inventory_stocks_pkey PRIMARY KEY (id);


--
-- Name: inventory_stocks inventory_stocks_variant_id_warehouse_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stocks
    ADD CONSTRAINT inventory_stocks_variant_id_warehouse_id_key UNIQUE (variant_id, warehouse_id);


--
-- Name: inventory_warehouses inventory_warehouses_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_warehouses
    ADD CONSTRAINT inventory_warehouses_pkey PRIMARY KEY (id);


--
-- Name: mail_accounts mail_accounts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mail_accounts
    ADD CONSTRAINT mail_accounts_pkey PRIMARY KEY (id);


--
-- Name: mail_automation_node_logs mail_automation_node_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mail_automation_node_logs
    ADD CONSTRAINT mail_automation_node_logs_pkey PRIMARY KEY (id);


--
-- Name: mail_automation_runs mail_automation_runs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mail_automation_runs
    ADD CONSTRAINT mail_automation_runs_pkey PRIMARY KEY (id);


--
-- Name: mail_automations mail_automations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mail_automations
    ADD CONSTRAINT mail_automations_pkey PRIMARY KEY (id);


--
-- Name: mail_campaign_events mail_campaign_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mail_campaign_events
    ADD CONSTRAINT mail_campaign_events_pkey PRIMARY KEY (id);


--
-- Name: mail_campaigns mail_campaigns_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mail_campaigns
    ADD CONSTRAINT mail_campaigns_pkey PRIMARY KEY (id);


--
-- Name: mail_contacts mail_contacts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mail_contacts
    ADD CONSTRAINT mail_contacts_pkey PRIMARY KEY (id);


--
-- Name: mail_logs mail_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mail_logs
    ADD CONSTRAINT mail_logs_pkey PRIMARY KEY (id);


--
-- Name: mail_suppressions mail_suppressions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mail_suppressions
    ADD CONSTRAINT mail_suppressions_pkey PRIMARY KEY (id);


--
-- Name: mail_templates mail_templates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.mail_templates
    ADD CONSTRAINT mail_templates_pkey PRIMARY KEY (id);


--
-- Name: master_data_changes master_data_changes_pkey1; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.master_data_changes
    ADD CONSTRAINT master_data_changes_pkey1 PRIMARY KEY (id, create_time);


--
-- Name: master_data_changes_2026_09 master_data_changes_2026_09_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.master_data_changes_2026_09
    ADD CONSTRAINT master_data_changes_2026_09_pkey PRIMARY KEY (id, create_time);


--
-- Name: master_data_changes_2026_10 master_data_changes_2026_10_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.master_data_changes_2026_10
    ADD CONSTRAINT master_data_changes_2026_10_pkey PRIMARY KEY (id, create_time);


--
-- Name: master_data_changes_2026_11 master_data_changes_2026_11_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.master_data_changes_2026_11
    ADD CONSTRAINT master_data_changes_2026_11_pkey PRIMARY KEY (id, create_time);


--
-- Name: master_data_changes_2026_12 master_data_changes_2026_12_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.master_data_changes_2026_12
    ADD CONSTRAINT master_data_changes_2026_12_pkey PRIMARY KEY (id, create_time);


--
-- Name: master_data_changes_2027_01 master_data_changes_2027_01_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.master_data_changes_2027_01
    ADD CONSTRAINT master_data_changes_2027_01_pkey PRIMARY KEY (id, create_time);


--
-- Name: master_data_changes_default master_data_changes_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.master_data_changes_default
    ADD CONSTRAINT master_data_changes_default_pkey PRIMARY KEY (id, create_time);


--
-- Name: membership_assignments membership_assignments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.membership_assignments
    ADD CONSTRAINT membership_assignments_pkey PRIMARY KEY (id);


--
-- Name: membership_entitlements membership_entitlements_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.membership_entitlements
    ADD CONSTRAINT membership_entitlements_pkey PRIMARY KEY (id);


--
-- Name: membership_tiers membership_tiers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.membership_tiers
    ADD CONSTRAINT membership_tiers_pkey PRIMARY KEY (id);


--
-- Name: navigations navigations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.navigations
    ADD CONSTRAINT navigations_pkey PRIMARY KEY (id);


--
-- Name: order_items order_items_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.order_items
    ADD CONSTRAINT order_items_pkey PRIMARY KEY (id);


--
-- Name: order_return_items order_return_items_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.order_return_items
    ADD CONSTRAINT order_return_items_pkey PRIMARY KEY (id);


--
-- Name: order_returns order_returns_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.order_returns
    ADD CONSTRAINT order_returns_pkey PRIMARY KEY (id);


--
-- Name: order_status_logs order_status_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.order_status_logs
    ADD CONSTRAINT order_status_logs_pkey PRIMARY KEY (id);


--
-- Name: orders orders_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.orders
    ADD CONSTRAINT orders_pkey PRIMARY KEY (id);


--
-- Name: page_artifact_objects page_artifact_objects_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_artifact_objects
    ADD CONSTRAINT page_artifact_objects_pkey PRIMARY KEY (artifact_id, content_hash);


--
-- Name: page_artifacts page_artifacts_id_page_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_artifacts
    ADD CONSTRAINT page_artifacts_id_page_id_key UNIQUE (id, page_id);


--
-- Name: page_artifacts page_artifacts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_artifacts
    ADD CONSTRAINT page_artifacts_pkey PRIMARY KEY (id);


--
-- Name: page_component_pins page_component_pins_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_component_pins
    ADD CONSTRAINT page_component_pins_pkey PRIMARY KEY (page_id, component_id);


--
-- Name: page_dependencies page_dependencies_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_dependencies
    ADD CONSTRAINT page_dependencies_pkey PRIMARY KEY (artifact_id, dependency_kind, dependency_key);


--
-- Name: page_publication_plans page_publication_plans_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_publication_plans
    ADD CONSTRAINT page_publication_plans_pkey PRIMARY KEY (page_id, lang);


--
-- Name: page_publications page_publications_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_publications
    ADD CONSTRAINT page_publications_pkey PRIMARY KEY (page_id, lang);


--
-- Name: page_revisions page_revisions_page_id_version_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_revisions
    ADD CONSTRAINT page_revisions_page_id_version_key UNIQUE (page_id, version);


--
-- Name: page_revisions page_revisions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_revisions
    ADD CONSTRAINT page_revisions_pkey PRIMARY KEY (id);


--
-- Name: page_routes page_routes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_routes
    ADD CONSTRAINT page_routes_pkey PRIMARY KEY (project_id, path);


--
-- Name: page_schedules page_schedules_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_schedules
    ADD CONSTRAINT page_schedules_pkey PRIMARY KEY (id);


--
-- Name: page_site_slots page_site_slots_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_site_slots
    ADD CONSTRAINT page_site_slots_pkey PRIMARY KEY (id);


--
-- Name: page_stagings page_stagings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_stagings
    ADD CONSTRAINT page_stagings_pkey PRIMARY KEY (page_id, lang);


--
-- Name: page_views page_views_pkey1; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_views
    ADD CONSTRAINT page_views_pkey1 PRIMARY KEY (id, viewed_at);


--
-- Name: page_views_2026_09 page_views_2026_09_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_views_2026_09
    ADD CONSTRAINT page_views_2026_09_pkey PRIMARY KEY (id, viewed_at);


--
-- Name: page_views_2026_10 page_views_2026_10_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_views_2026_10
    ADD CONSTRAINT page_views_2026_10_pkey PRIMARY KEY (id, viewed_at);


--
-- Name: page_views_2026_11 page_views_2026_11_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_views_2026_11
    ADD CONSTRAINT page_views_2026_11_pkey PRIMARY KEY (id, viewed_at);


--
-- Name: page_views_2026_12 page_views_2026_12_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_views_2026_12
    ADD CONSTRAINT page_views_2026_12_pkey PRIMARY KEY (id, viewed_at);


--
-- Name: page_views_2027_01 page_views_2027_01_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_views_2027_01
    ADD CONSTRAINT page_views_2027_01_pkey PRIMARY KEY (id, viewed_at);


--
-- Name: page_views_daily page_views_daily_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_views_daily
    ADD CONSTRAINT page_views_daily_pkey PRIMARY KEY (project_id, day, scope, path);


--
-- Name: page_views_default page_views_default_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_views_default
    ADD CONSTRAINT page_views_default_pkey PRIMARY KEY (id, viewed_at);


--
-- Name: pages pages_id_project_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pages
    ADD CONSTRAINT pages_id_project_id_key UNIQUE (id, project_id);


--
-- Name: pages pages_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pages
    ADD CONSTRAINT pages_pkey PRIMARY KEY (id);


--
-- Name: plugin_registry plugin_registry_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.plugin_registry
    ADD CONSTRAINT plugin_registry_pkey PRIMARY KEY (plugin_id);


--
-- Name: presentation_artifacts presentation_artifacts_id_presentation_instance_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.presentation_artifacts
    ADD CONSTRAINT presentation_artifacts_id_presentation_instance_id_key UNIQUE (id, presentation_instance_id);


--
-- Name: presentation_artifacts presentation_artifacts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.presentation_artifacts
    ADD CONSTRAINT presentation_artifacts_pkey PRIMARY KEY (id);


--
-- Name: presentation_dependencies presentation_dependencies_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.presentation_dependencies
    ADD CONSTRAINT presentation_dependencies_pkey PRIMARY KEY (artifact_id, dependency_kind, dependency_key);


--
-- Name: presentation_instances presentation_instances_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.presentation_instances
    ADD CONSTRAINT presentation_instances_pkey PRIMARY KEY (id);


--
-- Name: presentation_instances presentation_instances_project_id_url_path_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.presentation_instances
    ADD CONSTRAINT presentation_instances_project_id_url_path_key UNIQUE (project_id, url_path);


--
-- Name: presentation_publications presentation_publications_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.presentation_publications
    ADD CONSTRAINT presentation_publications_pkey PRIMARY KEY (presentation_id, lang);


--
-- Name: product_attributes product_attributes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_attributes
    ADD CONSTRAINT product_attributes_pkey PRIMARY KEY (id);


--
-- Name: product_brands product_brands_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_brands
    ADD CONSTRAINT product_brands_pkey PRIMARY KEY (id);


--
-- Name: product_categories product_categories_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_categories
    ADD CONSTRAINT product_categories_pkey PRIMARY KEY (id);


--
-- Name: product_outbox_events product_outbox_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_outbox_events
    ADD CONSTRAINT product_outbox_events_pkey PRIMARY KEY (id);


--
-- Name: product_price_adjustment_items product_price_adjustment_items_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_price_adjustment_items
    ADD CONSTRAINT product_price_adjustment_items_pkey PRIMARY KEY (id);


--
-- Name: product_price_adjustments product_price_adjustments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_price_adjustments
    ADD CONSTRAINT product_price_adjustments_pkey PRIMARY KEY (id);


--
-- Name: product_ratings product_ratings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_ratings
    ADD CONSTRAINT product_ratings_pkey PRIMARY KEY (id);


--
-- Name: product_tags product_tags_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_tags
    ADD CONSTRAINT product_tags_pkey PRIMARY KEY (id);


--
-- Name: product_variants product_variants_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_variants
    ADD CONSTRAINT product_variants_pkey PRIMARY KEY (id);


--
-- Name: product_variants product_variants_product_id_sku_code_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_variants
    ADD CONSTRAINT product_variants_product_id_sku_code_key UNIQUE (product_id, sku_code);


--
-- Name: products products_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.products
    ADD CONSTRAINT products_pkey PRIMARY KEY (id);


--
-- Name: project_locales project_locales_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.project_locales
    ADD CONSTRAINT project_locales_pkey PRIMARY KEY (project_id, lang);


--
-- Name: projects projects_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.projects
    ADD CONSTRAINT projects_pkey PRIMARY KEY (id);


--
-- Name: publication_receipts publication_receipts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.publication_receipts
    ADD CONSTRAINT publication_receipts_pkey PRIMARY KEY (id);


--
-- Name: sys_admin sys_admin_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_admin
    ADD CONSTRAINT sys_admin_pkey PRIMARY KEY (id);


--
-- Name: sys_area sys_area_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_area
    ADD CONSTRAINT sys_area_pkey PRIMARY KEY (code);


--
-- Name: sys_attachment sys_attachment_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_attachment
    ADD CONSTRAINT sys_attachment_pkey PRIMARY KEY (id);


--
-- Name: sys_casbin_rule sys_casbin_rule_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_casbin_rule
    ADD CONSTRAINT sys_casbin_rule_pkey PRIMARY KEY (id);


--
-- Name: sys_config sys_config_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_config
    ADD CONSTRAINT sys_config_pkey PRIMARY KEY (id);


--
-- Name: sys_dept sys_dept_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_dept
    ADD CONSTRAINT sys_dept_pkey PRIMARY KEY (id);


--
-- Name: sys_dict sys_dict_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_dict
    ADD CONSTRAINT sys_dict_pkey PRIMARY KEY (type, code);


--
-- Name: sys_file_category sys_file_category_category_code_unique; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_file_category
    ADD CONSTRAINT sys_file_category_category_code_unique UNIQUE (category_code);


--
-- Name: sys_file_category sys_file_category_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_file_category
    ADD CONSTRAINT sys_file_category_pkey PRIMARY KEY (id);


--
-- Name: sys_i18n sys_i18n_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_i18n
    ADD CONSTRAINT sys_i18n_pkey PRIMARY KEY (id);


--
-- Name: sys_i18n_revision sys_i18n_revision_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_i18n_revision
    ADD CONSTRAINT sys_i18n_revision_pkey PRIMARY KEY (id);


--
-- Name: sys_media_variant sys_media_variant_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_media_variant
    ADD CONSTRAINT sys_media_variant_pkey PRIMARY KEY (id);


--
-- Name: sys_menu_permission sys_menu_permission_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_menu_permission
    ADD CONSTRAINT sys_menu_permission_pkey PRIMARY KEY (id);


--
-- Name: sys_menus sys_menus_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_menus
    ADD CONSTRAINT sys_menus_pkey PRIMARY KEY (id);


--
-- Name: sys_permission sys_permission_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_permission
    ADD CONSTRAINT sys_permission_pkey PRIMARY KEY (id);


--
-- Name: sys_role sys_role_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_role
    ADD CONSTRAINT sys_role_pkey PRIMARY KEY (id);


--
-- Name: sys_rule_assignment sys_rule_assignment_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_rule_assignment
    ADD CONSTRAINT sys_rule_assignment_pkey PRIMARY KEY (id);


--
-- Name: sys_rule sys_rule_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_rule
    ADD CONSTRAINT sys_rule_pkey PRIMARY KEY (id);


--
-- Name: themes themes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.themes
    ADD CONSTRAINT themes_pkey PRIMARY KEY (id);


--
-- Name: ai_event uk_ai_event_session_seq; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_event
    ADD CONSTRAINT uk_ai_event_session_seq UNIQUE (session_id, seq);


--
-- Name: ai_provider uk_ai_provider_provider_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_provider
    ADD CONSTRAINT uk_ai_provider_provider_key UNIQUE (provider_key);


--
-- Name: ai_session uk_ai_session_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_session
    ADD CONSTRAINT uk_ai_session_key UNIQUE (session_key);


--
-- Name: ai_tool_idempotency uk_ai_tool_idempotency; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.ai_tool_idempotency
    ADD CONSTRAINT uk_ai_tool_idempotency UNIQUE (tool_name, idem_key);


--
-- Name: orders uk_orders_no; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.orders
    ADD CONSTRAINT uk_orders_no UNIQUE (order_no);


--
-- Name: sys_admin uk_sys_admin_username; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_admin
    ADD CONSTRAINT uk_sys_admin_username UNIQUE (username);


--
-- Name: sys_config uk_sys_config_group_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_config
    ADD CONSTRAINT uk_sys_config_group_key UNIQUE (group_key);


--
-- Name: sys_dept uk_sys_dept_code; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_dept
    ADD CONSTRAINT uk_sys_dept_code UNIQUE (dept_code);


--
-- Name: sys_i18n uk_sys_i18n_key_lang; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_i18n
    ADD CONSTRAINT uk_sys_i18n_key_lang UNIQUE (item_key, lang);


--
-- Name: sys_permission uk_sys_permission_code; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_permission
    ADD CONSTRAINT uk_sys_permission_code UNIQUE (permission_code);


--
-- Name: sys_role uk_sys_role_code; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_role
    ADD CONSTRAINT uk_sys_role_code UNIQUE (role_code);


--
-- Name: inventory_stocks uq_inventory_stocks_warehouse_sku; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stocks
    ADD CONSTRAINT uq_inventory_stocks_warehouse_sku UNIQUE (warehouse_id, sku_code);


--
-- Name: sys_media_variant uq_media_variant_attachment_type; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_media_variant
    ADD CONSTRAINT uq_media_variant_attachment_type UNIQUE (attachment_id, variant_type);


--
-- Name: user_preferences user_preferences_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_preferences
    ADD CONSTRAINT user_preferences_pkey PRIMARY KEY (id);


--
-- Name: user_profiles user_profiles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_profiles
    ADD CONSTRAINT user_profiles_pkey PRIMARY KEY (id);


--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);


--
-- Name: webhook_deliveries webhook_deliveries_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webhook_deliveries
    ADD CONSTRAINT webhook_deliveries_pkey PRIMARY KEY (id);


--
-- Name: webhook_endpoints webhook_endpoints_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webhook_endpoints
    ADD CONSTRAINT webhook_endpoints_pkey PRIMARY KEY (id);


--
-- Name: idx_ai_access_token_prefix; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ai_access_token_prefix ON public.ai_access_token USING btree (token_prefix);


--
-- Name: idx_ai_access_token_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ai_access_token_user ON public.ai_access_token USING btree (user_id, id DESC);


--
-- Name: idx_ai_call_log_session_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ai_call_log_session_time ON public.ai_call_log USING btree (session_id, create_time DESC);


--
-- Name: idx_ai_call_log_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ai_call_log_time ON public.ai_call_log USING btree (create_time DESC);


--
-- Name: idx_ai_call_log_user_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ai_call_log_user_time ON public.ai_call_log USING btree (user_id, create_time DESC);


--
-- Name: idx_ai_event_session_kind; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ai_event_session_kind ON public.ai_event USING btree (session_id, kind);


--
-- Name: idx_ai_event_session_model; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ai_event_session_model ON public.ai_event USING btree (session_id, provider_key, model_id);


--
-- Name: idx_ai_event_user_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ai_event_user_time ON public.ai_event USING btree (user_id, create_time DESC);


--
-- Name: idx_ai_provider_sort; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ai_provider_sort ON public.ai_provider USING btree (sort);


--
-- Name: idx_ai_provider_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ai_provider_status ON public.ai_provider USING btree (status);


--
-- Name: idx_ai_session_status_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ai_session_status_time ON public.ai_session USING btree (status, update_time DESC);


--
-- Name: idx_ai_tool_call_log_session; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ai_tool_call_log_session ON public.ai_tool_call_log USING btree (session_id, id DESC);


--
-- Name: idx_ai_tool_call_log_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ai_tool_call_log_time ON public.ai_tool_call_log USING btree (create_time DESC);


--
-- Name: idx_ai_tool_call_log_tool_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ai_tool_call_log_tool_status ON public.ai_tool_call_log USING btree (tool_name, status);


--
-- Name: idx_ai_tool_call_log_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_ai_tool_call_log_user ON public.ai_tool_call_log USING btree (user_id, id DESC);


--
-- Name: idx_att_cat_time_alive; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_att_cat_time_alive ON public.sys_attachment USING btree (category_id, create_time DESC) WHERE (status = 1);


--
-- Name: idx_att_create_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_att_create_time ON public.sys_attachment USING btree (create_time);


--
-- Name: idx_att_extra_info_gin; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_att_extra_info_gin ON public.sys_attachment USING gin (extra_info);


--
-- Name: idx_att_file_name_trgm; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_att_file_name_trgm ON public.sys_attachment USING gin (file_name ext_shared.gin_trgm_ops);


--
-- Name: idx_att_file_path_alive; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_att_file_path_alive ON public.sys_attachment USING btree (file_path) WHERE (status = 1);


--
-- Name: idx_att_file_size; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_att_file_size ON public.sys_attachment USING btree (file_size);


--
-- Name: idx_att_status_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_att_status_time ON public.sys_attachment USING btree (status, create_time);


--
-- Name: idx_att_storage_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_att_storage_type ON public.sys_attachment USING btree (storage_type);


--
-- Name: idx_att_type_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_att_type_status ON public.sys_attachment USING btree (file_type, status);


--
-- Name: idx_att_update_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_att_update_time ON public.sys_attachment USING btree (update_time);


--
-- Name: idx_blocks_category; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_blocks_category ON public.blocks USING btree (project_id, category);


--
-- Name: idx_blocks_reuse_mode; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_blocks_reuse_mode ON public.blocks USING btree (project_id, reuse_mode);


--
-- Name: idx_build_jobs_claim; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_build_jobs_claim ON public.build_jobs USING btree (create_time) WHERE (status = 'pending'::text);


--
-- Name: idx_build_jobs_lease_expires; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_build_jobs_lease_expires ON public.build_jobs USING btree (lease_expires_time) WHERE (status = 'running'::text);


--
-- Name: idx_comments_author_recent; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_comments_author_recent ON public.comments USING btree (project_id, user_id, create_time DESC) WHERE (deleted_at IS NULL);


--
-- Name: idx_comments_entity_list; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_comments_entity_list ON public.comments USING btree (project_id, entity_type, entity_id, status, create_time DESC) WHERE (deleted_at IS NULL);


--
-- Name: idx_comments_parent; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_comments_parent ON public.comments USING btree (parent_id) WHERE (deleted_at IS NULL);


--
-- Name: idx_comments_review_queue; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_comments_review_queue ON public.comments USING btree (status, create_time DESC) WHERE (deleted_at IS NULL);


--
-- Name: idx_content_templates_default_per_project_type; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_content_templates_default_per_project_type ON public.content_templates USING btree (project_id, entity_type) WHERE (is_default = true);


--
-- Name: idx_content_templates_project_role; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_content_templates_project_role ON public.content_templates USING btree (project_id, entity_type, template_role);


--
-- Name: idx_contents_title_trgm; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_contents_title_trgm ON public.contents USING gin (title ext_shared.gin_trgm_ops);


--
-- Name: idx_contents_type_updated; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_contents_type_updated ON public.contents USING btree (entity_type, update_time DESC, id DESC);


--
-- Name: idx_coupon_redemptions_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_coupon_redemptions_project ON public.coupon_redemptions USING btree (project_id, coupon_id);


--
-- Name: idx_coupon_redemptions_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_coupon_redemptions_user ON public.coupon_redemptions USING btree (coupon_id, user_id);


--
-- Name: idx_coupons_project_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_coupons_project_status ON public.coupons USING btree (project_id, status);


--
-- Name: idx_fc_create_by; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_fc_create_by ON public.sys_file_category USING btree (create_by);


--
-- Name: idx_fc_parent_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_fc_parent_id ON public.sys_file_category USING btree (parent_id);


--
-- Name: idx_fc_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_fc_status ON public.sys_file_category USING btree (status);


--
-- Name: idx_fc_update_by; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_fc_update_by ON public.sys_file_category USING btree (update_by);


--
-- Name: idx_inventory_bom_component; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_bom_component ON public.inventory_bom_items USING btree (component_variant_id);


--
-- Name: idx_inventory_bom_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_bom_project ON public.inventory_bom_items USING btree (project_id);


--
-- Name: idx_inventory_movements_batch; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_movements_batch ON ONLY public.inventory_stock_movements USING btree (batch_id, create_time DESC);


--
-- Name: idx_inventory_movements_project_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_movements_project_time ON ONLY public.inventory_stock_movements USING btree (project_id, create_time DESC);


--
-- Name: idx_inventory_movements_reason; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_movements_reason ON ONLY public.inventory_stock_movements USING btree (reason_code);


--
-- Name: idx_inventory_movements_variant_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_movements_variant_time ON ONLY public.inventory_stock_movements USING btree (variant_id, create_time DESC);


--
-- Name: idx_inventory_movements_wh_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_movements_wh_time ON ONLY public.inventory_stock_movements USING btree (warehouse_id, create_time DESC);


--
-- Name: idx_inventory_purchase_lines_order; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_purchase_lines_order ON public.inventory_purchase_order_lines USING btree (order_id, sort, create_time);


--
-- Name: idx_inventory_purchase_lines_variant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_purchase_lines_variant ON public.inventory_purchase_order_lines USING btree (project_id, variant_id);


--
-- Name: idx_inventory_purchase_orders_project_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_purchase_orders_project_status ON public.inventory_purchase_orders USING btree (project_id, status, create_time DESC);


--
-- Name: idx_inventory_purchase_orders_source; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_purchase_orders_source ON public.inventory_purchase_orders USING btree (source_id);


--
-- Name: idx_inventory_purchase_receipt_items_receipt; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_purchase_receipt_items_receipt ON public.inventory_purchase_receipt_items USING btree (receipt_id);


--
-- Name: idx_inventory_purchase_receipt_items_sku; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_purchase_receipt_items_sku ON public.inventory_purchase_receipt_items USING btree (project_id, sku_code, create_time DESC);


--
-- Name: idx_inventory_purchase_receipt_items_variant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_purchase_receipt_items_variant ON public.inventory_purchase_receipt_items USING btree (project_id, variant_id, create_time DESC);


--
-- Name: idx_inventory_purchase_receipts_order; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_purchase_receipts_order ON public.inventory_purchase_receipts USING btree (order_id, received_at DESC);


--
-- Name: idx_inventory_purchase_receipts_source; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_purchase_receipts_source ON public.inventory_purchase_receipts USING btree (source_id, received_at DESC);


--
-- Name: idx_inventory_reasons_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_reasons_project ON public.inventory_change_reasons USING btree (project_id, direction, sort);


--
-- Name: idx_inventory_sources_project_party; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_sources_project_party ON public.inventory_sources USING btree (project_id, related_party);


--
-- Name: idx_inventory_sources_project_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_sources_project_type ON public.inventory_sources USING btree (project_id, type);


--
-- Name: idx_inventory_stocks_product; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_stocks_product ON public.inventory_stocks USING btree (product_id);


--
-- Name: idx_inventory_stocks_project_sku; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_stocks_project_sku ON public.inventory_stocks USING btree (project_id, sku_code);


--
-- Name: idx_inventory_stocks_variant_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_stocks_variant_project ON public.inventory_stocks USING btree (project_id, variant_id);


--
-- Name: idx_inventory_stocks_warehouse; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_stocks_warehouse ON public.inventory_stocks USING btree (warehouse_id);


--
-- Name: idx_inventory_stocks_warehouse_external_sku; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_stocks_warehouse_external_sku ON public.inventory_stocks USING btree (warehouse_id, external_sku);


--
-- Name: idx_inventory_stocks_warehouse_nonzero; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_inventory_stocks_warehouse_nonzero ON public.inventory_stocks USING btree (warehouse_id) WHERE (quantity <> 0);


--
-- Name: idx_mail_accounts_default; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_mail_accounts_default ON public.mail_accounts USING btree (purpose) WHERE is_default;


--
-- Name: idx_mail_accounts_purpose; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_mail_accounts_purpose ON public.mail_accounts USING btree (purpose);


--
-- Name: idx_mail_automation_node_logs_run; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_mail_automation_node_logs_run ON public.mail_automation_node_logs USING btree (run_id, id);


--
-- Name: idx_mail_automation_runs_active; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_mail_automation_runs_active ON public.mail_automation_runs USING btree (automation_id, contact_id) WHERE ((status)::text = ANY ((ARRAY['running'::character varying, 'waiting'::character varying])::text[]));


--
-- Name: idx_mail_automation_runs_contact; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_mail_automation_runs_contact ON public.mail_automation_runs USING btree (contact_id);


--
-- Name: idx_mail_automation_runs_due; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_mail_automation_runs_due ON public.mail_automation_runs USING btree (next_run_at) WHERE ((status)::text = 'waiting'::text);


--
-- Name: idx_mail_automations_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_mail_automations_status ON public.mail_automations USING btree (status);


--
-- Name: idx_mail_campaign_events_campaign; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_mail_campaign_events_campaign ON public.mail_campaign_events USING btree (campaign_id, event_type);


--
-- Name: idx_mail_campaign_events_contact; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_mail_campaign_events_contact ON public.mail_campaign_events USING btree (contact_id);


--
-- Name: idx_mail_campaign_events_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_mail_campaign_events_time ON public.mail_campaign_events USING btree (campaign_id, create_time DESC);


--
-- Name: idx_mail_campaigns_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_mail_campaigns_status ON public.mail_campaigns USING btree (status);


--
-- Name: idx_mail_contacts_email; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_mail_contacts_email ON public.mail_contacts USING btree (lower((email)::text));


--
-- Name: idx_mail_contacts_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_mail_contacts_status ON public.mail_contacts USING btree (status);


--
-- Name: idx_mail_contacts_tags; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_mail_contacts_tags ON public.mail_contacts USING gin (tags);


--
-- Name: idx_mail_contacts_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_mail_contacts_user_id ON public.mail_contacts USING btree (user_id) WHERE (user_id IS NOT NULL);


--
-- Name: idx_mail_logs_campaign; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_mail_logs_campaign ON public.mail_logs USING btree (campaign_id) WHERE (campaign_id IS NOT NULL);


--
-- Name: idx_mail_logs_contact; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_mail_logs_contact ON public.mail_logs USING btree (contact_id) WHERE (contact_id IS NOT NULL);


--
-- Name: idx_mail_logs_create_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_mail_logs_create_time ON public.mail_logs USING btree (create_time DESC);


--
-- Name: idx_mail_logs_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_mail_logs_status ON public.mail_logs USING btree (status);


--
-- Name: idx_mail_logs_to_email; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_mail_logs_to_email ON public.mail_logs USING btree (to_email);


--
-- Name: idx_mail_suppressions_email; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_mail_suppressions_email ON public.mail_suppressions USING btree (lower((email)::text));


--
-- Name: idx_mail_templates_key_locale; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_mail_templates_key_locale ON public.mail_templates USING btree (template_key, locale);


--
-- Name: idx_master_data_changes_entity; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_master_data_changes_entity ON ONLY public.master_data_changes USING btree (project_id, entity_type, entity_id, create_time DESC);


--
-- Name: idx_master_data_changes_field; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_master_data_changes_field ON ONLY public.master_data_changes USING btree (project_id, entity_type, field, create_time DESC);


--
-- Name: idx_master_data_changes_label_trgm; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_master_data_changes_label_trgm ON ONLY public.master_data_changes USING gin (entity_label ext_shared.gin_trgm_ops);


--
-- Name: idx_master_data_changes_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_master_data_changes_time ON ONLY public.master_data_changes USING btree (project_id, create_time DESC);


--
-- Name: idx_membership_assignments_project_tier; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_membership_assignments_project_tier ON public.membership_assignments USING btree (project_id, tier_id);


--
-- Name: idx_membership_tiers_project_sort; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_membership_tiers_project_sort ON public.membership_tiers USING btree (project_id, sort_order DESC) WHERE (deleted_at IS NULL);


--
-- Name: idx_mva_attachment; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_mva_attachment ON public.sys_media_variant USING btree (attachment_id);


--
-- Name: idx_mva_file_path; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_mva_file_path ON public.sys_media_variant USING btree (file_path);


--
-- Name: idx_mva_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_mva_status ON public.sys_media_variant USING btree (status);


--
-- Name: idx_navigations_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_navigations_project ON public.navigations USING btree (project_id, kind, sort_order);


--
-- Name: idx_navigations_source; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_navigations_source ON public.navigations USING btree (source_type, source_id);


--
-- Name: idx_order_items_order; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_order_items_order ON public.order_items USING btree (order_id);


--
-- Name: idx_order_items_variant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_order_items_variant ON public.order_items USING btree (variant_id);


--
-- Name: idx_order_return_items_order_item; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_order_return_items_order_item ON public.order_return_items USING btree (order_item_id);


--
-- Name: idx_order_return_items_return; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_order_return_items_return ON public.order_return_items USING btree (return_id);


--
-- Name: idx_order_returns_order; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_order_returns_order ON public.order_returns USING btree (order_id);


--
-- Name: idx_order_returns_project_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_order_returns_project_status ON public.order_returns USING btree (project_id, status);


--
-- Name: idx_order_returns_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_order_returns_user ON public.order_returns USING btree (user_id);


--
-- Name: idx_order_status_logs_order; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_order_status_logs_order ON public.order_status_logs USING btree (order_id, id);


--
-- Name: idx_orders_create_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_orders_create_time ON public.orders USING btree (create_time DESC);


--
-- Name: idx_orders_customer_email; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_orders_customer_email ON public.orders USING btree (customer_email);


--
-- Name: idx_orders_customer_email_trgm; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_orders_customer_email_trgm ON public.orders USING gin (customer_email ext_shared.gin_trgm_ops);


--
-- Name: idx_orders_customer_name_trgm; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_orders_customer_name_trgm ON public.orders USING gin (customer_name ext_shared.gin_trgm_ops);


--
-- Name: idx_orders_order_no_trgm; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_orders_order_no_trgm ON public.orders USING gin (order_no ext_shared.gin_trgm_ops);


--
-- Name: idx_orders_project_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_orders_project_status ON public.orders USING btree (project_id, status);


--
-- Name: idx_orders_project_status_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_orders_project_status_time ON public.orders USING btree (project_id, status, create_time DESC);


--
-- Name: idx_orders_transaction; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_orders_transaction ON public.orders USING btree (transaction_id) WHERE ((transaction_id)::text <> ''::text);


--
-- Name: idx_orders_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_orders_user ON public.orders USING btree (user_id) WHERE (user_id IS NOT NULL);


--
-- Name: idx_page_artifacts_gc; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_page_artifacts_gc ON public.page_artifacts USING btree (create_time) WHERE (payload_state = 'available'::text);


--
-- Name: idx_page_artifacts_page_hash; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_page_artifacts_page_hash ON public.page_artifacts USING btree (page_id, artifact_hash);


--
-- Name: idx_page_artifacts_registry; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_page_artifacts_registry ON public.page_artifacts USING btree (registry_version) WHERE (payload_state = 'available'::text);


--
-- Name: idx_page_deps_lookup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_page_deps_lookup ON public.page_dependencies USING btree (dependency_kind, dependency_key);


--
-- Name: idx_page_publication_plans_lang; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_page_publication_plans_lang ON public.page_publication_plans USING btree (lang);


--
-- Name: idx_page_routes_project_kind_path; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_page_routes_project_kind_path ON public.page_routes USING btree (project_id, route_kind, path);


--
-- Name: idx_page_schedules_due; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_page_schedules_due ON public.page_schedules USING btree (status, scheduled_at);


--
-- Name: idx_page_schedules_lease; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_page_schedules_lease ON public.page_schedules USING btree (lease_expires_time) WHERE (status = 'running'::text);


--
-- Name: idx_page_schedules_page_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_page_schedules_page_created ON public.page_schedules USING btree (page_id, create_time DESC, id DESC);


--
-- Name: idx_page_site_slots_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_page_site_slots_project ON public.page_site_slots USING btree (project_id);


--
-- Name: idx_page_views_daily_project_day; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_page_views_daily_project_day ON public.page_views_daily USING btree (project_id, day DESC);


--
-- Name: idx_page_views_daily_rank; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_page_views_daily_rank ON public.page_views_daily USING btree (project_id, day, scope, path);


--
-- Name: idx_page_views_project_path_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_page_views_project_path_time ON ONLY public.page_views USING btree (project_id, path, viewed_at DESC);


--
-- Name: idx_page_views_project_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_page_views_project_time ON ONLY public.page_views USING btree (project_id, viewed_at DESC);


--
-- Name: idx_pages_blockref; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_pages_blockref ON public.pages USING gin (jsonb_path_query_array(draft_document, '$.**."blockId"'::jsonpath)) WHERE (deleted_at IS NULL);


--
-- Name: idx_pages_structure_footer; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_pages_structure_footer ON public.pages USING btree (((((draft_document -> 'settings'::text) -> 'structure'::text) ->> 'footerBlockId'::text))) WHERE (deleted_at IS NULL);


--
-- Name: idx_pages_structure_header; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_pages_structure_header ON public.pages USING btree (((((draft_document -> 'settings'::text) -> 'structure'::text) ->> 'headerBlockId'::text))) WHERE (deleted_at IS NULL);


--
-- Name: idx_pages_theme_alive; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_pages_theme_alive ON public.pages USING btree (theme_id, update_time DESC) WHERE (deleted_at IS NULL);


--
-- Name: idx_pres_deps_lookup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_pres_deps_lookup ON public.presentation_dependencies USING btree (dependency_kind, dependency_key);


--
-- Name: idx_presentation_instances_project_role; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_presentation_instances_project_role ON public.presentation_instances USING btree (project_id, instance_role);


--
-- Name: idx_product_brands_project_sort; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_product_brands_project_sort ON public.product_brands USING btree (project_id, sort);


--
-- Name: idx_product_categories_parent; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_product_categories_parent ON public.product_categories USING btree (parent_id);


--
-- Name: idx_product_categories_project_sort; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_product_categories_project_sort ON public.product_categories USING btree (project_id, sort);


--
-- Name: idx_product_outbox_entity; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_product_outbox_entity ON public.product_outbox_events USING btree (entity_type, entity_id, id);


--
-- Name: idx_product_outbox_pending; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_product_outbox_pending ON public.product_outbox_events USING btree (id) WHERE (processed_time IS NULL);


--
-- Name: idx_product_price_adjustment_items_adjustment; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_product_price_adjustment_items_adjustment ON public.product_price_adjustment_items USING btree (adjustment_id);


--
-- Name: idx_product_price_adjustment_items_variant; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_product_price_adjustment_items_variant ON public.product_price_adjustment_items USING btree (variant_id, create_time DESC);


--
-- Name: idx_product_price_adjustments_project; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_product_price_adjustments_project ON public.product_price_adjustments USING btree (project_id, create_time DESC, id DESC);


--
-- Name: idx_product_ratings_product_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_product_ratings_product_id ON public.product_ratings USING btree (product_id);


--
-- Name: idx_product_ratings_project_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_product_ratings_project_id ON public.product_ratings USING btree (project_id);


--
-- Name: idx_product_variants_option_values; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_product_variants_option_values ON public.product_variants USING gin (option_values jsonb_path_ops);


--
-- Name: idx_product_variants_price_enabled; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_product_variants_price_enabled ON public.product_variants USING btree (product_id, price) WHERE enabled;


--
-- Name: idx_product_variants_product; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_product_variants_product ON public.product_variants USING btree (product_id);


--
-- Name: idx_products_attribute_ids; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_products_attribute_ids ON public.products USING gin (attribute_ids jsonb_path_ops);


--
-- Name: idx_products_brand_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_products_brand_id ON public.products USING btree (brand_id) WHERE (brand_id IS NOT NULL);


--
-- Name: idx_products_bundle_items_gin; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_products_bundle_items_gin ON public.products USING gin (bundle_items jsonb_path_ops);


--
-- Name: idx_products_category_ids; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_products_category_ids ON public.products USING gin (category_ids jsonb_path_ops);


--
-- Name: idx_products_name_trgm; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_products_name_trgm ON public.products USING gin (name ext_shared.gin_trgm_ops);


--
-- Name: idx_products_primary_category; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_products_primary_category ON public.products USING btree (primary_category_id);


--
-- Name: idx_products_project_status_sort; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_products_project_status_sort ON public.products USING btree (project_id, status, sort, create_time, id);


--
-- Name: idx_products_published_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_products_published_at ON public.products USING btree (project_id, published_at);


--
-- Name: idx_products_tag_ids; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_products_tag_ids ON public.products USING gin (tag_ids jsonb_path_ops);


--
-- Name: idx_publication_receipts_pending; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_publication_receipts_pending ON public.publication_receipts USING btree (create_time) WHERE (receipt_state = 'pending'::text);


--
-- Name: idx_sys_admin_email; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sys_admin_email ON public.sys_admin USING btree (email);


--
-- Name: idx_sys_admin_phone; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sys_admin_phone ON public.sys_admin USING btree (phone);


--
-- Name: idx_sys_admin_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sys_admin_status ON public.sys_admin USING btree (status);


--
-- Name: idx_sys_area_kind_level; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sys_area_kind_level ON public.sys_area USING btree (kind, level);


--
-- Name: idx_sys_area_parent; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sys_area_parent ON public.sys_area USING btree (parent_code);


--
-- Name: idx_sys_area_source; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sys_area_source ON public.sys_area USING btree (source);


--
-- Name: idx_sys_casbin_rule; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_sys_casbin_rule ON public.sys_casbin_rule USING btree (ptype, v0, v1, v2, v3, v4, v5);


--
-- Name: idx_sys_config_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sys_config_status ON public.sys_config USING btree (status);


--
-- Name: idx_sys_dict_type_enabled; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sys_dict_type_enabled ON public.sys_dict USING btree (type, enabled, sort_order);


--
-- Name: idx_sys_i18n_category; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sys_i18n_category ON public.sys_i18n USING btree (category);


--
-- Name: idx_sys_i18n_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sys_i18n_status ON public.sys_i18n USING btree (status);


--
-- Name: idx_sys_menu_permission_code; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sys_menu_permission_code ON public.sys_menu_permission USING btree (permission_code);


--
-- Name: idx_sys_menus_parent_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sys_menus_parent_id ON public.sys_menus USING btree (parent_id);


--
-- Name: idx_sys_permission_module; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sys_permission_module ON public.sys_permission USING btree (module);


--
-- Name: idx_sys_permission_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sys_permission_status ON public.sys_permission USING btree (status);


--
-- Name: idx_sys_rule_assignment_rule_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sys_rule_assignment_rule_id ON public.sys_rule_assignment USING btree (rule_id);


--
-- Name: idx_sys_rule_assignment_target_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sys_rule_assignment_target_id ON public.sys_rule_assignment USING btree (target_id);


--
-- Name: idx_sys_rule_assignment_target_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sys_rule_assignment_target_type ON public.sys_rule_assignment USING btree (target_type);


--
-- Name: idx_sys_rule_domain; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sys_rule_domain ON public.sys_rule USING btree (domain);


--
-- Name: idx_sys_rule_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sys_rule_status ON public.sys_rule USING btree (status);


--
-- Name: idx_sys_translation_context_lang; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sys_translation_context_lang ON public.sys_translation USING btree (context, lang);


--
-- Name: idx_sys_translation_engine_lang; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sys_translation_engine_lang ON public.sys_translation USING btree (engine, lang);


--
-- Name: idx_sys_translation_hash_lang; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sys_translation_hash_lang ON public.sys_translation USING btree (source_hash, lang);


--
-- Name: idx_sys_translation_project_hash_lang; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sys_translation_project_hash_lang ON public.sys_translation USING btree (project_id, source_hash, lang);


--
-- Name: idx_user_preferences_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_user_preferences_user_id ON public.user_preferences USING btree (user_id);


--
-- Name: idx_user_profiles_user_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_user_profiles_user_id ON public.user_profiles USING btree (user_id);


--
-- Name: idx_users_activation_key; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_activation_key ON public.users USING btree (activation_key) WHERE (activation_key IS NOT NULL);


--
-- Name: idx_users_deleted_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_deleted_at ON public.users USING btree (deleted_at);


--
-- Name: idx_users_email; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_users_email ON public.users USING btree (lower((email)::text)) WHERE ((email)::text <> ''::text);


--
-- Name: idx_users_search_trgm; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_search_trgm ON public.users USING gin (search_text ext_shared.gin_trgm_ops);


--
-- Name: idx_users_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_users_status ON public.users USING btree (status);


--
-- Name: idx_users_username; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_users_username ON public.users USING btree (lower((username)::text));


--
-- Name: idx_webhook_deliveries_event_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webhook_deliveries_event_type ON public.webhook_deliveries USING btree (event_type);


--
-- Name: idx_webhook_deliveries_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webhook_deliveries_status ON public.webhook_deliveries USING btree (status);


--
-- Name: idx_webhook_endpoints_event_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webhook_endpoints_event_type ON public.webhook_endpoints USING btree (event_type);


--
-- Name: idx_webhook_endpoints_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webhook_endpoints_status ON public.webhook_endpoints USING btree (status);


--
-- Name: inventory_stock_movements_2026_09_batch_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2026_09_batch_id_created_at_idx ON public.inventory_stock_movements_2026_09 USING btree (batch_id, create_time DESC);


--
-- Name: inventory_stock_movements_2026_09_project_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2026_09_project_id_created_at_idx ON public.inventory_stock_movements_2026_09 USING btree (project_id, create_time DESC);


--
-- Name: inventory_stock_movements_2026_09_reason_code_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2026_09_reason_code_idx ON public.inventory_stock_movements_2026_09 USING btree (reason_code);


--
-- Name: inventory_stock_movements_2026_09_variant_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2026_09_variant_id_created_at_idx ON public.inventory_stock_movements_2026_09 USING btree (variant_id, create_time DESC);


--
-- Name: inventory_stock_movements_2026_09_warehouse_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2026_09_warehouse_id_created_at_idx ON public.inventory_stock_movements_2026_09 USING btree (warehouse_id, create_time DESC);


--
-- Name: inventory_stock_movements_2026_10_batch_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2026_10_batch_id_created_at_idx ON public.inventory_stock_movements_2026_10 USING btree (batch_id, create_time DESC);


--
-- Name: inventory_stock_movements_2026_10_project_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2026_10_project_id_created_at_idx ON public.inventory_stock_movements_2026_10 USING btree (project_id, create_time DESC);


--
-- Name: inventory_stock_movements_2026_10_reason_code_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2026_10_reason_code_idx ON public.inventory_stock_movements_2026_10 USING btree (reason_code);


--
-- Name: inventory_stock_movements_2026_10_variant_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2026_10_variant_id_created_at_idx ON public.inventory_stock_movements_2026_10 USING btree (variant_id, create_time DESC);


--
-- Name: inventory_stock_movements_2026_10_warehouse_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2026_10_warehouse_id_created_at_idx ON public.inventory_stock_movements_2026_10 USING btree (warehouse_id, create_time DESC);


--
-- Name: inventory_stock_movements_2026_11_batch_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2026_11_batch_id_created_at_idx ON public.inventory_stock_movements_2026_11 USING btree (batch_id, create_time DESC);


--
-- Name: inventory_stock_movements_2026_11_project_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2026_11_project_id_created_at_idx ON public.inventory_stock_movements_2026_11 USING btree (project_id, create_time DESC);


--
-- Name: inventory_stock_movements_2026_11_reason_code_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2026_11_reason_code_idx ON public.inventory_stock_movements_2026_11 USING btree (reason_code);


--
-- Name: inventory_stock_movements_2026_11_variant_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2026_11_variant_id_created_at_idx ON public.inventory_stock_movements_2026_11 USING btree (variant_id, create_time DESC);


--
-- Name: inventory_stock_movements_2026_11_warehouse_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2026_11_warehouse_id_created_at_idx ON public.inventory_stock_movements_2026_11 USING btree (warehouse_id, create_time DESC);


--
-- Name: inventory_stock_movements_2026_12_batch_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2026_12_batch_id_created_at_idx ON public.inventory_stock_movements_2026_12 USING btree (batch_id, create_time DESC);


--
-- Name: inventory_stock_movements_2026_12_project_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2026_12_project_id_created_at_idx ON public.inventory_stock_movements_2026_12 USING btree (project_id, create_time DESC);


--
-- Name: inventory_stock_movements_2026_12_reason_code_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2026_12_reason_code_idx ON public.inventory_stock_movements_2026_12 USING btree (reason_code);


--
-- Name: inventory_stock_movements_2026_12_variant_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2026_12_variant_id_created_at_idx ON public.inventory_stock_movements_2026_12 USING btree (variant_id, create_time DESC);


--
-- Name: inventory_stock_movements_2026_12_warehouse_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2026_12_warehouse_id_created_at_idx ON public.inventory_stock_movements_2026_12 USING btree (warehouse_id, create_time DESC);


--
-- Name: inventory_stock_movements_2027_01_batch_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2027_01_batch_id_created_at_idx ON public.inventory_stock_movements_2027_01 USING btree (batch_id, create_time DESC);


--
-- Name: inventory_stock_movements_2027_01_project_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2027_01_project_id_created_at_idx ON public.inventory_stock_movements_2027_01 USING btree (project_id, create_time DESC);


--
-- Name: inventory_stock_movements_2027_01_reason_code_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2027_01_reason_code_idx ON public.inventory_stock_movements_2027_01 USING btree (reason_code);


--
-- Name: inventory_stock_movements_2027_01_variant_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2027_01_variant_id_created_at_idx ON public.inventory_stock_movements_2027_01 USING btree (variant_id, create_time DESC);


--
-- Name: inventory_stock_movements_2027_01_warehouse_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_2027_01_warehouse_id_created_at_idx ON public.inventory_stock_movements_2027_01 USING btree (warehouse_id, create_time DESC);


--
-- Name: inventory_stock_movements_default_batch_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_default_batch_id_created_at_idx ON public.inventory_stock_movements_default USING btree (batch_id, create_time DESC);


--
-- Name: inventory_stock_movements_default_project_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_default_project_id_created_at_idx ON public.inventory_stock_movements_default USING btree (project_id, create_time DESC);


--
-- Name: inventory_stock_movements_default_reason_code_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_default_reason_code_idx ON public.inventory_stock_movements_default USING btree (reason_code);


--
-- Name: inventory_stock_movements_default_variant_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_default_variant_id_created_at_idx ON public.inventory_stock_movements_default USING btree (variant_id, create_time DESC);


--
-- Name: inventory_stock_movements_default_warehouse_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX inventory_stock_movements_default_warehouse_id_created_at_idx ON public.inventory_stock_movements_default USING btree (warehouse_id, create_time DESC);


--
-- Name: master_data_changes_2026_09_entity_label_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX master_data_changes_2026_09_entity_label_idx ON public.master_data_changes_2026_09 USING gin (entity_label ext_shared.gin_trgm_ops);


--
-- Name: master_data_changes_2026_09_project_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX master_data_changes_2026_09_project_id_created_at_idx ON public.master_data_changes_2026_09 USING btree (project_id, create_time DESC);


--
-- Name: master_data_changes_2026_09_project_id_entity_type_entity_i_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX master_data_changes_2026_09_project_id_entity_type_entity_i_idx ON public.master_data_changes_2026_09 USING btree (project_id, entity_type, entity_id, create_time DESC);


--
-- Name: master_data_changes_2026_09_project_id_entity_type_field_cr_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX master_data_changes_2026_09_project_id_entity_type_field_cr_idx ON public.master_data_changes_2026_09 USING btree (project_id, entity_type, field, create_time DESC);


--
-- Name: master_data_changes_2026_10_entity_label_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX master_data_changes_2026_10_entity_label_idx ON public.master_data_changes_2026_10 USING gin (entity_label ext_shared.gin_trgm_ops);


--
-- Name: master_data_changes_2026_10_project_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX master_data_changes_2026_10_project_id_created_at_idx ON public.master_data_changes_2026_10 USING btree (project_id, create_time DESC);


--
-- Name: master_data_changes_2026_10_project_id_entity_type_entity_i_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX master_data_changes_2026_10_project_id_entity_type_entity_i_idx ON public.master_data_changes_2026_10 USING btree (project_id, entity_type, entity_id, create_time DESC);


--
-- Name: master_data_changes_2026_10_project_id_entity_type_field_cr_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX master_data_changes_2026_10_project_id_entity_type_field_cr_idx ON public.master_data_changes_2026_10 USING btree (project_id, entity_type, field, create_time DESC);


--
-- Name: master_data_changes_2026_11_entity_label_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX master_data_changes_2026_11_entity_label_idx ON public.master_data_changes_2026_11 USING gin (entity_label ext_shared.gin_trgm_ops);


--
-- Name: master_data_changes_2026_11_project_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX master_data_changes_2026_11_project_id_created_at_idx ON public.master_data_changes_2026_11 USING btree (project_id, create_time DESC);


--
-- Name: master_data_changes_2026_11_project_id_entity_type_entity_i_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX master_data_changes_2026_11_project_id_entity_type_entity_i_idx ON public.master_data_changes_2026_11 USING btree (project_id, entity_type, entity_id, create_time DESC);


--
-- Name: master_data_changes_2026_11_project_id_entity_type_field_cr_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX master_data_changes_2026_11_project_id_entity_type_field_cr_idx ON public.master_data_changes_2026_11 USING btree (project_id, entity_type, field, create_time DESC);


--
-- Name: master_data_changes_2026_12_entity_label_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX master_data_changes_2026_12_entity_label_idx ON public.master_data_changes_2026_12 USING gin (entity_label ext_shared.gin_trgm_ops);


--
-- Name: master_data_changes_2026_12_project_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX master_data_changes_2026_12_project_id_created_at_idx ON public.master_data_changes_2026_12 USING btree (project_id, create_time DESC);


--
-- Name: master_data_changes_2026_12_project_id_entity_type_entity_i_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX master_data_changes_2026_12_project_id_entity_type_entity_i_idx ON public.master_data_changes_2026_12 USING btree (project_id, entity_type, entity_id, create_time DESC);


--
-- Name: master_data_changes_2026_12_project_id_entity_type_field_cr_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX master_data_changes_2026_12_project_id_entity_type_field_cr_idx ON public.master_data_changes_2026_12 USING btree (project_id, entity_type, field, create_time DESC);


--
-- Name: master_data_changes_2027_01_entity_label_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX master_data_changes_2027_01_entity_label_idx ON public.master_data_changes_2027_01 USING gin (entity_label ext_shared.gin_trgm_ops);


--
-- Name: master_data_changes_2027_01_project_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX master_data_changes_2027_01_project_id_created_at_idx ON public.master_data_changes_2027_01 USING btree (project_id, create_time DESC);


--
-- Name: master_data_changes_2027_01_project_id_entity_type_entity_i_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX master_data_changes_2027_01_project_id_entity_type_entity_i_idx ON public.master_data_changes_2027_01 USING btree (project_id, entity_type, entity_id, create_time DESC);


--
-- Name: master_data_changes_2027_01_project_id_entity_type_field_cr_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX master_data_changes_2027_01_project_id_entity_type_field_cr_idx ON public.master_data_changes_2027_01 USING btree (project_id, entity_type, field, create_time DESC);


--
-- Name: master_data_changes_default_entity_label_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX master_data_changes_default_entity_label_idx ON public.master_data_changes_default USING gin (entity_label ext_shared.gin_trgm_ops);


--
-- Name: master_data_changes_default_project_id_created_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX master_data_changes_default_project_id_created_at_idx ON public.master_data_changes_default USING btree (project_id, create_time DESC);


--
-- Name: master_data_changes_default_project_id_entity_type_entity_i_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX master_data_changes_default_project_id_entity_type_entity_i_idx ON public.master_data_changes_default USING btree (project_id, entity_type, entity_id, create_time DESC);


--
-- Name: master_data_changes_default_project_id_entity_type_field_cr_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX master_data_changes_default_project_id_entity_type_field_cr_idx ON public.master_data_changes_default USING btree (project_id, entity_type, field, create_time DESC);


--
-- Name: page_views_2026_09_project_id_path_viewed_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX page_views_2026_09_project_id_path_viewed_at_idx ON public.page_views_2026_09 USING btree (project_id, path, viewed_at DESC);


--
-- Name: page_views_2026_09_project_id_viewed_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX page_views_2026_09_project_id_viewed_at_idx ON public.page_views_2026_09 USING btree (project_id, viewed_at DESC);


--
-- Name: page_views_2026_10_project_id_path_viewed_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX page_views_2026_10_project_id_path_viewed_at_idx ON public.page_views_2026_10 USING btree (project_id, path, viewed_at DESC);


--
-- Name: page_views_2026_10_project_id_viewed_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX page_views_2026_10_project_id_viewed_at_idx ON public.page_views_2026_10 USING btree (project_id, viewed_at DESC);


--
-- Name: page_views_2026_11_project_id_path_viewed_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX page_views_2026_11_project_id_path_viewed_at_idx ON public.page_views_2026_11 USING btree (project_id, path, viewed_at DESC);


--
-- Name: page_views_2026_11_project_id_viewed_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX page_views_2026_11_project_id_viewed_at_idx ON public.page_views_2026_11 USING btree (project_id, viewed_at DESC);


--
-- Name: page_views_2026_12_project_id_path_viewed_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX page_views_2026_12_project_id_path_viewed_at_idx ON public.page_views_2026_12 USING btree (project_id, path, viewed_at DESC);


--
-- Name: page_views_2026_12_project_id_viewed_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX page_views_2026_12_project_id_viewed_at_idx ON public.page_views_2026_12 USING btree (project_id, viewed_at DESC);


--
-- Name: page_views_2027_01_project_id_path_viewed_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX page_views_2027_01_project_id_path_viewed_at_idx ON public.page_views_2027_01 USING btree (project_id, path, viewed_at DESC);


--
-- Name: page_views_2027_01_project_id_viewed_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX page_views_2027_01_project_id_viewed_at_idx ON public.page_views_2027_01 USING btree (project_id, viewed_at DESC);


--
-- Name: page_views_default_project_id_path_viewed_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX page_views_default_project_id_path_viewed_at_idx ON public.page_views_default USING btree (project_id, path, viewed_at DESC);


--
-- Name: page_views_default_project_id_viewed_at_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX page_views_default_project_id_viewed_at_idx ON public.page_views_default USING btree (project_id, viewed_at DESC);


--
-- Name: uk_orders_request_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_orders_request_id ON public.orders USING btree (request_id) WHERE ((request_id)::text <> ''::text);


--
-- Name: uk_page_artifacts_page_version_lang; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_page_artifacts_page_version_lang ON public.page_artifacts USING btree (page_id, version, lang);


--
-- Name: uk_presentation_artifacts_instance_version_lang; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uk_presentation_artifacts_instance_version_lang ON public.presentation_artifacts USING btree (presentation_instance_id, version, lang);


--
-- Name: uq_ai_access_token_hash; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_ai_access_token_hash ON public.ai_access_token USING btree (token_hash);


--
-- Name: uq_attachment_md5_type_active; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_attachment_md5_type_active ON public.sys_attachment USING btree (md5, file_type) WHERE ((status = 1) AND (md5 IS NOT NULL));


--
-- Name: uq_blocks_project_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_blocks_project_name ON public.blocks USING btree (project_id, name);


--
-- Name: uq_blocks_project_name_lower; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_blocks_project_name_lower ON public.blocks USING btree (project_id, lower(name));


--
-- Name: uq_build_jobs_pending; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_build_jobs_pending ON public.build_jobs USING btree (source_type, source_id, build_input_hash, lang, intent) WHERE (status = 'pending'::text);


--
-- Name: uq_build_jobs_running_source; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_build_jobs_running_source ON public.build_jobs USING btree (source_type, source_id) WHERE (status = 'running'::text);


--
-- Name: uq_contents_type_slug; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_contents_type_slug ON public.contents USING btree (entity_type, slug);


--
-- Name: uq_coupon_redemptions_coupon_order; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_coupon_redemptions_coupon_order ON public.coupon_redemptions USING btree (coupon_id, order_id);


--
-- Name: uq_coupons_project_code; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_coupons_project_code ON public.coupons USING btree (project_id, upper((code)::text));


--
-- Name: uq_inventory_purchase_lines_order_variant; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_inventory_purchase_lines_order_variant ON public.inventory_purchase_order_lines USING btree (order_id, variant_id);


--
-- Name: uq_inventory_purchase_orders_project_code; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_inventory_purchase_orders_project_code ON public.inventory_purchase_orders USING btree (project_id, upper(code));


--
-- Name: uq_inventory_purchase_receipts_project_code; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_inventory_purchase_receipts_project_code ON public.inventory_purchase_receipts USING btree (project_id, upper(code));


--
-- Name: uq_inventory_purchase_receipts_project_request; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_inventory_purchase_receipts_project_request ON public.inventory_purchase_receipts USING btree (project_id, request_id) WHERE (request_id <> ''::text);


--
-- Name: uq_inventory_reasons_builtin_code; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_inventory_reasons_builtin_code ON public.inventory_change_reasons USING btree (lower(code)) WHERE (project_id IS NULL);


--
-- Name: uq_inventory_reasons_project_code; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_inventory_reasons_project_code ON public.inventory_change_reasons USING btree (project_id, lower(code)) WHERE (project_id IS NOT NULL);


--
-- Name: uq_inventory_sources_project_code; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_inventory_sources_project_code ON public.inventory_sources USING btree (project_id, upper(code));


--
-- Name: uq_inventory_warehouses_project_code; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_inventory_warehouses_project_code ON public.inventory_warehouses USING btree (project_id, upper(code));


--
-- Name: uq_inventory_warehouses_project_default; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_inventory_warehouses_project_default ON public.inventory_warehouses USING btree (project_id) WHERE is_default;


--
-- Name: uq_membership_assignments_project_user; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_membership_assignments_project_user ON public.membership_assignments USING btree (project_id, user_id);


--
-- Name: uq_membership_entitlements_tier_kind; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_membership_entitlements_tier_kind ON public.membership_entitlements USING btree (tier_id, kind);


--
-- Name: uq_membership_tiers_default; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_membership_tiers_default ON public.membership_tiers USING btree (project_id) WHERE (is_default AND (deleted_at IS NULL));


--
-- Name: uq_membership_tiers_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_membership_tiers_name ON public.membership_tiers USING btree (project_id, name) WHERE (deleted_at IS NULL);


--
-- Name: uq_membership_tiers_threshold; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_membership_tiers_threshold ON public.membership_tiers USING btree (project_id, threshold_amount) WHERE ((deleted_at IS NULL) AND (is_default = false));


--
-- Name: uq_navigation_project_kind_path; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_navigation_project_kind_path ON public.navigations USING btree (project_id, kind, path);


--
-- Name: uq_order_returns_no; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_order_returns_no ON public.order_returns USING btree (project_id, return_no);


--
-- Name: uq_order_returns_request; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_order_returns_request ON public.order_returns USING btree (project_id, request_id) WHERE ((request_id)::text <> ''::text);


--
-- Name: uq_page_schedules_active; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_page_schedules_active ON public.page_schedules USING btree (page_id, lang, action) WHERE (status = ANY (ARRAY['pending'::text, 'running'::text]));


--
-- Name: uq_page_site_slots_project_slot; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_page_site_slots_project_slot ON public.page_site_slots USING btree (project_id, slot);


--
-- Name: uq_presentation_instances_entity_role; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_presentation_instances_entity_role ON public.presentation_instances USING btree (entity_type, entity_id, instance_role);


--
-- Name: uq_product_attributes_project_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_product_attributes_project_key ON public.product_attributes USING btree (project_id, key);


--
-- Name: uq_product_attributes_project_key_nonempty; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_product_attributes_project_key_nonempty ON public.product_attributes USING btree (project_id, key) WHERE (btrim(key) <> ''::text);


--
-- Name: uq_product_brands_project_slug; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_product_brands_project_slug ON public.product_brands USING btree (project_id, slug);


--
-- Name: uq_product_categories_project_slug; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_product_categories_project_slug ON public.product_categories USING btree (project_id, slug);


--
-- Name: uq_product_tags_project_slug; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_product_tags_project_slug ON public.product_tags USING btree (project_id, slug);


--
-- Name: uq_product_variants_product_options; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_product_variants_product_options ON public.product_variants USING btree (product_id, option_values) WHERE (option_values <> '{}'::jsonb);


--
-- Name: uq_products_project_sku_code; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_products_project_sku_code ON public.products USING btree (project_id, sku_code) WHERE (sku_code <> ''::text);


--
-- Name: uq_products_project_slug; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_products_project_slug ON public.products USING btree (project_id, slug);


--
-- Name: uq_project_locales_default; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_project_locales_default ON public.project_locales USING btree (project_id) WHERE is_default;


--
-- Name: uq_sys_menu_permission_menu_code; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_sys_menu_permission_menu_code ON public.sys_menu_permission USING btree (menu_id, permission_code);


--
-- Name: uq_sys_translation_scope_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_sys_translation_scope_key ON public.sys_translation USING btree (project_id, source_hash, context, lang) NULLS NOT DISTINCT;


--
-- Name: uq_themes_project_active; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_themes_project_active ON public.themes USING btree (project_id) WHERE (is_active = true);


--
-- Name: uq_themes_project_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_themes_project_name ON public.themes USING btree (project_id, name);


--
-- Name: inventory_stock_movements_2026_09_batch_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_batch ATTACH PARTITION public.inventory_stock_movements_2026_09_batch_id_created_at_idx;


--
-- Name: inventory_stock_movements_2026_09_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.inventory_stock_movements_pkey1 ATTACH PARTITION public.inventory_stock_movements_2026_09_pkey;


--
-- Name: inventory_stock_movements_2026_09_project_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_project_time ATTACH PARTITION public.inventory_stock_movements_2026_09_project_id_created_at_idx;


--
-- Name: inventory_stock_movements_2026_09_reason_code_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_reason ATTACH PARTITION public.inventory_stock_movements_2026_09_reason_code_idx;


--
-- Name: inventory_stock_movements_2026_09_variant_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_variant_time ATTACH PARTITION public.inventory_stock_movements_2026_09_variant_id_created_at_idx;


--
-- Name: inventory_stock_movements_2026_09_warehouse_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_wh_time ATTACH PARTITION public.inventory_stock_movements_2026_09_warehouse_id_created_at_idx;


--
-- Name: inventory_stock_movements_2026_10_batch_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_batch ATTACH PARTITION public.inventory_stock_movements_2026_10_batch_id_created_at_idx;


--
-- Name: inventory_stock_movements_2026_10_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.inventory_stock_movements_pkey1 ATTACH PARTITION public.inventory_stock_movements_2026_10_pkey;


--
-- Name: inventory_stock_movements_2026_10_project_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_project_time ATTACH PARTITION public.inventory_stock_movements_2026_10_project_id_created_at_idx;


--
-- Name: inventory_stock_movements_2026_10_reason_code_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_reason ATTACH PARTITION public.inventory_stock_movements_2026_10_reason_code_idx;


--
-- Name: inventory_stock_movements_2026_10_variant_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_variant_time ATTACH PARTITION public.inventory_stock_movements_2026_10_variant_id_created_at_idx;


--
-- Name: inventory_stock_movements_2026_10_warehouse_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_wh_time ATTACH PARTITION public.inventory_stock_movements_2026_10_warehouse_id_created_at_idx;


--
-- Name: inventory_stock_movements_2026_11_batch_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_batch ATTACH PARTITION public.inventory_stock_movements_2026_11_batch_id_created_at_idx;


--
-- Name: inventory_stock_movements_2026_11_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.inventory_stock_movements_pkey1 ATTACH PARTITION public.inventory_stock_movements_2026_11_pkey;


--
-- Name: inventory_stock_movements_2026_11_project_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_project_time ATTACH PARTITION public.inventory_stock_movements_2026_11_project_id_created_at_idx;


--
-- Name: inventory_stock_movements_2026_11_reason_code_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_reason ATTACH PARTITION public.inventory_stock_movements_2026_11_reason_code_idx;


--
-- Name: inventory_stock_movements_2026_11_variant_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_variant_time ATTACH PARTITION public.inventory_stock_movements_2026_11_variant_id_created_at_idx;


--
-- Name: inventory_stock_movements_2026_11_warehouse_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_wh_time ATTACH PARTITION public.inventory_stock_movements_2026_11_warehouse_id_created_at_idx;


--
-- Name: inventory_stock_movements_2026_12_batch_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_batch ATTACH PARTITION public.inventory_stock_movements_2026_12_batch_id_created_at_idx;


--
-- Name: inventory_stock_movements_2026_12_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.inventory_stock_movements_pkey1 ATTACH PARTITION public.inventory_stock_movements_2026_12_pkey;


--
-- Name: inventory_stock_movements_2026_12_project_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_project_time ATTACH PARTITION public.inventory_stock_movements_2026_12_project_id_created_at_idx;


--
-- Name: inventory_stock_movements_2026_12_reason_code_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_reason ATTACH PARTITION public.inventory_stock_movements_2026_12_reason_code_idx;


--
-- Name: inventory_stock_movements_2026_12_variant_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_variant_time ATTACH PARTITION public.inventory_stock_movements_2026_12_variant_id_created_at_idx;


--
-- Name: inventory_stock_movements_2026_12_warehouse_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_wh_time ATTACH PARTITION public.inventory_stock_movements_2026_12_warehouse_id_created_at_idx;


--
-- Name: inventory_stock_movements_2027_01_batch_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_batch ATTACH PARTITION public.inventory_stock_movements_2027_01_batch_id_created_at_idx;


--
-- Name: inventory_stock_movements_2027_01_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.inventory_stock_movements_pkey1 ATTACH PARTITION public.inventory_stock_movements_2027_01_pkey;


--
-- Name: inventory_stock_movements_2027_01_project_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_project_time ATTACH PARTITION public.inventory_stock_movements_2027_01_project_id_created_at_idx;


--
-- Name: inventory_stock_movements_2027_01_reason_code_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_reason ATTACH PARTITION public.inventory_stock_movements_2027_01_reason_code_idx;


--
-- Name: inventory_stock_movements_2027_01_variant_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_variant_time ATTACH PARTITION public.inventory_stock_movements_2027_01_variant_id_created_at_idx;


--
-- Name: inventory_stock_movements_2027_01_warehouse_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_wh_time ATTACH PARTITION public.inventory_stock_movements_2027_01_warehouse_id_created_at_idx;


--
-- Name: inventory_stock_movements_default_batch_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_batch ATTACH PARTITION public.inventory_stock_movements_default_batch_id_created_at_idx;


--
-- Name: inventory_stock_movements_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.inventory_stock_movements_pkey1 ATTACH PARTITION public.inventory_stock_movements_default_pkey;


--
-- Name: inventory_stock_movements_default_project_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_project_time ATTACH PARTITION public.inventory_stock_movements_default_project_id_created_at_idx;


--
-- Name: inventory_stock_movements_default_reason_code_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_reason ATTACH PARTITION public.inventory_stock_movements_default_reason_code_idx;


--
-- Name: inventory_stock_movements_default_variant_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_variant_time ATTACH PARTITION public.inventory_stock_movements_default_variant_id_created_at_idx;


--
-- Name: inventory_stock_movements_default_warehouse_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_inventory_movements_wh_time ATTACH PARTITION public.inventory_stock_movements_default_warehouse_id_created_at_idx;


--
-- Name: master_data_changes_2026_09_entity_label_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_master_data_changes_label_trgm ATTACH PARTITION public.master_data_changes_2026_09_entity_label_idx;


--
-- Name: master_data_changes_2026_09_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.master_data_changes_pkey1 ATTACH PARTITION public.master_data_changes_2026_09_pkey;


--
-- Name: master_data_changes_2026_09_project_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_master_data_changes_time ATTACH PARTITION public.master_data_changes_2026_09_project_id_created_at_idx;


--
-- Name: master_data_changes_2026_09_project_id_entity_type_entity_i_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_master_data_changes_entity ATTACH PARTITION public.master_data_changes_2026_09_project_id_entity_type_entity_i_idx;


--
-- Name: master_data_changes_2026_09_project_id_entity_type_field_cr_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_master_data_changes_field ATTACH PARTITION public.master_data_changes_2026_09_project_id_entity_type_field_cr_idx;


--
-- Name: master_data_changes_2026_10_entity_label_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_master_data_changes_label_trgm ATTACH PARTITION public.master_data_changes_2026_10_entity_label_idx;


--
-- Name: master_data_changes_2026_10_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.master_data_changes_pkey1 ATTACH PARTITION public.master_data_changes_2026_10_pkey;


--
-- Name: master_data_changes_2026_10_project_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_master_data_changes_time ATTACH PARTITION public.master_data_changes_2026_10_project_id_created_at_idx;


--
-- Name: master_data_changes_2026_10_project_id_entity_type_entity_i_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_master_data_changes_entity ATTACH PARTITION public.master_data_changes_2026_10_project_id_entity_type_entity_i_idx;


--
-- Name: master_data_changes_2026_10_project_id_entity_type_field_cr_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_master_data_changes_field ATTACH PARTITION public.master_data_changes_2026_10_project_id_entity_type_field_cr_idx;


--
-- Name: master_data_changes_2026_11_entity_label_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_master_data_changes_label_trgm ATTACH PARTITION public.master_data_changes_2026_11_entity_label_idx;


--
-- Name: master_data_changes_2026_11_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.master_data_changes_pkey1 ATTACH PARTITION public.master_data_changes_2026_11_pkey;


--
-- Name: master_data_changes_2026_11_project_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_master_data_changes_time ATTACH PARTITION public.master_data_changes_2026_11_project_id_created_at_idx;


--
-- Name: master_data_changes_2026_11_project_id_entity_type_entity_i_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_master_data_changes_entity ATTACH PARTITION public.master_data_changes_2026_11_project_id_entity_type_entity_i_idx;


--
-- Name: master_data_changes_2026_11_project_id_entity_type_field_cr_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_master_data_changes_field ATTACH PARTITION public.master_data_changes_2026_11_project_id_entity_type_field_cr_idx;


--
-- Name: master_data_changes_2026_12_entity_label_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_master_data_changes_label_trgm ATTACH PARTITION public.master_data_changes_2026_12_entity_label_idx;


--
-- Name: master_data_changes_2026_12_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.master_data_changes_pkey1 ATTACH PARTITION public.master_data_changes_2026_12_pkey;


--
-- Name: master_data_changes_2026_12_project_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_master_data_changes_time ATTACH PARTITION public.master_data_changes_2026_12_project_id_created_at_idx;


--
-- Name: master_data_changes_2026_12_project_id_entity_type_entity_i_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_master_data_changes_entity ATTACH PARTITION public.master_data_changes_2026_12_project_id_entity_type_entity_i_idx;


--
-- Name: master_data_changes_2026_12_project_id_entity_type_field_cr_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_master_data_changes_field ATTACH PARTITION public.master_data_changes_2026_12_project_id_entity_type_field_cr_idx;


--
-- Name: master_data_changes_2027_01_entity_label_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_master_data_changes_label_trgm ATTACH PARTITION public.master_data_changes_2027_01_entity_label_idx;


--
-- Name: master_data_changes_2027_01_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.master_data_changes_pkey1 ATTACH PARTITION public.master_data_changes_2027_01_pkey;


--
-- Name: master_data_changes_2027_01_project_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_master_data_changes_time ATTACH PARTITION public.master_data_changes_2027_01_project_id_created_at_idx;


--
-- Name: master_data_changes_2027_01_project_id_entity_type_entity_i_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_master_data_changes_entity ATTACH PARTITION public.master_data_changes_2027_01_project_id_entity_type_entity_i_idx;


--
-- Name: master_data_changes_2027_01_project_id_entity_type_field_cr_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_master_data_changes_field ATTACH PARTITION public.master_data_changes_2027_01_project_id_entity_type_field_cr_idx;


--
-- Name: master_data_changes_default_entity_label_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_master_data_changes_label_trgm ATTACH PARTITION public.master_data_changes_default_entity_label_idx;


--
-- Name: master_data_changes_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.master_data_changes_pkey1 ATTACH PARTITION public.master_data_changes_default_pkey;


--
-- Name: master_data_changes_default_project_id_created_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_master_data_changes_time ATTACH PARTITION public.master_data_changes_default_project_id_created_at_idx;


--
-- Name: master_data_changes_default_project_id_entity_type_entity_i_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_master_data_changes_entity ATTACH PARTITION public.master_data_changes_default_project_id_entity_type_entity_i_idx;


--
-- Name: master_data_changes_default_project_id_entity_type_field_cr_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_master_data_changes_field ATTACH PARTITION public.master_data_changes_default_project_id_entity_type_field_cr_idx;


--
-- Name: page_views_2026_09_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.page_views_pkey1 ATTACH PARTITION public.page_views_2026_09_pkey;


--
-- Name: page_views_2026_09_project_id_path_viewed_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_page_views_project_path_time ATTACH PARTITION public.page_views_2026_09_project_id_path_viewed_at_idx;


--
-- Name: page_views_2026_09_project_id_viewed_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_page_views_project_time ATTACH PARTITION public.page_views_2026_09_project_id_viewed_at_idx;


--
-- Name: page_views_2026_10_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.page_views_pkey1 ATTACH PARTITION public.page_views_2026_10_pkey;


--
-- Name: page_views_2026_10_project_id_path_viewed_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_page_views_project_path_time ATTACH PARTITION public.page_views_2026_10_project_id_path_viewed_at_idx;


--
-- Name: page_views_2026_10_project_id_viewed_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_page_views_project_time ATTACH PARTITION public.page_views_2026_10_project_id_viewed_at_idx;


--
-- Name: page_views_2026_11_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.page_views_pkey1 ATTACH PARTITION public.page_views_2026_11_pkey;


--
-- Name: page_views_2026_11_project_id_path_viewed_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_page_views_project_path_time ATTACH PARTITION public.page_views_2026_11_project_id_path_viewed_at_idx;


--
-- Name: page_views_2026_11_project_id_viewed_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_page_views_project_time ATTACH PARTITION public.page_views_2026_11_project_id_viewed_at_idx;


--
-- Name: page_views_2026_12_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.page_views_pkey1 ATTACH PARTITION public.page_views_2026_12_pkey;


--
-- Name: page_views_2026_12_project_id_path_viewed_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_page_views_project_path_time ATTACH PARTITION public.page_views_2026_12_project_id_path_viewed_at_idx;


--
-- Name: page_views_2026_12_project_id_viewed_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_page_views_project_time ATTACH PARTITION public.page_views_2026_12_project_id_viewed_at_idx;


--
-- Name: page_views_2027_01_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.page_views_pkey1 ATTACH PARTITION public.page_views_2027_01_pkey;


--
-- Name: page_views_2027_01_project_id_path_viewed_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_page_views_project_path_time ATTACH PARTITION public.page_views_2027_01_project_id_path_viewed_at_idx;


--
-- Name: page_views_2027_01_project_id_viewed_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_page_views_project_time ATTACH PARTITION public.page_views_2027_01_project_id_viewed_at_idx;


--
-- Name: page_views_default_pkey; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.page_views_pkey1 ATTACH PARTITION public.page_views_default_pkey;


--
-- Name: page_views_default_project_id_path_viewed_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_page_views_project_path_time ATTACH PARTITION public.page_views_default_project_id_path_viewed_at_idx;


--
-- Name: page_views_default_project_id_viewed_at_idx; Type: INDEX ATTACH; Schema: public; Owner: -
--

ALTER INDEX public.idx_page_views_project_time ATTACH PARTITION public.page_views_default_project_id_viewed_at_idx;


--
-- Name: inventory_purchase_order_lines trg_inventory_purchase_lines_status_sync; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_inventory_purchase_lines_status_sync AFTER INSERT OR DELETE OR UPDATE ON public.inventory_purchase_order_lines FOR EACH ROW EXECUTE FUNCTION public.fn_inventory_purchase_order_status_sync();


--
-- Name: inventory_purchase_orders trg_inventory_purchase_orders_status_guard; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_inventory_purchase_orders_status_guard BEFORE UPDATE OF status ON public.inventory_purchase_orders FOR EACH ROW EXECUTE FUNCTION public.fn_inventory_purchase_order_status_guard();


--
-- Name: master_data_changes trg_master_data_changes_append_only; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_master_data_changes_append_only BEFORE DELETE OR UPDATE ON public.master_data_changes FOR EACH ROW EXECUTE FUNCTION public.master_data_changes_append_only();


--
-- Name: sys_menus trg_sys_menus_sync_menu_permission; Type: TRIGGER; Schema: public; Owner: -
--

CREATE TRIGGER trg_sys_menus_sync_menu_permission AFTER INSERT OR UPDATE OF permission_code ON public.sys_menus FOR EACH ROW EXECUTE FUNCTION public.sys_menus_sync_menu_permission();


--
-- Name: blocks blocks_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.blocks
    ADD CONSTRAINT blocks_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: blueprint_versions blueprint_versions_blueprint_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.blueprint_versions
    ADD CONSTRAINT blueprint_versions_blueprint_id_fkey FOREIGN KEY (blueprint_id) REFERENCES public.blueprints(id);


--
-- Name: content_template_component_pins content_template_component_pins_template_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.content_template_component_pins
    ADD CONSTRAINT content_template_component_pins_template_id_fkey FOREIGN KEY (template_id) REFERENCES public.content_templates(id);


--
-- Name: content_template_versions content_template_versions_template_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.content_template_versions
    ADD CONSTRAINT content_template_versions_template_id_fkey FOREIGN KEY (template_id) REFERENCES public.content_templates(id);


--
-- Name: content_templates content_templates_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.content_templates
    ADD CONSTRAINT content_templates_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);


--
-- Name: document_snapshots document_snapshots_presentation_instance_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.document_snapshots
    ADD CONSTRAINT document_snapshots_presentation_instance_id_fkey FOREIGN KEY (presentation_instance_id) REFERENCES public.presentation_instances(id);


--
-- Name: document_snapshots document_snapshots_source_template_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.document_snapshots
    ADD CONSTRAINT document_snapshots_source_template_version_id_fkey FOREIGN KEY (source_template_version_id) REFERENCES public.content_template_versions(id);


--
-- Name: inventory_purchase_order_lines fk_inventory_purchase_lines_product; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_purchase_order_lines
    ADD CONSTRAINT fk_inventory_purchase_lines_product FOREIGN KEY (product_id) REFERENCES public.products(id) ON DELETE RESTRICT;


--
-- Name: inventory_purchase_order_lines fk_inventory_purchase_lines_variant; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_purchase_order_lines
    ADD CONSTRAINT fk_inventory_purchase_lines_variant FOREIGN KEY (variant_id) REFERENCES public.product_variants(id) ON DELETE RESTRICT;


--
-- Name: inventory_purchase_receipt_items fk_inventory_receipt_items_product; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_purchase_receipt_items
    ADD CONSTRAINT fk_inventory_receipt_items_product FOREIGN KEY (product_id) REFERENCES public.products(id) ON DELETE RESTRICT;


--
-- Name: inventory_purchase_receipt_items fk_inventory_receipt_items_variant; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_purchase_receipt_items
    ADD CONSTRAINT fk_inventory_receipt_items_variant FOREIGN KEY (variant_id) REFERENCES public.product_variants(id) ON DELETE RESTRICT;


--
-- Name: inventory_stocks fk_inventory_stocks_product; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stocks
    ADD CONSTRAINT fk_inventory_stocks_product FOREIGN KEY (product_id) REFERENCES public.products(id) ON DELETE CASCADE;


--
-- Name: sys_media_variant fk_media_variant_attachment; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_media_variant
    ADD CONSTRAINT fk_media_variant_attachment FOREIGN KEY (attachment_id) REFERENCES public.sys_attachment(id) ON UPDATE CASCADE ON DELETE CASCADE;


--
-- Name: product_price_adjustment_items fk_product_price_adjustment_items_product; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_price_adjustment_items
    ADD CONSTRAINT fk_product_price_adjustment_items_product FOREIGN KEY (product_id) REFERENCES public.products(id) ON DELETE RESTRICT;


--
-- Name: product_price_adjustment_items fk_product_price_adjustment_items_variant; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_price_adjustment_items
    ADD CONSTRAINT fk_product_price_adjustment_items_variant FOREIGN KEY (variant_id) REFERENCES public.product_variants(id) ON DELETE RESTRICT;


--
-- Name: sys_area fk_sys_area_parent; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_area
    ADD CONSTRAINT fk_sys_area_parent FOREIGN KEY (parent_code) REFERENCES public.sys_area(code);


--
-- Name: sys_attachment fk_sys_attachment_category_id; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_attachment
    ADD CONSTRAINT fk_sys_attachment_category_id FOREIGN KEY (category_id) REFERENCES public.sys_file_category(id) ON UPDATE CASCADE ON DELETE SET NULL;


--
-- Name: inventory_bom_items inventory_bom_items_component_variant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_bom_items
    ADD CONSTRAINT inventory_bom_items_component_variant_id_fkey FOREIGN KEY (component_variant_id) REFERENCES public.product_variants(id) ON DELETE CASCADE;


--
-- Name: inventory_bom_items inventory_bom_items_parent_variant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_bom_items
    ADD CONSTRAINT inventory_bom_items_parent_variant_id_fkey FOREIGN KEY (parent_variant_id) REFERENCES public.product_variants(id) ON DELETE CASCADE;


--
-- Name: inventory_bom_items inventory_bom_items_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_bom_items
    ADD CONSTRAINT inventory_bom_items_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);


--
-- Name: inventory_change_reasons inventory_change_reasons_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_change_reasons
    ADD CONSTRAINT inventory_change_reasons_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);


--
-- Name: inventory_purchase_order_lines inventory_purchase_order_lines_order_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_purchase_order_lines
    ADD CONSTRAINT inventory_purchase_order_lines_order_id_fkey FOREIGN KEY (order_id) REFERENCES public.inventory_purchase_orders(id) ON DELETE CASCADE;


--
-- Name: inventory_purchase_order_lines inventory_purchase_order_lines_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_purchase_order_lines
    ADD CONSTRAINT inventory_purchase_order_lines_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);


--
-- Name: inventory_purchase_orders inventory_purchase_orders_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_purchase_orders
    ADD CONSTRAINT inventory_purchase_orders_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);


--
-- Name: inventory_purchase_orders inventory_purchase_orders_source_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_purchase_orders
    ADD CONSTRAINT inventory_purchase_orders_source_id_fkey FOREIGN KEY (source_id) REFERENCES public.inventory_sources(id);


--
-- Name: inventory_purchase_orders inventory_purchase_orders_warehouse_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_purchase_orders
    ADD CONSTRAINT inventory_purchase_orders_warehouse_id_fkey FOREIGN KEY (warehouse_id) REFERENCES public.inventory_warehouses(id);


--
-- Name: inventory_purchase_receipt_items inventory_purchase_receipt_items_line_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_purchase_receipt_items
    ADD CONSTRAINT inventory_purchase_receipt_items_line_id_fkey FOREIGN KEY (line_id) REFERENCES public.inventory_purchase_order_lines(id);


--
-- Name: inventory_purchase_receipt_items inventory_purchase_receipt_items_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_purchase_receipt_items
    ADD CONSTRAINT inventory_purchase_receipt_items_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);


--
-- Name: inventory_purchase_receipt_items inventory_purchase_receipt_items_receipt_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_purchase_receipt_items
    ADD CONSTRAINT inventory_purchase_receipt_items_receipt_id_fkey FOREIGN KEY (receipt_id) REFERENCES public.inventory_purchase_receipts(id) ON DELETE CASCADE;


--
-- Name: inventory_purchase_receipts inventory_purchase_receipts_order_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_purchase_receipts
    ADD CONSTRAINT inventory_purchase_receipts_order_id_fkey FOREIGN KEY (order_id) REFERENCES public.inventory_purchase_orders(id);


--
-- Name: inventory_purchase_receipts inventory_purchase_receipts_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_purchase_receipts
    ADD CONSTRAINT inventory_purchase_receipts_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);


--
-- Name: inventory_purchase_receipts inventory_purchase_receipts_source_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_purchase_receipts
    ADD CONSTRAINT inventory_purchase_receipts_source_id_fkey FOREIGN KEY (source_id) REFERENCES public.inventory_sources(id);


--
-- Name: inventory_purchase_receipts inventory_purchase_receipts_warehouse_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_purchase_receipts
    ADD CONSTRAINT inventory_purchase_receipts_warehouse_id_fkey FOREIGN KEY (warehouse_id) REFERENCES public.inventory_warehouses(id);


--
-- Name: inventory_sources inventory_sources_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_sources
    ADD CONSTRAINT inventory_sources_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);


--
-- Name: inventory_stocks inventory_stocks_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stocks
    ADD CONSTRAINT inventory_stocks_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);


--
-- Name: inventory_stocks inventory_stocks_variant_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stocks
    ADD CONSTRAINT inventory_stocks_variant_id_fkey FOREIGN KEY (variant_id) REFERENCES public.product_variants(id) ON DELETE CASCADE;


--
-- Name: inventory_stocks inventory_stocks_warehouse_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_stocks
    ADD CONSTRAINT inventory_stocks_warehouse_id_fkey FOREIGN KEY (warehouse_id) REFERENCES public.inventory_warehouses(id) ON DELETE CASCADE;


--
-- Name: inventory_warehouses inventory_warehouses_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.inventory_warehouses
    ADD CONSTRAINT inventory_warehouses_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);


--
-- Name: navigations navigations_panel_block_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.navigations
    ADD CONSTRAINT navigations_panel_block_id_fkey FOREIGN KEY (panel_block_id) REFERENCES public.blocks(id) ON DELETE SET NULL;


--
-- Name: navigations navigations_parent_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.navigations
    ADD CONSTRAINT navigations_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES public.navigations(id) ON DELETE CASCADE;


--
-- Name: navigations navigations_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.navigations
    ADD CONSTRAINT navigations_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: page_artifact_objects page_artifact_objects_artifact_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_artifact_objects
    ADD CONSTRAINT page_artifact_objects_artifact_id_fkey FOREIGN KEY (artifact_id) REFERENCES public.page_artifacts(id);


--
-- Name: page_artifact_objects page_artifact_objects_content_hash_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_artifact_objects
    ADD CONSTRAINT page_artifact_objects_content_hash_fkey FOREIGN KEY (content_hash) REFERENCES public.content_objects(content_hash) ON DELETE CASCADE;


--
-- Name: page_artifacts page_artifacts_page_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_artifacts
    ADD CONSTRAINT page_artifacts_page_id_fkey FOREIGN KEY (page_id) REFERENCES public.pages(id);


--
-- Name: page_component_pins page_component_pins_page_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_component_pins
    ADD CONSTRAINT page_component_pins_page_id_fkey FOREIGN KEY (page_id) REFERENCES public.pages(id);


--
-- Name: page_dependencies page_dependencies_artifact_id_page_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_dependencies
    ADD CONSTRAINT page_dependencies_artifact_id_page_id_fkey FOREIGN KEY (artifact_id, page_id) REFERENCES public.page_artifacts(id, page_id);


--
-- Name: page_revisions page_revisions_page_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_revisions
    ADD CONSTRAINT page_revisions_page_id_fkey FOREIGN KEY (page_id) REFERENCES public.pages(id);


--
-- Name: page_routes page_routes_presentation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_routes
    ADD CONSTRAINT page_routes_presentation_id_fkey FOREIGN KEY (presentation_id) REFERENCES public.presentation_instances(id);


--
-- Name: page_routes page_routes_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_routes
    ADD CONSTRAINT page_routes_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);


--
-- Name: page_views_daily page_views_daily_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.page_views_daily
    ADD CONSTRAINT page_views_daily_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: pages pages_active_artifact_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pages
    ADD CONSTRAINT pages_active_artifact_fk FOREIGN KEY (active_artifact_id, id) REFERENCES public.page_artifacts(id, page_id);


--
-- Name: pages pages_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pages
    ADD CONSTRAINT pages_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);


--
-- Name: pages pages_source_blueprint_version_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pages
    ADD CONSTRAINT pages_source_blueprint_version_id_fkey FOREIGN KEY (source_blueprint_version_id) REFERENCES public.blueprint_versions(id);


--
-- Name: pages pages_staged_artifact_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pages
    ADD CONSTRAINT pages_staged_artifact_fk FOREIGN KEY (staged_artifact_id, id) REFERENCES public.page_artifacts(id, page_id);


--
-- Name: pages pages_theme_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pages
    ADD CONSTRAINT pages_theme_id_fkey FOREIGN KEY (theme_id) REFERENCES public.themes(id) ON DELETE SET NULL;


--
-- Name: presentation_artifacts presentation_artifacts_presentation_instance_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.presentation_artifacts
    ADD CONSTRAINT presentation_artifacts_presentation_instance_id_fkey FOREIGN KEY (presentation_instance_id) REFERENCES public.presentation_instances(id);


--
-- Name: presentation_artifacts presentation_artifacts_snapshot_id_presentation_instance_i_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.presentation_artifacts
    ADD CONSTRAINT presentation_artifacts_snapshot_id_presentation_instance_i_fkey FOREIGN KEY (snapshot_id, presentation_instance_id) REFERENCES public.document_snapshots(id, presentation_instance_id);


--
-- Name: presentation_dependencies presentation_dependencies_artifact_id_presentation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.presentation_dependencies
    ADD CONSTRAINT presentation_dependencies_artifact_id_presentation_id_fkey FOREIGN KEY (artifact_id, presentation_id) REFERENCES public.presentation_artifacts(id, presentation_instance_id);


--
-- Name: presentation_instances presentation_instances_active_artifact_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.presentation_instances
    ADD CONSTRAINT presentation_instances_active_artifact_fk FOREIGN KEY (active_artifact_id, id) REFERENCES public.presentation_artifacts(id, presentation_instance_id);


--
-- Name: presentation_instances presentation_instances_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.presentation_instances
    ADD CONSTRAINT presentation_instances_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);


--
-- Name: presentation_instances presentation_instances_snapshot_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.presentation_instances
    ADD CONSTRAINT presentation_instances_snapshot_fk FOREIGN KEY (current_snapshot_id, id) REFERENCES public.document_snapshots(id, presentation_instance_id);


--
-- Name: presentation_instances presentation_instances_staged_artifact_fk; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.presentation_instances
    ADD CONSTRAINT presentation_instances_staged_artifact_fk FOREIGN KEY (staged_artifact_id, id) REFERENCES public.presentation_artifacts(id, presentation_instance_id);


--
-- Name: presentation_instances presentation_instances_template_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.presentation_instances
    ADD CONSTRAINT presentation_instances_template_id_fkey FOREIGN KEY (template_id) REFERENCES public.content_templates(id);


--
-- Name: presentation_publications presentation_publications_presentation_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.presentation_publications
    ADD CONSTRAINT presentation_publications_presentation_id_fkey FOREIGN KEY (presentation_id) REFERENCES public.presentation_instances(id) ON DELETE CASCADE;


--
-- Name: product_attributes product_attributes_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_attributes
    ADD CONSTRAINT product_attributes_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);


--
-- Name: product_brands product_brands_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_brands
    ADD CONSTRAINT product_brands_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);


--
-- Name: product_categories product_categories_parent_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_categories
    ADD CONSTRAINT product_categories_parent_id_fkey FOREIGN KEY (parent_id) REFERENCES public.product_categories(id) ON DELETE SET NULL;


--
-- Name: product_categories product_categories_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_categories
    ADD CONSTRAINT product_categories_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);


--
-- Name: product_price_adjustment_items product_price_adjustment_items_adjustment_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_price_adjustment_items
    ADD CONSTRAINT product_price_adjustment_items_adjustment_id_fkey FOREIGN KEY (adjustment_id) REFERENCES public.product_price_adjustments(id) ON DELETE CASCADE;


--
-- Name: product_price_adjustments product_price_adjustments_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_price_adjustments
    ADD CONSTRAINT product_price_adjustments_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);


--
-- Name: product_tags product_tags_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_tags
    ADD CONSTRAINT product_tags_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);


--
-- Name: product_variants product_variants_product_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.product_variants
    ADD CONSTRAINT product_variants_product_id_fkey FOREIGN KEY (product_id) REFERENCES public.products(id) ON DELETE CASCADE;


--
-- Name: products products_brand_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.products
    ADD CONSTRAINT products_brand_id_fkey FOREIGN KEY (brand_id) REFERENCES public.product_brands(id) ON DELETE SET NULL;


--
-- Name: products products_primary_category_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.products
    ADD CONSTRAINT products_primary_category_id_fkey FOREIGN KEY (primary_category_id) REFERENCES public.product_categories(id) ON DELETE SET NULL;


--
-- Name: products products_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.products
    ADD CONSTRAINT products_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id);


--
-- Name: sys_translation sys_translation_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sys_translation
    ADD CONSTRAINT sys_translation_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: themes themes_project_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.themes
    ADD CONSTRAINT themes_project_id_fkey FOREIGN KEY (project_id) REFERENCES public.projects(id) ON DELETE CASCADE;


--
-- Name: webhook_deliveries webhook_deliveries_endpoint_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webhook_deliveries
    ADD CONSTRAINT webhook_deliveries_endpoint_id_fkey FOREIGN KEY (endpoint_id) REFERENCES public.webhook_endpoints(id) ON DELETE CASCADE;


--
-- Name: blocks; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.blocks ENABLE ROW LEVEL SECURITY;

--
-- Name: comments; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.comments ENABLE ROW LEVEL SECURITY;

--
-- Name: content_templates; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.content_templates ENABLE ROW LEVEL SECURITY;

--
-- Name: coupon_redemptions; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.coupon_redemptions ENABLE ROW LEVEL SECURITY;

--
-- Name: coupons; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.coupons ENABLE ROW LEVEL SECURITY;

--
-- Name: inventory_bom_items; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.inventory_bom_items ENABLE ROW LEVEL SECURITY;

--
-- Name: inventory_change_reasons; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.inventory_change_reasons ENABLE ROW LEVEL SECURITY;

--
-- Name: inventory_purchase_order_lines; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.inventory_purchase_order_lines ENABLE ROW LEVEL SECURITY;

--
-- Name: inventory_purchase_orders; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.inventory_purchase_orders ENABLE ROW LEVEL SECURITY;

--
-- Name: inventory_purchase_receipt_items; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.inventory_purchase_receipt_items ENABLE ROW LEVEL SECURITY;

--
-- Name: inventory_purchase_receipts; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.inventory_purchase_receipts ENABLE ROW LEVEL SECURITY;

--
-- Name: inventory_sources; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.inventory_sources ENABLE ROW LEVEL SECURITY;

--
-- Name: inventory_stock_movements; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.inventory_stock_movements ENABLE ROW LEVEL SECURITY;

--
-- Name: inventory_stock_movements_2026_09; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.inventory_stock_movements_2026_09 ENABLE ROW LEVEL SECURITY;

--
-- Name: inventory_stock_movements_2026_10; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.inventory_stock_movements_2026_10 ENABLE ROW LEVEL SECURITY;

--
-- Name: inventory_stock_movements_2026_11; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.inventory_stock_movements_2026_11 ENABLE ROW LEVEL SECURITY;

--
-- Name: inventory_stock_movements_2026_12; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.inventory_stock_movements_2026_12 ENABLE ROW LEVEL SECURITY;

--
-- Name: inventory_stock_movements_2027_01; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.inventory_stock_movements_2027_01 ENABLE ROW LEVEL SECURITY;

--
-- Name: inventory_stock_movements_default; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.inventory_stock_movements_default ENABLE ROW LEVEL SECURITY;

--
-- Name: inventory_stocks; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.inventory_stocks ENABLE ROW LEVEL SECURITY;

--
-- Name: inventory_warehouses; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.inventory_warehouses ENABLE ROW LEVEL SECURITY;

--
-- Name: master_data_changes; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.master_data_changes ENABLE ROW LEVEL SECURITY;

--
-- Name: master_data_changes_2026_09; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.master_data_changes_2026_09 ENABLE ROW LEVEL SECURITY;

--
-- Name: master_data_changes_2026_10; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.master_data_changes_2026_10 ENABLE ROW LEVEL SECURITY;

--
-- Name: master_data_changes_2026_11; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.master_data_changes_2026_11 ENABLE ROW LEVEL SECURITY;

--
-- Name: master_data_changes_2026_12; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.master_data_changes_2026_12 ENABLE ROW LEVEL SECURITY;

--
-- Name: master_data_changes_2027_01; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.master_data_changes_2027_01 ENABLE ROW LEVEL SECURITY;

--
-- Name: master_data_changes_default; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.master_data_changes_default ENABLE ROW LEVEL SECURITY;

--
-- Name: membership_assignments; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.membership_assignments ENABLE ROW LEVEL SECURITY;

--
-- Name: membership_tiers; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.membership_tiers ENABLE ROW LEVEL SECURITY;

--
-- Name: navigations; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.navigations ENABLE ROW LEVEL SECURITY;

--
-- Name: order_returns; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.order_returns ENABLE ROW LEVEL SECURITY;

--
-- Name: orders; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.orders ENABLE ROW LEVEL SECURITY;

--
-- Name: comments p_comments_project; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY p_comments_project ON public.comments USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: membership_assignments p_membership_assignments_project; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY p_membership_assignments_project ON public.membership_assignments USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: membership_tiers p_membership_tiers_project; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY p_membership_tiers_project ON public.membership_tiers USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: page_routes; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.page_routes ENABLE ROW LEVEL SECURITY;

--
-- Name: page_site_slots; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.page_site_slots ENABLE ROW LEVEL SECURITY;

--
-- Name: page_views; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.page_views ENABLE ROW LEVEL SECURITY;

--
-- Name: page_views_2026_09; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.page_views_2026_09 ENABLE ROW LEVEL SECURITY;

--
-- Name: page_views_2026_10; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.page_views_2026_10 ENABLE ROW LEVEL SECURITY;

--
-- Name: page_views_2026_11; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.page_views_2026_11 ENABLE ROW LEVEL SECURITY;

--
-- Name: page_views_2026_12; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.page_views_2026_12 ENABLE ROW LEVEL SECURITY;

--
-- Name: page_views_2027_01; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.page_views_2027_01 ENABLE ROW LEVEL SECURITY;

--
-- Name: page_views_daily; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.page_views_daily ENABLE ROW LEVEL SECURITY;

--
-- Name: page_views_default; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.page_views_default ENABLE ROW LEVEL SECURITY;

--
-- Name: pages; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.pages ENABLE ROW LEVEL SECURITY;

--
-- Name: presentation_instances; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.presentation_instances ENABLE ROW LEVEL SECURITY;

--
-- Name: product_attributes; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.product_attributes ENABLE ROW LEVEL SECURITY;

--
-- Name: product_brands; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.product_brands ENABLE ROW LEVEL SECURITY;

--
-- Name: product_categories; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.product_categories ENABLE ROW LEVEL SECURITY;

--
-- Name: product_price_adjustments; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.product_price_adjustments ENABLE ROW LEVEL SECURITY;

--
-- Name: product_ratings; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.product_ratings ENABLE ROW LEVEL SECURITY;

--
-- Name: product_tags; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.product_tags ENABLE ROW LEVEL SECURITY;

--
-- Name: products; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.products ENABLE ROW LEVEL SECURITY;

--
-- Name: blocks project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.blocks USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: content_templates project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.content_templates USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: coupon_redemptions project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.coupon_redemptions USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: coupons project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.coupons USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: inventory_bom_items project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.inventory_bom_items USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: inventory_change_reasons project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.inventory_change_reasons USING (((project_id IS NULL) OR (project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid))) WITH CHECK (((project_id IS NULL) OR (project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)));


--
-- Name: inventory_purchase_order_lines project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.inventory_purchase_order_lines USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: inventory_purchase_orders project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.inventory_purchase_orders USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: inventory_purchase_receipt_items project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.inventory_purchase_receipt_items USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: inventory_purchase_receipts project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.inventory_purchase_receipts USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: inventory_sources project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.inventory_sources USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: inventory_stock_movements project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.inventory_stock_movements USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: inventory_stock_movements_2026_09 project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.inventory_stock_movements_2026_09 USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: inventory_stock_movements_2026_10 project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.inventory_stock_movements_2026_10 USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: inventory_stock_movements_2026_11 project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.inventory_stock_movements_2026_11 USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: inventory_stock_movements_2026_12 project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.inventory_stock_movements_2026_12 USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: inventory_stock_movements_2027_01 project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.inventory_stock_movements_2027_01 USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: inventory_stock_movements_default project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.inventory_stock_movements_default USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: inventory_stocks project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.inventory_stocks USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: inventory_warehouses project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.inventory_warehouses USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: master_data_changes project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.master_data_changes USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: master_data_changes_2026_09 project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.master_data_changes_2026_09 USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: master_data_changes_2026_10 project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.master_data_changes_2026_10 USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: master_data_changes_2026_11 project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.master_data_changes_2026_11 USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: master_data_changes_2026_12 project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.master_data_changes_2026_12 USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: master_data_changes_2027_01 project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.master_data_changes_2027_01 USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: master_data_changes_default project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.master_data_changes_default USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: navigations project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.navigations USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: order_returns project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.order_returns USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: orders project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.orders USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: page_routes project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.page_routes USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: page_site_slots project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.page_site_slots USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: page_views project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.page_views USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: page_views_2026_09 project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.page_views_2026_09 USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: page_views_2026_10 project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.page_views_2026_10 USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: page_views_2026_11 project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.page_views_2026_11 USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: page_views_2026_12 project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.page_views_2026_12 USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: page_views_2027_01 project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.page_views_2027_01 USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: page_views_daily project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.page_views_daily USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: page_views_default project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.page_views_default USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: pages project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.pages USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: presentation_instances project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.presentation_instances USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: product_attributes project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.product_attributes USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: product_brands project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.product_brands USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: product_categories project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.product_categories USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: product_price_adjustments project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.product_price_adjustments USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: product_ratings project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.product_ratings USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: product_tags project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.product_tags USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: products project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.products USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: project_locales project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.project_locales USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: sys_translation project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.sys_translation USING (((project_id IS NULL) OR (project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid))) WITH CHECK (((project_id IS NULL) OR (project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)));


--
-- Name: themes project_isolation; Type: POLICY; Schema: public; Owner: -
--

CREATE POLICY project_isolation ON public.themes USING ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid)) WITH CHECK ((project_id = (NULLIF(current_setting('app.project_id'::text, true), ''::text))::uuid));


--
-- Name: project_locales; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.project_locales ENABLE ROW LEVEL SECURITY;

--
-- Name: sys_translation; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.sys_translation ENABLE ROW LEVEL SECURITY;

--
-- Name: themes; Type: ROW SECURITY; Schema: public; Owner: -
--

ALTER TABLE public.themes ENABLE ROW LEVEL SECURITY;

--
-- PostgreSQL database dump complete
--


