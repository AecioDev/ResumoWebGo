param(
    [switch]$OnlyBuild,
    [switch]$ConsoleMode
)

$ErrorActionPreference = "Stop"

$ProjectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$DistDir = Join-Path $ProjectRoot "dist"
$ExeName = "ResumoPlanilhas.exe"
$ExePath = Join-Path $DistDir $ExeName
$IconPath = Join-Path $ProjectRoot "icone.ico"
$SysoPath = Join-Path $ProjectRoot "cmd\resumoplanilhas\rsrc_windows_amd64.syso"
$FaviconPath = Join-Path $ProjectRoot "internal\server\web\favicon.ico"

New-Item -ItemType Directory -Force -Path $DistDir | Out-Null

$LdFlags = if ($ConsoleMode) { "-s -w" } else { "-s -w -H=windowsgui" }

Push-Location $ProjectRoot
try {
    if (Test-Path $IconPath) {
        Write-Host "Embutindo ícone: $IconPath" -ForegroundColor Cyan
        go run ./cmd/mkicon -in $IconPath -out $SysoPath -favicon $FaviconPath
    } elseif (Test-Path $SysoPath) {
        Remove-Item -Force $SysoPath
    }

    Write-Host "Compilando $ExeName..." -ForegroundColor Cyan
    $env:CGO_ENABLED = "0"
    go build -trimpath -ldflags $LdFlags -o $ExePath ./cmd/resumoplanilhas
} finally {
    Pop-Location
}

Write-Host "Build concluído: $ExePath" -ForegroundColor Green

if ($OnlyBuild) {
    exit 0
}

$InstallDir = Join-Path $env:LOCALAPPDATA "ResumoPlanilhas"
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null

$InstalledExe = Join-Path $InstallDir $ExeName
Copy-Item -Force $ExePath $InstalledExe

$InstalledIcon = Join-Path $InstallDir "icone.ico"
$HasIcon = $false
if (Test-Path $IconPath) {
    Copy-Item -Force $IconPath $InstalledIcon
    $HasIcon = $true
} elseif (Test-Path $InstalledIcon) {
    $HasIcon = $true
}

# Cria atalho na Área de Trabalho do usuário já com o ícone
$DesktopDir = [Environment]::GetFolderPath("Desktop")
$ShortcutPath = Join-Path $DesktopDir "Resumo de Planilhas.lnk"

$WScriptShell = New-Object -ComObject WScript.Shell
$Shortcut = $WScriptShell.CreateShortcut($ShortcutPath)
$Shortcut.TargetPath = $InstalledExe
$Shortcut.WorkingDirectory = $InstallDir
$Shortcut.Description = "Painel de Resumo de Planilhas (Google Drive)"
if ($HasIcon) {
    $Shortcut.IconLocation = "$InstalledIcon,0"
} else {
    $Shortcut.IconLocation = "$InstalledExe,0"
}
$Shortcut.Save()

Write-Host "Instalado em: $InstalledExe" -ForegroundColor Green
if ($HasIcon) {
    Write-Host "Ícone aplicado: $InstalledIcon" -ForegroundColor Green
}
Write-Host "Atalho criado em: $ShortcutPath" -ForegroundColor Green
Write-Host "Configurações e cache ficam em: $(Join-Path $env:APPDATA 'ResumoPlanilhas')" -ForegroundColor Yellow
