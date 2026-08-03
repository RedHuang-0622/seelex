#Requires -Version 7.0
<#
.SYNOPSIS
    Git Worktree 管理脚本 — Plan 模式下的隔离开发
.DESCRIPTION
    提供 worktree 创建、列表、删除、移动、清理操作。
    支持 Plan 模式 SubAgent 集成：为每个节点创建独立 worktree、依赖协调、合并与清理。
.NOTES
    Module:    git-worktree
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

function Get-RepoRoot {
    return (git rev-parse --show-toplevel 2>$null)
}

#endregion

#region Worktree 基础操作

function Add-GitWorktree {
    <#
    .SYNOPSIS
        创建新的 worktree
    .PARAMETER Path
        worktree 路径
    .PARAMETER Branch
        分支名（可选，不指定则基于当前 HEAD）
    .PARAMETER Commit
        基于特定 commit 创建（detached HEAD）
    .PARAMETER Tag
        基于特定 tag 创建（detached HEAD）
    .EXAMPLE
        Add-GitWorktree -Path "../project-feature-a" -Branch "feature/a"
    #>
    [CmdletBinding(DefaultParameterSetName = "Branch")]
    param(
        [Parameter(Mandatory)]
        [string]$Path,
        [Parameter(ParameterSetName = "Branch")]
        [string]$Branch,
        [Parameter(ParameterSetName = "Commit")]
        [string]$Commit,
        [Parameter(ParameterSetName = "Tag")]
        [string]$Tag
    )

    Assert-GitAvailable

    $argsList = @("worktree", "add", $Path)

    # 确保路径是绝对路径
    if (-not [System.IO.Path]::IsPathRooted($Path)) {
        $repoRoot = Get-RepoRoot
        $parentDir = Split-Path $repoRoot -Parent
        $Path = Join-Path $parentDir $Path
        $argsList[-1] = $Path
    }

    if ($Branch) {
        $argsList += "-b"
        $argsList += $Branch
    } elseif ($Commit) {
        $argsList += $Commit
    } elseif ($Tag) {
        $argsList += $Tag
    }

    Write-Host "→ 创建 worktree: $Path" -ForegroundColor Cyan
    if ($Branch) { Write-Host "  分支: $Branch" -ForegroundColor Gray }
    if ($Commit) { Write-Host "  Commit: $Commit" -ForegroundColor Gray }
    if ($Tag) { Write-Host "  Tag: $Tag" -ForegroundColor Gray }

    git @argsList

    if ($LASTEXITCODE -eq 0) {
        Write-Host "✓ Worktree 已创建: $Path" -ForegroundColor Green
    }
}

function Get-GitWorktree {
    <#
    .SYNOPSIS
        列出所有 worktree
    #>
    [CmdletBinding()]
    param()

    Assert-GitAvailable
    $output = git worktree list

    Write-Host "Worktree 列表：" -ForegroundColor Cyan
    Write-Host "──────────────────────────────────────" -ForegroundColor Gray

    $output | ForEach-Object {
        Write-Host $_ -ForegroundColor White
    }

    return $output
}

function Remove-GitWorktree {
    <#
    .SYNOPSIS
        删除 worktree
    .PARAMETER Path
        worktree 路径
    .PARAMETER Force
        强制删除（即使有未提交的修改）
    #>
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)]
        [string]$Path,
        [switch]$Force
    )

    Assert-GitAvailable

    # 确保路径是绝对路径
    if (-not [System.IO.Path]::IsPathRooted($Path)) {
        $repoRoot = Get-RepoRoot
        $parentDir = Split-Path $repoRoot -Parent
        $Path = Join-Path $parentDir $Path
    }

    $argsList = @("worktree", "remove", $Path)
    if ($Force) { $argsList += "--force" }

    Write-Host "→ 删除 worktree: $Path" -ForegroundColor Yellow
    git @argsList

    if ($LASTEXITCODE -eq 0) {
        Write-Host "✓ Worktree 已删除" -ForegroundColor Green
    }
}

