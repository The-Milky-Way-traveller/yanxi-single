# Test script for yanxi-single MCP server
param([string]$ServerExe = "..\cmd\yanxi-mcp\yanxi-mcp.exe", [string]$ProjectDir = $PWD)

$serverPath = Join-Path $PSScriptRoot $ServerExe
$serverPath = (Resolve-Path $serverPath).Path

Write-Host "=== yanxi-single Tool Test ===" -ForegroundColor Cyan
Write-Host "Server: $serverPath"
Write-Host "Project: $ProjectDir"
Write-Host ""

# Start server process
$psi = New-Object System.Diagnostics.ProcessStartInfo
$psi.FileName = $serverPath
$psi.RedirectStandardInput = $true
$psi.RedirectStandardOutput = $true
$psi.RedirectStandardError = $true
$psi.UseShellExecute = $false
$psi.CreateNoWindow = $true

$script:proc = New-Object System.Diagnostics.Process
$script:proc.StartInfo = $psi
$script:proc.Start() | Out-Null

Start-Sleep -Milliseconds 500
$null = $script:proc.StandardOutput.ReadLine()

function Invoke-McpTool($method, $name, $argsTable) {
    if ($method -eq "tools/call") {
        $body = @{jsonrpc="2.0"; id=1; method="tools/call"; params=@{name=$name; arguments=$argsTable}}
    } else {
        $body = @{jsonrpc="2.0"; id=1; method=$method}
        if ($method -eq "initialize") {
            $body.params = @{protocolVersion="2024-11-05"; clientInfo=@{name="test-client"; version="1.0"}}
        }
    }
    $json = ($body | ConvertTo-Json -Depth 10 -Compress)
    $script:proc.StandardInput.WriteLine($json)
    $script:proc.StandardInput.Flush()
    Start-Sleep -Milliseconds 800
    $resp = $script:proc.StandardOutput.ReadLine()
    return $resp
}

$results = @{}
$errors = @{}

