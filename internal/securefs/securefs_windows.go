//go:build windows
// +build windows

package securefs

import (
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// SecureFS provides filesystem operations with path validation on Windows.
// This implementation uses lexical sandboxing under baseDir and os.* calls,
// instead of os.Root + heavy symlink resolution, to avoid hangs seen on Windows.
type SecureFS struct {
	baseDir         string
	maxReadFileSize int64
	pipeName        string
}

// New creates a new secure filesystem rooted at baseDir.
func New(baseDir string) (*SecureFS, error) {
	abs, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o750); err != nil {
		return nil, err
	}
	return &SecureFS{
		baseDir: filepath.Clean(abs),
	}, nil
}

// Close is a no-op on Windows, kept for API parity.
func (sfs *SecureFS) Close() error {
	return nil
}

// BaseDir returns the absolute base directory.
func (sfs *SecureFS) BaseDir() string {
	return sfs.baseDir
}

// absUnderBase resolves p to an absolute path and ensures it is under baseDir (lexically).
func (sfs *SecureFS) absUnderBase(p string) (string, error) {
	if p == "" {
		return sfs.baseDir, nil
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(sfs.baseDir, p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)

	rel, err := filepath.Rel(sfs.baseDir, abs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q outside base %q", abs, sfs.baseDir)
	}
	return abs, nil
}

// RelativePath returns a baseDir-relative path after sandbox validation.
func (sfs *SecureFS) RelativePath(path string) (string, error) {
	abs, err := sfs.absUnderBase(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(sfs.baseDir, abs)
	if err != nil {
		return "", err
	}
	rel = filepath.Clean(rel)
	rel = strings.TrimPrefix(rel, string(filepath.Separator))
	return rel, nil
}

// ValidateRelativePath cleans and validates a relative path.
func (sfs *SecureFS) ValidateRelativePath(relPath string) (string, error) {
	clean := filepath.Clean(relPath)
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("absolute paths not allowed: %s", clean)
	}
	if clean == "." || clean == "" {
		return "", nil
	}
	if strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("path traversal not allowed: %s", clean)
	}
	return clean, nil
}

// MkdirAll creates a directory hierarchy under the sandbox root.
func (sfs *SecureFS) MkdirAll(path string, perm os.FileMode) error {
	abs, err := sfs.absUnderBase(path)
	if err != nil {
		return err
	}
	return os.MkdirAll(abs, perm)
}

// RemoveAll removes a directory tree under the sandbox root.
func (sfs *SecureFS) RemoveAll(path string) error {
	abs, err := sfs.absUnderBase(path)
	if err != nil {
		return err
	}
	return os.RemoveAll(abs)
}

// Remove removes a single file under the sandbox root.
func (sfs *SecureFS) Remove(path string) error {
	abs, err := sfs.absUnderBase(path)
	if err != nil {
		return err
	}
	return os.Remove(abs)
}

// Rename renames a file or directory under the sandbox root.
func (sfs *SecureFS) Rename(oldpath, newpath string) error {
	oldAbs, err := sfs.absUnderBase(oldpath)
	if err != nil {
		return err
	}
	newAbs, err := sfs.absUnderBase(newpath)
	if err != nil {
		return err
	}
	return os.Rename(oldAbs, newAbs)
}

// OpenFile opens a file under the sandbox root.
func (sfs *SecureFS) OpenFile(path string, flag int, perm os.FileMode) (*os.File, error) {
	abs, err := sfs.absUnderBase(path)
	if err != nil {
		return nil, err
	}
	return os.OpenFile(abs, flag, perm)
}

// Open opens a file for reading under the sandbox root.
func (sfs *SecureFS) Open(path string) (*os.File, error) {
	return sfs.OpenFile(path, os.O_RDONLY, 0)
}

// Stat returns file info for a path under the sandbox root.
func (sfs *SecureFS) Stat(path string) (fs.FileInfo, error) {
	abs, err := sfs.absUnderBase(path)
	if err != nil {
		return nil, err
	}
	return os.Stat(abs)
}

// Lstat returns file info without following symlinks.
func (sfs *SecureFS) Lstat(path string) (fs.FileInfo, error) {
	abs, err := sfs.absUnderBase(path)
	if err != nil {
		return nil, err
	}
	return os.Lstat(abs)
}