function Move-GitWorktree {
    <#
    .SYNOPSIS
        移动 worktree 到新路径
    .PARAMETER Source
        原路径
    .PARAMETER Destination
        新路径
    #>
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)]
        [string]$Source,
        [Parameter(Mandatory)]
        [string]$Destination
    )

    Assert-GitAvailable
    git worktree move $Source $Destination

    if ($LASTEXITCODE -eq 0) {
        Write-Host "✓ Worktree 已移动到: $Destination" -ForegroundColor Green
    }
}

function Clear-GitWorktree {
    <#
    .SYNOPSIS
        清理已删除 worktree 的残留记录（git worktree prune）
    .PARAMETER Expire
        过期时间，如 "1.day" 或 "2.weeks"
    #>
    [CmdletBinding()]
    param(
        [string]$Expire = "1.day"
    )

    Assert-GitAvailable
    Write-Host "→ 清理过期 worktree 记录（过期时间: $Expire）..." -ForegroundColor Cyan
    git worktree prune --expire $Expire
    Write-Host "✓ 完成" -ForegroundColor Green
}

#endregion

#region Plan 模式 SubAgent 集成

function Add-PlanSubAgentWorktree {
    <#
    .SYNOPSIS
        为 Plan 模式下的 SubAgent 创建隔离 worktree
    .DESCRIPTION
        为每个 Plan DAG 节点创建独立 worktree，确保 SubAgent 间物理隔离。
    .PARAMETER NodeId
        Plan DAG 节点 ID
    .PARAMETER BranchName
        分支名，默认 feat/<NodeId>
    .PARAMETER BaseBranch
        基于哪个分支创建，默认 main
    .PARAMETER WorktreeBaseDir
        worktree 基础目录，相对于仓库父目录
    .EXAMPLE
        Add-PlanSubAgentWorktree -NodeId "impl-core" -BranchName "feat/impl-core"
    #>
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)]
        [string]$NodeId,
        [string]$BranchName,
        [string]$BaseBranch = "main",
        [string]$WorktreeBaseDir
    )

    Assert-GitAvailable
    $repoRoot = Get-RepoRoot
    $parentDir = Split-Path $repoRoot -Parent

    if (-not $BranchName) {
        $BranchName = "feat/$NodeId"
    }

    if (-not $WorktreeBaseDir) {
        $worktreePath = Join-Path $parentDir "$(Split-Path $repoRoot -Leaf)-$NodeId"
    } else {
        $worktreePath = Join-Path $parentDir $WorktreeBaseDir
    }

    # 检查 worktree 是否已存在
    $existing = git worktree list 2>$null | Where-Object { $_ -match [regex]::Escape($worktreePath) }
    if ($existing) {
        Write-Host "ℹ Worktree 已存在: $worktreePath" -ForegroundColor Yellow
        return $worktreePath
    }

    Write-Host "═══════════════════════════════════════" -ForegroundColor Magenta
    Write-Host "  Plan SubAgent: $NodeId" -ForegroundColor Cyan
    Write-Host "  分支: $BranchName" -ForegroundColor Cyan
    Write-Host "  目录: $worktreePath" -ForegroundColor Cyan
    Write-Host "═══════════════════════════════════════" -ForegroundColor Magenta

    git fetch origin 2>$null
    git worktree add -b $BranchName $worktreePath "origin/$BaseBranch"

    if ($LASTEXITCODE -eq 0) {
        Write-Host "✓ Worktree 已就绪: $worktreePath [$BranchName]" -ForegroundColor Green
        return $worktreePath
    } else {
        Write-Error "创建 worktree 失败"
        return $null
    }
}

