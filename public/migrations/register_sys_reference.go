package migrations

import "sync"

// register_sys_reference.go — 参考数据与系统配置（结构 484 / 485 / 486，种子 487 / 488 / 489 / 490）。
//
// 同一批次用一个 register 函数注册：
//
//	484 sys_config —— 系统配置（分组 JSON + 乐观锁版本号）
//	485 sys_area   —— 地理地区树（洲 / 国家 / 行政区划自引用一体）
//	486 sys_dict   —— 语言与货币字典（平面，type 判别）
//	487 seed       —— sys_area 初始数据（洲 6 / 次区域 22 / 国家 247 / 中国省级 34 / 中国地级 333）
//	488 seed       —— sys_dict 初始数据（语言 100 / 货币 178）
//	489 seed       —— sys_config 初始数据（i18n 组：default_lang / site_lang_url_mode）
//	490 seed       —— sys_config 的 i18n 组补 lang_url_codes（语言码 → URL 短码覆盖）
//
// 三张表都是新表（此前没有任何形态），但结构迁移**刻意不带 TableName**（理由见
// registerSysReferenceSQL 的注释）：每次启动都整条执行，靠文件内语句自身幂等，
// 而不是靠默认的「表存在即跳过」检查 —— 那个检查会让文件里后续新增/修改的 COMMENT ON 永不生效。
// 两条种子走 Seeds 台账 —— 迁移台账先跑、种子台账后跑（register.go 的 init() 顺序无关，
// 两个台账各自排序执行），所以建表一定先于灌数据，seed 的判定 SQL 不会打到不存在的表上。
//
// 门槛判据（红线：必须枚举本批自己的对象、上界封闭）：seed **不用** LIKE 前缀、**不用**全库总量，
// 按 485 的 source 列与 486 的 type 列**分别精确计数** —— 这两列是本批自有的批次标记列，
// 上界天然封闭（见 485 文件头 «source 列标记批次来源»）。
// 判据取「达到本批行数」这个下界：
//
//	· 运维删掉本批某行（source / type 不变）→ 条件不成立 → 下次启动重跑补回，幂等无害；
//	· 将来在同一 source / type 下加行 → 计数偏高、条件成立 → 本批被跳过 —— 但那是**同一批次内**
//	  的扩容，正确做法是连同这里的门槛一起改（与 190 的 key 列表同一条规矩）。
//
// 偏差方向刻意选「宁可重跑，不可静默跳过」（AGENTS.md「数据库」节）。
//
// 注册方式：由 register.go 的 init() 显式调用（不在文件尾自注册）。
func registerSysReference() {
	registerSysReferenceOnce.Do(registerSysReferenceSQL)
}

// registerSysReferenceOnce 让重复调用成为空操作（不会重复 register）。
var registerSysReferenceOnce sync.Once

