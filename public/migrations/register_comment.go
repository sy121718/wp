package migrations

func init() {
	register(Migration{
		Version:   "466-comments",
		TableName: "comments",
		SQL:       mustSQL("466_comments.sql"),
	})
}
