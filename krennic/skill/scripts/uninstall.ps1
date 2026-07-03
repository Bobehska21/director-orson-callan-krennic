# Removes the Krennic Windows service and binary. Keychain (Credential Manager)
# secrets are left intact — remove them with `krennic keys del <name>`.
$ErrorActionPreference = "SilentlyContinue"

$InstallDir = Join-Path $env:LOCALAPPDATA "Krennic"
$Bin = Join-Path $InstallDir "krennic.exe"

Write-Host "== Krennic uninstall (windows) =="
sc.exe stop Krennic | Out-Null
sc.exe delete Krennic | Out-Null
Remove-Item $Bin -Force
Write-Host "Odinstalováno. Tajemství v Credential Manageru zůstala."
