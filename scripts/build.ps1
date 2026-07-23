# Seelex 构建与打包脚本 (PowerShell)
# 使用: .\scripts\build.ps1 [-Version "1.0.0"] [-SkipClean]
param(
    [string]$Version = "dev",
    [switch]$SkipClean
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
$ArchiveVersion = $Version.TrimStart("v")

# ─── 目标平台 ────────────────────────────────────────
$Targets = @(
    @{ OS = "windows"; Arch = "amd64";   Ext = ".exe" }
    @{ OS = "linux";   Arch = "amd64";   Ext = ""     }
    @{ OS = "darwin";  Arch = "amd64";   Ext = ""     }
    @{ OS = "darwin";  Arch = "arm64";   Ext = ""     }
)

# ─── 输出根目录 ──────────────────────────────────────
$DistRoot = Join-Path $Root "dist"

if (-not $SkipClean -and (Test-Path $DistRoot)) {
    Write-Host "[clean] 清理 $DistRoot" -ForegroundColor Yellow
    Remove-Item -Recurse -Force $DistRoot
}

Write-Host "[build] 版本: $Version" -ForegroundColor Cyan

# ─── 构建每个目标 ────────────────────────────────────
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

    go build -trimpath -ldflags "-s -w -X main.Version=$Version" -o $binPath .

    if ($LASTEXITCODE -ne 0) {
        Write-Host "[error] 构建 $os/$arch 失败" -ForegroundColor Red
        exit $LASTEXITCODE
    }

    # ─── 复制运行时文件 ───────────────────────────────
    Write-Host "[copy]  运行时文件 -> $outDir" -ForegroundColor DarkGray

    # config/ — only publish the tracked example, never local account files.
    $configOut = Join-Path $outDir "config"
    New-Item -ItemType Directory -Force -Path $configOut | Out-Null
    Copy-Item (Join-Path $Root "config/accounts.example.yaml") $configOut -Force

    # plugins/
    Copy-Item -Recurse (Join-Path $Root "plugins") $outDir -Force

    # seele.yaml
    Copy-Item (Join-Path $Root "seele.yaml") $outDir -Force
    Copy-Item (Join-Path $Root "LICENSE") $outDir -Force
    Copy-Item (Join-Path $Root "CHANGELOG.md") $outDir -Force
    Copy-Item (Join-Path $Root "README.md") $outDir -Force

    Write-Host "[ok]   $os/$arch 完成 ($( "{0:N0}" -f (Get-Item $binPath).Length) bytes)" -ForegroundColor Green
}

# ─── 归档（可选） ────────────────────────────────────
Write-Host ""
Write-Host "[pack] 生成归档..." -ForegroundColor Cyan

foreach ($t in $Targets) {
    $os = $t.OS
    $arch = $t.Arch
    $dirName = "seelex-v$ArchiveVersion-$os-$arch"
    $srcDir = Join-Path $DistRoot "$os-$arch"
    $archive = Join-Path $DistRoot "$dirName.zip"

    Write-Host "[zip]  $archive" -ForegroundColor DarkGray

    # 在 dist 内创建临时目录以控制压缩包内路径
    $tmpDir = Join-Path $DistRoot $dirName
    if (Test-Path $tmpDir) { Remove-Item -Recurse -Force $tmpDir }
    Copy-Item -Recurse $srcDir $tmpDir

    Compress-Archive -Path $tmpDir -DestinationPath $archive -Force
    Remove-Item -Recurse -Force $tmpDir
}

# ─── 摘要 ────────────────────────────────────────────
Write-Host ""
Write-Host "=== 打包完成 ===" -ForegroundColor Cyan
Write-Host "输出目录: $DistRoot" -ForegroundColor White

Get-ChildItem $DistRoot -Recurse -File | ForEach-Object {
    $rel = $_.FullName.Replace("$DistRoot\", "")
    Write-Host "  $rel  ($( "{0:N0}" -f $_.Length) bytes)"
}

Write-Host ""
Write-Host "目录结构:" -ForegroundColor White
Write-Host "  dist/"
Write-Host "    windows-amd64/"
Write-Host "      seelex.exe"
Write-Host "      config/"
Write-Host "      plugins/"
Write-Host "      seele.yaml"
Write-Host "    linux-amd64/"
Write-Host "      seelex"
Write-Host "      config/"
Write-Host "      plugins/"
Write-Host "      seele.yaml"
Write-Host "    darwin-amd64/"
Write-Host "      seelex"
Write-Host "      config/"
Write-Host "      plugins/"
Write-Host "      seele.yaml"
Write-Host "    darwin-arm64/"
Write-Host "      seelex"
Write-Host "      config/"
Write-Host "      plugins/"
Write-Host "      seele.yaml"
