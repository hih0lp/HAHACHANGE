param([string]$BaseUrl = 'http://127.0.0.1:18080')
$ErrorActionPreference = 'Stop'
$values = @{}
Get-Content (Join-Path $PSScriptRoot '..\.env') | ForEach-Object {
    if ($_ -match '^([A-Z0-9_]+)=(.*)$') { $values[$matches[1]] = $matches[2] }
}
function Login([string]$username, [string]$envName) {
    $body = @{ username = $username; password = $values[$envName] } | ConvertTo-Json -Compress
    (Invoke-RestMethod "$BaseUrl/v1/auth/login" -Method Post -ContentType 'application/json' -Body $body).access_token
}
function Call([string]$token, [string]$method, [string]$path, $body = $null, [string]$idem = '') {
    $headers = @{ Authorization = "Bearer $token" }
    if ($idem) { $headers['Idempotency-Key'] = $idem }
    $args = @{ Uri = "$BaseUrl$path"; Method = $method; Headers = $headers }
    if ($null -ne $body) { $args.ContentType = 'application/json'; $args.Body = ($body | ConvertTo-Json -Depth 8 -Compress) }
    Invoke-RestMethod @args
}
function Assert($ok, [string]$message) { if (-not $ok) { throw $message } }
$intern = Login 'intern' 'TEAM_ACCESS_INTERN_PASSWORD'
$other = Login 'intern2' 'TEAM_ACCESS_INTERN2_PASSWORD'
$approver = Login 'approver' 'TEAM_ACCESS_APPROVER_PASSWORD'
Assert ((Call $intern 'GET' '/v1/me').id -eq 'intern') 'profile identity'
Assert ((Call $other 'GET' '/v1/me').id -eq 'intern2') 'second profile identity'
$resources = (Call $intern 'GET' '/v1/teams/demo-team/resources').resources
Assert (($resources | Where-Object id -eq 'demo-jira').Count -eq 1) 'resource catalog'
$check = @{ resource_id = 'demo-jira'; capability = 'browse_issues' }
$before = Call $intern 'POST' '/v1/access/check' $check
Assert ($before.state -in @('missing','granted')) 'initial check'
if ($before.state -eq 'missing') {
    $input = @{ resource_id = 'demo-jira'; capability = 'browse_issues'; reason = 'Требуется работа с учебной задачей'; form_values = @{}; confirmed = $true }
    $key = [guid]::NewGuid().ToString()
    $created = Call $intern 'POST' '/v1/access-requests' $input $key
    Assert ($created.status -eq 'pending') 'pending request'
    $again = Call $intern 'POST' '/v1/access-requests' $input $key
    Assert ($created.id -eq $again.id) 'idempotent replay'
    Assert ((Call $intern 'POST' '/v1/access/check' $check).state -eq 'pending') 'pending check'
    $foreignVisible = $true
    try { Call $other 'GET' "/v1/access-requests/$($created.id)" | Out-Null } catch { $foreignVisible = $false }
    Assert (-not $foreignVisible) 'cross-user request isolation'
    $selfApproved = $true
    try { Call $intern 'POST' "/v1/access-requests/$($created.id)/decision" @{ decision = 'approve' } | Out-Null } catch { $selfApproved = $false }
    Assert (-not $selfApproved) 'self approval forbidden'
    $approved = Call $approver 'POST' "/v1/access-requests/$($created.id)/decision" @{ decision = 'approve'; comment = 'Учебная заявка проверена' }
    Assert ($approved.status -eq 'approved') 'approval transition'
}
Assert ((Call $intern 'POST' '/v1/access/check' $check).state -eq 'granted') 'grant after approval'
Assert ((Call $other 'POST' '/v1/access/check' $check).state -eq 'missing') 'grant isolation'
$rejectInput = @{ resource_id = 'demo-jira'; capability = 'browse_issues'; reason = 'Проверка отказа'; form_values = @{}; confirmed = $true }
$rejectedRequest = Call $other 'POST' '/v1/access-requests' $rejectInput ([guid]::NewGuid().ToString())
$rejected = Call $approver 'POST' "/v1/access-requests/$($rejectedRequest.id)/decision" @{ decision = 'reject'; comment = 'Недостаточно обоснования' }
Assert ($rejected.status -eq 'rejected') 'rejection transition'
Assert ((Call $other 'POST' '/v1/access/check' $check).state -eq 'missing') 'rejection does not grant'
Write-Output 'team-access smoke: PASS (identity, catalog, pending, isolation, approval, rejection, grant)'
