//go:build windows

package backup

import (
	"os"
)

// populateUnixMetadata is a no-op on Windows
func populateUnixMetadata(metadata *FileMetadata, info os.FileInfo) {
	// No Unix metadata on Windows
}
