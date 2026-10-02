-- 485 · sys_area：地理地区树（洲 / 国家 / 行政区划一体，自引用）。
--
-- 一张表承载这几类，因为它们形状同构（都有码、有父级、有名称），且「国家属于哪个洲」
-- 天然就是树的父级 —— 分开建表反而要额外一列 region_code 去表达同一件事。
--
-- level 分五级：1 洲 / 2 次区域 / 3 国家 / 4 省级 / 5 地级。**洲与次区域同为 kind='continent'**，
-- 靠 level 区分（洲 = 1 且 parent_code 为空，次区域 = 2 且 parent_code 指向所属洲）；kind 本身
-- 分不出这两者，因此按 level 缩进渲染树时不会把次区域与洲并排。
--
-- code 口径（**分层用各自的标准码**，贯穿全树的只有主码一套）：
--   · continent：UN M.49 地理区划码 —— 洲（level 1）如 142 = 亚洲、019 = 美洲、150 = 欧洲；
--     次区域（level 2）如 030 = 东亚。
--   · country（level 3）：ISO 3166-1 alpha-2，如 CN / US；alpha3 存三位码（CHN）。
--   · division（level 4+）：一级行政区取 ISO 3166-2（CN-BJ），二级及以下**没有国际码**，
--     取国家统计局《统计用区划代码》带国家前缀（CN-110100）。
--   · 主码刻意选「能贯穿到最深层级」的那一套（统计码体系覆盖到区县，ISO 3166-2 只到一级），
--     否则父子链会在半途从字母码跳成数字码；国际码一律进 iso_code 列，对外对接不受影响。
--
-- 口径记录（避免将来评审当成 bug）：
--   · 香港 / 澳门 / 台湾按中国口径作为**省级行政单位**挂在 CN 之下（kind='division'），
--     code 取 CN-810000 / CN-820000 / CN-710000（统计码），iso_code 存 HK / MO / TW。
--   · 只处理中国相关条目；ISO 3166-1 的其他属地（波多黎各 PR / 关岛 GU / 格陵兰 GL 等）
--     保持 kind='country' 原样。数据上因此存在「HK 是 division、PR 是 country」的双标准，
--     这是本项目知情的口径选择，不是数据错误。
--   · 名称一律落库（name_zh / name_en），**不走 CLDR 运行时解析** —— CLDR 的 territory
--     命名自带它的立场，与本表口径不一致；行政区划名 CLDR 也根本不覆盖。
--
-- source 列标记批次来源（iso3166 / dr5hn / modood-cn / manual）：洲与国家十年不变、
-- 行政区划每年会变，同一张表里两种更新节奏，重灌时必须按 source 或 parent_code 限定
-- 范围，禁止整表 upsert（会覆盖手工调整过的行）。
--
-- 幂等：CREATE TABLE IF NOT EXISTS / CREATE INDEX IF NOT EXISTS，重复执行安全。
-- 注册见 register_sys_reference.go：**本迁移刻意不带 TableName**，每次启动都会执行本文件全部语句 ——
-- 因此本文件**只能包含幂等语句**（CREATE TABLE IF NOT EXISTS / CREATE INDEX IF NOT EXISTS / COMMENT ON）。
-- 禁止加入 ALTER TABLE … ADD COLUMN 这类非幂等语句（会每次启动重复执行）；将来改结构要另开新迁移。
-- 理由：带 TableName 的迁移在表已存在时整条跳过，文件里 COMMENT ON 的改动永不生效，库与文件静默漂移。

CREATE TABLE IF NOT EXISTS sys_area (
    code             VARCHAR(16)  PRIMARY KEY,
    kind             VARCHAR(16)  NOT NULL,
    level            SMALLINT     NOT NULL,
    parent_code      VARCHAR(16),
    name_zh          VARCHAR(128) NOT NULL,
    name_en          VARCHAR(128),
    iso_code         VARCHAR(16),
    alpha3           VARCHAR(3),
    numeric_code     VARCHAR(8),
    dial_code        VARCHAR(8),
    default_currency VARCHAR(8),
    default_lang     VARCHAR(16),
    source           VARCHAR(32)  NOT NULL DEFAULT 'manual',
    enabled          BOOLEAN      NOT NULL DEFAULT true,
    sort_order       INTEGER      NOT NULL DEFAULT 0,
    create_time      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    update_time      TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CONSTRAINT ck_sys_area_kind  CHECK (kind IN ('continent', 'country', 'division')),
    CONSTRAINT ck_sys_area_level CHECK (level BETWEEN 1 AND 5),
    CONSTRAINT fk_sys_area_parent FOREIGN KEY (parent_code) REFERENCES sys_area (code)
);

-- 按父级取子级（下拉级联的唯一查询形态）。
CREATE INDEX IF NOT EXISTS idx_sys_area_parent ON sys_area (parent_code);
-- 按类别取（"所有国家"下拉：kind='country'）。
CREATE INDEX IF NOT EXISTS idx_sys_area_kind_level ON sys_area (kind, level);
-- 重灌时按来源批次定位（见文件头 source 说明）。
CREATE INDEX IF NOT EXISTS idx_sys_area_source ON sys_area (source);

COMMENT ON TABLE  sys_area IS '地理地区树：洲与次区域（continent）/ 国家（country）/ 行政区划（division）自引用一体表';
COMMENT ON COLUMN sys_area.code IS '主码：洲与次区域=UN M.49（142 亚洲 / 030 东亚）/ 国家=ISO 3166-1 alpha-2（CN）/ 一级区划=ISO 3166-2（CN-BJ）/ 二级及以下=统计码带国家前缀（CN-110100）';
COMMENT ON COLUMN sys_area.kind IS '类别：continent 洲与次区域 / country 国家 / division 行政区划（判据列，不要靠 level 推）';
COMMENT ON COLUMN sys_area.level IS '层级：1 洲 2 次区域 3 国家 4 省级 5 地级（仅用于缩进与排序）；kind=''continent'' 同时涵盖洲与次区域，两者只能靠 level 区分（1 洲 / 2 次区域）';
COMMENT ON COLUMN sys_area.parent_code IS '父级 code（洲为空）；自引用外键';
COMMENT ON COLUMN sys_area.name_zh IS '中文名（本系统自有口径，不取第三方立场）';
COMMENT ON COLUMN sys_area.name_en IS '英文名';
COMMENT ON COLUMN sys_area.iso_code IS '对外对接用国际码：国家=ISO 3166-1（HK/MO/TW），一级区划=ISO 3166-2';
COMMENT ON COLUMN sys_area.alpha3 IS '国家三位码（ISO 3166-1 alpha-3，仅国家行）';
COMMENT ON COLUMN sys_area.numeric_code IS 'ISO 3166-1 数字码（仅国家行）';
COMMENT ON COLUMN sys_area.dial_code IS '国际电话区号（仅国家行）';
COMMENT ON COLUMN sys_area.default_currency IS '默认货币 code（仅国家行，**非强关联**：只作表单预填，不参与校验）';
COMMENT ON COLUMN sys_area.default_lang IS '默认语言 code（仅国家行，**非强关联**：只作表单预填，不参与校验）';
COMMENT ON COLUMN sys_area.source IS '数据来源批次：iso3166 / dr5hn / modood-cn / manual（重灌按它限定范围）';
COMMENT ON COLUMN sys_area.enabled IS '是否启用（停用即不在下拉出现，不物理删除）';
COMMENT ON COLUMN sys_area.sort_order IS '同父级内的展示顺序';
