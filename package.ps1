$ErrorActionPreference = "Stop"

$projectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$releaseRoot = Join-Path $projectRoot "releases"
$binRoot = Join-Path $projectRoot "bin"
$timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
$packageName = "game-dl-tool-windows-amd64-$timestamp"
$packageDir = Join-Path $releaseRoot $packageName
$zipPath = Join-Path $releaseRoot "$packageName.zip"

$env:CGO_ENABLED = "1"
$goBin = Join-Path (go env GOPATH) "bin"
$gccBin = "C:\msys64\ucrt64\bin"
$env:PATH = "$goBin;$gccBin;$env:PATH"

if (-not (Get-Command gcc -ErrorAction SilentlyContinue)) {
    throw "gcc was not found in PATH. Install MSYS2 UCRT64 and add C:\\msys64\\ucrt64\\bin to PATH before packaging."
}
if (-not (Get-Command wails -ErrorAction SilentlyContinue)) {
    throw "wails CLI was not found in PATH. Run `go install github.com/wailsapp/wails/v2/cmd/wails@v2.11.0` first."
}

if (Test-Path -LiteralPath $binRoot) {
    Remove-Item -LiteralPath $binRoot -Recurse -Force
}

Get-ChildItem -LiteralPath $projectRoot -File -ErrorAction SilentlyContinue |
    Where-Object { $_.Extension -in ".exe", ".zip" } |
    Remove-Item -Force -ErrorAction SilentlyContinue

New-Item -ItemType Directory -Path $releaseRoot -Force | Out-Null
if (Test-Path -LiteralPath $packageDir) {
    Remove-Item -LiteralPath $packageDir -Recurse -Force -ErrorAction SilentlyContinue
}
if (Test-Path -LiteralPath $zipPath) {
    Remove-Item -LiteralPath $zipPath -Force -ErrorAction SilentlyContinue
}
New-Item -ItemType Directory -Path $packageDir -Force | Out-Null

Push-Location $projectRoot
try {
    npm install --prefix frontend
    go test ./...
    wails build -clean -trimpath -o game-dl-tool.exe
} finally {
    Pop-Location
}

$builtBinary = Join-Path $projectRoot "build\bin\game-dl-tool.exe"
if (-not (Test-Path -LiteralPath $builtBinary)) {
    $builtBinary = Join-Path $projectRoot "build\bin\game-dl-tool"
}
if (-not (Test-Path -LiteralPath $builtBinary)) {
    throw "Could not find built game-dl-tool binary in build\\bin."
}
Copy-Item -LiteralPath $builtBinary -Destination (Join-Path $packageDir "game-dl-tool.exe")
Copy-Item -LiteralPath (Join-Path $projectRoot "README.md") -Destination (Join-Path $packageDir "README.md")

Compress-Archive -Path (Join-Path $packageDir "*") -DestinationPath $zipPath -Force

$releaseItems = Get-ChildItem -LiteralPath $releaseRoot -Force -ErrorAction SilentlyContinue
foreach ($item in $releaseItems) {
    if ($item.FullName -eq $packageDir -or $item.FullName -eq $zipPath) {
        continue
    }

    try {
        Remove-Item -LiteralPath $item.FullName -Recurse -Force -ErrorAction Stop
    } catch {
        Write-Warning "Could not remove old release item: $($item.FullName)"
    }
}

Write-Host "Package directory: $packageDir"
Write-Host "Zip package: $zipPath"
