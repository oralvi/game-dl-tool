$ErrorActionPreference = "Stop"

$projectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$releaseRoot = Join-Path $projectRoot "releases"
$timestamp = Get-Date -Format "yyyyMMdd-HHmmss"
$packageName = "game-dl-tool-windows-amd64-$timestamp"
$packageDir = Join-Path $releaseRoot $packageName
$zipPath = Join-Path $releaseRoot "$packageName.zip"

New-Item -ItemType Directory -Path $releaseRoot -Force | Out-Null
if (Test-Path -LiteralPath $packageDir) {
    Remove-Item -LiteralPath $packageDir -Recurse -Force
}
if (Test-Path -LiteralPath $zipPath) {
    Remove-Item -LiteralPath $zipPath -Force
}

New-Item -ItemType Directory -Path $packageDir -Force | Out-Null

Push-Location $projectRoot
try {
    go test ./...
    go build -o (Join-Path $packageDir "game-dl-tool.exe") .
} finally {
    Pop-Location
}

Copy-Item -LiteralPath (Join-Path $projectRoot "README.md") -Destination (Join-Path $packageDir "README.md")

Compress-Archive -Path (Join-Path $packageDir "*") -DestinationPath $zipPath -Force

Write-Host "Package directory: $packageDir"
Write-Host "Zip package: $zipPath"
