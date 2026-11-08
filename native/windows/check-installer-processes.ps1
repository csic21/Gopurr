$ErrorActionPreference = 'Stop'
try {
    $directory = [IO.Path]::GetFullPath($env:GOPURR_INSTALL_DIR).TrimEnd('\') + '\'
    $deadline = (Get-Date).AddSeconds(15)
    do {
        $running = @(Get-CimInstance Win32_Process | Where-Object {
            $_.ExecutablePath -and $_.Name -ne 'Uninstall.exe' -and $_.ExecutablePath.StartsWith($directory, [StringComparison]::OrdinalIgnoreCase)
        })
        if ($running.Count -eq 0) { exit 0 }
        Start-Sleep -Milliseconds 250
    } while ((Get-Date) -lt $deadline)
    exit 2
} catch { exit 1 }