// StatRel stats a relative path under the sandbox root.
func (sfs *SecureFS) StatRel(relPath string) (fs.FileInfo, error) {
	valid, err := sfs.ValidateRelativePath(relPath)
	if err != nil {
		return nil, err
	}
	return sfs.Stat(valid)
}

// Exists checks if a path exists under the sandbox root.
func (sfs *SecureFS) Exists(path string) (bool, error) {
	abs, err := sfs.absUnderBase(path)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(abs)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// ExistsNoErr is a helper that ignores errors.
func (sfs *SecureFS) ExistsNoErr(path string) bool {
	ok, _ := sfs.Exists(path)
	return ok
}

// SetMaxReadFileSize sets a soft limit for ReadFile.
func (sfs *SecureFS) SetMaxReadFileSize(maxSize int64) {
	sfs.maxReadFileSize = maxSize
}

// GetMaxReadFileSize returns the current max file size.
func (sfs *SecureFS) GetMaxReadFileSize() int64 {
	return sfs.maxReadFileSize
}

// ReadFile reads a file under the sandbox root with optional size limit.
func (sfs *SecureFS) ReadFile(path string) ([]byte, error) {
	f, err := sfs.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	if sfs.maxReadFileSize > 0 {
		info, err := f.Stat()
		if err != nil {
			return nil, err
		}
		if info.Size() > sfs.maxReadFileSize {
			return nil, fmt.Errorf("file too large: %d > %d", info.Size(), sfs.maxReadFileSize)
		}
	}

	return io.ReadAll(f)
}

// WriteFile writes data to a file under the sandbox root.
func (sfs *SecureFS) WriteFile(path string, data []byte, perm os.FileMode) error {
	f, err := sfs.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

// ServeFile serves a file identified by path, which may be absolute or relative to baseDir.
func (sfs *SecureFS) ServeFile(c echo.Context, path string) error {
	abs, err := sfs.absUnderBase(path)
	if err != nil {
		return c.NoContent(http.StatusNotFound)
	}
	return c.File(abs)
}

// ServeRelativeFile serves a file where relPath is already relative to baseDir.
func (sfs *SecureFS) ServeRelativeFile(c echo.Context, relPath string) error {
	valid, err := sfs.ValidateRelativePath(relPath)
	if err != nil {
		return c.NoContent(http.StatusNotFound)
	}
	return sfs.ServeFile(c, valid)
}

// ReadDir reads directory entries under the sandbox root.
func (sfs *SecureFS) ReadDir(path string) ([]os.DirEntry, error) {
	abs, err := sfs.absUnderBase(path)
	if err != nil {
		return nil, err
	}
	return os.ReadDir(abs)
}

// ParentPath returns the parent directory of a path, or empty if at base.
func (sfs *SecureFS) ParentPath(path string) (string, error) {
	abs, err := sfs.absUnderBase(path)
	if err != nil {
		return "", err
	}
	if abs == sfs.baseDir {
		return "", nil
	}
	parent := filepath.Dir(abs)
	if parent == sfs.baseDir {
		return "", nil
	}
	return parent, nil
}

// Readlink returns the target of a symlink under the sandbox root.
func (sfs *SecureFS) Readlink(path string) (string, error) {
	abs, err := sfs.absUnderBase(path)
	if err != nil {
		return "", err
	}
	return os.Readlink(abs)
}

// Cache-related methods are no-ops on Windows to keep API parity.
func (sfs *SecureFS) ClearExpiredCache()                     {}
func (sfs *SecureFS) GetCacheStats() CacheStats             { return CacheStats{} }
func (sfs *SecureFS) StartCacheCleanup(time.Duration) chan<- struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

// Pipe helpers – keep API parity; fifo_windows.go implements CreateFIFO/openNamedPipePlatform.
func (sfs *SecureFS) SetPipeName(pipeName string) {
	sfs.pipeName = pipeName
}

func (sfs *SecureFS) GetPipeName() string {
	return sfs.pipeName
}

// Utility for MIME detection – reuses existing behavior via extension.
func detectContentTypeFromExt(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == "" {
		return "application/octet-stream"
	}
	if ctype := mime.TypeByExtension(ext); ctype != "" {
		return ctype
	}
	return "application/octet-stream"
}

