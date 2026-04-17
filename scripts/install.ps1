$ErrorActionPreference = "Stop"

$Repo   = "srschreiber/nito-client"
$Binary = "nito.exe"

Write-Host "Fetching latest nito release..."
$Release = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest"
$Tag     = $Release.tag_name
if (-not $Tag) {
    Write-Error "Failed to determine the latest release tag."
    exit 1
}
Write-Host "Latest release: $Tag"

$AssetName   = "nito-windows-amd64.exe"
$DownloadUrl = "https://github.com/$Repo/releases/download/$Tag/$AssetName"
$ChecksumUrl = "$DownloadUrl.sha256"

$TmpDir = Join-Path $env:TEMP "nito-install-$([System.IO.Path]::GetRandomFileName())"
New-Item -ItemType Directory -Path $TmpDir | Out-Null

try {
    $ExePath      = Join-Path $TmpDir $AssetName
    $ChecksumPath = "$ExePath.sha256"

    Write-Host "Downloading $AssetName..."
    Invoke-WebRequest $DownloadUrl -OutFile $ExePath

    Write-Host "Downloading checksum..."
    Invoke-WebRequest $ChecksumUrl -OutFile $ChecksumPath

    Write-Host "Verifying checksum..."
    $Expected = (Get-Content $ChecksumPath).Split(" ")[0].Trim()
    $Actual   = (Get-FileHash $ExePath -Algorithm SHA256).Hash.ToLower()
    if ($Expected -ne $Actual) {
        Write-Error "Checksum verification failed!`n  Expected: $Expected`n  Actual:   $Actual"
        exit 1
    }
    Write-Host "Checksum verified."

    $InstallDir = Join-Path $env:LOCALAPPDATA "Programs\nito"
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null

    $Dest = Join-Path $InstallDir $Binary
    Move-Item $ExePath $Dest -Force

    # Add install dir to user PATH if not already present.
    $CurrentPath = [Environment]::GetEnvironmentVariable("PATH", "User")
    if ($CurrentPath -notlike "*$InstallDir*") {
        [Environment]::SetEnvironmentVariable("PATH", "$CurrentPath;$InstallDir", "User")
        Write-Host ""
        Write-Host "Added $InstallDir to your PATH."
        Write-Host "Restart your terminal, then run: nito"
    } else {
        Write-Host ""
        Write-Host "nito $Tag installed. Run it with: nito"
    }
} finally {
    Remove-Item $TmpDir -Recurse -Force -ErrorAction SilentlyContinue
}
