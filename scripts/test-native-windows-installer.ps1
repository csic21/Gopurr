param([string]$Installer = '')
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$root = Split-Path $PSScriptRoot -Parent
Set-Location $root
$config = Get-Content mygo.json -Raw | ConvertFrom-Json
if (-not $Installer) { $Installer = "dist/native-windows-amd64/gopurr-setup-$($config.version)-amd64.exe" }
$work = [IO.Path]::GetFullPath((Join-Path $env:TEMP ('gp-install-' + [Guid]::NewGuid().ToString('N'))))
$install = Join-Path $work 'application'
$fixtureAppData = Join-Path $work 'appdata'
$fixtureHome = Join-Path $fixtureAppData 'Gopurr'
$session = Join-Path $work 'session.json'
$previousPath = $env:PATH
$previousAppData = $env:APPDATA
$previousSessionOutput = $env:GOPURR_NATIVE_SESSION_OUTPUT
$started = $null
try {
    New-Item $fixtureHome -ItemType Directory -Force | Out-Null
    $oldProfile = 'mode: rule' + "`n" + 'rules: ["MATCH,DIRECT"]' + "`n"
    [IO.File]::WriteAllText((Join-Path $fixtureHome 'local.yaml'), $oldProfile)
    [IO.File]::WriteAllText((Join-Path $fixtureHome 'prefs.json'), '{"appearance":"dark","language":"en","systemProxyStartup":"off","restoreSystemProxy":false,"checkAppUpdates":false,"autoUpdateHours":0}')
    $fixturePort = 0
    for ($attempt = 0; $attempt -lt 100; $attempt++) {
        $candidate = Get-Random -Minimum 10000 -Maximum 40000
        $tcp = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, $candidate)
        $udp = $null
        try {
            $tcp.Start()
            $udp = [Net.Sockets.UdpClient]::new([Net.IPEndPoint]::new([Net.IPAddress]::Loopback, $candidate))
            $fixturePort = $candidate
            break
        } catch { } finally { $tcp.Stop(); if ($udp) { $udp.Dispose() } }
    }
    if (-not $fixturePort) { throw 'Could not reserve a fixture TCP/UDP port' }
    [IO.File]::WriteAllText((Join-Path $fixtureHome 'overrides.json'), (@{mixedPort=$fixturePort;tunEnable=$false} | ConvertTo-Json -Compress))
    $env:APPDATA = $fixtureAppData
    $env:GOPURR_NATIVE_SESSION_OUTPUT = $session
    $process = Start-Process ([IO.Path]::GetFullPath($Installer)) -ArgumentList @('/S', "/D=$install") -PassThru -Wait
    if ($process.ExitCode -ne 0) { throw "Installer failed: $($process.ExitCode)" }
    foreach ($file in @('Gopurr.exe','gopurr-desktop.exe','nsh/bundle.nshc','Uninstall.exe')) {
        if (-not (Test-Path (Join-Path $install $file))) { throw "Incomplete installed NSH payload: $file" }
    }
    $entry = Get-ItemProperty 'HKCU:/Software/Microsoft/Windows/CurrentVersion/Uninstall/io.github.gopurr.app'
    if ($entry.DisplayVersion -ne $config.version -or $entry.InstallLocation -ne $install) { throw 'Installer changed the established uninstall identity' }
    foreach ($scheme in @('gopurr','clash')) {
        $command = (Get-Item "HKCU:/Software/Classes/$scheme/shell/open/command").GetValue('')
        $expectedExecutable = [IO.Path]::GetFullPath((Join-Path $install 'Gopurr.exe'))
        if ($command -notmatch '^"([^"\r\n]+)"\s+"%1"$' -or -not [IO.Path]::GetFullPath($Matches[1]).Equals($expectedExecutable, [StringComparison]::OrdinalIgnoreCase)) {
            throw "Invalid NSH protocol command for ${scheme}: $command (expected $expectedExecutable)"
        }
    }
    # Runtime may use only the OS DLLs and its complete installed payload.
    # A development toolchain on the runner must not satisfy a missing DLL.
    $env:PATH = Join-Path $env:SystemRoot 'System32'
    $started = Start-Process (Join-Path $install 'Gopurr.exe') -RedirectStandardError (Join-Path $work 'host-stderr.log') -RedirectStandardOutput (Join-Path $work 'host-stdout.log') -PassThru
    $deadline = (Get-Date).AddSeconds(50)
    while (-not (Test-Path $session) -and (Get-Date) -lt $deadline) { Start-Sleep -Milliseconds 200 }
    if (-not (Test-Path $session)) { throw 'Installed NSH host did not start its backend' }
    if (-not (Test-Path (Join-Path $fixtureHome 'migration-nsh-v1/manifest.json'))) { throw 'Installed NSH host did not snapshot old data' }
    # The uninstaller must request the actual frontend/backend graceful quit.
    $process = Start-Process (Join-Path $install 'Uninstall.exe') -ArgumentList '/S' -PassThru -Wait
    if ($process.ExitCode -ne 0) { throw "Uninstaller failed: $($process.ExitCode)" }
    $deadline = (Get-Date).AddSeconds(30)
    while ((Test-Path $install) -and (Get-Date) -lt $deadline) { Start-Sleep -Milliseconds 250 }
    if (Test-Path $install) { throw 'Application files survived uninstall' }
    if ([IO.File]::ReadAllText((Join-Path $fixtureHome 'local.yaml')) -ne $oldProfile) { throw 'Uninstall changed old user data' }
    if ([IO.File]::ReadAllText((Join-Path $fixtureHome 'migration-nsh-v1/local.yaml')) -ne $oldProfile) { throw 'Uninstall changed the migration snapshot' }
    if (Test-Path 'HKCU:/Software/Microsoft/Windows/CurrentVersion/Uninstall/io.github.gopurr.app') { throw 'Uninstall registry entry survived' }
    Write-Host 'NSH per-user installer, protocol handlers, migration startup, graceful uninstall and retained old data passed.'
} catch {
    foreach ($log in @('host-stderr.log','host-stdout.log','session.error.log')) {
        $path = Join-Path $work $log
        if (Test-Path $path) { Write-Host "${log}:"; Get-Content $path -Raw | Write-Host }
    }
    throw
} finally {
    if ($started -and -not $started.HasExited) {
        # Only this fixture's own frontend can be stopped. Its backend parent
        # watchdog owns cleanup if an assertion interrupted graceful shutdown.
        Stop-Process -Id $started.Id -ErrorAction SilentlyContinue
        Start-Sleep -Seconds 3
    }
    $env:PATH = $previousPath
    $env:APPDATA = $previousAppData
    $env:GOPURR_NATIVE_SESSION_OUTPUT = $previousSessionOutput
    if (Test-Path $work) { Remove-Item $work -Recurse -Force -ErrorAction SilentlyContinue }
}
