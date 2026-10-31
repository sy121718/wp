package view

type Result interface {
	PageStatus() int

	PageNoStore() bool
}
