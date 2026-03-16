//go:build !windows

package backup

import (
	"os"
	"syscall"
)

// populateUnixMetadata populates Unix-specific file metadata
func populateUnixMetadata(metadata *FileMetadata, info os.FileInfo) {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		metadata.UID = int(stat.Uid)
		metadata.GID = int(stat.Gid)
		metadata.IsUnix = true
	}
}
