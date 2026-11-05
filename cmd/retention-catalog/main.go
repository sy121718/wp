package main

import (
	"fmt"

	"go_wp/pkg/database/retention"
)

func main() {
	fmt.Print(retention.FormatDeclarations())
}
