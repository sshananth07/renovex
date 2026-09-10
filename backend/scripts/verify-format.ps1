[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [string]$TargetPath
)

Set-StrictMode -Version Latest

# Default to the Go module root, not the caller's working directory. This lets
# developers and future CI invoke the repository-owned gate from anywhere.
if ([string]::IsNullOrWhiteSpace($TargetPath)) {
    $TargetPath = Split-Path -Parent $PSScriptRoot
}

try {
    $resolvedTarget = (Resolve-Path -LiteralPath $TargetPath -ErrorAction Stop).Path
}
catch {
    Write-Error "Formatting target does not exist: $TargetPath"
    exit 2
}

if ($null -eq (Get-Command gofmt -ErrorAction SilentlyContinue)) {
    Write-Error "gofmt is required but was not found on PATH."
    exit 2
}

# Preserve gofmt's real file list in the failure output. A generic non-zero
# exit would tell the contractor that formatting drift exists but not what to
# fix, and would make CI logs needlessly opaque.
$unformatted = @(& gofmt -l $resolvedTarget 2>&1)
$gofmtExitCode = $LASTEXITCODE
if ($gofmtExitCode -ne 0) {
    $unformatted | ForEach-Object { Write-Output $_ }
    exit $gofmtExitCode
}

if ($unformatted.Count -gt 0) {
    Write-Output "gofmt reported unformatted Go files:"
    $unformatted | ForEach-Object { Write-Output $_ }
    exit 1
}

Write-Output "gofmt check passed: $resolvedTarget"
exit 0
