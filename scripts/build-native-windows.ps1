param([string]$Output = 'bin/native/windows-amd64', [string]$Version = '', [string]$UpdateFeed = '', [switch]$SkipRust)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$root = Split-Path $PSScriptRoot -Parent
Set-Location $root
if (-not $IsWindows) { throw 'Build the NSH Windows host on Windows x64.' }
$config = Get-Content mygo.json -Raw | ConvertFrom-Json
if (-not $Version) { $Version = $config.version }
if ($Version -notmatch '^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$') { throw 'Invalid application version' }
if (-not $UpdateFeed) { $UpdateFeed = "https://github.com/$($config.updates.github)/releases/latest/download/update-native-windows-amd64.json" }
$stage = [IO.Path]::GetFullPath($Output)
if (Test-Path $stage) { Remove-Item $stage -Recurse -Force }
New-Item $stage -ItemType Directory -Force | Out-Null
$env:GOPATH = "$root/.cache/go"
$env:GOMODCACHE = "$root/.cache/mod"
$env:GOCACHE = "$root/.cache/build"
$env:GOTOOLCHAIN = 'go1.27.3'
$env:GOFLAGS = '-tags=with_gvisor'
$env:CARGO_HOME = "$root/.cache/cargo"
$env:CARGO_TARGET_DIR = "$root/.cache/cargo-target"
$env:RUSTFLAGS = '-C target-feature=+crt-static'
$env:GOPURR_VERSION = $Version
$env:GOPURR_WINDOWS_ICON = "$stage/Gopurr.ico"
$env:LIBCLANG_PATH = 'C:/Program Files/LLVM/bin'
$env:PATH = "C:/Program Files/LLVM/bin;$env:PATH"
$sdk = "$root/.cache/nsh-windows"
$nsh = "$sdk/nsh/bin/nsh.exe"
$hash = 'e4cc3893700a1fcceb974c73ef8e08bc94a47a7a85c96d797d49c16dde95dff3'
if (-not (Test-Path "$sdk/verified-0.4.0")) {
    New-Item $sdk -ItemType Directory -Force | Out-Null
    $archive = "$sdk/nsh-windows-x86_64.zip"
    if (-not (Test-Path $archive) -or (Get-FileHash $archive -Algorithm SHA256).Hash.ToLowerInvariant() -ne $hash) {
        & gh release download v0.4.0 --repo longbridge/nsh --pattern nsh-windows-x86_64.zip --output "$archive.part" --clobber
        if ($LASTEXITCODE) { throw 'Download the official pinned NSH Windows SDK failed' }
        if ((Get-FileHash "$archive.part" -Algorithm SHA256).Hash.ToLowerInvariant() -ne $hash) { throw 'NSH Windows SDK checksum mismatch' }
        Move-Item "$archive.part" $archive -Force
    }
    Expand-Archive $archive -DestinationPath $sdk -Force
    if (-not (Test-Path $nsh)) { throw 'NSH SDK executable missing' }
    $sdkVersion = & $nsh --version
    if ($LASTEXITCODE -or $sdkVersion -notmatch '0\.4\.0.*e626a8d') { throw "Unexpected NSH SDK: $sdkVersion" }
    Set-Content "$sdk/verified-0.4.0" $hash
}
& python native/ui/sync-assets.py
if ($LASTEXITCODE) { throw 'Sync NSH assets failed' }
& $nsh build native/ui/Desktop.tsx --release --out "$stage/nsh"
if ($LASTEXITCODE) { throw 'Build shared desktop UI failed' }
# Runtime and installer share the rendered brand, with real ICO dimensions.
& go run ./scripts/windows-icon -out "$stage/Gopurr.ico"
if ($LASTEXITCODE) { throw 'Build Windows icon failed' }
$env:CGO_ENABLED = '0'
$flags = "-s -w -H=windowsgui -X gopurr/internal/buildinfo.Version=$Version -X github.com/egoist/mygo.production=1 -X github.com/egoist/mygo.packageName=Gopurr -X github.com/egoist/mygo.packageIdentifier=io.github.gopurr.app -X github.com/egoist/mygo.packageURLSchemes=gopurr,clash -X github.com/egoist/mygo.packageVersion=$Version -X github.com/egoist/mygo.packageUpdateFeed=$UpdateFeed -X github.com/egoist/mygo.packageUpdateKey=$($config.updates.publicKey)"
& go build -trimpath -ldflags $flags -o "$stage/gopurr-desktop.exe" .
if ($LASTEXITCODE) { throw 'Build desktop backend failed' }
$gcc = @('C:/msys64/mingw64/bin/gcc.exe', 'C:/mingw64/bin/gcc.exe', 'C:/msys64/ucrt64/bin/gcc.exe') | Where-Object { Test-Path $_ } | Select-Object -First 1
if (-not $gcc) { $gcc = (Get-Command gcc -ErrorAction Stop).Source }
$env:CC = $gcc
$env:CGO_ENABLED = '1'
& go build -trimpath -buildmode=c-shared -ldflags "-s -w -X gopurr/internal/buildinfo.Version=$Version" -o "$stage/GopurrBridge.dll" ./cmd/gopurr-bridge
if ($LASTEXITCODE) { throw 'Build desktop C ABI failed' }
Remove-Item "$stage/GopurrBridge.h"
if (-not $SkipRust) {
    & cargo build --locked --release --manifest-path native/windows/Cargo.toml
    if ($LASTEXITCODE) { throw 'Build NSH Windows frontend failed' }
}
& cargo metadata --locked --format-version 1 --filter-platform x86_64-pc-windows-msvc --manifest-path native/windows/Cargo.toml | Set-Content "$root/.cache/windows-cargo-metadata.json" -Encoding utf8NoBOM
if ($LASTEXITCODE) { throw 'Read locked Rust dependency metadata failed' }
& python scripts/cargo-notices.py --metadata "$root/.cache/windows-cargo-metadata.json" --out "$stage/licenses/rust"
if ($LASTEXITCODE) { throw 'Collect bundled Rust license notices failed' }
Copy-Item "$env:CARGO_TARGET_DIR/release/Gopurr.exe" "$stage/Gopurr.exe"
Copy-Item LICENSE,THIRD_PARTY_NOTICES.md $stage
Copy-Item licenses/* "$stage/licenses" -Recurse -Force
@{schema=1;ui='nsh-desktop';backend='desktop-v2';platform='windows-amd64';version=$Version;updateFeed=$UpdateFeed;updatePublicKey=$config.updates.publicKey;updateEnabled=$true;dataDirectory='Gopurr'} | ConvertTo-Json | Set-Content "$stage/native-build.json" -Encoding utf8NoBOM
Write-Host "Built NSH Windows application: $stage"
