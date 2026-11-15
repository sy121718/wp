-- table: currency_rates（由 cmd/schema-split 生成；改结构直接改本文件）

-- @phase pre
CREATE SEQUENCE public.currency_rates_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

-- @phase ddl
CREATE TABLE public.currency_rates (
    id bigint NOT NULL,
    base_code text NOT NULL,
    quote_code text NOT NULL,
    rate numeric(18,8) NOT NULL,
    rate_at timestamp with time zone DEFAULT now() NOT NULL,
    source text DEFAULT 'manual'::text NOT NULL,
    update_time timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_currency_rates_code CHECK (((btrim(base_code) <> ''::text) AND (btrim(quote_code) <> ''::text))),
    CONSTRAINT ck_currency_rates_rate CHECK ((rate > (0)::numeric))
);

ALTER SEQUENCE public.currency_rates_id_seq OWNED BY public.currency_rates.id;

ALTER TABLE ONLY public.currency_rates ALTER COLUMN id SET DEFAULT nextval('public.currency_rates_id_seq'::regclass);

CREATE UNIQUE INDEX uq_currency_rates_pair ON public.currency_rates USING btree (upper(base_code), upper(quote_code));

-- @phase constr
ALTER TABLE ONLY public.currency_rates
    ADD CONSTRAINT currency_rates_pkey PRIMARY KEY (id);

-- @phase comment
COMMENT ON TABLE public.currency_rates IS '汇率（币种对，覆盖式：同一币对只留最新一条）';
