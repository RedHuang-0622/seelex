# ============================================================================
# Seelex canonical build layout - SINGLE SOURCE OF TRUTH (PowerShell side).
# ----------------------------------------------------------------------------
# Every PowerShell build script MUST obtain artifact paths from Get-SeelexLayout
# and MUST NOT hardcode its own output directories. This file mirrors the
# partition table in .claude/build-convention.md; the Makefile, build.sh,
# build-dev.sh and .github/workflows/release.yml mirror the same table.
# Consistency guard (must stay green):
#   go test . -run 'BuildLayout' -count=1
#   make guard-dist-layout
# ============================================================================

$script:SeelexLayoutRoot = Split-Path -Parent $PSScriptRoot

function Get-SeelexLayout {
    # Returns a PSCustomObject with every canonical partition path.
    #
    # dist/  (external artifacts; only these partitions are allowed)
    #   <os>-<arch>/            P1 platform release tree (CLI + runtime files)
    #   seelex-gui-dev/         P2 dev GUI baseline (USER DATA - never cleaned)
    #   archive/                P3 versioned release archives (*.zip|*.tar.gz|*.sha256)
    #   dev/                    P4 post-commit quick builds
    #   stage-gui/              P5 GUI staging area (flow Stage output; the
    #                           single "pending release" exe placement area)
    # tmp/build/                (pipeline intermediate state; no user data)
    #   smoke/                  T1 smoke reports
    #   stash/seelex-gui-dev/   T2 rollback stash
    #   deploy.log              T3 deploy log
    $distRoot   = Join-Path $script:SeelexLayoutRoot "dist"
    $tmpBuild   = Join-Path $script:SeelexLayoutRoot "tmp\build"
    return [PSCustomObject]@{
        DistRoot          = $distRoot
        # P1 platform tree base: dist/<os>-<arch>/ (caller appends platform dir)
        # P2 dev GUI baseline (user data; kept by every clean by default)
        DevBaselineDir    = Join-Path $distRoot "seelex-gui-dev"
        DevBaselineExe    = Join-Path $distRoot "seelex-gui-dev\seelex-gui.exe"
        # P3 release archive partition (never write archives flat into dist/)
        ArchiveRoot       = Join-Path $distRoot "archive"
        # P4 local quick-build partition (post-commit hook)
        DevQuickDir       = Join-Path $distRoot "dev"
        DevQuickCliExe    = Join-Path $distRoot "dev\seelex.exe"
        # P5 GUI staging (flow Stage; pending-release exe placement area)
        StageDir          = Join-Path $distRoot "stage-gui"
        StageExe          = Join-Path $distRoot "stage-gui\seelex-gui.exe"
        StageVersionFile  = Join-Path $distRoot "stage-gui\version.txt"
        # T1 smoke reports (flow Smoke)
        SmokeDir          = Join-Path $tmpBuild "smoke"
        # T2 rollback stash (flow Deploy/Rollback)
        StashDir          = Join-Path $tmpBuild "stash\seelex-gui-dev"
        StashPrevious     = Join-Path $tmpBuild "stash\seelex-gui-dev\seelex-gui.previous.exe"
        # T3 deploy log (flow Deploy/Rollback)
        DeployLog         = Join-Path $tmpBuild "deploy.log"
        # Everything allowed directly under dist/ (anything else is drift).
        # ".seelex" is gitignored runtime/session data created when a dev binary
        # is run with dist/ as its working directory - allowed but never written
        # by build scripts.
        AllowedDistEntries = @(
            "archive", "seelex-gui-dev", "dev", "stage-gui", ".seelex",
            "windows-amd64", "linux-amd64", "darwin-amd64", "darwin-arm64"
        )
    }
}

function Assert-DistRootCleanLayout {
    # Fails when an unexpected entry exists directly under dist/. Guarantees the
    # root stays limited to the canonical partitions (P1..P5).
    param(
        [string]$DistRoot,
        [string[]]$ExtraAllowed = @()
    )
    if (-not (Test-Path -LiteralPath $DistRoot -PathType Container)) { return }
    $layout = Get-SeelexLayout
    $allowed = @($layout.AllowedDistEntries) + @($ExtraAllowed)
    foreach ($entry in Get-ChildItem -LiteralPath $DistRoot -Force) {
        if ($allowed -notcontains $entry.Name) {
            throw ("unexpected entry under dist/ (layout drift): {0}" -f $entry.FullName)
        }
    }
}
