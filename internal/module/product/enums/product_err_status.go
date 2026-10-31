package productenums

// StatusFor 返回错误声明中的状态码；零值交由统一出口按默认档处理。
func StatusFor(key string) int { return errFallbacks[key].status }
