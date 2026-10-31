package migrations

func init() {
	register(Migration{
		Version:   "496-media-variant-full-slot",
		TableName: "sys_media_variant",
		SQL:       mustSQL("496_media_variant_full_slot.sql"),
	})
}
