package sanitizer

import (
	"regexp"
	"strings"
)

// SanitizeForSymlink cleans up a filename/path for safe symlink creation
// Returns the sanitized path and a boolean indicating if changes were made
func SanitizeForSymlink(path string) (string, bool) {
	original := path

	// Trim leading/trailing whitespace (main issue)
	cleaned := strings.TrimSpace(path)

	// Replace internal spaces with dots
	cleaned = regexp.MustCompile(`\s+`).ReplaceAllString(cleaned, ".")

	// Clean up any double dots that might result
	cleaned = regexp.MustCompile(`\.+`).ReplaceAllString(cleaned, ".")

	// Trim leading/trailing dots that might result from the above operations
	cleaned = strings.Trim(cleaned, ".")

	return cleaned, cleaned != original
}

// NeedsSanitization checks if a path needs to be sanitized
func NeedsSanitization(path string) bool {
	_, needsCleaning := SanitizeForSymlink(path)
	return needsCleaning
}

// GenerateSymlinkName creates a unique symlink name based on the sanitized path
// Adds a suffix if the clean name already exists to avoid conflicts
func GenerateSymlinkName(path string, baseName string) string {
	cleanPath, _ := SanitizeForSymlink(path)

	// If we have a base name preference, use it as prefix
	if baseName != "" {
		return baseName + "_clean_" + strings.ReplaceAll(cleanPath, "/", "_")
	}

	// Otherwise just use the cleaned path with a prefix
	return "grabarr_clean_" + strings.ReplaceAll(cleanPath, "/", "_")
}