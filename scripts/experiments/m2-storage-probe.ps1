$ErrorActionPreference = 'Stop'
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '../..')).Path
$cacheRoot = Join-Path $repoRoot '.cache'
$probePath = Join-Path $cacheRoot 'm2_storage_probe.go'
$goCache = Join-Path $cacheRoot 'gocache'
$goTemp = Join-Path $cacheRoot 'gotmp'
New-Item -ItemType Directory -Force -Path $goCache, $goTemp | Out-Null

$source = @'
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	root := `.cache`
	dir, err := os.MkdirTemp(root, "m2-storage-probe-")
	if err != nil { panic(err) }
	old := filepath.Join(dir, "manifest")
	next := filepath.Join(dir, "manifest.next")
	defer os.Remove(dir)
	defer os.Remove(next)
	defer os.Remove(old)
	if err = os.WriteFile(old, []byte("old"), 0600); err != nil { panic(err) }
	if err = os.WriteFile(next, []byte("new"), 0600); err != nil { panic(err) }
	oldFile, err := os.OpenFile(old, os.O_WRONLY, 0)
	if err != nil { panic(err) }
	if err = oldFile.Sync(); err != nil { panic(err) }
	if err = oldFile.Close(); err != nil { panic(err) }
	nextFile, err := os.OpenFile(next, os.O_WRONLY, 0)
	if err != nil { panic(err) }
	if err = nextFile.Sync(); err != nil { panic(err) }
	if err = nextFile.Close(); err != nil { panic(err) }
	if err = os.Rename(next, old); err != nil { panic(err) }
	content, err := os.ReadFile(old)
	if err != nil { panic(err) }
	directory, openErr := os.Open(dir)
	var directorySyncErr error
	if openErr == nil {
		directorySyncErr = directory.Sync()
		_ = directory.Close()
	}
	fmt.Printf("replace-existing=%q\n", content)
	fmt.Printf("directory-open-error=%v\n", openErr)
	fmt.Printf("directory-sync-error=%v\n", directorySyncErr)
}
'@
[System.IO.File]::WriteAllText($probePath, $source, [System.Text.UTF8Encoding]::new($false))
Push-Location $repoRoot
try {
    $env:GOCACHE = $goCache
    $env:GOTMPDIR = $goTemp
    $env:GO111MODULE = 'off'
    go run $probePath
    if ($LASTEXITCODE -ne 0) { throw "go run failed with exit code $LASTEXITCODE" }
}
finally {
    Remove-Item -LiteralPath $probePath -ErrorAction SilentlyContinue
    Pop-Location
}
