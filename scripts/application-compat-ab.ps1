[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$BaselineRoot,

    [Parameter(Mandatory = $true)]
    [string]$CandidateRoot,

    [string]$ReportRoot = (Join-Path $env:TEMP "seelex-application-compat-ab")
)

$ErrorActionPreference = "Stop"

$baseline = (Resolve-Path -LiteralPath $BaselineRoot).Path
$candidate = (Resolve-Path -LiteralPath $CandidateRoot).Path
if (-not (Test-Path -LiteralPath (Join-Path $baseline "go.mod") -PathType Leaf)) {
    throw "BaselineRoot is not the Seelex repository: $baseline"
}
if (-not (Test-Path -LiteralPath (Join-Path $candidate "go.mod") -PathType Leaf)) {
    throw "CandidateRoot is not the Seelex repository: $candidate"
}

New-Item -ItemType Directory -Path $ReportRoot -Force | Out-Null

$commands = @(
    @("go", "test", "./application/...", "-count=1", "-timeout=180s"),
    @("go", "test", "./gui", "./tui", "./headless", "./console", "./wiring/...", "-count=1", "-timeout=180s"),
    @("go", "build", "./...")
)

function Invoke-Arm {
    param(
        [string]$Name,
        [string]$Root
    )

    $captured = New-Object System.Collections.Generic.List[string]
    $captured.Add("root=$Root")
    $captured.Add("commit=$((git -C $Root rev-parse HEAD).Trim())")
    $failed = $false
    foreach ($command in $commands) {
        $started = Get-Date
        $display = $command -join " "
        $captured.Add("")
        $captured.Add("$ $display")
        Push-Location -LiteralPath $Root
        try {
            # go 在编译错误时把诊断写到 stderr；Stop 会把 stderr 记录升级成终止性
            # NativeCommandError，导致整轮在落盘报告前就中断（失败臂反而没有证据）。
            # 命令成败只由 $LASTEXITCODE 判定，因此这里临时降回 Continue。
            $preference = $ErrorActionPreference
            $ErrorActionPreference = "Continue"
            try {
                $output = & $command[0] $command[1..($command.Count - 1)] 2>&1
                $exitCode = $LASTEXITCODE
            } finally {
                $ErrorActionPreference = $preference
            }
        } finally {
            Pop-Location
        }
        $elapsed = [Math]::Round(((Get-Date) - $started).TotalSeconds, 2)
        $captured.Add("exit=$exitCode elapsed_seconds=$elapsed")
        if ($output) {
            $captured.AddRange([string[]]$output)
        }
        if ($exitCode -ne 0) {
            $failed = $true
        }
    }
    $path = Join-Path $ReportRoot "$Name.log"
    [System.IO.File]::WriteAllLines($path, $captured)
    return [pscustomobject]@{ Name = $Name; Failed = $failed; Report = $path }
}

$baselineResult = Invoke-Arm -Name "baseline" -Root $baseline
$candidateResult = Invoke-Arm -Name "candidate" -Root $candidate

[pscustomobject]@{
    baseline = $baselineResult
    candidate = $candidateResult
    equivalent_pass = (-not $baselineResult.Failed) -and (-not $candidateResult.Failed)
} | ConvertTo-Json -Depth 4

if ($baselineResult.Failed -or $candidateResult.Failed) {
    exit 1
}
