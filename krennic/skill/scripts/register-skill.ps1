# Zaregistruje Krennic skill pro Claude Code (Windows).
# Vytvoří junction %USERPROFILE%\.claude\skills\krennic -> tato složka krennic\skill.
# Junction nepotřebuje práva správce.
$ErrorActionPreference = "Stop"

$SkillDir = Resolve-Path (Join-Path $PSScriptRoot "..")
$SkillsRoot = Join-Path $env:USERPROFILE ".claude\skills"
$Dest = Join-Path $SkillsRoot "krennic"

New-Item -ItemType Directory -Force -Path $SkillsRoot | Out-Null
if (Test-Path $Dest) { Remove-Item $Dest -Recurse -Force }
New-Item -ItemType Junction -Path $Dest -Target $SkillDir | Out-Null

Write-Host "✓ Skill zaregistrován:"
Write-Host "    $Dest  ->  $SkillDir"
Write-Host ""
Write-Host "Teď v Claude Code funguje /krennic — nebo mu řekni: `"nainstaluj a spusť krennic`"."
