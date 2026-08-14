# Seelex Windows GUI build and package script.
# Usage: .\scripts\build-gui.ps1 [-Version "v0.0.2"] [-BuildKind Publish|Dev] [-LocalConfigPath "config/accounts.yaml"]
param(
    [string]$Version = "dev",
    [ValidateSet("Publish", "Dev")]
    [string]$BuildKind = "Publish",
    [string]$LocalConfigPath = ""
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$DistRoot = Join-Path $Root "dist"
$ArchiveVersion = $Version.TrimStart("v")
$PackageName = "seelex-v$ArchiveVersion-windows-amd64-gui"
$PackageRoot = Join-Path $DistRoot $PackageName
$ArchivePath = Join-Path $DistRoot "$PackageName.zip"

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

New-Item -ItemType Directory -Force -Path $DistRoot | Out-Null
if (Test-Path $PackageRoot) {
    Remove-Item -Recurse -Force -LiteralPath $PackageRoot
}
New-Item -ItemType Directory -Force -Path (Join-Path $PackageRoot "config") | Out-Null

$binary = Join-Path $PackageRoot "seelex-gui.exe"
go build -C $Root -tags "gui,desktop,production" -trimpath `
    -ldflags "-s -w -H windowsgui -X github.com/RedHuang-0622/seelex/internal/buildinfo.Version=$Version -X github.com/RedHuang-0622/seelex/internal/buildinfo.DefaultFrontend=gui" `
    -o $binary .
if ($LASTEXITCODE -ne 0) {
    exit $LASTEXITCODE
}

Copy-Item (Join-Path $Root "config/accounts.example.yaml") (Join-Path $PackageRoot "config/")
Copy-Item (Join-Path $Root "config/README.md") (Join-Path $PackageRoot "config/")
if ($BuildKind -eq "Dev") {
    Copy-Item -LiteralPath $configSource -Destination (Join-Path $PackageRoot "config/accounts.yaml")
}
Copy-Item -Recurse (Join-Path $Root "plugins") (Join-Path $PackageRoot "plugins")
Copy-Item (Join-Path $Root "config/seele.yaml") (Join-Path $PackageRoot "config/")  # 权限
Copy-Item (Join-Path $Root "config/seelex.yaml") (Join-Path $PackageRoot "config/")  # 运行参数
Copy-Item (Join-Path $Root "LICENSE") $PackageRoot
Copy-Item (Join-Path $Root "CHANGELOG.md") $PackageRoot
Copy-Item (Join-Path $Root "README.md") $PackageRoot
if (Test-Path -LiteralPath (Join-Path $Root "README_EN.md") -PathType Leaf) {
    Copy-Item (Join-Path $Root "README_EN.md") $PackageRoot
}
if ($BuildKind -eq "Dev" -and (Test-Path -LiteralPath (Join-Path $Root "README-dev.md") -PathType Leaf)) {
    Copy-Item (Join-Path $Root "README-dev.md") $PackageRoot
}

if ($BuildKind -eq "Publish") {
    $unsafe = Get-ChildItem -LiteralPath $PackageRoot -Recurse -Force | Where-Object {
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