// registerSysReferenceSQL 注册 484 / 485 / 486 与种子 487 / 488 / 489（真正干活的那一半，被 Once 包一层）。
//
// 484 / 485 / 486 **刻意不填 TableName**：migrator.apply 的默认 CheckSQL 是
// SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = ?，
// 且只在 CheckSQL 里出现 ? 时才把 TableName 传进去 —— TableName 为空即查「table_name 等于空串」，恒为 0，
// 于是整条 SQL 每次启动都会执行。带 TableName 时表一旦建成 count > 0，apply 直接
// 「迁移对象已存在，跳过」整条 SQL：文件里后加的 COMMENT ON / 新索引永不生效，库与文件静默漂移
// （本批 485 / 486 的列注释改动就撞上过，当时只能手动落库，不可复现）。
// 代价：这三个文件**只允许幂等语句**（CREATE TABLE/INDEX IF NOT EXISTS + COMMENT ON），
// 禁止 ALTER TABLE ... ADD COLUMN 之类非幂等语句；将来改结构要另开一条新迁移。
func registerSysReferenceSQL() {
	register(Migration{
		Version: "484-sys-config",
		SQL:     mustSQL("484_sys_config.sql"),
	})
	register(Migration{
		Version: "485-sys-area",
		SQL:     mustSQL("485_sys_area.sql"),
	})
	register(Migration{
		Version: "486-sys-dict",
		SQL:     mustSQL("486_sys_dict.sql"),
	})

	// 487：sys_area 初始数据 —— 洲 6 + 次区域 22（source='un-m49'，判据 28）、
	// 国家 247（source='iso3166'）、中国省级 34 + 地级 333（source='modood-cn'，判据 367），共 642 行。
	// 判据按**本批三个 source 分别精确计数**（三个子查询各自上界封闭，互不掩盖）。
	registerSeed(Seed{
		Version:   "487-seed-sys-area",
		TableName: "sys_area",
		ConditionSQL: "SELECT CASE WHEN " +
			"(SELECT count(*) FROM sys_area WHERE source = 'un-m49') >= 28 AND " +
			"(SELECT count(*) FROM sys_area WHERE source = 'iso3166') >= 247 AND " +
			"(SELECT count(*) FROM sys_area WHERE source = 'modood-cn') >= 367 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("487_seed_sys_area.sql"),
	})

	// 488：sys_dict 初始数据 —— 语言 100（type='language'）+ 货币 178（type='currency'），共 278 行。
	// 判据按 type 精确计数（两个子查询各自上界封闭）。
	registerSeed(Seed{
		Version:   "488-seed-sys-dict",
		TableName: "sys_dict",
		ConditionSQL: "SELECT CASE WHEN " +
			"(SELECT count(*) FROM sys_dict WHERE type = 'language') >= 100 AND " +
			"(SELECT count(*) FROM sys_dict WHERE type = 'currency') >= 178 " +
			"THEN 1 ELSE 0 END",
		SQL: mustSQL("488_seed_sys_dict.sql"),
	})

	// 489：sys_config 初始数据 —— i18n 组一行（default_lang=zh-CN / site_lang_url_mode=default_plain）。
	//
	// 判据按**本批自己的 key 逐条枚举**：group_key = 'i18n' 且组内两个键都在。上界封闭
	//（不用 LIKE 前缀、也不用全库总量）—— 将来别的批次往别组加行不会让计数虚高，运维删掉
	// 本行时条件不成立 → 下次启动补回。偏差方向刻意选「宁可重跑，不可静默跳过」。
	//
	// 用 jsonb_exists(config_data, '键') 而不是 `?` 操作符：Seed 的 ConditionSQL 由 gorm 直接
	// 执行，`?` 在 PostgreSQL 里不是占位符 —— 驱动把它当普通 SQL 文本送进服务端会直接语法错，
	// 这条 seed 从此跑不起来（且卡在本台账，挡住其后的所有种子）。
	registerSeed(Seed{
		Version:   "489-seed-sys-config",
		TableName: "sys_config",
		ConditionSQL: "SELECT CASE WHEN (" +
			"SELECT count(*) FROM sys_config WHERE group_key = 'i18n' " +
			"AND jsonb_exists(config_data, 'default_lang') " +
			"AND jsonb_exists(config_data, 'site_lang_url_mode') " +
			") >= 1 THEN 1 ELSE 0 END",
		SQL: mustSQL("489_seed_sys_config.sql"),
	})

	// 490：sys_config 的 i18n 组补 `lang_url_codes`（语言码 → URL 短码覆盖：en-AU → en）。
	//
	// **独立成一条迁移而不是改 489**：489 已执行（i18n 组那行已存在），而它是
	// 「ON CONFLICT (group_key) DO NOTHING」的单语句 seed —— 改它只对**全新库**生效，
	// 存量库那行已存在、静默什么都不会发生，两边从此分叉（新装站点有覆盖、老站点没有）。
	//
	// 门槛判据按**本批自己的键**：group_key = 'i18n' 且组内已有 lang_url_codes。
	// 上界封闭（不用 LIKE 前缀、不用全库总量）；运维删掉该键时条件不成立 → 下次启动补回。
	registerSeed(Seed{
		Version:   "490-seed-sys-config-lang-url-codes",
		TableName: "sys_config",
		ConditionSQL: "SELECT CASE WHEN (" +
			"SELECT count(*) FROM sys_config WHERE group_key = 'i18n' " +
			"AND jsonb_exists(config_data, 'lang_url_codes') " +
			") >= 1 THEN 1 ELSE 0 END",
		SQL: mustSQL("490_sys_config_lang_url_codes.sql"),
	})
}
