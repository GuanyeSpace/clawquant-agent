[CmdletBinding()]
param(
    [string]$ProjectName = "clawquant-agent",
    [string]$ModulePath = "github.com/GuanyeSpace/clawquant-agent",
    [string]$MainPackage = "./cmd/agent",
    [string]$Version,
    [string]$Commit,
    [string]$BuildTime,
    [string]$TargetsCsv,
    [string[]]$Targets = @(
        "windows/amd64",
        "windows/arm64",
        "linux/amd64",
        "linux/arm64",
        "darwin/amd64",
        "darwin/arm64"
    ),
    [switch]$Clean
)

$ErrorActionPreference = "Stop"

if (-not [string]::IsNullOrWhiteSpace($TargetsCsv)) {
    $Targets = @(
        $TargetsCsv.Split(",", [System.StringSplitOptions]::RemoveEmptyEntries) |
            ForEach-Object { $_.Trim() } |
            Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
    )
}

function Resolve-GitValue {
    param(
        [string[]]$Command,
        [string]$Fallback
    )

    try {
        $value = (& git @Command 2>$null | Out-String).Trim()
        if ([string]::IsNullOrWhiteSpace($value)) {
            return $Fallback
        }

        return $value
    } catch {
        return $Fallback
    }
}

$root = Split-Path -Parent $PSScriptRoot
$distDir = Join-Path $root "dist"
$cmdPath = $MainPackage

$originalEnv = @{
    CGO_ENABLED = $env:CGO_ENABLED
    GOOS        = $env:GOOS
    GOARCH      = $env:GOARCH
}

Push-Location $root
try {
    if ([string]::IsNullOrWhiteSpace($Version)) {
        $Version = Resolve-GitValue -Command @("describe", "--tags", "--always", "--dirty") -Fallback "dev"
    }

    if ([string]::IsNullOrWhiteSpace($Commit)) {
        $Commit = Resolve-GitValue -Command @("rev-parse", "--short", "HEAD") -Fallback "none"
    }

    if ([string]::IsNullOrWhiteSpace($BuildTime)) {
        $BuildTime = [DateTime]::UtcNow.ToString("yyyy-MM-ddTHH:mm:ssZ")
    }

    if ($Clean -and (Test-Path $distDir)) {
        Remove-Item -Recurse -Force $distDir
    }

    New-Item -ItemType Directory -Force -Path $distDir | Out-Null

    $ldflags = @(
        "-s",
        "-w",
        "-X $ModulePath/internal/buildinfo.Version=$Version",
        "-X $ModulePath/internal/buildinfo.Commit=$Commit",
        "-X $ModulePath/internal/buildinfo.BuildTime=$BuildTime"
    ) -join " "

    $checksums = New-Object System.Collections.Generic.List[string]

    foreach ($target in $Targets) {
        $parts = $target -split "/"
        if ($parts.Count -ne 2) {
            throw "Invalid build target: $target"
        }

        $goos = $parts[0]
        $goarch = $parts[1]
        $targetDir = Join-Path $distDir "$ProjectName-$goos-$goarch"
        $binaryName = if ($goos -eq "windows") { "$ProjectName.exe" } else { $ProjectName }
        $outputPath = Join-Path $targetDir $binaryName

        New-Item -ItemType Directory -Force -Path $targetDir | Out-Null

        Write-Host "==> Building $goos/$goarch"

        $env:CGO_ENABLED = "0"
        $env:GOOS = $goos
        $env:GOARCH = $goarch

        & go build -trimpath -ldflags $ldflags -o $outputPath $cmdPath
        if ($LASTEXITCODE -ne 0) {
            throw "go build failed for $target"
        }

        $hash = (Get-FileHash -Algorithm SHA256 $outputPath).Hash.ToLowerInvariant()
        $relativePath = (Resolve-Path -Relative $outputPath).ToString().Replace("\", "/")
        if ($relativePath.StartsWith("./")) {
            $relativePath = $relativePath.Substring(2)
        } elseif ($relativePath.StartsWith(".\\")) {
            $relativePath = $relativePath.Substring(2)
        }
        $checksums.Add("$hash  $relativePath") | Out-Null
    }

    $checksums | Set-Content -Encoding ascii (Join-Path $distDir "SHA256SUMS.txt")
    Write-Host "Build artifacts written to $distDir"
} finally {
    if ($null -eq $originalEnv.CGO_ENABLED) {
        Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue
    } else {
        $env:CGO_ENABLED = $originalEnv.CGO_ENABLED
    }

    if ($null -eq $originalEnv.GOOS) {
        Remove-Item Env:GOOS -ErrorAction SilentlyContinue
    } else {
        $env:GOOS = $originalEnv.GOOS
    }

    if ($null -eq $originalEnv.GOARCH) {
        Remove-Item Env:GOARCH -ErrorAction SilentlyContinue
    } else {
        $env:GOARCH = $originalEnv.GOARCH
    }

    Pop-Location
}
