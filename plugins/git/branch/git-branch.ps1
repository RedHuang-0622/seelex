#Requires -Version 7.0
<#
.SYNOPSIS
    Git 分支策略与生命周期管理脚本
.DESCRIPTION
    提供分支创建（含命名规范校验）、分支同步、分支清理、commit 规范校验等功能。
    支持 Trunk-Based、GitHub Flow、Git Flow 三种分支模型。
.NOTES
    Module:    git-branch
    Author:    Seelex Git Plugin
#>

#region 命名规范定义

$script:ValidBranchTypes = @{
    "feature"    = "新功能开发"
    "fix"        = "Bug 修复"
    "hotfix"     = "紧急修复"
    "release"    = "发布准备"
    "experiment" = "实验性功能"
    "chore"      = "杂项/维护"
    "docs"       = "文档"
    "refactor"   = "重构"
    "test"       = "测试"
}

$script:ValidCommitTypes = @{
    "feat"     = "新功能"
    "fix"      = "修复"
    "refactor" = "重构"
    "docs"     = "文档"
    "chore"    = "杂项"
    "test"     = "测试"
    "revert"   = "回滚"
    "perf"     = "性能优化"
    "style"    = "代码风格"
    "ci"       = "CI/CD"
}

#endregion

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

#endregion

#region 分支创建

function New-GitBranch {
    <#
    .SYNOPSIS
        创建新分支，遵循命名规范
    .PARAMETER BranchType
        分支类型：feature/fix/hotfix/release/experiment/chore/docs/refactor/test
    .PARAMETER Description
        分支描述，用短横线连接的小写字母
    .PARAMETER BaseBranch
        基于哪个分支创建，默认 main
    .EXAMPLE
        New-GitBranch -BranchType feature -Description "user-auth"
        # 创建：feature/user-auth
    #>
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)]
        [ValidateScript({
            if (-not $script:ValidBranchTypes.ContainsKey($_)) {
                throw "无效的分支类型 '$_'。有效类型: $($script:ValidBranchTypes.Keys -join ', ')"
            }
            return $true
        })]
        [string]$BranchType,
        [Parameter(Mandatory)]
        [string]$Description,
        [string]$BaseBranch = "main"
    )

    Assert-GitAvailable

    # 校验描述格式
    if ($Description -match '\s') {
        Write-Warning "描述包含空格，将自动替换为短横线"
        $Description = $Description -replace '\s+', '-'
    }

    $branchName = "$BranchType/$Description"
    $fullBranch = "$BranchType/$Description"

    # 检查是否已存在
    $exists = git branch --list $fullBranch 2>$null
    if ($exists) {
        Write-Warning "分支 '$fullBranch' 已存在"
        return
    }

    Write-Host "→ 正在从 $BaseBranch 创建分支: $fullBranch" -ForegroundColor Green
    git switch $BaseBranch
    git pull --rebase
    git switch -c $fullBranch

    if ($LASTEXITCODE -eq 0) {
        Write-Host "✓ 已创建并切换到: $fullBranch" -ForegroundColor Green
        return $fullBranch
    }
}

function Copy-GitBranch {
    <#
    .SYNOPSIS
        从远端分支创建工作副本
    .PARAMETER RemoteBranch
        远端分支名，如 origin/feature/xxx
    #>
    [CmdletBinding()]
    param(
        [Parameter(Mandatory)]
        [string]$RemoteBranch
    )

    Assert-GitAvailable
    $localName = $RemoteBranch -replace '^origin/', ''
    git fetch origin
    git switch -c $localName $RemoteBranch
    Write-Host "✓ 已创建本地分支 $localName 跟踪 $RemoteBranch" -ForegroundColor Green
}

#endregion

#region 分支同步

function Sync-GitBranch {
    <#
    .SYNOPSIS
        将当前分支与 main 同步（rebase 或 merge 模式）
    .PARAMETER Method
        同步方式：rebase（默认）、merge
    #>
    [CmdletBinding()]
    param(
        [ValidateSet("rebase", "merge")]
        [string]$Method = "rebase"
    )

    Assert-GitAvailable
    $branch = Get-CurrentBranch

    if ($branch -eq "main") {
        Write-Host "已在 main 分支，直接 pull" -ForegroundColor Cyan
        git pull --rebase
        return
    }

    Write-Host "→ 正在同步 $branch 到最新 main ..." -ForegroundColor Cyan
    git fetch origin

    if ($Method -eq "rebase") {
        git rebase "origin/main"
    } else {
        git merge "origin/main"
    }

    if ($LASTEXITCODE -eq 0) {
        Write-Host "✓ 同步完成" -ForegroundColor Green
    } else {
        Write-Host "⚠️ 同步过程中出现问题，请手动解决冲突" -ForegroundColor Red
    }
}

#endregion

#region 分支清理

