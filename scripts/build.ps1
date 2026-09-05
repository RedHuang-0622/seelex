# ============================================================================
# Seelex cross-platform CLI build and package script (PowerShell).
# Usage: .\scripts\build.ps1 [-Version "1.0.0"] [-SkipClean] [-CleanDev]
# ----------------------------------------------------------------------------
# Canonical layout (single source of truth: scripts/build-layout.ps1, mirror
# table in .claude/build-convention.md):
#   binaries + runtime -> dist/<os>-<arch>/
#   archives + sha256  -> dist/archive/
# The dev GUI baseline partition dist/seelex-gui-dev/ (user data) is NEVER
# cleaned unless -CleanDev is passed explicitly.
# ============================================================================
param(
    [string]$Version = "dev",
    [switch]$SkipClean,
    [switch]$CleanDev
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot

. (Join-Path $PSScriptRoot "build-layout.ps1")
$Layout = Get-SeelexLayout
$DistRoot      = $Layout.DistRoot
$ArchiveRoot   = $Layout.ArchiveRoot
$DevBaselineDir = $Layout.DevBaselineDir
Assert-DistRootCleanLayout -DistRoot $DistRoot

$ArchiveVersion = $Version.TrimStart("v")

# ---- targets --------------------------------------------------------------
$Targets = @(
    @{ OS = "windows"; Arch = "amd64"; Ext = ".exe"; Archive = "zip" },
    @{ OS = "linux";   Arch = "amd64"; Ext = "";     Archive = "tar.gz" },
    @{ OS = "darwin";  Arch = "amd64"; Ext = "";     Archive = "tar.gz" },
    @{ OS = "darwin";  Arch = "arm64"; Ext = "";     Archive = "tar.gz" }
)

# ---- clean (derived partitions only; P2 dev baseline preserved) ------------
if (-not $SkipClean) {
    foreach ($t in $Targets) {
        $outDir = Join-Path $DistRoot "$($t.OS)-$($t.Arch)"
        if (Test-Path -LiteralPath $outDir) {
            Write-Host "[clean] $outDir" -ForegroundColor Yellow
            Remove-Item -Recurse -Force -LiteralPath $outDir
        }
    }
    if (Test-Path -LiteralPath $ArchiveRoot) {
        Write-Host "[clean] $ArchiveRoot" -ForegroundColor Yellow
        Remove-Item -Recurse -Force -LiteralPath $ArchiveRoot
    }
    if (Test-Path -LiteralPath $DevBaselineDir) {
        if ($CleanDev) {
            Write-Host "[clean] $DevBaselineDir (explicit -CleanDev)" -ForegroundColor Yellow
            Remove-Item -Recurse -Force -LiteralPath $DevBaselineDir
        }
        else {
            Write-Host "[clean] keep $DevBaselineDir (dev GUI baseline contains user data; pass -CleanDev to remove)" -ForegroundColor DarkGray
        }
    }
}

Write-Host "[build] version: $Version" -ForegroundColor Cyan

# ---- build each platform tree ---------------------------------------------
foreach ($t in $Targets) {
    $os = $t.OS
    $arch = $t.Arch
    $name = "seelex$($t.Ext)"
    $outDir = Join-Path $DistRoot "$os-$arch"
    $binPath = Join-Path $outDir $name

    Write-Host "[build] GOOS=$os GOARCH=$arch -> $binPath" -ForegroundColor Green
    New-Item -ItemType Directory -Force -Path $outDir | Out-Null

    $env:GOOS = $os
    $env:GOARCH = $arch
    $env:CGO_ENABLED = "0"

    go build -trimpath -ldflags "-s -w -X github.com/RedHuang-0622/seelex/internal/buildinfo.Version=$Version" -o $binPath .
    if ($LASTEXITCODE -ne 0) {
        Write-Host "[error] build failed for $os/$arch" -ForegroundColor Red
        exit $LASTEXITCODE
    }

    # runtime files: example config + permission/runtime yaml live under config/
    $configOut = Join-Path $outDir "config"
    New-Item -ItemType Directory -Force -Path $configOut | Out-Null
    Copy-Item (Join-Path $Root "config/accounts.example.yaml") $configOut -Force
    Copy-Item (Join-Path $Root "config/README.md") $configOut -Force
    Copy-Item (Join-Path $Root "config/seele.yaml") $configOut -Force
    Copy-Item (Join-Path $Root "config/seelex.yaml") $configOut -Force
    Copy-Item -Recurse (Join-Path $Root "plugins") $outDir -Force
    Copy-Item (Join-Path $Root "LICENSE") $outDir -Force
    Copy-Item (Join-Path $Root "CHANGELOG.md") $outDir -Force
    Copy-Item (Join-Path $Root "README.md") $outDir -Force
    if (Test-Path -LiteralPath (Join-Path $Root "README_EN.md") -PathType Leaf) {
        Copy-Item (Join-Path $Root "README_EN.md") $outDir -Force
    }

    Write-Host "[ok]   $os/$arch done ($("{0:N0}" -f (Get-Item $binPath).Length) bytes)" -ForegroundColor Green
}

# ---- archive into dist/archive --------------------------------------------
Write-Host ""
Write-Host "[pack] generating archives into $ArchiveRoot" -ForegroundColor Cyan
New-Item -ItemType Directory -Force -Path $ArchiveRoot | Out-Null

foreach ($t in $Targets) {
    $os = $t.OS
    $arch = $t.Arch
    $dirName = "seelex-v$ArchiveVersion-$os-$arch"
    $srcDir = Join-Path $DistRoot "$os-$arch"
    $stagingDir = Join-Path $ArchiveRoot $dirName
    if (Test-Path -LiteralPath $stagingDir) {
        Remove-Item -Recurse -Force -LiteralPath $stagingDir
    }
    Copy-Item -Recurse $srcDir $stagingDir

    if ($t.Archive -eq "zip") {
        $archive = "$stagingDir.zip"
        Compress-Archive -Path $stagingDir -DestinationPath $archive -Force
    }
    else {
        $archive = Join-Path $ArchiveRoot "$dirName.tar.gz"
        & tar -czf $archive -C $ArchiveRoot $dirName
        if ($LASTEXITCODE -ne 0) { throw "tar archive failed: $archive" }
    }
    Remove-Item -Recurse -Force -LiteralPath $stagingDir

    $hash = (Get-FileHash $archive -Algorithm SHA256).Hash.ToLowerInvariant()
    "$hash  $([System.IO.Path]::GetFileName($archive))" | Set-Content "$archive.sha256"
    Write-Host "[pack] $archive" -ForegroundColor DarkGray
}

# ---- summary ---------------------------------------------------------------
Write-Host ""
Write-Host "=== build complete ===" -ForegroundColor Cyan
Write-Host "platform trees: $DistRoot/<os>-<arch>/"
Write-Host "archives:       $ArchiveRoot"
Get-ChildItem $ArchiveRoot -File -ErrorAction SilentlyContinue | ForEach-Object {
    Write-Host "  $($_.Name)  ($("{0:N0}" -f $_.Length) bytes)"
}
