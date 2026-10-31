package migrations

func init() {
	register(Migration{
		Version:   "462-membership",
		TableName: "membership_tiers",
		SQL:       mustSQL("462_membership.sql"),
	})
}
