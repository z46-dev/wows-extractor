package main

import (
	"slices"
	"testing"
)

// TestExtractPrefixes keeps the default archive small while preserving explicit selections.
func TestExtractPrefixes(t *testing.T) {
	var (
		prefixes []string
		err      error
	)

	if prefixes, err = extractPrefixes(extractCommand{}); err != nil || !slices.Equal(prefixes, []string{"scripts/", "content/GameParams.data", "bin/res/texts/"}) {
		t.Fatalf("default prefixes = %v, %v", prefixes, err)
	}
	if prefixes, err = extractPrefixes(extractCommand{Include: []string{"gui/", "spaces/"}}); err != nil || !slices.Equal(prefixes, []string{"gui/", "spaces/"}) {
		t.Fatalf("custom prefixes = %v, %v", prefixes, err)
	}
	if prefixes, err = extractPrefixes(extractCommand{All: true}); err != nil || prefixes != nil {
		t.Fatalf("all prefixes = %v, %v", prefixes, err)
	}
	if _, err = extractPrefixes(extractCommand{All: true, Include: []string{"gui/"}}); err == nil {
		t.Fatal("--all with --include was accepted")
	}
}
