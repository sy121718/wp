package migrations

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

func Fingerprint() string {
	h := sha256.New()
	for _, m := range All() {
		kind := "sql"
		if m.Func != nil {
			kind = "func"
		}
		fmt.Fprintf(h, "%d:%s|%d:%s|%d:%s|%d:%s|%d:%s\\n",
			len(m.Version), m.Version, len(m.TableName), m.TableName,
			len(m.CheckSQL), m.CheckSQL, len(m.SQL), m.SQL, len(kind), kind)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
