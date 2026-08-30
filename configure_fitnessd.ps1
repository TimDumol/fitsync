[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$privateDirectory = Join-Path $projectRoot ".private\fitnessd"
$tokenPath = Join-Path $privateDirectory "ingest_token.clixml"

New-Item -ItemType Directory -Path $privateDirectory -Force | Out-Null
$secureToken = Read-Host "Paste the Fitnessd ingest token for tim" -AsSecureString
if ($secureToken.Length -eq 0) {
    throw "No ingest token was entered."
}

# On Windows, Export-Clixml protects SecureString values with DPAPI. The file
# can only be decrypted by this Windows user on this computer.
$secureToken | Export-Clixml -LiteralPath $tokenPath
Write-Host "Saved the Fitnessd token for this Windows user at $tokenPath"
Write-Host "The token is under .private and is excluded from version control."
