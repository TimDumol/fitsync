[CmdletBinding()]
param(
    [ValidatePattern("^([01]\d|2[0-3]):[0-5]\d$")]
    [string]$At = "06:30",
    [ValidateRange(1, 30)]
    [int]$Days = 3,
    [string]$TaskName = "Garmin Fitnessd Sync",
    [switch]$DownloadOriginals,
    [string]$ApiUrl = "https://api.aries.timdumol.com"
)

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$runnerPath = Join-Path $projectRoot "run_fitnessd_sync.ps1"
$tokenPath = Join-Path $projectRoot ".private\fitnessd\ingest_token.clixml"

if (-not (Test-Path -LiteralPath $tokenPath)) {
    throw "Fitnessd token not configured. Run .\configure_fitnessd.ps1 first."
}

$powerShellPath = (Get-Command powershell.exe).Source
$arguments = "-NoProfile -NonInteractive -ExecutionPolicy Bypass -File `"$runnerPath`" -Days $Days -ApiUrl `"$ApiUrl`""
if ($DownloadOriginals) {
    $arguments += " -DownloadOriginals"
}
$action = New-ScheduledTaskAction -Execute $powerShellPath -Argument $arguments -WorkingDirectory $projectRoot
$trigger = New-ScheduledTaskTrigger -Daily -At $At
$settings = New-ScheduledTaskSettingsSet `
    -StartWhenAvailable `
    -AllowStartIfOnBatteries `
    -DontStopIfGoingOnBatteries `
    -ExecutionTimeLimit (New-TimeSpan -Hours 1)

Register-ScheduledTask `
    -TaskName $TaskName `
    -Action $action `
    -Trigger $trigger `
    -Settings $settings `
    -Description "Sync raw Garmin fitness data to the Fitnessd SQLite server." `
    -Force | Out-Null

Write-Host "Scheduled '$TaskName' daily at $At for the current Windows user."
Write-Host "The task runs when that user is logged in and starts later if the computer was asleep."
