package migrations

// register_inventory_transfer_approval_i18n.go — 651b：调拨审批的界面词条。
//
// 门槛逐条枚举本批的 22 个 key（中英共 44 行）：迁移台账只记「跑过」，而 seed 是
// 默认值来源 —— 用「本批代表 key」判，存量库不会恒满足、也不会因为别处新增词条而误判。

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