function Remove-GitBranch {
    <#
    .SYNOPSIS
        删除本地和远程分支
    .PARAMETER BranchName
        要删除的分支名，默认为当前分支
    .PARAMETER KeepRemote
        保留远程分支
    .PARAMETER Force
        强制删除
    #>
    [CmdletBinding()]
    param(
        [string]$BranchName = (Get-CurrentBranch),
        [switch]$KeepRemote,
        [switch]$Force
    )

    Assert-GitAvailable

    # 保护 main/develop 分支
    if ($BranchName -in @("main", "develop", "master")) {
        Write-Warning "禁止删除保护分支: $BranchName"
        return
    }

    # 切换到 main
    if ((Get-CurrentBranch) -eq $BranchName) {
        Write-Host "→ 切换到 main" -ForegroundColor Cyan
        git switch main
    }

    # 删除本地分支
    $localArgs = @("branch")
    if ($Force) { $localArgs += "-D" } else { $localArgs += "-d" }
    $localArgs += $BranchName
    git @localArgs

    if ($LASTEXITCODE -eq 0) {
        Write-Host "✓ 已删除本地分支: $BranchName" -ForegroundColor Green
    } else {
        Write-Host "本地分支删除失败（可能未合并或不存在）" -ForegroundColor Yellow
    }

    # 删除远程分支
    if (-not $KeepRemote) {
        $remoteExists = git ls-remote --heads origin $BranchName 2>$null
        if ($remoteExists) {
            git push origin --delete $BranchName
            Write-Host "✓ 已删除远程分支: origin/$BranchName" -ForegroundColor Green
        }
    }
}

function Clear-MergedBranches {
    <#
    .SYNOPSIS
        清理已合并到目标分支的本地分支
    .PARAMETER TargetBranch
        目标分支，默认 main
    .PARAMETER DryRun
        只列出不删除
    #>
    [CmdletBinding()]
    param(
        [string]$TargetBranch = "main",
        [switch]$DryRun
    )

    Assert-GitAvailable
    $merged = git branch --merged $TargetBranch `
        | ForEach-Object { $_.Trim() } `
        | Where-Object { $_ -notin @("main", "develop", "master", "*") -and $_ -ne (Get-CurrentBranch) }

    if (-not $merged) {
        Write-Host "没有已合并的可清理分支" -ForegroundColor Green
        return
    }

    Write-Host "以下分支已合并到 $TargetBranch，可清理：" -ForegroundColor Cyan
    $merged | ForEach-Object { Write-Host "  - $_" -ForegroundColor Yellow }

    if (-not $DryRun) {
        $confirm = Read-Host "确定删除以上分支？(y/N)"
        if ($confirm -eq 'y' -or $confirm -eq 'Y') {
            $merged | ForEach-Object {
                git branch -d $_
                Write-Host "  ✓ 已删除: $_" -ForegroundColor Green
            }
        }
    }
}

function Clear-RemoteStale {
    <#
    .SYNOPSIS
        清理远程已删除分支的本地跟踪引用
    #>
    [CmdletBinding()]
    param()

    Assert-GitAvailable
    Write-Host "→ 正在清理远端残留引用..." -ForegroundColor Cyan
    git remote prune origin
    Write-Host "✓ 完成" -ForegroundColor Green
}

#endregion

#region Commit 规范校验

function Test-GitCommitMessage {
    <#
    .SYNOPSIS
        校验 commit message 是否符合 Conventional Commits 规范
    .PARAMETER Message
        commit message 文本
    .EXAMPLE
        Test-GitCommitMessage -Message "feat(auth): 添加 OAuth2.0 登录支持"
        # 返回 $true
    #>
    [CmdletBinding()]
    param(
        [Parameter(Mandatory, ValueFromPipeline)]
        [string]$Message
    )

    process {
        $pattern = '^(feat|fix|refactor|docs|chore|test|revert|perf|style|ci)(\([a-z0-9_-]+\))?: .{1,}$'
        $headerPattern = '^[A-Z]+-\d+ .+$'  # JIRA 风格

        if ($Message -match $pattern) {
            $type = $Matches[1]
            $desc = $Message -replace '^[^(]+(\([^)]+\))?: ', ''
            Write-Host "✓ 有效格式 | 类型: $type | 描述: $desc" -ForegroundColor Green
            return $true
        }
        elseif ($Message -match $headerPattern) {
            Write-Host "✓ 有效格式 (JIRA 风格)" -ForegroundColor Green
            return $true
        }
        else {
            Write-Host "✗ 无效格式" -ForegroundColor Red
            Write-Host "  正确格式: <type>(<scope>): <description>" -ForegroundColor Yellow
            Write-Host "  类型: $($script:ValidCommitTypes.Keys -join '|')" -ForegroundColor Yellow
            return $false
        }
    }
}

