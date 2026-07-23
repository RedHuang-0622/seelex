# Seelex Windows GUI build and package script.
# Usage: .\scripts\build-gui.ps1 [-Version "v0.1.0-alpha.1"]
param(
    [string]$Version = "dev"
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$DistRoot = Join-Path $Root "dist"
$ArchiveVersion = $Version.TrimStart("v")
$PackageName = "seelex-v$ArchiveVersion-windows-amd64-gui"
$PackageRoot = Join-Path $DistRoot $PackageName
$ArchivePath = Join-Path $DistRoot "$PackageName.zip"

New-Item -ItemType Directory -Force -Path $DistRoot | Out-Null
if (Test-Path $PackageRoot) {
    Remove-Item -Recurse -Force -LiteralPath $PackageRoot
}
New-Item -ItemType Directory -Force -Path (Join-Path $PackageRoot "config") | Out-Null

$binary = Join-Path $PackageRoot "seelex-gui.exe"
go build -C $Root -tags "gui,desktop,production" -trimpath `
    -ldflags "-s -w -H windowsgui -X main.Version=$Version -X main.DefaultFrontend=gui" `
    -o $binary .
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}

Copy-Item (Join-Path $Root "config/accounts.example.yaml") (Join-Path $PackageRoot "config/")
Copy-Item -Recurse (Join-Path $Root "plugins") (Join-Path $PackageRoot "plugins")
Copy-Item (Join-Path $Root "seele.yaml") $PackageRoot
Copy-Item (Join-Path $Root "LICENSE") $PackageRoot
Copy-Item (Join-Path $Root "CHANGELOG.md") $PackageRoot
Copy-Item (Join-Path $Root "README.md") $PackageRoot

$compressed = $false
for ($attempt = 1; $attempt -le 5; $attempt++) {
    try {
        Compress-Archive -Path $PackageRoot -DestinationPath $ArchivePath -Force
        $compressed = $true
        break
    }
    catch {
        if ($attempt -eq 5) { throw }
        Start-Sleep -Seconds 1
    }
}
if (-not $compressed) {
    throw "failed to create GUI archive"
}
$hash = (Get-FileHash $ArchivePath -Algorithm SHA256).Hash.ToLowerInvariant()
"$hash  $PackageName.zip" | Set-Content "$ArchivePath.sha256"

Write-Host "[ok] GUI package: $ArchivePath"
