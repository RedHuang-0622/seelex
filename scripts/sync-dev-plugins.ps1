# ============================================================================
# Sync the shipped plugin payload from the repository into the dev GUI baseline.
# ----------------------------------------------------------------------------
# 为什么要有这个脚本（2026-10-05 事故）：运行树 dist/seelex-gui-dev/plugins/ 一直
# 不是"每次都会刷新"的，它是一个**冻结的快照**——仓库把 curated.yaml 加进来之后，
# 包内那份至今缺席，app 启动期于是报"精选目录读不到（责任链上 3 个根都没有）"。
#
# 两条刷新通路，规则必须一致（本脚本 = PowerShell 侧，scripts/build-dev.sh 的
# sync_package_plugins = POSIX 侧）：
#   源   仓库 plugins/（发行载荷：插件目录 + curated.yaml + README.md）
#   目标 <P2 dev GUI 基线>/plugins/（build-layout.ps1 的 DevBaselineDir）
#   **只覆盖、不删除**：包内"本机自加、不入库"的插件目录原样保留并报出来；仓库里
#   已删掉的插件不因此被清掉——脚本不替使用者销毁现场（要清请手工删）。
#   幂等：两侧内容一致时一个字节都不动。
#
# 用法：
#   scripts/sync-dev-plugins.ps1           # 刷新并打印本次变化
#   scripts/sync-dev-plugins.ps1 -Quiet     # 只打印一行结论（flow / hook 用）
#   make sync-dev-plugins
# ============================================================================
param(
    [switch]$Quiet
)

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot

. (Join-Path $PSScriptRoot "build-layout.ps1")
$Layout = Get-SeelexLayout
Assert-DistRootCleanLayout -DistRoot $Layout.DistRoot

$src = Join-Path $Root "plugins"
$dst = Join-Path $Layout.DevBaselineDir "plugins"

function Write-Line([string]$Text) {
    if (-not $Quiet) { Write-Host $Text }
}

if (-not (Test-Path -LiteralPath $src -PathType Container)) {
    throw "仓库 plugins/ 不存在（发行载荷的唯一事实源）: $src"
}

# 1) 仓库里的每个文件与包内那份逐一做**内容**比对（不看时间戳：Copy-Item 保 mtime、
#    cp -r 不保，用时间戳判定会在两种工具之间翻转）。
$changed = New-Object System.Collections.Generic.List[string]
foreach ($file in @(Get-ChildItem -LiteralPath $src -Recurse -File -Force)) {
    $rel = $file.FullName.Substring($src.Length).TrimStart("\")
    $target = Join-Path $dst $rel
    if (-not (Test-Path -LiteralPath $target -PathType Leaf)) {
        $changed.Add("+ $rel")
        continue
    }
    $a = (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash
    $b = (Get-FileHash -LiteralPath $target -Algorithm SHA256).Hash
    if ($a -ne $b) { $changed.Add("~ $rel") }
}

# 2) 包内多出来的顶级目录 = 本机自加、不入库的插件：保留，但要报出来（不然"刷新过"
#    与"没刷新过"在日志里长得一样）。
$shipped = @(Get-ChildItem -LiteralPath $src -Directory -Force | ForEach-Object { $_.Name })
$localOnly = @()
if (Test-Path -LiteralPath $dst -PathType Container) {
    $localOnly = @(Get-ChildItem -LiteralPath $dst -Directory -Force |
        Where-Object { $shipped -notcontains $_.Name } | ForEach-Object { $_.Name })
}
if ($localOnly.Count -gt 0) {
    Write-Line ("[plugins] 保留本机自加（不在仓库载荷里）: {0}" -f ($localOnly -join ", "))
}

if ($changed.Count -eq 0) {
    Write-Line "[plugins] 已与仓库一致，未改动一个字节"
    return
}

New-Item -ItemType Directory -Force -Path $dst | Out-Null
Copy-Item -Path (Join-Path $src "*") -Destination $dst -Recurse -Force

Write-Line ("[plugins] {0} -> {1}（只覆盖，不删包内自加目录；本次刷新 {2} 个文件）" -f $src, $dst, $changed.Count)
if (-not $Quiet) {
    foreach ($line in ($changed | Sort-Object)) { Write-Host ("  {0}" -f $line) }
}
