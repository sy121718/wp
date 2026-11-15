-- 基础对象：schema、扩展、触发器函数与初始化水位表。
-- 由 cmd/schema-split 生成；新增全局对象时改本文件。

CREATE SCHEMA IF NOT EXISTS ext_shared;

CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA ext_shared;

CREATE OR REPLACE FUNCTION public.fn_inventory_purchase_order_status_guard()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
DECLARE
    v_status text;
BEGIN
    -- 人工态（draft / exception / finished / cancelled）：不按明细行改判。
    -- 草稿改行不该自动变成「已下单」；异常中的单收货会被服务层拒绝，
    -- 行数据也就不会变 —— 推导在这里没有意义，只会把人的决定抹掉。
    IF NEW.status NOT IN ('ordered', 'partial', 'received') THEN
        RETURN NEW;
    END IF;

    SELECT CASE
               WHEN COUNT(*) = 0 THEN 'ordered'
               WHEN COUNT(*) FILTER (WHERE received_quantity < quantity) = 0 THEN 'received'
               WHEN COUNT(*) FILTER (WHERE received_quantity > 0) > 0 THEN 'partial'
               ELSE 'ordered'
           END
      INTO v_status
      FROM inventory_purchase_order_lines
     WHERE order_id = NEW.id;

    IF NEW.status IS DISTINCT FROM v_status THEN
        NEW.status := v_status;
    END IF;
    RETURN NEW;
END;
$function$;

CREATE OR REPLACE FUNCTION public.fn_inventory_purchase_order_status_sync()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
DECLARE
    v_order_id uuid;
    v_status   text;
BEGIN
    v_order_id := COALESCE(NEW.order_id, OLD.order_id);
    IF v_order_id IS NULL THEN
        RETURN NULL;
    END IF;

    SELECT CASE
               WHEN COUNT(*) = 0 THEN 'ordered'
               WHEN COUNT(*) FILTER (WHERE received_quantity < quantity) = 0 THEN 'received'
               WHEN COUNT(*) FILTER (WHERE received_quantity > 0) > 0 THEN 'partial'
               ELSE 'ordered'
           END
      INTO v_status
      FROM inventory_purchase_order_lines
     WHERE order_id = v_order_id;

    UPDATE inventory_purchase_orders
       SET status = v_status, update_time = now()
     WHERE id = v_order_id
       AND status IN ('ordered', 'partial', 'received')
       AND status IS DISTINCT FROM v_status;

    RETURN NULL;
END;
$function$;

CREATE OR REPLACE FUNCTION public.master_data_changes_append_only()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    RAISE EXCEPTION 'master_data_changes is append-only: % is rejected', TG_OP;
END;
$function$;

CREATE OR REPLACE FUNCTION public.sys_menus_sync_menu_permission()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    IF NEW.permission_code IS NOT NULL AND NEW.permission_code <> '' THEN
        INSERT INTO sys_menu_permission (menu_id, permission_code, create_time, update_time)
        VALUES (NEW.id, NEW.permission_code, now(), now())
        ON CONFLICT (menu_id, permission_code) DO NOTHING;
    END IF;
    RETURN NEW;
END;
$function$;

CREATE SEQUENCE public.sys_baseline_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

CREATE TABLE public.sys_baseline (
    id bigint NOT NULL,
    version text NOT NULL,
    manifest_sha256 character(64) NOT NULL,
    applied_at timestamp with time zone DEFAULT now() NOT NULL,
    note text DEFAULT ''::text NOT NULL
);

ALTER SEQUENCE public.sys_baseline_id_seq OWNED BY public.sys_baseline.id;

ALTER TABLE ONLY public.sys_baseline ALTER COLUMN id SET DEFAULT nextval('public.sys_baseline_id_seq'::regclass);

ALTER TABLE ONLY public.sys_baseline
    ADD CONSTRAINT sys_baseline_pkey PRIMARY KEY (id);
