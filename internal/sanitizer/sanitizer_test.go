package sanitizer

import (
	"testing"
)

func TestSanitizeForSymlink(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		changed  bool
	}{
		{
			input:    "The.X-Files.S02.1080p.BluRay.REMUX.AVC.DTS-HD.MA.5.1-NOGRP ",
			expected: "The.X-Files.S02.1080p.BluRay.REMUX.AVC.DTS-HD.MA.5.1-NOGRP",
			changed:  true,
		},
		{
			input:    "Movie Title [2023] (Director's Cut)",
			expected: "Movie.Title.[2023].(Director's.Cut)",
			changed:  true,
		},
		{
			input:    "  Leading and trailing spaces  ",
			expected: "Leading.and.trailing.spaces",
			changed:  true,
		},
		{
			input:    "Multiple    Internal    Spaces",
			expected: "Multiple.Internal.Spaces",
			changed:  true,
		},
		{
			input:    "Already.Clean.No.Spaces",
			expected: "Already.Clean.No.Spaces",
			changed:  false,
		},
		{
			input:    "Mixed. Spaces .And. Dots .",
			expected: "Mixed.Spaces.And.Dots",
			changed:  true,
		},
		{
			input:    "",
			expected: "",
			changed:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			result, changed := SanitizeForSymlink(test.input)
			if result != test.expected {
				t.Errorf("Expected %q, got %q", test.expected, result)
			}
			if changed != test.changed {
				t.Errorf("Expected changed=%t, got changed=%t", test.changed, changed)
			}
		})
	}
}

func TestNeedsSanitization(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"The.X-Files.S02.1080p.BluRay.REMUX.AVC.DTS-HD.MA.5.1-NOGRP ", true},
		{"Movie Title [2023]", true},
		{"Already.Clean.No.Spaces", false},
		{"  Leading spaces", true},
		{"", false},
	}

	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			result := NeedsSanitization(test.input)
			if result != test.expected {
				t.Errorf("For input %q, expected %t, got %t", test.input, test.expected, result)
			}
		})
	}
}

func TestGenerateSymlinkName(t *testing.T) {
	tests := []struct {
		path     string
		baseName string
		expected string
	}{
		{
			path:     "The.X-Files.S02 ",
			baseName: "xfiles",
			expected: "xfiles_clean_The.X-Files.S02",
		},
		{
			path:     "downloads/completed/Movie Title",
			baseName: "",
			expected: "grabarr_clean_downloads_completed_Movie.Title",
		},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			result := GenerateSymlinkName(test.path, test.baseName)
			if result != test.expected {
				t.Errorf("Expected %q, got %q", test.expected, result)
			}
		})
	}
}