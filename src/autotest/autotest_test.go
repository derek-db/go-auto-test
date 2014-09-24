package main

import (
	"testing"
)

func TestContains(t *testing.T) {
	data := []string{"a", "b"}
	if !contains(data, "a") {
		t.Error("expected contains(a) to be true")
	}
	if contains(data, "c") {
		t.Error("expected contains(c) to be false")
	}
}
