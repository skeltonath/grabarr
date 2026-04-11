package archive

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Archive extension patterns.
var (
	// Matches .r00-.r99 (old-style multi-part RAR)
	rNNRegex = regexp.MustCompile(`(?i)^r\d{2}$`)
	// Matches .partN.rar or .part01.rar etc (new-style multi-part RAR)
	partNRarRegex = regexp.MustCompile(`(?i)\.part(\d+)\.rar$`)
)

// IsArchive returns true if the filename has a recognized archive extension.
func IsArchive(filename string) bool {
	ext := strings.TrimPrefix(filepath.Ext(filename), ".")
	ext = strings.ToLower(ext)

	switch ext {
	case "rar", "zip":
		return true
	}

	// Check for .r00-.r99 pattern
	if rNNRegex.MatchString(ext) {
		return true
	}

	return false
}

// GroupKey returns a key that groups together all parts of a multi-part archive.
// Files that share the same GroupKey belong to the same archive set.
// The key is the full path with the archive extension stripped.
//
// Examples:
//
//	/path/Movie.rar     → /path/Movie
//	/path/Movie.r00     → /path/Movie
//	/path/Movie.r25     → /path/Movie
//	/path/Movie.part2.rar → /path/Movie
//	/path/File.zip      → /path/File
func GroupKey(path string) string {
	// Handle .partN.rar pattern first
	if loc := partNRarRegex.FindStringIndex(path); loc != nil {
		return path[:loc[0]]
	}

	dir := filepath.Dir(path)
	base := filepath.Base(path)
	ext := strings.TrimPrefix(filepath.Ext(base), ".")

	// Strip known archive extensions
	lower := strings.ToLower(ext)
	switch {
	case lower == "rar" || lower == "zip":
		base = strings.TrimSuffix(base, "."+ext)
	case rNNRegex.MatchString(lower):
		base = strings.TrimSuffix(base, "."+ext)
	default:
		// Not a recognized archive extension, return as-is
		return path
	}

	return filepath.Join(dir, base)
}

// IsFirstPart returns true if filename is the "first part" of an archive set,
// meaning it's the file you should pass to `unrar x` to extract the whole set.
//
// Priority:
//  1. A .rar file that is NOT a .partN.rar → always first
//  2. .part1.rar or .part01.rar (lowest part number) → first
//  3. .r00 → first only if no .rar exists in the group
//
// groupFiles should contain all filenames (not full paths) in the same archive group.
func IsFirstPart(filename string, groupFiles []string) bool {
	lower := strings.ToLower(filename)

	// Case 1: plain .rar (not .partN.rar)
	if strings.HasSuffix(lower, ".rar") && !partNRarRegex.MatchString(lower) {
		return true
	}

	// Case 2: .partN.rar — check if this is part1/part01
	if m := partNRarRegex.FindStringSubmatch(lower); m != nil {
		partNum := m[1]
		// Strip leading zeros to check if it's "1"
		partNum = strings.TrimLeft(partNum, "0")
		if partNum == "1" || partNum == "" {
			return true
		}
		return false
	}

	// Case 3: .zip — always first (standalone archive)
	if strings.HasSuffix(lower, ".zip") {
		return true
	}

	// Case 4: .r00 — first only if no .rar file exists in group
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(lower), "."))
	if ext == "r00" {
		for _, other := range groupFiles {
			otherLower := strings.ToLower(other)
			if strings.HasSuffix(otherLower, ".rar") && !partNRarRegex.MatchString(otherLower) {
				// A .rar file exists, so .r00 is not the first part
				return false
			}
		}
		return true
	}

	return false
}

// ArchiveExtensionPatterns converts user-facing archive extension names into
// find-compatible -name patterns for use in SSH find commands.
//
// "rar" expands to: ["*.rar", "*.r[0-9][0-9]"] to catch both .rar and .r00-.r99
// "zip" expands to: ["*.zip"]
func ArchiveExtensionPatterns(extensions []string) []string {
	var patterns []string
	for _, ext := range extensions {
		switch strings.ToLower(ext) {
		case "rar":
			patterns = append(patterns, "*.rar", "*.r[0-9][0-9]")
		default:
			patterns = append(patterns, fmt.Sprintf("*.%s", ext))
		}
	}
	return patterns
}
