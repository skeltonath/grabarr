package executor

import (
	"testing"
)

func TestIsRetryableExtractionOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name: "missing volume is retryable - the volume is usually still transferring",
			output: `Extracting from /downloads/Rec/rec.1080p-culthd.r40
Cannot find volume /downloads/Rec/rec.1080p-culthd.r41
rec.1080p.bluray.x264-culthd.mkv - checksum error
Total errors: 1`,
			want: true,
		},
		{
			name:   "checksum error alone is retryable",
			output: "movie.mkv - checksum error\nTotal errors: 1",
			want:   true,
		},
		{
			name:   "unexpected end of archive is retryable",
			output: "Unexpected end of archive",
			want:   true,
		},
		{
			name:   "corrupt header is permanent",
			output: "Corrupt header is found",
			want:   false,
		},
		{
			name:   "wrong password is permanent",
			output: "The specified password is incorrect",
			want:   false,
		},
		{
			name:   "unknown format is permanent",
			output: "Unknown archive format",
			want:   false,
		},
		{
			name:   "empty output is permanent",
			output: "",
			want:   false,
		},
		{
			name:   "matching is case insensitive",
			output: "CANNOT FIND VOLUME /downloads/x.r05",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetryableExtractionOutput(tt.output); got != tt.want {
				t.Errorf("isRetryableExtractionOutput(%q) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}
