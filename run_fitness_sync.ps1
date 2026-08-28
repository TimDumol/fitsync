[CmdletBinding()]
param(
    [ValidateRange(1, 30)]
    [int]$Days = 3,

    [switch]$NeonOnly
)

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$pythonPath = Join-Path $projectRoot ".venv\Scripts\python.exe"
$syncPath = Join-Path $projectRoot "garmin_sync.py"
$keyPath = Join-Path $projectRoot ".private\neon\api_key.clixml"
$logDirectory = Join-Path $projectRoot ".private\logs"
$logPath = Join-Path $logDirectory "fitness-sync.log"

if (-not (Test-Path -LiteralPath $pythonPath)) {
    throw "Python environment not found at $pythonPath"
}
if (-not (Test-Path -LiteralPath $keyPath)) {
    throw "Neon key not configured. Run .\configure_neon.ps1 first."
}

New-Item -ItemType Directory -Path $logDirectory -Force | Out-Null
$secureKey = Import-Clixml -LiteralPath $keyPath
$credential = [System.Management.Automation.PSCredential]::new("neon", $secureKey)

$env:NEON_API_KEY = $credential.GetNetworkCredential().Password
$env:NEON_PROJECT_ID = "dark-scene-81288077"
try {
    Add-Content -LiteralPath $logPath -Value "`n[$(Get-Date -Format o)] Starting fitness sync"
    if ($NeonOnly) {
        $syncArguments = @($syncPath, "--neon-only")
    }
    else {
        $syncArguments = @($syncPath, "--days", $Days, "--extended", "--neon")
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
        throw "Fitness sync exited with code $syncExitCode. See $logPath"
    }
}
finally {
    Remove-Item Env:NEON_API_KEY -ErrorAction SilentlyContinue
    Remove-Item Env:NEON_PROJECT_ID -ErrorAction SilentlyContinue
    $credential = $null
    $secureKey = $null
}

Write-Host "Fitness sync completed. Log: $logPath"
