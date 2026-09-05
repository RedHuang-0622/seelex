# ============================================================================
# Seelex Windows GUI build and package script.
# Usage:
#   .\scripts\build-gui.ps1 [-Version "v0.0.2"] [-BuildKind Publish|Dev]
#                           [-LocalConfigPath "config/accounts.yaml"]
# ----------------------------------------------------------------------------
# Output follows the canonical build layout (single source of truth:
# scripts/build-layout.ps1, mirror table in .claude/build-convention.md).
# The final package lands ONLY in
#   dist/archive/seelex-v<ver>-windows-amd64-gui.zip
#   dist/archive/seelex-v<ver>-windows-amd64-gui.zip.sha256
# The expanded package folder is a transient staging dir under dist/archive/
# and is removed after compression. Nothing is written flat into dist/ or
# anywhere outside the canonical partitions.
# ============================================================================
param(
    [string]$Version = "dev",
    [ValidateSet("Publish", "Dev")]
    [string]$BuildKind = "Publish",
    [string]$LocalConfigPath = ""
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot

. (Join-Path $PSScriptRoot "build-layout.ps1")
$Layout = Get-SeelexLayout
$DistRoot    = $Layout.DistRoot
$ArchiveRoot = $Layout.ArchiveRoot
Assert-DistRootCleanLayout -DistRoot $DistRoot

$ArchiveVersion = $Version.TrimStart("v")
$PackageName    = "seelex-v$ArchiveVersion-windows-amd64-gui"
$ArchivePath    = Join-Path $ArchiveRoot "$PackageName.zip"
$StageRoot      = Join-Path $ArchiveRoot ".stage-$PackageName"

$configSource = $null
if ($BuildKind -eq "Dev") {
    if (-not $LocalConfigPath) {
        throw "dev GUI build requires a local account configuration"
    }
    $configSource = $LocalConfigPath
    if (-not [System.IO.Path]::IsPathRooted($configSource)) {
        $configSource = Join-Path $Root $configSource
    }
    if (-not (Test-Path -LiteralPath $configSource -PathType Leaf)) {
        throw "local GUI account configuration is not a regular file"
    }
}
elseif ($LocalConfigPath) {
    throw "publish GUI build must not receive a local account configuration"
}

New-Item -ItemType Directory -Force -Path $ArchiveRoot | Out-Null
if (Test-Path $StageRoot) {
    Remove-Item -Recurse -Force -LiteralPath $StageRoot
}
New-Item -ItemType Directory -Force -Path (Join-Path $StageRoot "config") | Out-Null

$binary = Join-Path $StageRoot "seelex-gui.exe"
go build -C $Root -tags "gui,desktop,production" -trimpath `
    -ldflags "-s -w -H windowsgui -X github.com/RedHuang-0622/seelex/internal/buildinfo.Version=$Version -X github.com/RedHuang-0622/seelex/internal/buildinfo.DefaultFrontend=gui" `
    -o $binary .
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}

Copy-Item (Join-Path $Root "config/accounts.example.yaml") (Join-Path $StageRoot "config/")
Copy-Item (Join-Path $Root "config/README.md") (Join-Path $StageRoot "config/")
if ($BuildKind -eq "Dev") {
    Copy-Item -LiteralPath $configSource -Destination (Join-Path $StageRoot "config/accounts.yaml")
}
Copy-Item -Recurse (Join-Path $Root "plugins") (Join-Path $StageRoot "plugins")
Copy-Item (Join-Path $Root "config/seele.yaml") (Join-Path $StageRoot "config/")  # permission rules
Copy-Item (Join-Path $Root "config/seelex.yaml") (Join-Path $StageRoot "config/")  # runtime settings
Copy-Item (Join-Path $Root "LICENSE") $StageRoot
Copy-Item (Join-Path $Root "CHANGELOG.md") $StageRoot
Copy-Item (Join-Path $Root "README.md") $StageRoot
if (Test-Path -LiteralPath (Join-Path $Root "README_EN.md") -PathType Leaf) {
    Copy-Item (Join-Path $Root "README_EN.md") $StageRoot
}
if ($BuildKind -eq "Dev" -and (Test-Path -LiteralPath (Join-Path $Root "README-dev.md") -PathType Leaf)) {
    Copy-Item (Join-Path $Root "README-dev.md") $StageRoot
}

if ($BuildKind -eq "Publish") {
    $unsafe = Get-ChildItem -LiteralPath $StageRoot -Recurse -Force | Where-Object {
        $_.FullName -match '[\\/]\.seelex([\\/]|$)' -or
        $_.FullName -match '[\\/]config[\\/]accounts\.yaml$' -or
        $_.Name -match '\.(local|secret)\.yaml$'
    }
    if ($unsafe) {
        $unsafe.FullName | Write-Error
        throw "publish GUI package contains private or runtime-local files"
    }
}

$compressed = $false
for ($attempt = 1; $attempt -le 5; $attempt++) {
    try {
        Compress-Archive -Path $StageRoot -DestinationPath $ArchivePath -Force
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

# Expanded staging dir is transient; only zip + sha256 stay in the partition.
if (Test-Path -LiteralPath $StageRoot) {
    Remove-Item -Recurse -Force -LiteralPath $StageRoot
}

$hash = (Get-FileHash $ArchivePath -Algorithm SHA256).Hash.ToLowerInvariant()
"$hash  $PackageName.zip" | Set-Content "$ArchivePath.sha256"

Write-Host "[ok] GUI package: $ArchivePath"
