package sync

import (
	"context"
	"errors"
	"strings"
	"testing"

	"grabarr/internal/config"
)

func TestFilterVolumesForGroup(t *testing.T) {
	tests := []struct {
		name     string
		paths    []string
		groupKey string
		want     []string
	}{
		{
			name: "keeps only volumes of the requested group",
			paths: []string{
				"/dl/Movie/movie.rar",
				"/dl/Movie/movie.r00",
				"/dl/Movie/movie.r01",
				"/dl/Movie/other.rar",
				"/dl/Movie/other.r00",
			},
			groupKey: "/dl/Movie/movie",
			want:     []string{"movie.r00", "movie.r01", "movie.rar"},
		},
		{
			name: "ignores non-archive files in the same directory",
			paths: []string{
				"/dl/Movie/movie.rar",
				"/dl/Movie/movie.nfo",
				"/dl/Movie/sample.mkv",
			},
			groupKey: "/dl/Movie/movie",
			want:     []string{"movie.rar"},
		},
		{
			name:     "no matches yields nothing",
			paths:    []string{"/dl/Movie/other.rar"},
			groupKey: "/dl/Movie/movie",
			want:     nil,
		},
		{
			name: "handles partN sets",
			paths: []string{
				"/dl/Movie/movie.part1.rar",
				"/dl/Movie/movie.part2.rar",
			},
			groupKey: "/dl/Movie/movie",
			want:     []string{"movie.part1.rar", "movie.part2.rar"},
		},
		{
			name:     "empty input",
			paths:    nil,
			groupKey: "/dl/Movie/movie",
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterVolumesForGroup(tt.paths, tt.groupKey)

			if len(got) != len(tt.want) {
				t.Fatalf("filterVolumesForGroup() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("filterVolumesForGroup()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func testScannerWithRunner(runner sshRunner) *Scanner {
	cfg := &config.Config{
		Remotes: []config.RemoteConfig{{
			Name:    "whatbox",
			SSHHost: "seedbox.example",
			SSHUser: "user",
			WatchedPaths: []config.WatchedPath{{
				RemotePath:        "/home/user/downloads/completed/dp/",
				ArchiveExtensions: []string{"rar", "zip"},
			}},
		}},
	}

	s := New(cfg, nil, nil)
	s.runSSH = runner
	return s
}

func TestListArchiveVolumesReturnsSourceVolumeSet(t *testing.T) {
	var gotCmd string

	s := testScannerWithRunner(func(_ context.Context, _ config.RemoteConfig, cmd string) (string, error) {
		gotCmd = cmd
		return strings.Join([]string{
			"/home/user/downloads/completed/dp/Burning/gimchi.rar",
			"/home/user/downloads/completed/dp/Burning/gimchi.r00",
			"/home/user/downloads/completed/dp/Burning/gimchi.r01",
		}, "\n"), nil
	})

	got, err := s.ListArchiveVolumes(context.Background(),
		"/home/user/downloads/completed/dp/Burning/gimchi")
	if err != nil {
		t.Fatalf("ListArchiveVolumes() error = %v", err)
	}

	want := []string{"gimchi.r00", "gimchi.r01", "gimchi.rar"}
	if len(got) != len(want) {
		t.Fatalf("ListArchiveVolumes() = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("volume[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	if !strings.Contains(gotCmd, "/home/user/downloads/completed/dp/Burning") {
		t.Errorf("find command %q should target the group's directory", gotCmd)
	}
}

func TestListArchiveVolumesErrorsForPathOutsideWatchedRemotes(t *testing.T) {
	s := testScannerWithRunner(func(context.Context, config.RemoteConfig, string) (string, error) {
		t.Fatal("ssh should not run for an unknown path")
		return "", nil
	})

	_, err := s.ListArchiveVolumes(context.Background(), "/somewhere/else/movie")
	if err == nil {
		t.Fatal("ListArchiveVolumes() error = nil, want error for unmatched path")
	}
}

func TestListArchiveVolumesPropagatesSSHError(t *testing.T) {
	s := testScannerWithRunner(func(context.Context, config.RemoteConfig, string) (string, error) {
		return "", errors.New("connection refused")
	})

	_, err := s.ListArchiveVolumes(context.Background(),
		"/home/user/downloads/completed/dp/Burning/gimchi")
	if err == nil {
		t.Fatal("ListArchiveVolumes() error = nil, want the ssh failure surfaced")
	}
}
