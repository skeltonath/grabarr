package archive

import (
	"testing"
)

func TestIsArchive(t *testing.T) {
	tests := []struct {
		filename string
		want     bool
	}{
		// RAR files
		{"Movie.rar", true},
		{"Movie.RAR", true},
		{"Movie.Rar", true},

		// Multi-part RAR (old style)
		{"Movie.r00", true},
		{"Movie.r01", true},
		{"Movie.r99", true},
		{"Movie.R00", true},
		{"Movie.R55", true},

		// ZIP files
		{"Archive.zip", true},
		{"Archive.ZIP", true},

		// Non-archive files
		{"Movie.mkv", false},
		{"Movie.mp4", false},
		{"Movie.srt", false},
		{"Movie.txt", false},
		{"", false},
		{"noext", false},

		// Edge cases
		{"Movie.rar.txt", false},   // .txt is the extension
		{"Movie.r100", false},      // r100 is not r[0-9][0-9]
		{"Movie.part1.rar", true},  // .rar extension
		{"Movie.part02.rar", true}, // .rar extension
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			got := IsArchive(tt.filename)
			if got != tt.want {
				t.Errorf("IsArchive(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

func TestGroupKey(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		// Standard .rar + .r00-.r99
		{"/path/to/Movie.rar", "/path/to/Movie"},
		{"/path/to/Movie.r00", "/path/to/Movie"},
		{"/path/to/Movie.r25", "/path/to/Movie"},
		{"/path/to/Movie.R01", "/path/to/Movie"},

		// .partN.rar pattern
		{"/path/to/Movie.part1.rar", "/path/to/Movie"},
		{"/path/to/Movie.part02.rar", "/path/to/Movie"},
		{"/path/to/Movie.part15.rar", "/path/to/Movie"},
		{"/path/to/Movie.Part1.Rar", "/path/to/Movie"},

		// ZIP
		{"/path/to/Archive.zip", "/path/to/Archive"},

		// Non-archive (returned as-is)
		{"/path/to/Movie.mkv", "/path/to/Movie.mkv"},

		// Files with dots in name
		{"/path/to/Movie.2024.rar", "/path/to/Movie.2024"},
		{"/path/to/Movie.2024.r05", "/path/to/Movie.2024"},
		{"/path/to/Movie.2024.part1.rar", "/path/to/Movie.2024"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := GroupKey(tt.path)
			if got != tt.want {
				t.Errorf("GroupKey(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestIsFirstPart(t *testing.T) {
	tests := []struct {
		name       string
		filename   string
		groupFiles []string
		want       bool
	}{
		{
			name:       "plain .rar is always first",
			filename:   "Movie.rar",
			groupFiles: []string{"Movie.rar", "Movie.r00", "Movie.r01", "Movie.r02"},
			want:       true,
		},
		{
			name:       ".r00 is NOT first when .rar exists",
			filename:   "Movie.r00",
			groupFiles: []string{"Movie.rar", "Movie.r00", "Movie.r01"},
			want:       false,
		},
		{
			name:       ".r01 is never first",
			filename:   "Movie.r01",
			groupFiles: []string{"Movie.rar", "Movie.r00", "Movie.r01"},
			want:       false,
		},
		{
			name:       ".r00 IS first when no .rar in group",
			filename:   "Movie.r00",
			groupFiles: []string{"Movie.r00", "Movie.r01", "Movie.r02"},
			want:       true,
		},
		{
			name:       ".r01 is not first even without .rar",
			filename:   "Movie.r01",
			groupFiles: []string{"Movie.r00", "Movie.r01", "Movie.r02"},
			want:       false,
		},
		{
			name:       ".part1.rar is first",
			filename:   "Movie.part1.rar",
			groupFiles: []string{"Movie.part1.rar", "Movie.part2.rar", "Movie.part3.rar"},
			want:       true,
		},
		{
			name:       ".part01.rar is first",
			filename:   "Movie.part01.rar",
			groupFiles: []string{"Movie.part01.rar", "Movie.part02.rar"},
			want:       true,
		},
		{
			name:       ".part2.rar is not first",
			filename:   "Movie.part2.rar",
			groupFiles: []string{"Movie.part1.rar", "Movie.part2.rar"},
			want:       false,
		},
		{
			name:       "case insensitive .RAR",
			filename:   "Movie.RAR",
			groupFiles: []string{"Movie.RAR", "Movie.R00", "Movie.R01"},
			want:       true,
		},
		{
			name:       "zip is first (standalone)",
			filename:   "Archive.zip",
			groupFiles: []string{"Archive.zip"},
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsFirstPart(tt.filename, tt.groupFiles)
			if got != tt.want {
				t.Errorf("IsFirstPart(%q, %v) = %v, want %v", tt.filename, tt.groupFiles, got, tt.want)
			}
		})
	}
}

func TestArchiveExtensionPatterns(t *testing.T) {
	tests := []struct {
		name       string
		extensions []string
		want       []string
	}{
		{
			name:       "rar expands to rar + r[0-9][0-9]",
			extensions: []string{"rar"},
			want:       []string{"*.rar", "*.r[0-9][0-9]"},
		},
		{
			name:       "zip stays as-is",
			extensions: []string{"zip"},
			want:       []string{"*.zip"},
		},
		{
			name:       "rar and zip together",
			extensions: []string{"rar", "zip"},
			want:       []string{"*.rar", "*.r[0-9][0-9]", "*.zip"},
		},
		{
			name:       "empty input",
			extensions: []string{},
			want:       nil,
		},
		{
			name:       "RAR case insensitive",
			extensions: []string{"RAR"},
			want:       []string{"*.rar", "*.r[0-9][0-9]"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ArchiveExtensionPatterns(tt.extensions)
			if len(got) != len(tt.want) {
				t.Fatalf("ArchiveExtensionPatterns(%v) returned %d patterns, want %d: got %v", tt.extensions, len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("pattern[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}
