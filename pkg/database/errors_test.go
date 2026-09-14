package database

import (
	"errors"
	"testing"
)

func TestIsUniqueViolation(t *testing.T) {
	if IsUniqueViolation(nil) {
		t.Fatal("nil should be false")
	}
	if !IsUniqueViolation(errors.New("ERROR: duplicate key value (SQLSTATE 23505)")) {
		t.Fatal("23505 should match")
	}
	if IsUniqueViolation(errors.New("other error")) {
		t.Fatal("unrelated error should be false")
	}
}
