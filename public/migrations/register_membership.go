package migrations

// register_membership.go — 会员等级与权益（迁移 462，BIZ-3）。
//
// 三张表都在本迁移里新建（此前没有任何形态），因此用默认的「表存在即跳过」检查即可：
// 表不存在就整条执行，执行完必然存在。代表的 TableName 取 membership_tiers ——
// 它是三张表里唯一「主实体」（归属与权益都挂在它或它指向的东西上）。
//
// 本迁移同时做两件不属于「建表」但必须与表同批到达的事，理由都写在 462 的文件头：
//
//	· 给两张带 project_id 的表铺 RLS 策略（ENABLE + FORCE + POLICY）——
//	  表已上线而策略还没铺的那段时间里，model 里包了 InProjectScope 也一行都挡不住；
//	· 给 orders 加 membership_discount_total 列（决策：折扣与券相加扣减、各自独立计账）。
//
// 注册方式：本文件自带 init()，不在 register.go 的 init() 里再加一行 ——
// 「谁负责注册」只能有一个真源（与 460a / 459 / 399 的既有写法同形）。
func init() {
	register(Migration{
		Version:   "462-membership",
		TableName: "membership_tiers",
		SQL:       mustSQL("462_membership.sql"),
	})
}
