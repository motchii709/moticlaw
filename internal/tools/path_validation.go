package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// validateSymlinkPath resolves all symlinks in the constructed workDir/userID/path
// and verifies the resolved path stays within the allowed base directory (workDir/userID).
//
// It handles three cases:
//  1. The path exists -> resolves the full path and validates it.
//  2. The path is a dangling symlink -> resolves the symlink's target and validates it.
//  3. The path doesn't exist -> walks up to the deepest existing ancestor and validates that.
func validateSymlinkPath(workDir, userID, path string) error {
	fullPath := filepath.Join(workDir, userID, path)
	allowedBase := filepath.Clean(filepath.Join(workDir, userID))

	// Case 1: Full path exists — resolve and validate.
	resolved, err := filepath.EvalSymlinks(fullPath)
	if err == nil {
		if !strings.HasPrefix(resolved, allowedBase+string(os.PathSeparator)) && resolved != allowedBase {
			return fmt.Errorf("path escapes allowed directory: %s", path)
		}
		return nil
	}

	// Case 2: The path component itself is a dangling symlink.
	fi, lerr := os.Lstat(fullPath)
	if lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
		target, rerr := os.Readlink(fullPath)
		if rerr != nil {
			return fmt.Errorf("invalid path: %w", rerr)
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(fullPath), target)
		}
		if !strings.HasPrefix(target, allowedBase+string(os.PathSeparator)) && target != allowedBase {
			return fmt.Errorf("path escapes allowed directory: %s", path)
		}
		return nil
	}

	// Case 3: Path doesn't exist — walk up to the deepest existing ancestor.
	checkPath := filepath.Dir(fullPath)
	for {
		resolved, err := filepath.EvalSymlinks(checkPath)
		if err == nil {
			if !strings.HasPrefix(resolved, allowedBase+string(os.PathSeparator)) && resolved != allowedBase {
				return fmt.Errorf("path escapes allowed directory: %s", path)
			}
			return nil
		}
		parent := filepath.Dir(checkPath)
		if parent == checkPath {
			return fmt.Errorf("path does not exist: %s", path)
		}
		checkPath = parent
	}
}
