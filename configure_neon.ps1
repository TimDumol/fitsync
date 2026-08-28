[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$privateDirectory = Join-Path $projectRoot ".private\neon"
$keyPath = Join-Path $privateDirectory "api_key.clixml"

New-Item -ItemType Directory -Path $privateDirectory -Force | Out-Null
$secureKey = Read-Host "Paste your project-scoped Neon API key" -AsSecureString
if ($secureKey.Length -eq 0) {
    throw "No API key was entered."
}

# On Windows, Export-Clixml protects SecureString values with DPAPI. The file
# can only be decrypted by this Windows user on this computer.
$secureKey | Export-Clixml -LiteralPath $keyPath
Write-Host "Saved the Neon key for this Windows user at $keyPath"
Write-Host "The key is under .private and is excluded from version control."