function Sync-PlanSubAgentWorktree {
    <#
    .SYNOPSIS
        同步 SubAgent worktree 到最新依赖
    .DESCRIPTION
        当 SubAgent B 依赖 SubAgent A 的产出时，将 B 的 worktree rebase 到 A 的最新代码。
    .PARAMETER WorktreePath
        worktree 路径
    .PARAMETER DepBranch
        依赖的远端分支名
    #>
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)]
        [string]$WorktreePath,
        [Parameter(Mandatory)]
        [string]$DepBranch
    )

    Assert-GitAvailable
    Write-Host "→ 同步 $WorktreePath 到 $DepBranch ..." -ForegroundColor Cyan

    Push-Location $WorktreePath
    try {
        git fetch origin
        git rebase "origin/$DepBranch"
        if ($LASTEXITCODE -eq 0) {
            Write-Host "✓ 同步完成" -ForegroundColor Green
        }
    } finally {
        Pop-Location
    }
}

function Merge-PlanSubAgentWorktree {
    <#
    .SYNOPSIS
        将 SubAgent worktree 的变更合并回主仓库
    .DESCRIPTION
        完成开发后，将 worktree 分支合并到 main，清理 worktree 和分支。
    .PARAMETER WorktreePath
        worktree 路径
    .PARAMETER BranchName
        分支名
    .PARAMETER TargetBranch
        目标分支，默认 main
    .PARAMETER DeleteAfterMerge
        合并后删除 worktree 和分支，默认 $true
    .PARAMETER Method
        合并方式：merge（默认）、squash、rebase
    #>
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)]
        [string]$WorktreePath,
        [Parameter(Mandatory)]
        [string]$BranchName,
        [string]$TargetBranch = "main",
        [switch]$DeleteAfterMerge = $true,
        [ValidateSet("merge", "squash", "rebase")]
        [string]$Method = "merge"
    )

    Assert-GitAvailable
    $repoRoot = Get-RepoRoot

    Write-Host "→ 合并 $BranchName 到 $TargetBranch ..." -ForegroundColor Cyan

    # 合并
    Push-Location $repoRoot
    try {
        git switch $TargetBranch
        git pull --rebase

        switch ($Method) {
            "merge" { git merge $BranchName }
            "squash" { git merge --squash $BranchName }
            "rebase" {
                git rebase $BranchName
            }
        }

        if ($LASTEXITCODE -eq 0) {
            Write-Host "✓ 合并完成" -ForegroundColor Green

            # 推送
            git push origin $TargetBranch

            # 清理
            if ($DeleteAfterMerge) {
                # 删除本地分支
                git branch -d $BranchName 2>$null
                # 删除远程分支
                git push origin --delete $BranchName 2>$null
                # 删除 worktree
                git worktree remove $WorktreePath 2>$null
                git worktree prune
                Write-Host "✓ 已清理分支 $BranchName 和 worktree" -ForegroundColor Green
            }

            # 切回原分支
            git switch - 2>$null
        }
    } finally {
        Pop-Location
    }
}

#endregion

#region 上下文切换

function Switch-GitTask {
    <#
    .SYNOPSIS
        上下文切换：为紧急任务创建临时 worktree
    .DESCRIPTION
        在不影响当前工作区的情况下，为紧急修复/审查创建独立 worktree。
    .PARAMETER Description
        任务描述
    .PARAMETER BranchType
        分支类型，默认 hotfix
    .PARAMETER BaseBranch
        基于哪个分支，默认 main
    #>
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)]
        [string]$Description,
        [string]$BranchType = "hotfix",
        [string]$BaseBranch = "main"
    )

    Assert-GitAvailable
    $repoRoot = Get-RepoRoot
    $parentDir = Split-Path $repoRoot -Parent
    $branchName = "$BranchType/$Description"
    $worktreeDir = "$(Split-Path $repoRoot -Leaf)-$Description"
    $worktreePath = Join-Path $parentDir $worktreeDir

    Write-Host "═══════════════════════════════════════" -ForegroundColor Magenta
    Write-Host "  紧急任务: $Description" -ForegroundColor Red
    Write-Host "  分支: $branchName" -ForegroundColor Cyan
    Write-Host "  目录: $worktreePath" -ForegroundColor Cyan
    Write-Host "═══════════════════════════════════════" -ForegroundColor Magenta

    git fetch origin
    git worktree add -b $branchName $worktreePath "origin/$BaseBranch"

    if ($LASTEXITCODE -eq 0) {
        Write-Host "✓ 已创建隔离 worktree: $worktreePath" -ForegroundColor Green
        Write-Host "  使用: cd $worktreePath" -ForegroundColor Yellow
        Write-Host "  完成合并: Merge-PlanSubAgentWorktree -WorktreePath '$worktreePath' -BranchName '$branchName'" -ForegroundColor Yellow
        return $worktreePath
    }
}

