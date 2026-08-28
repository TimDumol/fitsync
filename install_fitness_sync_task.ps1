[CmdletBinding()]
param(
    [ValidatePattern("^([01]\d|2[0-3]):[0-5]\d$")]
    [string]$At = "06:30",
    [ValidateRange(1, 30)]
    [int]$Days = 3,
    [string]$TaskName = "Garmin Fitness Sync"
)

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$runnerPath = Join-Path $projectRoot "run_fitness_sync.ps1"
$keyPath = Join-Path $projectRoot ".private\neon\api_key.clixml"

if (-not (Test-Path -LiteralPath $keyPath)) {
    throw "Neon key not configured. Run .\configure_neon.ps1 first."
}

$powerShellPath = (Get-Command powershell.exe).Source
$arguments = "-NoProfile -NonInteractive -ExecutionPolicy Bypass -File `"$runnerPath`" -Days $Days"
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
    -Description "Sync curated Garmin fitness data to Neon Postgres." `
    -Force | Out-Null

Write-Host "Scheduled '$TaskName' daily at $At for the current Windows user."
Write-Host "The task runs when that user is logged in and starts later if the computer was asleep."
