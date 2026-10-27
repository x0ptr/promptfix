# promptfix installer for Windows.
#
#   irm https://raw.githubusercontent.com/x0ptr/promptfix/main/install.ps1 | iex
#
# Environment:
#   PROMPTFIX_VERSION        release tag to install, e.g. v0.1.0 (default: latest)
#   PROMPTFIX_INSTALL_DIR    target directory (default: %LOCALAPPDATA%\Programs\promptfix)
#   PROMPTFIX_DOWNLOAD_URL   base URL serving the release assets (tests, mirrors)
#
# The install directory is added to the user PATH (no admin rights needed).

function Install-Promptfix {
    $ErrorActionPreference = 'Stop'
    $ProgressPreference = 'SilentlyContinue'
    # Windows PowerShell 5.1 may default to TLS 1.0.
    [Net.ServicePointManager]::SecurityProtocol = [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

    $repo = 'x0ptr/promptfix'
    $version = $env:PROMPTFIX_VERSION
    $dir = $env:PROMPTFIX_INSTALL_DIR
    if (-not $dir) { $dir = Join-Path $env:LOCALAPPDATA 'Programs\promptfix' }

    $cpu = $env:PROCESSOR_ARCHITEW6432
    if (-not $cpu) { $cpu = $env:PROCESSOR_ARCHITECTURE }
    switch ($cpu) {
        'AMD64' { $arch = 'amd64' }
        'ARM64' { $arch = 'arm64' }
        default { throw "promptfix: unsupported platform windows/$cpu - supported: amd64, arm64" }
    }
    $asset = "promptfix_windows_$arch.zip"

    if ($env:PROMPTFIX_DOWNLOAD_URL) {
        $base = $env:PROMPTFIX_DOWNLOAD_URL.TrimEnd('/')
    } elseif ($version) {
        if (-not $version.StartsWith('v')) { throw "promptfix: PROMPTFIX_VERSION must be a tag like v0.1.0, got '$version'" }
        $base = "https://github.com/$repo/releases/download/$version"
    } else {
        $base = "https://github.com/$repo/releases/latest/download"
    }

    $tmp = Join-Path ([IO.Path]::GetTempPath()) ("promptfix-" + [Guid]::NewGuid())
    New-Item -ItemType Directory -Path $tmp | Out-Null
    try {
        $label = if ($version) { $version } else { '(latest)' }
        Write-Host "promptfix: downloading $asset $label"
        Invoke-WebRequest -UseBasicParsing -Uri "$base/$asset" -OutFile (Join-Path $tmp $asset)
        Invoke-WebRequest -UseBasicParsing -Uri "$base/checksums.txt" -OutFile (Join-Path $tmp 'checksums.txt')

        $want = $null
        foreach ($line in Get-Content (Join-Path $tmp 'checksums.txt')) {
            $parts = $line -split '\s+'
            if ($parts.Count -ge 2 -and $parts[1].TrimStart('*') -eq $asset) { $want = $parts[0].ToLower() }
        }
        if (-not $want) { throw "promptfix: no checksum for $asset in checksums.txt" }
        $got = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $asset)).Hash.ToLower()
        if ($want -ne $got) { throw "promptfix: checksum mismatch for $asset (expected $want, got $got) - nothing installed" }
        Write-Host 'promptfix: checksum ok'

        Expand-Archive -Path (Join-Path $tmp $asset) -DestinationPath (Join-Path $tmp 'x') -Force
        $exe = Join-Path $dir 'promptfix.exe'
        $old = $null
        if (Test-Path $exe) {
            try { $old = ((& $exe version) -split ' ')[1] } catch { }
        }
        New-Item -ItemType Directory -Force -Path $dir | Out-Null
        # A running exe cannot be overwritten but can be renamed.
        if (Test-Path $exe) {
            Move-Item -Force $exe "$exe.old"
        }
        Copy-Item (Join-Path $tmp 'x\promptfix.exe') $exe
        Remove-Item -Force "$exe.old" -ErrorAction SilentlyContinue

        $new = ((& $exe version) -split ' ')[1]
        if ($old -and $old -ne $new) {
            Write-Host "promptfix: updated ${exe}: $old -> $new"
        } else {
            Write-Host "promptfix: installed $exe $new"
        }

        $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
        $entries = @()
        if ($userPath) { $entries = $userPath -split ';' | Where-Object { $_ } }
        if ($entries -notcontains $dir) {
            [Environment]::SetEnvironmentVariable('Path', (($entries + $dir) -join ';'), 'User')
            $env:Path = "$env:Path;$dir"
            Write-Host "promptfix: added $dir to your user PATH (open a new terminal to use it)"
        }
    } finally {
        Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    }
}

Install-Promptfix
