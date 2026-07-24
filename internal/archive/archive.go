package archive

import (
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"strconv"
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

// MissingVolumes returns the names of volumes that are absent from the middle
// of a multi-part archive set, in ascending order.
//
// RAR volumes are numbered contiguously, so a gap means we are holding an
// incomplete set and must not extract yet. Note this only detects interior
// gaps: a set truncated at the tail (.rar….r05 when the release actually has
// 40 volumes) looks perfectly contiguous, so callers must also confirm the set
// is complete against the source.
//
// files are base filenames belonging to a single archive group.
func MissingVolumes(files []string) []string {
	// .partN.rar sets and old-style .rNN sets are numbered differently, so
	// collect each separately and report on whichever is in use.
	partNums := make(map[int]bool)
	rNums := make(map[int]bool)
	var partWidth int
	var partPrefix, rPrefix string
	var hasPlainRar bool

	for _, f := range files {
		lower := strings.ToLower(f)

		if strings.HasSuffix(lower, ".rar") && !partNRarRegex.MatchString(lower) {
			hasPlainRar = true
			continue
		}

		if m := partNRarRegex.FindStringSubmatch(lower); m != nil {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			partNums[n] = true
			if len(m[1]) > partWidth {
				partWidth = len(m[1])
			}
			if loc := partNRarRegex.FindStringIndex(lower); loc != nil {
				partPrefix = f[:loc[0]]
			}
			continue
		}

		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(lower), "."))
		if rNNRegex.MatchString(ext) {
			n, err := strconv.Atoi(ext[1:])
			if err != nil {
				continue
			}
			rNums[n] = true
			rPrefix = f[:len(f)-len(filepath.Ext(f))]
		}
	}

	if len(partNums) > 0 {
		return gaps(partNums, math.MinInt, func(n int) string {
			return fmt.Sprintf("%s.part%0*d.rar", partPrefix, partWidth, n)
		})
	}

	if len(rNums) > 0 {
		// A plain .rar is the volume before .r00, so its presence means the
		// numbered run has to start at zero — .rar plus .r01 is missing .r00.
		floor := math.MinInt
		if hasPlainRar {
			floor = 0
		}
		return gaps(rNums, floor, func(n int) string {
			return fmt.Sprintf("%s.r%02d", rPrefix, n)
		})
	}

	return nil
}

// gaps returns names for every integer missing between the lowest and highest
// keys present in nums. If floor is not math.MinInt the scan starts there
// instead of at the lowest key present.
func gaps(nums map[int]bool, floor int, name func(int) string) []string {
	lo, hi := math.MaxInt, math.MinInt
	for n := range nums {
		if n < lo {
			lo = n
		}
		if n > hi {
			hi = n
		}
	}

	if floor != math.MinInt && floor < lo {
		lo = floor
	}

	var missing []string
	for n := lo; n <= hi; n++ {
		if !nums[n] {
			missing = append(missing, name(n))
		}
	}
	return missing
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
