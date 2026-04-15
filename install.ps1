# UniFlow CLI Installation Script (Windows)
# Usage: iwr https://raw.githubusercontent.com/veer-singh4/FlowSpec/main/install.ps1 | iex

$Owner = "veer-singh4"
$Repo = "FlowSpec"
$BinaryName = "flow.exe"

# Detect Architecture
$Arch = "amd64"
if ([IntPtr]::Size -eq 4) { $Arch = "386" } # Unlikely but for completeness

echo "Detected Arch: $Arch"

# Get latest release tag from GitHub
$ReleaseUrl = "https://api.github.com/repos/$Owner/$Repo/releases/latest"
try {
    $Release = Invoke-RestMethod -Uri $ReleaseUrl
} catch {
    Write-Error "Could not find latest release. Please ensure you have created a release on GitHub."
    return
}
$Tag = $Release.tag_name
$Version = $Tag.TrimStart('v')

echo "Installing $Repo $Tag..."

# Construct download URL
# Example: FlowSpec_1.0.0_windows_amd64.zip
$DownloadUrl = "https://github.com/$Owner/$Repo/releases/download/$Tag/$($Repo)_$($Version)_windows_$Arch.zip"

echo "Downloading from $DownloadUrl..."
$TmpDir = [System.IO.Path]::GetTempPath() + [System.Guid]::NewGuid().ToString()
New-Item -ItemType Directory -Path $TmpDir | Out-Null
$ZipPath = Join-Path $TmpDir "flow.zip"

Invoke-WebRequest -Uri $DownloadUrl -OutFile $ZipPath

# Extract
Expand-Archive -Path $ZipPath -DestinationPath $TmpDir

# Install to User Profile
$InstallDir = Join-Path $HOME ".flow-cli"
if (!(Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir | Out-Null
}

echo "Installing to $InstallDir..."
Move-Item -Path (Join-Path $TmpDir $BinaryName) -Destination (Join-Path $InstallDir $BinaryName) -Force

# Add to PATH if not already there
$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath -notlike "*$InstallDir*") {
    echo "Adding $InstallDir to User PATH..."
    [Environment]::SetEnvironmentVariable("Path", "$UserPath;$InstallDir", "User")
    $env:Path += ";$InstallDir"
}

# Cleanup
Remove-Item -Recurse -Force $TmpDir

echo "✓ Successfully installed UniFlow CLI to $InstallDir"
echo "Please restart your terminal or run '`$env:Path = [Environment]::GetEnvironmentVariable(\"Path\", \"User\")' to use 'flow'."
