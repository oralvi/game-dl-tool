package main

import (
	"reflect"
	"testing"
)

func TestParseIndexSelectionCompactDigits(t *testing.T) {
	got, err := parseIndexSelection("135", 6)
	if err != nil {
		t.Fatalf("parseIndexSelection returned error: %v", err)
	}

	want := []int{1, 3, 5}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseIndexSelection = %v, want %v", got, want)
	}
}

func TestParseIndexSelectionMultiDigit(t *testing.T) {
	got, err := parseIndexSelection("10,12", 12)
	if err != nil {
		t.Fatalf("parseIndexSelection returned error: %v", err)
	}

	want := []int{10, 12}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseIndexSelection = %v, want %v", got, want)
	}
}
