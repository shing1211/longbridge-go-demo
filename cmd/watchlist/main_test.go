package main

import (
	"testing"

	"github.com/longbridge/openapi-go/quote"
)

// quote.WatchlistUpdateMode is a string enum, so a wrong mapping is not a wrong
// number but a wrong HTTP body field: "replace" sent where "add" was asked for
// silently deletes the symbols the user did not name. Each accepted word is
// pinned to its own SDK constant and the constants' own values are pinned too.

// The SDK constants are themselves the words, so a swap in the switch is
// invisible to a "no error" assertion. Asserted against the constants, not
// against string literals.
func TestParseUpdateMode_EveryAcceptedValueMapsToTheSDKConstant(t *testing.T) {
	tests := []struct {
		in   string
		want quote.WatchlistUpdateMode
	}{
		{"add", quote.AddWatchlist},
		{"remove", quote.RemoveWatchlist},
		{"replace", quote.ReplaceWatchlist},
		// ToLower+TrimSpace.
		{"ADD", quote.AddWatchlist},
		{"Remove", quote.RemoveWatchlist},
		{"REPLACE", quote.ReplaceWatchlist},
		{"  add  ", quote.AddWatchlist},
		{"\tremove\n", quote.RemoveWatchlist},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseUpdateMode(tt.in)
			if err != nil {
				t.Fatalf("parseUpdateMode(%q) = error %v, want %q", tt.in, err, tt.want)
			}
			if got != tt.want {
				t.Errorf("parseUpdateMode(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// The three constants must be mutually distinct, otherwise two of the three
// switch cases could be swapped and every test above would still pass.
func TestParseUpdateMode_TheSDKConstantsAreDistinct(t *testing.T) {
	seen := map[quote.WatchlistUpdateMode]string{}
	for in, want := range map[string]quote.WatchlistUpdateMode{
		"add": quote.AddWatchlist, "remove": quote.RemoveWatchlist, "replace": quote.ReplaceWatchlist,
	} {
		if want == "" {
			t.Fatalf("the SDK constant for %q is the empty string, so an unknown value would look valid", in)
		}
		if prev, dup := seen[want]; dup {
			t.Errorf("%q and %q both map to %q", prev, in, want)
		}
		seen[want] = in
	}
	// The wire values are the plain lowercase words.
	if quote.AddWatchlist != "add" || quote.RemoveWatchlist != "remove" || quote.ReplaceWatchlist != "replace" {
		t.Errorf("SDK wire values changed: add=%q remove=%q replace=%q",
			quote.AddWatchlist, quote.RemoveWatchlist, quote.ReplaceWatchlist)
	}
}

func TestParseUpdateMode_UnknownValueIsAnErrorNotAnEmptyMode(t *testing.T) {
	tests := []struct {
		in   string
		note string
	}{
		{"", "empty string; the flag default is \"add\", so an empty value is a mistake"},
		{"   ", "whitespace only"},
		{"insert", "a plausible synonym"},
		{"delete", "what -action delete does, but not an update mode"},
		{"append", "a plausible synonym"},
		{"subtract", "a plausible synonym"},
		{"addremove", "concatenation of two valid words"},
		{"replaced", "past tense"},
		{"remove_all", "underscore form"},
		{"overwrite", "a plausible synonym"},
		{"none", "a word the SDK does not define"},
		{"0", "numeric id where a word belongs"},
		{"1", "numeric id where a word belongs"},
		// Words that are valid in other commands of this repo must not be
		// accepted here: -pin-mode uses "add"/"remove", not "replace".
		{"pin", "a -pin-mode value"},
		{"unpin", "a -pin-mode value"},
	}
	for _, tt := range tests {
		t.Run(tt.in+"/"+tt.note, func(t *testing.T) {
			got, err := parseUpdateMode(tt.in)
			if err == nil {
				t.Fatalf("parseUpdateMode(%q) = %q with no error, want an error", tt.in, got)
			}
			if got != "" {
				t.Errorf("parseUpdateMode(%q) = %q alongside the error, want the zero value \"\"", tt.in, got)
			}
		})
	}
}

// "-pin-mode" is a separate switch in the same file with a different vocabulary.
// A value valid there must be rejected here and vice versa, so the two can
// never be merged by accident.
func TestParseUpdateMode_PinModeVocabularyIsNotUpdateModeVocabulary(t *testing.T) {
	for _, in := range []string{"pin", "unpin"} {
		if got, err := parseUpdateMode(in); err == nil {
			t.Errorf("parseUpdateMode(%q) = %q with no error, want an error", in, got)
		}
	}
	// "add" and "remove" are shared, which is why the pin switch and this one
	// are separate functions rather than one.
	for _, in := range []string{"add", "remove"} {
		if _, err := parseUpdateMode(in); err != nil {
			t.Errorf("parseUpdateMode(%q) = error %v, want it accepted", in, err)
		}
	}
}