try {
    Write-Host "1. Testing initialize..." -NoNewline
    $r = Invoke-McpTool "initialize" $null $null
    if ($r -match "serverInfo") { Write-Host " PASS" -ForegroundColor Green; $results["initialize"] = $true }
    else { Write-Host " FAIL" -ForegroundColor Red; $results["initialize"] = $false; $errors["initialize"] = $r }

    Write-Host "2. Testing tools/list..." -NoNewline
    $r = Invoke-McpTool "tools/list" $null $null
    if ($r -match "tools") { Write-Host " PASS" -ForegroundColor Green; $results["tools/list"] = $true }
    else { Write-Host " FAIL" -ForegroundColor Red; $results["tools/list"] = $false; $errors["tools/list"] = $r }

    Write-Host "3. Testing module_discover..." -NoNewline
    $r = Invoke-McpTool "tools/call" "module_discover" @{project_dir=$ProjectDir}
    if ($r -match "overview_text" -or $r -match "ModuleCount") { Write-Host " PASS" -ForegroundColor Green; $results["module_discover"] = $true }
    else { Write-Host " FAIL" -ForegroundColor Red; $results["module_discover"] = $false; $errors["module_discover"] = $r }

    Write-Host "4. Testing module_bootstrap (create hello)..." -NoNewline
    $r = Invoke-McpTool "tools/call" "module_bootstrap" @{name="hello"; language="python"; project_dir=$ProjectDir}
    if ($r -match "Created" -or $r -match "created" -or $r -match "result") { Write-Host " PASS" -ForegroundColor Green; $results["module_bootstrap"] = $true }
    else { Write-Host " FAIL" -ForegroundColor Red; $results["module_bootstrap"] = $false; $errors["module_bootstrap"] = $r }

    Write-Host "5. Testing module_create (world)..." -NoNewline
    $r = Invoke-McpTool "tools/call" "module_create" @{name="world"; language="python"; project_dir=$ProjectDir; description="World module"}
    if ($r -match "created" -or $r -match "status") { Write-Host " PASS" -ForegroundColor Green; $results["module_create"] = $true }
    else { Write-Host " FAIL" -ForegroundColor Red; $results["module_create"] = $false; $errors["module_create"] = $r }

    Write-Host "6. Testing aiexplain_generate..." -NoNewline
    $r = Invoke-McpTool "tools/call" "aiexplain_generate" @{project_dir=$ProjectDir}
    if ($r -match "regenerated" -or $r -match "status") { Write-Host " PASS" -ForegroundColor Green; $results["aiexplain_generate"] = $true }
    else { Write-Host " FAIL" -ForegroundColor Red; $results["aiexplain_generate"] = $false; $errors["aiexplain_generate"] = $r }

    Write-Host "7. Testing module_search..." -NoNewline
    $r = Invoke-McpTool "tools/call" "module_search" @{query="hello"; project_dir=$ProjectDir; top_k=5}
    if ($r -match "results" -or $r -match "total") { Write-Host " PASS" -ForegroundColor Green; $results["module_search"] = $true }
    else { Write-Host " FAIL" -ForegroundColor Red; $results["module_search"] = $false; $errors["module_search"] = $r }

    Write-Host "8. Testing module_search_loose..." -NoNewline
    $r = Invoke-McpTool "tools/call" "module_search_loose" @{query="handler"; project_dir=$ProjectDir; top_k=5}
    if ($r -match "results" -or $r -match "total") { Write-Host " PASS" -ForegroundColor Green; $results["module_search_loose"] = $true }
    else { Write-Host " FAIL" -ForegroundColor Red; $results["module_search_loose"] = $false; $errors["module_search_loose"] = $r }

    Write-Host "9. Testing module_wire..." -NoNewline
    $r = Invoke-McpTool "tools/call" "module_wire" @{project_dir=$ProjectDir}
    if ($r -match "wired" -or $r -match "status") { Write-Host " PASS" -ForegroundColor Green; $results["module_wire"] = $true }
    else { Write-Host " FAIL" -ForegroundColor Red; $results["module_wire"] = $false; $errors["module_wire"] = $r }

    Write-Host "10. Testing module_validate (hello)..." -NoNewline
    $r = Invoke-McpTool "tools/call" "module_validate" @{module="hello"; project_dir=$ProjectDir}
    if ($r -match "valid") { Write-Host " PASS" -ForegroundColor Green; $results["module_validate"] = $true }
    else { Write-Host " FAIL" -ForegroundColor Red; $results["module_validate"] = $false; $errors["module_validate"] = $r }

    Write-Host "11. Testing module_check_imports (hello)..." -NoNewline
    $r = Invoke-McpTool "tools/call" "module_check_imports" @{module="hello"; project_dir=$ProjectDir}
    if ($r -match "ok:true") { Write-Host " PASS" -ForegroundColor Green; $results["module_check_imports"] = $true }
    else { Write-Host " FAIL" -ForegroundColor Red; $results["module_check_imports"] = $false; $errors["module_check_imports"] = $r }

    # Summary
    Write-Host ""
    Write-Host "=== Summary ===" -ForegroundColor Cyan
    $passCount = 0
    $totalCount = $results.Count
    foreach ($key in ($results.Keys | Sort-Object)) {
        if ($results[$key]) { $passCount++; Write-Host "  [PASS] $key" -ForegroundColor Green }
        else { Write-Host "  [FAIL] $key" -ForegroundColor Red; if ($errors.ContainsKey($key)) { Write-Host "         $($errors[$key])" -ForegroundColor Gray } }
    }
    Write-Host ""
    if ($passCount -eq $totalCount) { Write-Host "$passCount / $totalCount tests passed" -ForegroundColor Green }
    else { Write-Host "$passCount / $totalCount tests passed" -ForegroundColor Yellow }
} finally {
    $script:proc.Kill()
    $script:proc.Dispose()
}
