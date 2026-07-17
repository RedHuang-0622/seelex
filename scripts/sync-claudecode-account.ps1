param(
    [string]$ClaudeSettingsPath = "$HOME\.claude\settings.json",
    [string]$OutputPath = "config\account-claudecode.local.yaml",
    [string]$AccountName = "claudecode-minimax"
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path -LiteralPath $ClaudeSettingsPath)) {
    throw "Claude Code settings not found: $ClaudeSettingsPath"
}

$settings = Get-Content -Raw -LiteralPath $ClaudeSettingsPath | ConvertFrom-Json
$sourceBaseUrl = [string]$settings.env.ANTHROPIC_BASE_URL
$token = [string]$settings.env.ANTHROPIC_AUTH_TOKEN
$sourceModel = [string]$settings.env.ANTHROPIC_MODEL

if ([string]::IsNullOrWhiteSpace($token)) {
    throw "ANTHROPIC_AUTH_TOKEN is missing from Claude Code settings"
}

if ($sourceBaseUrl -match '^https://api\.minimaxi\.com/anthropic/?$') {
    $openAIBaseUrl = "https://api.minimaxi.com/v1"
} else {
    throw "Unsupported Claude Code endpoint for automatic OpenAI conversion: $sourceBaseUrl"
}

if ($sourceModel -match '^minimax-m3$') {
    $openAIModel = "MiniMax-M3"
} elseif ([string]::IsNullOrWhiteSpace($sourceModel)) {
    $openAIModel = "MiniMax-M3"
} else {
    $openAIModel = $sourceModel
}

function ConvertTo-YamlSingleQuoted([string]$Value) {
    return "'" + $Value.Replace("'", "''") + "'"
}

$yaml = @"
# Generated from Claude Code settings. This file is local-only and gitignored.
defaults:
  provider: openai
  max_tokens: 8192
  timeout: 120s
  temperature: 0

accounts:
  - name: $(ConvertTo-YamlSingleQuoted $AccountName)
    provider: openai
    model: $(ConvertTo-YamlSingleQuoted $openAIModel)
    base_url: $(ConvertTo-YamlSingleQuoted $openAIBaseUrl)
    api_key: $(ConvertTo-YamlSingleQuoted $token)
"@

$resolvedOutput = [IO.Path]::GetFullPath((Join-Path (Get-Location) $OutputPath))
$outputDirectory = Split-Path -Parent $resolvedOutput
New-Item -ItemType Directory -Path $outputDirectory -Force | Out-Null
[IO.File]::WriteAllText($resolvedOutput, $yaml, [Text.UTF8Encoding]::new($false))

Write-Output "Created local account pool: $resolvedOutput"
Write-Output "Account: $AccountName"
Write-Output "Provider: openai"
Write-Output "Model: $openAIModel"
Write-Output "Base URL: $openAIBaseUrl"
