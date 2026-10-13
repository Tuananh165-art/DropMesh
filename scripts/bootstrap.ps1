param([string]$InstallName = 'go')
$ErrorActionPreference = 'Stop'
$InstallName = if ([string]::IsNullOrWhiteSpace($InstallName)) { 'go' } else { $InstallName }
if ($InstallName -notmatch '^go(?:-[a-z0-9-]+)?$') { throw 'InstallName must be go or a simple go-* name.' }
$version = '1.27.2'
$expected = '1314008898bd40df77af4b014f777f08873dbdfbcd3d92308728ee03304fe04f'
$root = Split-Path -Parent $PSScriptRoot
$tools = Join-Path $root '.tools'
$cache = Join-Path $root '.cache\bootstrap'
$archive = Join-Path $cache "go$version.windows-amd64.zip"
$install = Join-Path $tools $InstallName
New-Item -ItemType Directory -Force -Path $cache,$tools | Out-Null
if (Test-Path -LiteralPath $install) {
    $existing = Join-Path $install 'bin\go.exe'
    if (Test-Path -LiteralPath $existing) {
        $installedVersion = & $existing version
        if ($installedVersion -match "go$version") { Write-Output $installedVersion; exit 0 }
    }
    throw "A different or incomplete toolchain exists at $install; inspect it before replacing it."
}
if (-not (Test-Path -LiteralPath $archive)) {
    Invoke-WebRequest -Uri "https://go.dev/dl/go$version.windows-amd64.zip" -OutFile $archive
}
$actual = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actual -ne $expected) { throw "Go archive SHA-256 mismatch: $actual" }
Add-Type -AssemblyName System.IO.Compression.FileSystem
$stage = $install
New-Item -ItemType Directory -Path $stage | Out-Null
$zip = [IO.Compression.ZipFile]::OpenRead($archive)
$prefix = 'go/'
$stageRoot = [IO.Path]::GetFullPath($stage)
try {
    foreach ($entry in $zip.Entries) {
        $relative = $entry.FullName.Substring($prefix.Length)
        $include = $relative -in @('go.env','VERSION') -or $relative.StartsWith('bin/') -or $relative.StartsWith('pkg/') -or $relative.StartsWith('lib/time/zoneinfo.zip') -or ($relative.StartsWith('src/') -and $relative -notmatch '/testdata[^/]*/' -and $relative -notmatch '^src/cmd/vendor/')
        if (-not $include -or $entry.FullName.EndsWith('/')) { continue }
        $target = [IO.Path]::GetFullPath((Join-Path $stageRoot ($relative -replace '/', '\')))
        if (-not $target.StartsWith($stageRoot + [IO.Path]::DirectorySeparatorChar,[StringComparison]::OrdinalIgnoreCase)) { throw 'Archive path escaped the toolchain directory.' }
        [IO.Directory]::CreateDirectory([IO.Path]::GetDirectoryName($target)) | Out-Null
        $inputStream = $entry.Open()
        $outputStream = [IO.File]::Open($target,[IO.FileMode]::CreateNew,[IO.FileAccess]::Write,[IO.FileShare]::None)
        try { $inputStream.CopyTo($outputStream) }
        finally { $outputStream.Dispose(); $inputStream.Dispose() }
    }
}
finally { $zip.Dispose() }
if (-not (Test-Path -LiteralPath (Join-Path $stage 'bin\go.exe'))) { throw 'Verified archive did not contain the expected Go toolchain.' }
& (Join-Path $install 'bin\go.exe') version
