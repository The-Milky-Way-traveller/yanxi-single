# Edge case tests for yanxi-single
param([string]$ServerExe = "..\cmd\yanxi-mcp\yanxi-mcp.exe", [string]$ProjectDir = $PWD)

$serverPath = Join-Path $PSScriptRoot $ServerExe
$serverPath = (Resolve-Path $serverPath).Path

Write-Host "=== Edge Case Tests ===" -ForegroundColor Cyan
Write-Host "Server: $serverPath"
Write-Host "Project: $ProjectDir"
Write-Host ""

$script:proc = New-Object System.Diagnostics.Process
$script:proc.StartInfo = New-Object System.Diagnostics.ProcessStartInfo -Property @{
    FileName = $serverPath
    RedirectStandardInput = $true
    RedirectStandardOutput = $true
    RedirectStandardError = $true
    UseShellExecute = $false
    CreateNoWindow = $true
}
$script:proc.Start() | Out-Null
Start-Sleep -Milliseconds 500
$null = $script:proc.StandardOutput.ReadLine()

function Send($body) {
    $json = ($body | ConvertTo-Json -Depth 10 -Compress)
    $script:proc.StandardInput.WriteLine($json)
    $script:proc.StandardInput.Flush()
    Start-Sleep -Milliseconds 500
    return $script:proc.StandardOutput.ReadLine()
}

$global:nextId = 1

function Call-Tool($name, $argsTable) {
    $id = $global:nextId
    $global:nextId++
    $body = @{jsonrpc="2.0"; id=$id; method="tools/call"; params=@{name=$name; arguments=$argsTable}}
    return Send $body
}

try {
    # Initialize
    $init = @{jsonrpc="2.0"; id=0; method="initialize"; params=@{protocolVersion="2024-11-05"; clientInfo=@{name="t"; version="1"}}}
    Send $init | Out-Null

    Write-Host "1. module_search with empty query..." -NoNewline
    $r = Call-Tool "module_search" @{query=""; project_dir=$ProjectDir}
    if ($r -match "error" -or $r -match "required") { Write-Host " EXP" -ForegroundColor Yellow; Write-Host "   $r" }
    else { Write-Host " RESP"; Write-Host "   $r" }

    Write-Host "2. module_validate on nonexistent module..." -NoNewline
    $r = Call-Tool "module_validate" @{module="nonexistent"; project_dir=$ProjectDir}
    if ($r -match '"valid":false' -or $r -match "not found") { Write-Host " EXP (error detected)" -ForegroundColor Yellow }
    else { Write-Host " UNEXP"; Write-Host "   $r" }

    Write-Host "3. module_search with vector mode (should fallback to BM25)..." -NoNewline
    $r = Call-Tool "module_search" @{query="hello"; mode="vector"; project_dir=$ProjectDir; top_k=3}
    if ($r -match "results") { Write-Host " PASS" -ForegroundColor Green }
    else { Write-Host " FAIL"; Write-Host "   $r" }

    Write-Host "4. module_discover on non-existent directory..." -NoNewline
    $r = Call-Tool "module_discover" @{project_dir="C:\nonexistent"}
    if ($r -match "overview_text" -or $r -match "ModuleCount") { Write-Host " RESP (graceful)" -ForegroundColor Yellow }
    else { Write-Host " UNEXP"; Write-Host "   $r" }

    Write-Host "5. module_create with empty name..." -NoNewline
    $r = Call-Tool "module_create" @{name=""; project_dir=$ProjectDir}
    if ($r -match "error" -or $r -match "required") { Write-Host " EXP (error)" -ForegroundColor Yellow }
    else { Write-Host " RESP"; Write-Host "   $r" }

    Write-Host "6. module_create duplicate (hello already exists)..." -NoNewline
    $r = Call-Tool "module_create" @{name="hello"; project_dir=$ProjectDir}
    if ($r -match "error" -or $r -match "exists") { Write-Host " EXP (error)" -ForegroundColor Yellow }
    else { Write-Host " RESP"; Write-Host "   $r" }

    Write-Host "7. module_wire with missing source/main..." -NoNewline
    # clean the main dir first
    Remove-Item -Recurse -Force "$ProjectDir\source\main" -ErrorAction SilentlyContinue
    $r = Call-Tool "module_wire" @{project_dir=$ProjectDir}
    if ($r -match "wired") { Write-Host " PASS" -ForegroundColor Green }
    else { Write-Host " RESP"; Write-Host "   $r" }

} finally {
    $script:proc.Kill()
    $script:proc.Dispose()
}
