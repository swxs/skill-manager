# Windows launcher. Runs runtime/ when its recorded version matches SKILL.md.
# Otherwise download that version's platform zip, unpack the runtime,
# and check it against checksums.txt before replacing and executing.
$ErrorActionPreference = "Stop"

function Fail([string]$Message) {
    [Console]::Error.WriteLine($Message)
    exit 1
}

function Read-Version([string]$Path) {
    $lines = Get-Content -LiteralPath $Path
    $inFront = $false
    foreach ($line in $lines) {
        if (-not $inFront) {
            if ($line -eq "---") { $inFront = $true }
            continue
        }
        if ($line -eq "---") { break }
        if ($line -match '^version:\s*(\S+)\s*$') {
            return $Matches[1]
        }
    }
    return ""
}

$skillRoot = Split-Path -Parent $PSScriptRoot
$version = Read-Version (Join-Path $skillRoot "SKILL.md")
switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64" { $arch = "amd64" }
    "ARM64" { $arch = "arm64" }
    default { $arch = $env:PROCESSOR_ARCHITECTURE.ToLowerInvariant() }
}
$name = "skill-manager-windows-$arch.exe"
$runtimeDir = Join-Path $skillRoot "runtime"
$dest = Join-Path $runtimeDir $name
$versionFile = Join-Path $runtimeDir "version"
$recorded = ""
if (Test-Path -LiteralPath $versionFile) {
    $recorded = ([System.IO.File]::ReadAllText($versionFile)).Trim()
}

if (-not ((Test-Path -LiteralPath $dest) -and $version -and ($recorded -eq $version))) {
    if (-not $version) {
        Fail "没有这一版的运行时: $name"
    }
    $base = "https://github.com/swxs/skill-manager/releases/download/$version"
    $zipName = "skill-manager-windows-$arch.zip"
    $zipUrl = "$base/$zipName"
    $sumUrl = "$base/checksums.txt"
    $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("skill-manager-" + [guid]::NewGuid().ToString("n"))
    New-Item -ItemType Directory -Path $tmp | Out-Null
    $zipPath = Join-Path $tmp "pkg.zip"
    $sumPath = Join-Path $tmp "checksums.txt"
    $unpack = Join-Path $tmp "unpack"
    try {
        try {
            Invoke-WebRequest -Uri $zipUrl -OutFile $zipPath -UseBasicParsing
        } catch {
            Fail "下载失败: $zipUrl"
        }
        try {
            Invoke-WebRequest -Uri $sumUrl -OutFile $sumPath -UseBasicParsing
        } catch {
            Fail "下载失败: $sumUrl"
        }
        try {
            Expand-Archive -LiteralPath $zipPath -DestinationPath $unpack -Force
        } catch {
            Fail "下载失败: $zipUrl"
        }
        $found = Get-ChildItem -LiteralPath $unpack -Recurse -File -Filter $name | Select-Object -First 1
        if (-not $found) {
            Fail "没有这一版的运行时: $name"
        }
        $got = (Get-FileHash -Algorithm SHA256 -LiteralPath $found.FullName).Hash.ToLowerInvariant()
        $want = ""
        foreach ($row in (Get-Content -LiteralPath $sumPath)) {
            $row = $row.Trim()
            if ($row -match '^([0-9a-fA-F]{64})  (.+)$' -and $Matches[2] -eq $name) {
                $want = $Matches[1].ToLowerInvariant()
                break
            }
        }
        if (-not $want -or $want -ne $got) {
            Fail "校验和不符: $name"
        }
        New-Item -ItemType Directory -Force -Path $runtimeDir | Out-Null
        Copy-Item -LiteralPath $found.FullName -Destination $dest -Force
        $utf8 = New-Object System.Text.UTF8Encoding $false
        [System.IO.File]::WriteAllText($versionFile, "$version`n", $utf8)
    } finally {
        if (Test-Path -LiteralPath $tmp) {
            Remove-Item -LiteralPath $tmp -Recurse -Force
        }
    }
}

& $dest @args
exit $LASTEXITCODE
