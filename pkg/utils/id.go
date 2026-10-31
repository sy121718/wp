package utils

import "github.com/google/uuid"

func NewTimeOrderedID() string {
	return uuid.Must(uuid.NewV7()).String()
}
