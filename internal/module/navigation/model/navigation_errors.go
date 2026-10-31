package navigationmodel

import "errors"

var ErrTransactionRequired = errors.New("navigationmodel: 该入口必须在调用方事务内调用")