function Set-GitCommitTemplate {
    <#
    .SYNOPSIS
        设置 git commit 模板以遵循规范
    #>
    [CmdletBinding()]
    param()

    $template = @"
# <type>(<scope>): <subject>
# |<---- 最多 50 字符 ---->|
#
# 类型: feat, fix, refactor, docs, chore, test, revert, perf, style, ci
# 示例: feat(auth): 添加 OAuth2.0 登录支持
#
# 正文（可选，72 字符换行）
# 解释为什么做这个改动
#
# Footer（可选）
# BREAKING CHANGE: <描述>
# Closes #123, #456
"@

    $templatePath = ".gitcommit_template"
    Set-Content -Path $templatePath -Value $template -Encoding UTF8
    git config commit.template $templatePath
    Write-Host "✓ 已设置 commit 模板: $templatePath" -ForegroundColor Green
}

#endregion

#region 分支状态

function Get-GitBranchStatus {
    <#
    .SYNOPSIS
        显示所有本地分支的详细状态
    #>
    [CmdletBinding()]
    param()

    Assert-GitAvailable
    $currentBranch = Get-CurrentBranch
    $branches = git branch | ForEach-Object { $_.Trim().TrimStart('*').Trim() }

    $results = foreach ($branch in $branches) {
        $isCurrent = $branch -eq $currentBranch
        $isMerged = [bool](git branch --merged main 2>$null | Where-Object { $_.Trim() -eq $branch })
        $hasRemote = [bool](git config "branch.$branch.remote" 2>$null)
        $unpushed = git rev-list --count "origin/$branch..$branch" 2>$null
        if ($LASTEXITCODE -ne 0) { $unpushed = "?" }

        [PSCustomObject]@{
            Branch     = $branch
            Current    = $isCurrent
            MergedToMain = $isMerged
            HasRemote  = $hasRemote
            Unpushed   = $unpushed
        }
    }

    $results | Format-Table -Property @(
        @{Label = "当前"; Expression = { if ($_.Current) { "→" } else { " " } }; Width = 3}
        @{Label = "分支名"; Expression = { $_.Branch }; Width = 30}
        @{Label = "已合并到 main"; Expression = { if ($_.MergedToMain) { "✓" } else { " " } }; Width = 5}
        @{Label = "远程"; Expression = { if ($_.HasRemote) { "☁" } else { " " } }; Width = 3}
        @{Label = "未推送"; Expression = { $_.Unpushed }; Width = 5}
    )
}

#endregion

#region 交互菜单

function Show-BranchMenu {
    <#
    .SYNOPSIS
        分支管理交互菜单
    #>
    [CmdletBinding()]
    param()

    Assert-GitAvailable
    $branch = Get-CurrentBranch

    do {
        Clear-Host
        Write-Host @"
╔══════════════════════════════════════════╗
║        Git 分支管理工具箱                  ║
║     当前分支: $branch                 ║
╚══════════════════════════════════════════╝

"@ -ForegroundColor Cyan

        Write-Host "1. 创建新分支" -ForegroundColor White
        Write-Host "2. 同步当前分支到 main" -ForegroundColor White
        Write-Host "3. 删除分支" -ForegroundColor White
        Write-Host "4. 清理已合并的分支" -ForegroundColor White
        Write-Host "5. 清理远程残留引用" -ForegroundColor White
        Write-Host "6. 查看分支状态" -ForegroundColor White
        Write-Host "7. 校验 commit 消息" -ForegroundColor White
        Write-Host "8. 设置 commit 模板" -ForegroundColor White
        Write-Host "Q. 退出" -ForegroundColor White

        $choice = Read-Host "`n请选择"
        switch ($choice) {
            "1" {
                Write-Host "`n分支类型：" -ForegroundColor Cyan
                foreach ($kv in $script:ValidBranchTypes.GetEnumerator()) {
                    Write-Host "  $($kv.Key) - $($kv.Value)" -ForegroundColor Gray
                }
                $bt = Read-Host "输入类型"
                $desc = Read-Host "输入描述（如 user-auth）"
                New-GitBranch -BranchType $bt -Description $desc
            }
            "2" { Sync-GitBranch }
            "3" { Remove-GitBranch -BranchName (Read-Host "输入分支名（留空=当前分支）") }
            "4" { Clear-MergedBranches }
            "5" { Clear-RemoteStale }
            "6" { Get-GitBranchStatus }
            "7" {
                $msg = Read-Host "输入 commit message"
                Test-GitCommitMessage -Message $msg
            }
            "8" { Set-GitCommitTemplate }
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
    "New-GitBranch",
    "Copy-GitBranch",
    "Sync-GitBranch",
    "Remove-GitBranch",
    "Clear-MergedBranches",
    "Clear-RemoteStale",
    "Test-GitCommitMessage",
    "Set-GitCommitTemplate",
    "Get-GitBranchStatus",
    "Show-BranchMenu"
)
