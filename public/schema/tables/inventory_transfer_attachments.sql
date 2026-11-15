-- table: inventory_transfer_attachments（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.inventory_transfer_attachments_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.inventory_transfer_attachments (
    id bigint NOT NULL,
    transfer_id uuid NOT NULL,
    attachment_id bigint NOT NULL,
    usage_type text NOT NULL,
    sort integer DEFAULT 0 NOT NULL,
    create_time timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_inventory_transfer_attachments_usage CHECK ((usage_type = ANY (ARRAY['request'::text, 'ship'::text, 'receive'::text])))
);

ALTER SEQUENCE public.inventory_transfer_attachments_id_seq OWNED BY public.inventory_transfer_attachments.id;

ALTER TABLE ONLY public.inventory_transfer_attachments ALTER COLUMN id SET DEFAULT nextval('public.inventory_transfer_attachments_id_seq'::regclass);

CREATE UNIQUE INDEX uq_inventory_transfer_attachments_row ON public.inventory_transfer_attachments USING btree (transfer_id, attachment_id, usage_type);

-- @phase constr
ALTER TABLE ONLY public.inventory_transfer_attachments
    ADD CONSTRAINT inventory_transfer_attachments_pkey PRIMARY KEY (id);

-- @phase fk
ALTER TABLE ONLY public.inventory_transfer_attachments
    ADD CONSTRAINT inventory_transfer_attachments_transfer_id_fkey FOREIGN KEY (transfer_id) REFERENCES public.inventory_transfers(id) ON DELETE CASCADE;

-- @phase comment
COMMENT ON TABLE public.inventory_transfer_attachments IS '调拨单凭证：出货与签收各需至少一张（service 强制），申请凭证可选';

-- @phase data
SELECT pg_catalog.setval('public.inventory_transfer_attachments_id_seq', 1, false);
