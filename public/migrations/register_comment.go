package migrations

// register_comment.go — 评论表（迁移 466，BIZ-5）。
//
// 本迁移新建 comments 一张表（此前没有任何形态），因此用默认的「表存在即跳过」检查即可：
// 表不存在就整条执行，执行完必然存在。
//
// 本迁移同时做一件不属于「建表」但必须与表同批到达的事，理由写在 466 的文件头：
// 给带 project_id 的 comments 铺 RLS 策略（ENABLE + FORCE + POLICY）——
// 表已上线而策略还没铺的那段时间里，ENABLE RLS 的表对所有非属主连接一行都不可见，
// model 里包了 InProjectScope 也救不回来（策略不存在 = 谓词不参与判定，默认 deny）。
//
// 注册方式：本文件自带 init()，不在 register.go 的 init() 里再加一行 ——
// 「谁负责注册」只能有一个真源（与 462 / 460a / 459 的既有写法同形）。
func init() {
	register(Migration{
		Version:   "466-comments",
		TableName: "comments",
		SQL:       mustSQL("466_comments.sql"),
	})
}
