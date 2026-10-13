//go:build !windows && !darwin && !linux

package resume

import (
	"fmt"
	"os"
)

func acquireFileLock(*os.File) (func() error, error) {
	return nil, fmt.Errorf("resumable-state locking is unsupported on this platform")
}
