package utils

import "flag"

const testFlagName = "test.v"

func IsTestProcess() bool {
	return flag.Lookup(testFlagName) != nil
}
