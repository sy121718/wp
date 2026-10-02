-- 486 · sys_dict：语言与货币字典（平面，按 type 判别）。
--
-- 为什么与 sys_area 分开：这两类是**平面列表**（一行一个语言 / 一个货币），没有父子层级，
-- 也不随地理变化；地理树是自引用结构。混一张表会让「取子级」这类查询对所有行都要假设层级。
--
-- 名字为什么不落库（本表没有 name 列）：
--   · 语言名 / 国家名的本地化由 i18n 运行时解析（Go 侧 golang.org/x/text/language/display，
--     数据源 Unicode CLDR），因此不需要译名表 —— 项目已依赖 golang.org/x/text（go.mod）。
--   · 代价：SQL 层搜不到名字，后台下拉按名称搜索要在内存过滤（语言数百行、货币 180 行，
--     这个量级无压力）；将来真要 SQL 级搜索再补名字列。
--   · 货币**不要**本地化名（口径已定：一个符号 + 一个短码足够），只留 code + symbol。
--
-- minor_unit 不是"装饰"：它是金额计算的必需属性（JPY=0 / CNY=2 / KWD=3），
-- 缺了它这些货币的金额会被静默按 2 位舍入 —— 因此用真列 + CHECK 钉死，不进 JSON。
--
-- url_code 是语言的 URL 路径短码（zh / en）。注意与数据库 lang 列的口径区别：
-- 内部逻辑（sys_i18n.lang / sys_translation.lang / project_locales.lang / 产物 Manifest.lang）
-- **始终用完整语言码 zh-CN**，只有 URL 路径段用短码；回退算法（未收录语言按主语言
-- 子标签小写，fr-CA → fr）保留在代码里，本列只提供「显式覆盖值」。
--
-- 幂等：CREATE TABLE IF NOT EXISTS / CREATE INDEX IF NOT EXISTS，重复执行安全。
-- 注册见 register_sys_reference.go：**本迁移刻意不带 TableName**，每次启动都会执行本文件全部语句 ——
-- 因此本文件**只能包含幂等语句**（CREATE TABLE IF NOT EXISTS / CREATE INDEX IF NOT EXISTS / COMMENT ON）。
-- 禁止加入 ALTER TABLE … ADD COLUMN 这类非幂等语句（会每次启动重复执行）；将来改结构要另开新迁移。
-- 理由：带 TableName 的迁移在表已存在时整条跳过，文件里 COMMENT ON 的改动永不生效，库与文件静默漂移。

CREATE TABLE IF NOT EXISTS sys_dict (
    type         VARCHAR(16) NOT NULL,
    code         VARCHAR(16) NOT NULL,
    url_code     VARCHAR(16),
    iso639_1     VARCHAR(8),
    iso639_2     VARCHAR(8),
    ui_available BOOLEAN     NOT NULL DEFAULT false,
    symbol       VARCHAR(8),
    minor_unit   SMALLINT,
    numeric_code VARCHAR(8),
    enabled      BOOLEAN     NOT NULL DEFAULT true,
    sort_order   INTEGER     NOT NULL DEFAULT 0,
    create_time  TIMESTAMPTZ NOT NULL DEFAULT now(),
    update_time  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (type, code),
    CONSTRAINT ck_sys_dict_type     CHECK (type IN ('language', 'currency')),
    CONSTRAINT ck_sys_dict_currency CHECK (type <> 'currency' OR minor_unit IS NOT NULL),
    CONSTRAINT ck_sys_dict_language CHECK (type <> 'language' OR url_code IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS idx_sys_dict_type_enabled ON sys_dict (type, enabled, sort_order);

COMMENT ON TABLE  sys_dict IS '语言与货币字典（平面，type 判别）；名称不落库，由 i18n 运行时解析';
COMMENT ON COLUMN sys_dict.type IS '类别：language 语言 / currency 货币';
COMMENT ON COLUMN sys_dict.code IS '语言=完整语言码（zh-CN，与 sys_i18n.lang 同口径）/ 货币=ISO 4217 字母码（CNY）';
COMMENT ON COLUMN sys_dict.url_code IS '语言 URL 路径短码（zh / en），一律小写（zh-tw / en-gb）——URL 路径段惯例，且与项目内置短码映射同形，大写会对不上；仅为显式覆盖值，未收录语言的确定性回退在代码里';
COMMENT ON COLUMN sys_dict.iso639_1 IS 'ISO 639-1 两字母码（仅语言行，可空）';
COMMENT ON COLUMN sys_dict.iso639_2 IS 'ISO 639-2 三字母码（仅语言行，可空）';
COMMENT ON COLUMN sys_dict.ui_available IS '该语言是否已有后台界面译文（seed 时按 sys_i18n 实际语种填充的快照，不随 sys_i18n 自动更新；给 sys_i18n 增加界面语种时必须同步更新本列）';
COMMENT ON COLUMN sys_dict.symbol IS '货币符号（仅货币行，如 ¥ / $ / €）';
COMMENT ON COLUMN sys_dict.minor_unit IS '货币小数位（仅货币行，ISO 4217：JPY=0 / CNY=2 / KWD=3）——金额计算必需';
COMMENT ON COLUMN sys_dict.numeric_code IS 'ISO 4217 数字码（仅货币行）';
COMMENT ON COLUMN sys_dict.enabled IS '是否启用（停用即不在下拉出现，不物理删除）';
COMMENT ON COLUMN sys_dict.sort_order IS '同类型内的展示顺序';
