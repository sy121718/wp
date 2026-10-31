package migrations

func init() {
	register(Migration{
		Version: "617-returns-bulk-receipt-cleanup",
		SQL:     mustSQL("617_returns_bulk_receipt_cleanup.sql"),
	})
}
