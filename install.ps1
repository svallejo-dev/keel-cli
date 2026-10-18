# Installs keel from GitHub Releases on Windows. Keep this file ASCII: PowerShell 5.1 does not assume UTF-8.
# Env: KEEL_VERSION (default latest), KEEL_INSTALL_DIR.
# Usage: irm https://raw.githubusercontent.com/svallejo-dev/keel-cli/main/install.ps1 | iex

# The scriptblock keeps variables out of the caller's session, and throw (not exit) avoids closing it under iex.
& {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'

    if ($PSVersionTable.PSEdition -eq 'Core' -and -not $IsWindows) {
        throw 'keel: install.ps1 es solo para Windows; en macOS/Linux usa install.sh'
    }

    $repo = 'svallejo-dev/keel-cli'
    $version = if ($env:KEEL_VERSION) { $env:KEEL_VERSION } else { 'latest' }
    $binDir = if ($env:KEEL_INSTALL_DIR) { $env:KEEL_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\keel' }

    $osArch = try { [string][System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture } catch { $env:PROCESSOR_ARCHITECTURE }
    $arch = switch ($osArch) {
        { $_ -in 'X64', 'AMD64' } { 'amd64' }
        'Arm64' { 'arm64' }
        default { throw "keel: arquitectura no soportada: $osArch" }
    }

    $base = if ($env:KEEL_BASE_URL) { $env:KEEL_BASE_URL }
    elseif ($version -eq 'latest') { "https://github.com/$repo/releases/latest/download" }
    else { "https://github.com/$repo/releases/download/$version" }
    $asset = "keel_windows_$arch.zip"

    # Windows PowerShell 5.1 on older .NET may not offer TLS 1.2, which GitHub requires.
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("keel-" + [guid]::NewGuid())
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        Write-Host "keel: descargando $asset ($version)"
        Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset" -OutFile (Join-Path $tmp $asset)
        Invoke-WebRequest -UseBasicParsing -Uri "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt')

        $line = Get-Content (Join-Path $tmp 'checksums.txt') | Where-Object { $_ -match "\s$([regex]::Escape($asset))$" } | Select-Object -First 1
        $expected = if ($line) { ($line -split '\s+')[0].ToLower() } else { '' }
        $actual = (Get-FileHash (Join-Path $tmp $asset) -Algorithm SHA256).Hash.ToLower()
        if (-not $expected -or $expected -ne $actual) {
            throw "keel: el checksum de $asset no coincide, no se ha instalado nada"
        }

        Expand-Archive -Path (Join-Path $tmp $asset) -DestinationPath (Join-Path $tmp 'x') -Force
        New-Item -ItemType Directory -Path $binDir -Force | Out-Null
        Copy-Item (Join-Path $tmp 'x\keel.exe') (Join-Path $binDir 'keel.exe') -Force
    }
    finally {
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    }

    # Edit the registry value directly: [Environment]::SetEnvironmentVariable would expand
    # %VARS% already in the user's Path and save it as REG_SZ.
    $envKey = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
    $userPath = [string]$envKey.GetValue('Path', '', 'DoNotExpandEnvironmentNames')
    if (($userPath -split ';') -notcontains $binDir) {
        $envKey.SetValue('Path', (($userPath.TrimEnd(';'), $binDir) -join ';').TrimStart(';'), 'ExpandString')
        # Setting any user variable broadcasts WM_SETTINGCHANGE so new terminals pick up the Path.
        [Environment]::SetEnvironmentVariable('KEEL_INSTALLER', '1', 'User')
        [Environment]::SetEnvironmentVariable('KEEL_INSTALLER', $null, 'User')
        Write-Host "keel: $binDir agregado al Path de tu usuario (abre una terminal nueva)"
    }
    $envKey.Close()
    if (($env:Path -split ';') -notcontains $binDir) { $env:Path = "$env:Path;$binDir" }

    Write-Host "keel: instalado en $binDir\keel.exe ($(& (Join-Path $binDir 'keel.exe') --version))"
}
