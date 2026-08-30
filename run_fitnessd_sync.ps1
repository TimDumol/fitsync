[CmdletBinding()]
param(
    [ValidateRange(1, 30)]
    [int]$Days = 3,

    [switch]$ExistingOnly,

    [switch]$DownloadOriginals,

    [string]$ApiUrl = "https://api.aries.timdumol.com"
)

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$pythonPath = Join-Path $projectRoot ".venv\Scripts\python.exe"
$syncPath = Join-Path $projectRoot "garmin_sync.py"
$tokenPath = Join-Path $projectRoot ".private\fitnessd\ingest_token.clixml"
$logDirectory = Join-Path $projectRoot ".private\logs"
$logPath = Join-Path $logDirectory "fitnessd-sync.log"

if (-not (Test-Path -LiteralPath $pythonPath)) {
    throw "Python environment not found at $pythonPath"
}
if (-not (Test-Path -LiteralPath $tokenPath)) {
    throw "Fitnessd token not configured. Run .\configure_fitnessd.ps1 first."
}

New-Item -ItemType Directory -Path $logDirectory -Force | Out-Null
$secureToken = Import-Clixml -LiteralPath $tokenPath
$credential = [System.Management.Automation.PSCredential]::new("fitnessd", $secureToken)

$env:FITNESS_API_URL = $ApiUrl
$env:FITNESS_INGEST_TOKEN = $credential.GetNetworkCredential().Password
try {
    Add-Content -LiteralPath $logPath -Value "`n[$(Get-Date -Format o)] Starting Fitnessd sync"
    if ($ExistingOnly) {
        $syncArguments = @($syncPath, "--fitnessd-only")
    }
    else {
        $syncArguments = @($syncPath, "--days", $Days, "--extended", "--fitnessd")
        if ($DownloadOriginals) {
            $syncArguments += "--download-originals"
        }
    }

    $previousErrorActionPreference = $ErrorActionPreference
    try {
        # Native stderr should be logged and handled through the process exit
        # code, rather than converted into a noisy PowerShell RemoteException.
        $ErrorActionPreference = "Continue"
        & $pythonPath @syncArguments 2>&1 |
            Tee-Object -FilePath $logPath -Append
        $syncExitCode = $LASTEXITCODE
    }
    finally {
        $ErrorActionPreference = $previousErrorActionPreference
    }
    if ($syncExitCode -ne 0) {
        throw "Fitnessd sync exited with code $syncExitCode. See $logPath"
    }
}
finally {
    Remove-Item Env:FITNESS_API_URL -ErrorAction SilentlyContinue
    Remove-Item Env:FITNESS_INGEST_TOKEN -ErrorAction SilentlyContinue
    $credential = $null
    $secureToken = $null
}

Write-Host "Fitnessd sync completed. Log: $logPath"
