package productenums

func StatusFor(key string) int { return errFallbacks[key].status }
