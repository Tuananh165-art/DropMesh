$dropmeshRoot = Split-Path -Parent $PSScriptRoot
$goRoot = Join-Path $dropmeshRoot '.tools\go'
if (-not (Test-Path -LiteralPath (Join-Path $goRoot 'bin\go.exe'))) {
    throw 'Pinned Go is missing. Run ./scripts/bootstrap.ps1 first.'
}
$env:GOROOT = $goRoot
$env:GOTOOLCHAIN = 'local'
$env:GOTELEMETRY = 'off'
$env:GOCACHE = Join-Path $dropmeshRoot '.cache\b'
$env:GOMODCACHE = Join-Path $dropmeshRoot '.cache\m'
$env:GOPATH = Join-Path $dropmeshRoot '.cache\g'
$env:PATH = (Join-Path $goRoot 'bin') + [IO.Path]::PathSeparator + $env:PATH
