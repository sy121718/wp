package migrations

func registerShowcaseBlueprints() {
	const ids = "'3f1b6a52-7c4d-4a01-9e10-11f0a6b2c301', '3f1b6a52-7c4d-4a01-9e10-11f0a6b2c302', " +
		"'3f1b6a52-7c4d-4a01-9e10-11f0a6b2c303', '3f1b6a52-7c4d-4a01-9e10-11f0a6b2c304', " +
		"'3f1b6a52-7c4d-4a01-9e10-11f0a6b2c305', '3f1b6a52-7c4d-4a01-9e10-11f0a6b2c306'"
	registerSeed(Seed{
		Version:   "287-showcase-blueprints",
		TableName: "blueprints",
		ConditionSQL: "SELECT CASE WHEN " +
			"(SELECT COUNT(*) FROM blueprints WHERE id IN (" + ids + ")) + " +
			"(SELECT COUNT(*) FROM blueprint_versions WHERE version = 1 AND blueprint_id IN (" + ids + ")) " +
			"= 12 THEN 1 ELSE 0 END",
		SQL: mustSQL("287_showcase_blueprints.sql"),
	})
}

func init() { registerShowcaseBlueprints() }
