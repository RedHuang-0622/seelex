#Requires -Version 7.0
<#
.SYNOPSIS
    Git Rebase vs Merge 场景决策辅助脚本
.DESCRIPTION
    根据当前分支状态和共享情况，自动选择 git pull --rebase 或 git pull (merge) 策略。
    提供交互式 rebase 辅助、冲突检测与解决、rebase 中止/继续快捷操作。
.NOTES
    Module:    git-rebase-merge
    Author:    Seelex Git Plugin
#>

#region 辅助函数

function Test-CommandExists {
    param([string]$Command)
    return [bool](Get-Command -Name $Command -ErrorAction SilentlyContinue)
}

function Assert-GitAvailable {
    if (-not (Test-CommandExists "git")) {
        throw "Git 不可用，请先安装 Git"
    }
}

function Get-CurrentBranch {
    return (git rev-parse --abbrev-ref HEAD 2>$null)
}

function Test-BranchHasRemote {
    param([string]$BranchName)
    $remote = git config "branch.$BranchName.remote" 2>$null
    return [bool]$remote
}

function Test-BranchHasUnpushedCommits {
    param([string]$BranchName)
    $count = git rev-list --count "origin/$BranchName..$BranchName" 2>$null
    if ($LASTEXITCODE -ne 0) { return $false }
    return [int]$count -gt 0
}

#endregion

#region 核心功能

function Invoke-SmartPull {
    <#
    .SYNOPSIS
        智能 pull：根据分支状态自动选择 --rebase 或 merge
    .DESCRIPTION
        本地未 push 的分支 → git pull --rebase
        已 push 的分支     → git pull (merge)
        已共享的分支       → git pull (merge)
    #>
    [CmdletBinding()]
    param()

    Assert-GitAvailable
    $branch = Get-CurrentBranch
    Write-Host "当前分支: $branch" -ForegroundColor Cyan

    $hasRemote = Test-BranchHasRemote -BranchName $branch
    $hasUnpushed = Test-BranchHasUnpushedCommits -BranchName $branch

    if (-not $hasRemote) {
        Write-Host "→ 本地分支（未关联远程），使用 git pull --rebase" -ForegroundColor Green
        git pull --rebase
        return
    }

    if ($hasUnpushed) {
        Write-Host "→ 有未 push 的 commit，使用 git pull --rebase" -ForegroundColor Green
        git pull --rebase
    } else {
        Write-Host "→ 分支已同步/已共享，使用 git pull (merge) 以确保安全" -ForegroundColor Yellow
        git pull
    }
}

function Invoke-SafeRebase {
    <#
    .SYNOPSIS
        安全 rebase：先检测是否可安全 rebase，再执行
    .PARAMETER TargetBranch
        要变基到的目标分支，默认为 main
    #>
    [CmdletBinding()]
    param(
        [string]$TargetBranch = "main"
    )

    Assert-GitAvailable
    $branch = Get-CurrentBranch

    if ($branch -eq $TargetBranch) {
        Write-Warning "当前已在 $TargetBranch 分支上，无需 rebase"
        return
    }

    $hasRemote = Test-BranchHasRemote -BranchName $branch
    if ($hasRemote) {
        Write-Warning "⚠️ 分支 '$branch' 已关联远程仓库，rebase 会重写历史！"
        $confirm = Read-Host "确定要继续 rebase 吗？(y/N)"
        if ($confirm -ne 'y' -and $confirm -ne 'Y') {
            Write-Host "已取消 rebase" -ForegroundColor Yellow
            return
        }
    }

    Write-Host "→ 正在变基到 $TargetBranch ..." -ForegroundColor Green
    git fetch origin $TargetBranch
    git rebase "origin/$TargetBranch"

    if ($LASTEXITCODE -ne 0) {
        Write-Host "⚠️ Rebase 发生冲突！请解决冲突后运行：" -ForegroundColor Red
        Write-Host "  1. 解决所有冲突文件" -ForegroundColor Yellow
        Write-Host "  2. git add <已解决的文件>" -ForegroundColor Yellow
        Write-Host "  3. git rebase --continue" -ForegroundColor Yellow
        Write-Host "  或取消：git rebase --abort" -ForegroundColor Yellow
    }
}

function Invoke-InteractiveRebase {
    <#
    .SYNOPSIS
        交互式 rebase，整理最近 N 个 commit
    .PARAMETER Count
        要整理的 commit 数量，默认 3
    #>
    [CmdletBinding()]
    param(
        [int]$Count = 3
    )

    Assert-GitAvailable
    $branch = Get-CurrentBranch

    $hasRemote = Test-BranchHasRemote -BranchName $branch
    if ($hasRemote) {
        $hasUnpushed = Test-BranchHasUnpushedCommits -BranchName $branch
        if (-not $hasUnpushed) {
            Write-Warning "⚠️ 所有 commit 都已 push，交互式 rebase 会重写已共享历史！"
            $confirm = Read-Host "确定要继续吗？(y/N)"
            if ($confirm -ne 'y' -and $confirm -ne 'Y') {
                Write-Host "已取消" -ForegroundColor Yellow
                return
            }
        }
    }

    Write-Host "→ 启动交互式 rebase (最近 $Count 个 commit) ..." -ForegroundColor Green
    git rebase -i "HEAD~$Count"
}

