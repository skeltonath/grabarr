package executor

import "strings"

// retryableExtractionPatterns are unrar/7z messages that indicate the archive
// set is incomplete rather than broken.
//
// grabarr downloads each volume as its own job, so extraction can start while
// later volumes are still transferring. unrar reports that as a missing volume
// followed by a checksum error on the partially-written payload — both clear up
// on their own once the remaining parts land.
var retryableExtractionPatterns = []string{
	"cannot find volume",
	"checksum error",
	"unexpected end of archive",
}

// isRetryableExtractionOutput reports whether extraction output indicates a
// transient condition worth retrying.
func isRetryableExtractionOutput(output string) bool {
	lower := strings.ToLower(output)
	for _, p := range retryableExtractionPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}
