# Seelex 分阶段构建 / 部署 / 发布 / 回滚流程
#
# 目标: 让「更新当前可用的 dev GUI」与「产出跨平台发布包」变成
#       有门禁、可回滚、可验证的流程, 不再直接清理/覆盖 dist。
#
# 阶段划分:
#   Stage     把新 GUI 二进制构建到暂存区 tmp/staging-gui/ (不触碰基线工作区)
#   Smoke     对指定二进制做无头冒烟测试 (version + backend 启动链路),
#             报告写入 tmp/smoke/ 并保留 (每个时间戳独立文件, 不覆盖)
#   Deploy    检测运行中的 seelex 进程; 进程不存在或用户确认且进程退出后,
#             先把基线二进制存入 stash, 再覆盖基线工作区
#             dist/seelex-gui-dev/seelex-gui.exe (只替换二进制,
#             config/ 与 .seelex/ 等用户数据一律不动)
#   Rollback  从 stash 恢复「上一个可用版本」回基线工作区 (同样有门禁)
#   Release   构建各平台 CLI 发布包 + Windows GUI 发布包 (Publish),
#             携带版本 tag, 配置仅含 example (绝不含 accounts.yaml / *.local.yaml),
#             不清空 dist, 基线工作区不受影响
#   All       按 Stage -> Smoke -> Deploy -> Smoke -> Release 顺序执行
#             每阶段均有确认门禁; 首个冒烟报告保留在 tmp/smoke/ 供恢复参照
#
# 用法:
#   .\scripts\seelex-flow.ps1 -Stage Stage [-Version "v0.0.2"]
#   .\scripts\seelex-flow.ps1 -Stage Smoke [-SmokeTarget <路径>] [-Version "v0.0.2"]
#   .\scripts\seelex-flow.ps1 -Stage Deploy [-Version "v0.0.2"] [-Yes]
#   .\scripts\seelex-flow.ps1 -Stage Rollback [-Version "v0.0.2"] [-Yes]
#   .\scripts\seelex-flow.ps1 -Stage Release -Version "v0.0.2" [-Yes]
#   .\scripts\seelex-flow.ps1 -Stage All -Version "v0.0.2" [-Yes]
#
# -Yes 表示操作者已在对话中明确确认过, 跳过交互式确认门禁
#      (供 Agent / 自动化使用; 交互式使用不传该参数)。
param(
    [ValidateSet("Stage", "Smoke", "Deploy", "Rollback", "Release", "All")]
    [string]$Stage = "All",
    [string]$Version = "",
    [string]$SmokeTarget = "",
    [switch]$Yes
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot

# ---------- 目录与文件常量 ----------
$DistRoot      = Join-Path $Root "dist"
$BaselineDir   = Join-Path $DistRoot "seelex-gui-dev"
$BaselineExe   = Join-Path $BaselineDir "seelex-gui.exe"
$StagingRoot   = Join-Path $Root "tmp\staging-gui"
$StagedExe     = Join-Path $StagingRoot "seelex-gui.exe"
$VersionFile   = Join-Path $StagingRoot "version.txt"
$StashRoot     = Join-Path $Root "tmp\stash\seelex-gui-dev"
$StashPrevious = Join-Path $StashRoot "seelex-gui.previous.exe"
$SmokeDir      = Join-Path $Root "tmp\smoke"
$DeployLog     = Join-Path $Root "tmp\deploy.log"

$VersionPkg    = "github.com/RedHuang-0622/seelex/internal/buildinfo"
$Targets = @(
    @{ OS = "windows"; Arch = "amd64"; Ext = ".exe"; Archive = "zip" },
    @{ OS = "linux";   Arch = "amd64"; Ext = "";     Archive = "tar.gz" },
    @{ OS = "darwin";  Arch = "amd64"; Ext = "";     Archive = "tar.gz" },
    @{ OS = "darwin";  Arch = "arm64"; Ext = "";     Archive = "tar.gz" }
)

# ---------- 工具函数 ----------
function Write-Step([string]$Title) {
    Write-Host ""
    Write-Host "=== $Title ===" -ForegroundColor Cyan
}

function Get-CurrentVersion {
    $verFile = Join-Path $Root "internal\buildinfo\version.go"
    if (Test-Path -LiteralPath $verFile) {
        $match = Select-String -Path $verFile -Pattern 'var Version = "([^"]*)"'
        if ($match -and $match.Matches[0].Groups[1].Value) {
            return $match.Matches[0].Groups[1].Value
        }
    }
    return "dev"
}

function Confirm-Step([string]$Prompt) {
    if ($Yes) {
        Write-Host "[确认] $Prompt (已通过 -Yes 预先确认)" -ForegroundColor DarkYellow
        return $true
    }
    while ($true) {
        $answer = Read-Host "$Prompt (y=继续 / n=取消)"
        if ($answer -match '^(y|yes|是)$') { return $true }
        if ($answer -match '^(n|no|否)$') { return $false }
        Write-Host "请输入 y 或 n" -ForegroundColor Yellow
    }
}

function Get-SeelexProcesses {
    Get-Process -ErrorAction SilentlyContinue |
        Where-Object { $_.ProcessName -like "seelex*" } |
        Select-Object Id, ProcessName, Path
}

function Wait-ForProcessesGone {
    $procs = @(Get-SeelexProcesses)
    if ($procs.Count -eq 0) {
        Write-Host "[进程] 未发现运行中的 seelex 进程" -ForegroundColor Green
        return
    }
    if ($Yes) {
        Write-Host "[进程] 检测到运行中的 seelex:" -ForegroundColor Yellow
        $procs | Format-Table Id, ProcessName, Path -AutoSize | Out-String | Write-Host
        throw "seelex 进程仍在运行, 请先关闭后再执行 (或改用交互式模式等待退出)"
    }
    while ($procs.Count -gt 0) {
        Write-Host "[进程] 检测到运行中的 seelex:" -ForegroundColor Yellow
        $procs | Format-Table Id, ProcessName, Path -AutoSize | Out-String | Write-Host
        $cmd = Read-Host "请关闭 seelex 后按回车继续等待; 输入 c 取消"
        if ($cmd -match '^c$') { throw "用户取消: seelex 进程仍在运行" }
        Start-Sleep -Seconds 3
        $procs = @(Get-SeelexProcesses)
    }
    Write-Host "[进程] seelex 已全部退出" -ForegroundColor Green
}

function Save-StashCopy([string]$SourceExe) {
    # 把 SourceExe 存入 stash: 固定名 previous.exe(回滚源) + 时间戳历史(保留最近 5 份)
    New-Item -ItemType Directory -Force -Path $StashRoot | Out-Null
    if (-not (Test-Path -LiteralPath $SourceExe -PathType Leaf)) { return }
    $stamp = Get-Date -Format "yyyyMMdd-HHmmss"
    $history = Join-Path $StashRoot "seelex-gui.$stamp.exe"
    Copy-Item -LiteralPath $SourceExe -Destination $history -Force
    Copy-Item -LiteralPath $SourceExe -Destination $StashPrevious -Force
    Write-Host "[stash] 已存档: $history" -ForegroundColor Green
    $old = Get-ChildItem -LiteralPath $StashRoot -Filter "seelex-gui.*.exe" |
        Sort-Object LastWriteTime -Descending | Select-Object -Skip 5
    foreach ($item in $old) {
        Remove-Item -LiteralPath $item.FullName -Force
    }
    if (-not (Test-Path -LiteralPath (Join-Path $StashRoot "README.txt"))) {
        @"
此目录是 Seelex dev GUI 的 stash 回滚区, 由 scripts/seelex-flow.ps1 维护。

- seelex-gui.previous.exe : 上一次覆盖前的「上一个可用版本」(Rollback 默认恢复它)
- seelex-gui.<时间戳>.exe : 历史快照, 保留最近 5 份

回滚命令:
  .\scripts\seelex-flow.ps1 -Stage Rollback
或  make rollback-gui

恢复动作: 把 previous.exe 复制回 dist/seelex-gui-dev/seelex-gui.exe,
覆盖前仍会检查进程、再次备份、并校验哈希。
"@ | Set-Content -LiteralPath (Join-Path $StashRoot "README.txt") -Encoding utf8
    }
}

function Assert-PublishClean([string]$Dir) {
    $unsafe = Get-ChildItem -LiteralPath $Dir -Recurse -Force | Where-Object {
        $_.FullName -match '[\\/]\.seelex([\\/]|$)' -or
        $_.FullName -match '[\\/]config[\\/]accounts\.yaml$' -or
        $_.Name -match '\.(local|secret)\.yaml$'
    }
    if ($unsafe) {
        $unsafe.FullName | ForEach-Object { Write-Host "[审计] 发现私有文件: $_" -ForegroundColor Red }
        throw "发布目录包含私有/运行期文件: $Dir"
    }
    Write-Host "[审计] 无私有配置泄漏: $Dir" -ForegroundColor Green
}

function Copy-ReleaseRuntime([string]$OutDir) {
    $configOut = Join-Path $OutDir "config"
    New-Item -ItemType Directory -Force -Path $configOut | Out-Null
    Copy-Item (Join-Path $Root "config\accounts.example.yaml") $configOut -Force
    Copy-Item (Join-Path $Root "config\README.md") $configOut -Force
    Copy-Item (Join-Path $Root "config\seele.yaml") $configOut -Force
    Copy-Item (Join-Path $Root "config\seelex.yaml") $configOut -Force
    Copy-Item -Recurse (Join-Path $Root "plugins") $OutDir -Force
    Copy-Item (Join-Path $Root "LICENSE") $OutDir -Force
    Copy-Item (Join-Path $Root "CHANGELOG.md") $OutDir -Force
    Copy-Item (Join-Path $Root "README.md") $OutDir -Force
    if (Test-Path -LiteralPath (Join-Path $Root "README_EN.md") -PathType Leaf) {
        Copy-Item (Join-Path $Root "README_EN.md") $OutDir -Force
    }
}

function Write-Checksum([string]$Archive) {
    $hash = (Get-FileHash -LiteralPath $Archive -Algorithm SHA256).Hash.ToLowerInvariant()
    "$hash  $([System.IO.Path]::GetFileName($Archive))" |
        Set-Content -LiteralPath "$Archive.sha256" -Encoding ascii
    Write-Host "[sha256] $Archive.sha256" -ForegroundColor DarkGray
}

# ---------- 阶段 1: Stage ----------
function Invoke-Stage {
    Write-Step "阶段 1/4: 构建到暂存区"
    $ver = if ($Version) { $Version } else { Get-CurrentVersion }
    Write-Host "[stage] 版本: $ver" -ForegroundColor Cyan
    Write-Host "[stage] 输出: $StagedExe (暂存区, 不触碰基线工作区)" -ForegroundColor Cyan
    New-Item -ItemType Directory -Force -Path $StagingRoot | Out-Null
    & go build -C $Root -tags "gui,desktop,production" -trimpath `
        -ldflags "-s -w -H windowsgui -X $VersionPkg.Version=$ver -X $VersionPkg.DefaultFrontend=gui" `
        -o $StagedExe "."
    if ($LASTEXITCODE -ne 0) { throw "暂存区构建失败" }
    $size = (Get-Item -LiteralPath $StagedExe).Length
    $hash = (Get-FileHash -LiteralPath $StagedExe -Algorithm SHA256).Hash
    Set-Content -LiteralPath $VersionFile -Value $ver -Encoding ascii
    Write-Host "[stage] 完成: $StagedExe ($([Math]::Round($size / 1MB, 1)) MB)" -ForegroundColor Green
    Write-Host "[stage] 版本记录: $VersionFile -> $ver" -ForegroundColor DarkGray
    Write-Host "[stage] SHA256: $hash" -ForegroundColor DarkGray
}

# ---------- 阶段 2: Smoke ----------
function Invoke-Smoke {
    Write-Step "阶段 2/4: 无头冒烟测试"
    $target = if ($SmokeTarget) { $SmokeTarget } else { $StagedExe }
    if (-not [System.IO.Path]::IsPathRooted($target)) {
        $target = Join-Path $Root $target
    }
    if (-not (Test-Path -LiteralPath $target -PathType Leaf)) {
        throw "冒烟目标不存在: $target (可先执行 Stage 构建)"
    }
    $label = [System.IO.Path]::GetFileNameWithoutExtension($target)
    $stamp = Get-Date -Format "yyyyMMdd-HHmmss"
    New-Item -ItemType Directory -Force -Path $SmokeDir | Out-Null
    $report = Join-Path $SmokeDir "smoke-$label-$stamp.log"
    $lines = New-Object System.Collections.Generic.List[string]

    # 1) 版本探针: -version 应输出版本号并退出 0
    $oldEAP = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    $verOut = (& $target -version 2>&1 | Out-String).Trim()
    $verExit = $LASTEXITCODE
    $ErrorActionPreference = $oldEAP
    $lines.Add("[1/2] version  exit=$verExit output=$verOut")
    if ($verExit -ne 0) {
        $lines.Add("[1/2] FAIL: -version 退出码非 0")
    } elseif ($verOut -eq "") {
        $lines.Add("[1/2] FAIL: -version 无输出")
    } else {
        $lines.Add("[1/2] PASS: 版本输出 $verOut")
    }

    # 2) backend 启动链路: -frontend backend -backend-prompt /help
    #    使用独立 store 与 backend-log, 不触碰基线会话与配置
    $store = Join-Path $SmokeDir "store-$label-$stamp"
    $bootLog = Join-Path $SmokeDir "boot-$label-$stamp.log"
    $oldEAP = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    $bootOut = (& $target -frontend backend -backend-prompt "/help" `
        -store $store -backend-log $bootLog 2>&1 | Out-String).Trim()
    $bootExit = $LASTEXITCODE
    $ErrorActionPreference = $oldEAP
    $ready = $false
    if (Test-Path -LiteralPath $bootLog -PathType Leaf) {
        $ready = (Select-String -LiteralPath $bootLog -Pattern "startup.frontend.ready" -Quiet)
    }
    $lines.Add("[2/2] boot    exit=$bootExit ready=$ready log=$bootLog")
    if ($bootExit -ne 0) { $lines.Add("[2/2] FAIL: backend 退出码非 0") }
    if (-not $ready) { $lines.Add("[2/2] FAIL: 未观测到 startup.frontend.ready") }
    if ($bootExit -eq 0 -and $ready) { $lines.Add("[2/2] PASS: 启动链路完整, /help 本地请求处理成功") }

    $reportLines = $lines -join [Environment]::NewLine
    Set-Content -LiteralPath $report -Value $reportLines -Encoding utf8
    Write-Host "[smoke] 报告: $report" -ForegroundColor Cyan
    $reportLines -split [Environment]::NewLine | ForEach-Object { Write-Host "  $_" }

    $failed = @($lines | Where-Object { $_ -match "FAIL" }).Count
    if ($failed -gt 0) {
        throw "冒烟测试未通过 ($failed 项失败), 报告保留于 $report"
    }
    Write-Host "[smoke] 通过: $target" -ForegroundColor Green
}

# ---------- 阶段 3: Deploy ----------
function Invoke-Deploy {
    Write-Step "阶段 3/4: 部署到基线工作区"
    if (-not (Test-Path -LiteralPath $StagedExe -PathType Leaf)) {
        throw "暂存区没有二进制 $StagedExe, 请先执行 Stage 构建"
    }
    if (-not (Test-Path -LiteralPath $BaselineDir -PathType Directory)) {
        throw "未找到基线工作区 $BaselineDir, 请先准备带真实配置的基线目录"
    }
    $stagedHash = (Get-FileHash -LiteralPath $StagedExe -Algorithm SHA256).Hash
    $stagedVer = if (Test-Path -LiteralPath $VersionFile) {
        (Get-Content -LiteralPath $VersionFile -Raw).Trim()
    } else { "dev" }

    Write-Host "将覆盖:" -ForegroundColor Yellow
    Write-Host "  目标: $BaselineExe" -ForegroundColor Yellow
    Write-Host "  来源: $StagedExe (version=$stagedVer, SHA256 $($stagedHash.Substring(0, 16))...)" -ForegroundColor Yellow
    Write-Host "  影响: 仅替换该二进制文件; 基线工作区内的 config/accounts.yaml、" -ForegroundColor Yellow
    Write-Host "        config/seelex.yaml、config/seele.yaml、.seelex/ 会话记录与 plugins/ 均保持不变。" -ForegroundColor Yellow
    Write-Host "  可恢复: 覆盖前自动把当前二进制存入 stash 回滚区 $StashRoot," -ForegroundColor Yellow
    Write-Host "        可用 make rollback-gui 一键回滚。" -ForegroundColor Yellow

    if (-not (Confirm-Step "是否继续部署?")) { throw "部署已取消" }
    Wait-ForProcessesGone

    Save-StashCopy $BaselineExe
    Copy-Item -LiteralPath $StagedExe -Destination $BaselineExe -Force
    $newHash = (Get-FileHash -LiteralPath $BaselineExe -Algorithm SHA256).Hash
    if ($newHash -ne $stagedHash) {
        throw "覆盖后校验失败, 请从 stash 回滚: $StashPrevious"
    }
    $stamp = Get-Date -Format "yyyy-MM-dd HH:mm:ss"
    "deploy $stamp version=$stagedVer sha256=$newHash" |
        Add-Content -LiteralPath $DeployLog -Encoding utf8
    Write-Host "[部署] 完成: $BaselineExe" -ForegroundColor Green
    Write-Host "[校验] SHA256 一致; 可运行基线目录中的 seelex-gui.exe" -ForegroundColor Green
}

# ---------- 阶段 3b: Rollback ----------
function Invoke-Rollback {
    Write-Step "回滚: 从 stash 恢复上一个可用版本"
    if (-not (Test-Path -LiteralPath $StashPrevious -PathType Leaf)) {
        throw "stash 中没有可恢复的版本: $StashPrevious"
    }
    if (-not (Test-Path -LiteralPath $BaselineDir -PathType Directory)) {
        throw "未找到基线工作区 $BaselineDir"
    }
    $stashHash = (Get-FileHash -LiteralPath $StashPrevious -Algorithm SHA256).Hash
    Write-Host "将覆盖:" -ForegroundColor Yellow
    Write-Host "  目标: $BaselineExe" -ForegroundColor Yellow
    Write-Host "  来源: $StashPrevious (SHA256 $($stashHash.Substring(0, 16))...)" -ForegroundColor Yellow
    Write-Host "  影响: 仅替换二进制; 用户数据与配置不变; 当前二进制会先存入 stash 历史。" -ForegroundColor Yellow
    if (-not (Confirm-Step "是否执行回滚?")) { throw "回滚已取消" }
    Wait-ForProcessesGone

    Save-StashCopy $BaselineExe
    Copy-Item -LiteralPath $StashPrevious -Destination $BaselineExe -Force
    $newHash = (Get-FileHash -LiteralPath $BaselineExe -Algorithm SHA256).Hash
    if ($newHash -ne $stashHash) { throw "回滚后校验失败" }
    Write-Host "[回滚] 完成: $BaselineExe 已恢复为 stash 中的上一个可用版本" -ForegroundColor Green
}

# ---------- 阶段 4: Release ----------
function Invoke-Release {
    Write-Step "阶段 4/4: 构建各平台发布包"
    if (-not $Version -or $Version -eq "dev") {
        throw "发布必须携带版本 tag, 例如 -Version v0.0.2; 当前版本为 '$Version'"
    }
    if ($Version -notmatch '^v?[0-9]+\.[0-9]+\.[0-9]+([._-][A-Za-z0-9._-]+)?$') {
        throw "版本格式不正确: '$Version', 应为 SemVer tag 如 v0.0.2"
    }
    if (-not (Confirm-Step "开始构建各平台发布包? 仅包含 example 配置(不含 accounts.yaml 等密钥), 不清空 dist, 不影响基线工作区 $BaselineDir")) {
        throw "发布已取消"
    }

    $archiveVersion = $Version.TrimStart("v")
    $buildFlags = "-s -w -X $VersionPkg.Version=$Version"
    $savedEnv = @{
        GOOS = $env:GOOS; GOARCH = $env:GOARCH; CGO_ENABLED = $env:CGO_ENABLED
    }

    try {
        foreach ($t in $Targets) {
            $os = $t.OS; $arch = $t.Arch; $ext = $t.Ext
            $outDir = Join-Path $DistRoot "$os-$arch"
            $binPath = Join-Path $outDir ("seelex" + $ext)
            New-Item -ItemType Directory -Force -Path $outDir | Out-Null
            Write-Host "[build] GOOS=$os GOARCH=$arch -> $binPath" -ForegroundColor Green
            $env:GOOS = $os; $env:GOARCH = $arch; $env:CGO_ENABLED = "0"
            & go build -trimpath -ldflags $buildFlags -o $binPath "."
            if ($LASTEXITCODE -ne 0) { throw "构建 $os/$arch 失败" }
            Copy-ReleaseRuntime $outDir
            Assert-PublishClean $outDir
        }
    }
    finally {
        $env:GOOS = $savedEnv.GOOS; $env:GOARCH = $savedEnv.GOARCH
        $env:CGO_ENABLED = $savedEnv.CGO_ENABLED
    }

    Write-Host "[build] Windows GUI 发布包 (Publish, 仅 example 配置)" -ForegroundColor Green
    & (Join-Path $PSScriptRoot "build-gui.ps1") -Version $Version -BuildKind Publish
    if ($LASTEXITCODE -ne 0) { throw "GUI 发布包构建失败" }
    $guiRoot = Join-Path $DistRoot "seelex-v$archiveVersion-windows-amd64-gui"
    if (Test-Path -LiteralPath $guiRoot -PathType Directory) {
        Assert-PublishClean $guiRoot
    }

    Write-Host "[archive] 生成归档与校验和" -ForegroundColor Cyan
    foreach ($t in $Targets) {
        $os = $t.OS; $arch = $t.Arch
        $srcDir = Join-Path $DistRoot "$os-$arch"
        $dirName = "seelex-v$archiveVersion-$os-$arch"
        $stagingDir = Join-Path $DistRoot $dirName
        if (Test-Path -LiteralPath $stagingDir) { Remove-Item -Recurse -Force -LiteralPath $stagingDir }
        Copy-Item -Recurse -LiteralPath $srcDir -Destination $stagingDir
        if ($t.Archive -eq "zip") {
            $archive = "$stagingDir.zip"
            if (Test-Path -LiteralPath $archive) { Remove-Item -Force -LiteralPath $archive }
            Compress-Archive -Path $stagingDir -DestinationPath $archive -Force
        } else {
            $archive = Join-Path $DistRoot "$dirName.tar.gz"
            if (Test-Path -LiteralPath $archive) { Remove-Item -Force -LiteralPath $archive }
            & tar -czf $archive -C $DistRoot $dirName
            if ($LASTEXITCODE -ne 0) { throw "tar 归档失败: $archive" }
        }
        Remove-Item -Recurse -Force -LiteralPath $stagingDir
        Write-Checksum $archive
        Write-Host "[archive] $archive" -ForegroundColor Green
    }
    Write-Host "[release] 完成: 发布产物位于 $DistRoot, 版本 $Version (配置仅 example)" -ForegroundColor Green
}

# ---------- All 流程 ----------
function Invoke-All {
    Invoke-Stage
    Invoke-Smoke
    Invoke-Deploy
    Invoke-Smoke
    Invoke-Release
}

switch ($Stage) {
    "Stage"    { Invoke-Stage }
    "Smoke"    { Invoke-Smoke }
    "Deploy"   { Invoke-Deploy }
    "Rollback" { Invoke-Rollback }
    "Release"  { Invoke-Release }
    "All"      { Invoke-All }
}

Write-Host ""
Write-Host "=== 流程完成 ===" -ForegroundColor Cyan