#endregion

#region 交互菜单

function Show-WorktreeMenu {
    <#
    .SYNOPSIS
        Worktree 管理交互菜单
    #>
    [CmdletBinding()]
    param()

    Assert-GitAvailable
    $branch = Get-CurrentBranch

    do {
        Clear-Host
        Write-Host @"
╔══════════════════════════════════════════╗
║     Git Worktree 隔离开发工具箱            ║
║     当前分支: $branch                 ║
╚══════════════════════════════════════════╝

"@ -ForegroundColor Cyan

        Write-Host "基础操作：" -ForegroundColor White
        Write-Host "1. 创建 worktree" -ForegroundColor White
        Write-Host "2. 列出 worktree" -ForegroundColor White
        Write-Host "3. 删除 worktree" -ForegroundColor White
        Write-Host "4. 移动 worktree" -ForegroundColor White
        Write-Host "5. 清理过期 worktree" -ForegroundColor White

        Write-Host "`nPlan 模式集成：" -ForegroundColor White
        Write-Host "6. 创建 SubAgent worktree" -ForegroundColor White
        Write-Host "7. 同步 SubAgent worktree" -ForegroundColor White
        Write-Host "8. 合并 SubAgent worktree" -ForegroundColor White

        Write-Host "`n上下文切换：" -ForegroundColor White
        Write-Host "9. 创建紧急任务 worktree" -ForegroundColor White
        Write-Host "Q. 退出" -ForegroundColor White

        $choice = Read-Host "`n请选择"
        switch ($choice) {
            "1" {
                $path = Read-Host "输入 worktree 路径"
                $br = Read-Host "输入分支名（留空=基于当前 HEAD）"
                Add-GitWorktree -Path $path -Branch $br
            }
            "2" { Get-GitWorktree }
            "3" { Remove-GitWorktree -Path (Read-Host "输入 worktree 路径") }
            "4" {
                $src = Read-Host "输入原路径"
                $dst = Read-Host "输入新路径"
                Move-GitWorktree -Source $src -Destination $dst
            }
            "5" { Clear-GitWorktree }
            "6" {
                $nid = Read-Host "输入 SubAgent 节点 ID"
                $br = Read-Host "输入分支名（留空=自动生成）"
                if ($br) { Add-PlanSubAgentWorktree -NodeId $nid -BranchName $br }
                else { Add-PlanSubAgentWorktree -NodeId $nid }
            }
            "7" {
                $wp = Read-Host "输入 worktree 路径"
                $dep = Read-Host "输入依赖分支名"
                Sync-PlanSubAgentWorktree -WorktreePath $wp -DepBranch $dep
            }
            "8" {
                $wp = Read-Host "输入 worktree 路径"
                $br = Read-Host "输入分支名"
                Merge-PlanSubAgentWorktree -WorktreePath $wp -BranchName $br
            }
            "9" {
                $desc = Read-Host "输入任务描述"
                Switch-GitTask -Description $desc
            }
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
    "Add-GitWorktree",
    "Get-GitWorktree",
    "Remove-GitWorktree",
    "Move-GitWorktree",
    "Clear-GitWorktree",
    "Add-PlanSubAgentWorktree",
    "Sync-PlanSubAgentWorktree",
    "Merge-PlanSubAgentWorktree",
    "Switch-GitTask",
    "Show-WorktreeMenu"
)
