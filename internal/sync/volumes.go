package sync

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"grabarr/internal/archive"
	"grabarr/internal/config"
)

// ListArchiveVolumes returns the base filenames of every archive volume the
// seedbox holds for the given archive group, sorted for stable comparison.
//
// The queue uses this to confirm it has the complete set before extracting.
// Local state cannot answer that question: grabarr only knows about volumes a
// scan happened to see, and a scan that catches a torrent mid-arrival sees a
// contiguous prefix that is indistinguishable from a finished set.
func (s *Scanner) ListArchiveVolumes(ctx context.Context, groupKey string) ([]string, error) {
	remote, ok := s.remoteForPath(groupKey)
	if !ok {
		return nil, fmt.Errorf("no configured remote owns path %q", groupKey)
	}

	dir := filepath.Dir(groupKey)
	findCmd := fmt.Sprintf(
		"find %q -maxdepth 1 -type f \\( -name '*.rar' -o -name '*.r[0-9][0-9]' -o -name '*.zip' \\) 2>/dev/null",
		dir)

	stdout, err := s.runSSH(ctx, remote, findCmd)
	if err != nil {
		return nil, fmt.Errorf("list archive volumes for %q: %w", groupKey, err)
	}

	var paths []string
	for _, line := range strings.Split(stdout, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			paths = append(paths, line)
		}
	}

	return filterVolumesForGroup(paths, groupKey), nil
}

// remoteForPath finds the remote whose watched paths contain the given path.
func (s *Scanner) remoteForPath(path string) (config.RemoteConfig, bool) {
	for _, remote := range s.cfg.GetRemotes() {
		for _, wp := range remote.WatchedPaths {
			if strings.HasPrefix(path, wp.RemotePath) {
				return remote, true
			}
		}
	}
	return config.RemoteConfig{}, false
}

// filterVolumesForGroup reduces a directory listing to the base filenames that
// belong to groupKey, sorted ascending.
func filterVolumesForGroup(paths []string, groupKey string) []string {
	var out []string
	for _, p := range paths {
		if archive.GroupKey(p) == groupKey {
			out = append(out, filepath.Base(p))
		}
	}
	sort.Strings(out)
	return out
}
