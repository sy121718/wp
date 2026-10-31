package migrations

import "sync"

var registerInventoryTransferApprovalI18nOnce sync.Once

func init() { registerInventoryTransferApprovalI18n() }

func registerInventoryTransferApprovalI18n() {
	registerInventoryTransferApprovalI18nOnce.Do(func() {
		registerSeed(Seed{
			Version:   "651b-inventory-transfer-approval-i18n",
			TableName: "sys_i18n",
			ConditionSQL: `SELECT COUNT(*) FROM sys_i18n WHERE item_key IN (
				'admin.inventory.transfers.approve',
				'admin.inventory.transfers.approveHint',
				'admin.inventory.transfers.approveKeepTo',
				'admin.inventory.transfers.approveSubmit',
				'admin.inventory.transfers.approveTitle',
				'admin.inventory.transfers.approveToWh',
				'admin.inventory.transfers.attachAdd',
				'admin.inventory.transfers.attachEmpty',
				'admin.inventory.transfers.col.rejected',
				'admin.inventory.transfers.lineAdd',
				'admin.inventory.transfers.lines',
				'admin.inventory.transfers.linesEmpty',
				'admin.inventory.transfers.linesHint',
				'admin.inventory.transfers.reject',
				'admin.inventory.transfers.rejectReason',
				'admin.inventory.transfers.rejectReasonHint',
				'admin.inventory.transfers.rejectReasonPh',
				'admin.inventory.transfers.rejectSubmit',
				'admin.inventory.transfers.rejectTitle',
				'admin.inventory.transfers.status.rejected',
				'admin.inventory.transfers.status.requested',
				'ErrTransferRejectReasonRequired',
				'ErrTransferRejectReasonTooLong'
			)`,
			SQL: mustSQL("651b_inventory_transfer_approval_i18n.sql"),
		})
	})
}