function Invoke-RebaseAbort {
    <#
    .SYNOPSIS
        中止正在进行的 rebase 操作
    #>
    [CmdletBinding()]
    param()

    Assert-GitAvailable
    try {
        git rebase --abort
        Write-Host "✓ Rebase 已中止" -ForegroundColor Green
    } catch {
        Write-Host "当前没有正在进行的 rebase 操作" -ForegroundColor Yellow
    }
}

function Invoke-RebaseContinue {
    <#
    .SYNOPSIS
        冲突解决后继续 rebase
    #>
    [CmdletBinding()]
    param()

    Assert-GitAvailable
    try {
        git rebase --continue
    } catch {
        Write-Host "当前没有正在进行的 rebase 操作，或冲突尚未解决" -ForegroundColor Yellow
    }
}

function Invoke-SquashMerge {
    <#
    .SYNOPSIS
        将 feature 分支 squash merge 到目标分支
    .PARAMETER SourceBranch
        源分支（要合并的分支）
    .PARAMETER TargetBranch
        目标分支，默认为 main
    #>
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)]
        [string]$SourceBranch,
        [string]$TargetBranch = "main"
    )

    Assert-GitAvailable
    Write-Host "→ 将 $SourceBranch squash merge 到 $TargetBranch ..." -ForegroundColor Green
    git switch $TargetBranch
    git pull --rebase
    git merge --squash $SourceBranch

    if ($LASTEXITCODE -eq 0) {
        Write-Host "Squash 成功，请检查变更后 git commit" -ForegroundColor Green
    }
}

function Resolve-RebaseConflict {
    <#
    .SYNOPSIS
        列出 rebase 冲突文件，并打开冲突解决向导
    #>
    [CmdletBinding()]
    param()

    Assert-GitAvailable
    $conflictFiles = git diff --name-only --diff-filter=U 2>$null

    if (-not $conflictFiles) {
        Write-Host "✓ 没有未解决的冲突" -ForegroundColor Green
        return
    }

    Write-Host "⚠️ 存在冲突的文件：" -ForegroundColor Red
    $conflictFiles | ForEach-Object { Write-Host "  - $_" -ForegroundColor Yellow }

    Write-Host "`n解决步骤：" -ForegroundColor Cyan
    Write-Host "  1. 编辑冲突文件，解决冲突标记 (<<<<<<< / ======= / >>>>>>>)" -ForegroundColor White
    Write-Host "  2. git add <已解决的文件>" -ForegroundColor White
    Write-Host "  3. git rebase --continue" -ForegroundColor White
    Write-Host "  或中止：git rebase --abort" -ForegroundColor White
}

#endregion

#region 主入口

function Show-RebaseMergeMenu {
    <#
    .SYNOPSIS
        显示 Rebase/Merge 交互菜单
    #>
    [CmdletBinding()]
    param()

    Assert-GitAvailable
    $branch = Get-CurrentBranch

    do {
        Clear-Host
        Write-Host @"
╔══════════════════════════════════════════╗
║     Git Rebase vs Merge 工具箱           ║
║     当前分支: $branch                 ║
╚══════════════════════════════════════════╝

"@ -ForegroundColor Cyan

        Write-Host "1. 智能 Pull（自动选择 rebase/merge）" -ForegroundColor White
        Write-Host "2. 安全 Rebase 到 main" -ForegroundColor White
        Write-Host "3. 交互式 Rebase (rebase -i)" -ForegroundColor White
        Write-Host "4. 中止 Rebase" -ForegroundColor White
        Write-Host "5. 继续 Rebase" -ForegroundColor White
        Write-Host "6. 查看冲突文件" -ForegroundColor White
        Write-Host "7. Squash Merge" -ForegroundColor White
        Write-Host "Q. 退出" -ForegroundColor White

        $choice = Read-Host "`n请选择"
        switch ($choice) {
            "1" { Invoke-SmartPull }
            "2" { Invoke-SafeRebase }
            "3" { Invoke-InteractiveRebase }
            "4" { Invoke-RebaseAbort }
            "5" { Invoke-RebaseContinue }
            "6" { Resolve-RebaseConflict }
            "7" { Invoke-SquashMerge -SourceBranch (Read-Host "输入源分支名") }
            "q" { return }
            "Q" { return }
        }
        if ($choice -ne 'q' -and $choice -ne 'Q') {
            Write-Host "`n按任意键继续..." -ForegroundColor Gray
            $null = $Host.UI.RawUI.ReadKey("NoEcho,IncludeKeyDown")
        }
    } while ($true)
}

#endregion

# Export functions
Export-ModuleMember -Function @(
    "Invoke-SmartPull",
    "Invoke-SafeRebase",
    "Invoke-InteractiveRebase",
    "Invoke-RebaseAbort",
    "Invoke-RebaseContinue",
    "Invoke-SquashMerge",
    "Resolve-RebaseConflict",
    "Show-RebaseMergeMenu"
)
