# 同步「内嵌默认数据」与仓库规范档
#
#   config/seelex.yaml、config/seele.yaml  ->  internal/bootseed/assets/config/
#
# 为什么要这一步：包内配置缺失时，应用用内嵌的默认数据初始化（internal/bootseed
# 的 RuntimeConfigPack / PermissionConfigPack）。那份默认数据必须是仓库规范档的
# 逐字节副本，否则"初始化出来的配置"和"仓库里那份"立刻分叉——2026-09-29 的折叠
# 厚摘要开关就是被包内旧档吞掉的（docs/devlog/2026-09-29-dev-package-config-drift.md）。
#
# 口径：规范档是唯一事实源（只读它、不改它）；幂等（内容相同就跳过，比字节不比
# mtime）；只同步这两个文件（accounts.yaml 是本地凭据，一律不碰）。
#
# 忘了跑也不会静默漂移：internal/bootseed 的
# TestEmbeddedConfigDefaultsMatchRepository 会红，并指回这个脚本。

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$dst = Join-Path $root "internal/bootseed/assets/config"
New-Item -ItemType Directory -Force $dst | Out-Null

$sources = [ordered]@{
    "seelex.yaml"        = "config/seelex.yaml"
    "seele.yaml"         = "config/seele.yaml"
    "mcp.yaml"           = "config/mcp.example.yaml"
    "search_engine.yaml" = "config/search_engine.example.yaml"
}
foreach ($name in $sources.Keys) {
    $src = Join-Path $root $sources[$name]
    if (-not (Test-Path $src)) {
        Write-Host "[sync-bootseed] 跳过（仓库里没有）: $sources[$name]"
        continue
    }
    $target = Join-Path $dst $name
    if ((Test-Path $target) -and ((Get-FileHash $src).Hash -eq (Get-FileHash $target).Hash)) {
        continue
    }
    Copy-Item $src $target -Force
    Write-Host "[sync-bootseed] config/$name -> internal/bootseed/assets/config/"
}

Write-Host "[sync-bootseed] done"
