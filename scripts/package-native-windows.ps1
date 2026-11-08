param([string]$App = 'bin/native/windows-amd64', [string]$Output = 'dist/native-windows-amd64', [switch]$Release)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$root = Split-Path $PSScriptRoot -Parent
Set-Location $root
$appPath = [IO.Path]::GetFullPath($App)
$outPath = [IO.Path]::GetFullPath($Output)
$config = Get-Content mygo.json -Raw | ConvertFrom-Json
& go run ./scripts/package-native-windows -app $appPath
if ($LASTEXITCODE) { throw 'NSH Windows payload validation failed' }
New-Item $outPath -ItemType Directory -Force | Out-Null
$compiler = @('C:/Program Files (x86)/NSIS/makensis.exe','C:/Program Files/NSIS/makensis.exe',"$env:LOCALAPPDATA/mygo/nsis-3.13/Bin/makensis.exe","$env:LOCALAPPDATA/mygo/nsis-3.13/makensis.exe") | Where-Object { Test-Path $_ } | Select-Object -First 1
if (-not $compiler) {
    $cache = "$root/.cache/nsis"
    New-Item $cache -ItemType Directory -Force | Out-Null
    & gh release download nsis-3.13 --repo egoist/mygo --pattern nsis-3.13.zip --output "$cache/nsis-3.13.zip" --clobber
    if ($LASTEXITCODE) { throw 'Download pinned NSIS failed' }
    if ((Get-FileHash "$cache/nsis-3.13.zip" -Algorithm SHA256).Hash.ToLowerInvariant() -ne 'ba63dffc4410ee89193e1cb5a41989991bd77c61068da17e3156d136b7b0b3d8') { throw 'NSIS checksum mismatch' }
    Expand-Archive "$cache/nsis-3.13.zip" $cache -Force
    $compiler = (Get-ChildItem $cache -Filter makensis.exe -Recurse | Select-Object -First 1).FullName
}
$installer = "$outPath/gopurr-setup-$($config.version)-amd64.exe"
if (Test-Path $installer) { throw "Refusing to overwrite installer: $installer" }
& $compiler /V2 "/DPAYLOAD=$appPath" "/DOUTPUT=$installer" "/DVERSION=$($config.version)" "/DCHECK_SCRIPT=$root/native/windows/check-installer-processes.ps1" "$root/native/windows/installer.nsi"
if ($LASTEXITCODE) { throw 'Build NSH Windows installer failed' }
if ($Release) {
    if (-not $env:MYGO_UPDATER_PRIVATE_KEY) { throw 'Signing requires MYGO_UPDATER_PRIVATE_KEY' }
    & go run ./scripts/package-native-windows -app $appPath -out $outPath -sign -key-env MYGO_UPDATER_PRIVATE_KEY -notes docs/release-notes.md
    if ($LASTEXITCODE) { throw 'Sign NSH Windows update failed' }
}
Write-Host "Packaged NSH Windows installer: $installer"
