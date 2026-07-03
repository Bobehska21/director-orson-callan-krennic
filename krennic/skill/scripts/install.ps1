# Krennic installer for Windows. Installs the binary to %LOCALAPPDATA%\Krennic
# and registers a Windows service via SCM running as the logged-in user (needed
# for Credential Manager/DPAPI access). Run in an elevated PowerShell for sc.exe.
$ErrorActionPreference = "Stop"

$RepoRoot = Resolve-Path (Join-Path $PSScriptRoot "..\..")
$InstallDir = Join-Path $env:LOCALAPPDATA "Krennic"
$Bin = Join-Path $InstallDir "krennic.exe"

Write-Host "== Krennic install (windows) =="
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

$Prebuilt = Join-Path $RepoRoot "dist\krennic-windows-amd64.exe"
if (Test-Path $Prebuilt) {
    Copy-Item $Prebuilt $Bin -Force
    Write-Host "Použita předsestavená binárka."
} elseif (Get-Command go -ErrorAction SilentlyContinue) {
    Write-Host "Sestavuji ze zdrojů..."
    Push-Location $RepoRoot
    & go build -o $Bin ./cmd/krennic
    Pop-Location
} else {
    throw "Nenalezena binárka ani Go toolchain. Sestav 'make build' a zkopíruj dist\."
}
Write-Host "Nainstalováno: $Bin"

# Register a Windows service that runs `krennic run`.
$svc = "Krennic"
sc.exe stop $svc 2>$null | Out-Null
sc.exe delete $svc 2>$null | Out-Null
sc.exe create $svc binPath= "`"$Bin`" run" start= auto DisplayName= "Krennic AI code-review agent" | Out-Null
sc.exe description $svc "Zachytává změny, publikuje shadow ref a spouští AI review." | Out-Null
sc.exe start $svc | Out-Null

Write-Host ""
Write-Host "Dále:"
Write-Host "  $Bin init-config"
Write-Host "  $Bin keys set anthropic"
Write-Host "  $Bin doctor"
Write-Host "  Dashboard: http://127.0.0.1:7373"
