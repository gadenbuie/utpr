# Install script for utpr (Windows PowerShell)
# Usage: powershell -ExecutionPolicy ByPass -c "irm https://raw.githubusercontent.com/gadenbuie/utpr/main/scripts/install.ps1 | iex"

$ErrorActionPreference = "Stop"
$Repo = "gadenbuie/utpr"

function Get-LatestVersion {
    $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest"
    return $release.tag_name
}

function Get-Arch {
    $arch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
    switch ($arch) {
        "X64"  { return "amd64" }
        "Arm64" { return "arm64" }
        default {
            Write-Error "Unsupported architecture: $arch"
            exit 1
        }
    }
}

function Find-InstallDir {
    $localBin = Join-Path $env:LOCALAPPDATA "utpr" "bin"
    if (-not (Test-Path $localBin)) {
        New-Item -ItemType Directory -Path $localBin -Force | Out-Null
    }
    return $localBin
}

function Install-Utpr {
    $arch = Get-Arch
    $version = Get-LatestVersion
    $installDir = Find-InstallDir

    $archive = "utpr-windows-$arch.tar.gz"
    $url = "https://github.com/$Repo/releases/download/$version/$archive"

    Write-Host "Downloading utpr $version for windows/$arch..."

    $tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())
    New-Item -ItemType Directory -Path $tmpDir -Force | Out-Null

    try {
        $archivePath = Join-Path $tmpDir $archive
        Invoke-WebRequest -Uri $url -OutFile $archivePath -UseBasicParsing

        # Extract tar.gz
        tar -xzf $archivePath -C $tmpDir

        $srcExe = Join-Path $tmpDir "utpr-windows-$arch" "utpr.exe"
        $destExe = Join-Path $installDir "utpr.exe"
        Copy-Item -Path $srcExe -Destination $destExe -Force

        Write-Host "Installed utpr to $destExe"
    }
    catch {
        # Fall back to go install
        if (Get-Command go -ErrorAction SilentlyContinue) {
            Write-Host "Binary download failed, trying go install..."
            go install "github.com/$Repo@latest"
            Write-Host "Installed utpr via go install"
            return
        }

        Write-Error "Installation failed. Install Go (https://go.dev) and run: go install github.com/$Repo@latest"
        exit 1
    }
    finally {
        Remove-Item -Recurse -Force $tmpDir -ErrorAction SilentlyContinue
    }

    # Check if install directory is in PATH
    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ($userPath -notlike "*$installDir*") {
        Write-Host ""
        Write-Host "Adding $installDir to your PATH..."
        [Environment]::SetEnvironmentVariable(
            "Path",
            "$userPath;$installDir",
            "User"
        )
        $env:Path = "$env:Path;$installDir"
        Write-Host "Done. Restart your terminal for PATH changes to take effect."
    }
}

Install-Utpr
