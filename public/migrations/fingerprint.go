package migrations

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Fingerprint 返回当前迁移集合的指纹（版本 + 表名 + 判定 + SQL 内容）。
//
// 测试基建用它判断「缓存下来的模板库是否还代表当前迁移」：测试不再为每个用例重跑
// 209 条迁移，而是复制一份跑完迁移的空库（CREATE DATABASE ... TEMPLATE，实测约 65ms，
// 对比跑完整迁移约 1.1s）。指纹一变就换一个新的模板库名重建 —— 于是不会拿过期结构去
// 跑测试，也就不会退化成「测试库与生产静默分叉」那个老问题（PIPE-3：presentation 曾用
// AutoMigrate 把 model 里多出来的列补进测试库，真实缺陷因此在测试里永远看不见）。
//
// 每段带长度前缀，避免「字段内容里恰好含有分隔符」导致两套不同的迁移算出同一指纹。
func Fingerprint() string {
	h := sha256.New()
	for _, m := range All() {
		fmt.Fprintf(h, "%d:%s|%d:%s|%d:%s|%d:%s\\n",
			len(m.Version), m.Version, len(m.TableName), m.TableName,
			len(m.CheckSQL), m.CheckSQL, len(m.SQL), m.SQL)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
