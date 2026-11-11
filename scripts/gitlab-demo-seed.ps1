param(
    [Parameter(Mandatory)][string]$TokenFile,
    [uri]$Origin = 'http://127.0.0.1:18929'
)

$ErrorActionPreference = 'Stop'
if (-not $Origin.IsLoopback -or $Origin.AbsolutePath -ne '/' -or $Origin.UserInfo -or $Origin.Query -or $Origin.Fragment) {
    throw 'The seed script requires a loopback GitLab origin.'
}
$token = (Get-Content -LiteralPath $TokenFile -Raw).Trim()
if (-not $token) { throw 'The demo provisioning token file is empty.' }
$api = $Origin.GetLeftPart([System.UriPartial]::Authority) + '/api/v4'
$headers = @{ 'PRIVATE-TOKEN' = $token }

function Send-GitLab($route, $body) {
    Invoke-RestMethod -Method Post -Uri "$api$route" -Headers $headers -MaximumRedirection 0 -ContentType 'application/json; charset=utf-8' -Body ($body | ConvertTo-Json -Depth 10 -Compress)
}

$sourceRoot = Split-Path -Parent $PSScriptRoot
$files = @('internal/teamaccess/membership.go', 'cmd/team-access/main.go')
$actions = foreach ($file in $files) {
    $source = git -C $sourceRoot show "HEAD:$file"
    if ($LASTEXITCODE -ne 0) { throw "Cannot read tracked demo source: $file" }
    @{ action = 'create'; file_path = $file; content = ($source -join [Environment]::NewLine) }
}
$project = Send-GitLab '/projects' @{
    name = 'Bobby Team Directory Demo'
    path = 'bobby-team-directory-demo'
    visibility = 'private'
    initialize_with_readme = $true
    description = 'Training fixture based on the Bobby team directory service.'
}
$base = "/projects/$($project.id)"
$branch = $project.default_branch
$null = Send-GitLab "$base/repository/commits" @{
    branch = $branch; commit_message = 'Add team directory source examples'; actions = @($actions)
}
$examples = @(
    @{ branch = 'demo/member-visibility'; title = 'Document team membership visibility'; path = 'docs/member-visibility.md'; content = 'Verify that a member can read their team and cannot read another team.' },
    @{ branch = 'demo/mentor-handoff'; title = 'Draft: Document mentor handoff'; path = 'docs/mentor-handoff.md'; content = 'Describe the demo mentor handoff and identify unanswered questions.' }
)
foreach ($example in $examples) {
    $null = Send-GitLab "$base/repository/commits" @{
        branch = $example.branch; start_branch = $branch; commit_message = $example.title
        actions = @(@{ action = 'create'; file_path = $example.path; content = $example.content })
    }
    $mr = Send-GitLab "$base/merge_requests" @{
        source_branch = $example.branch; target_branch = $branch; title = $example.title
    }
    [pscustomobject]@{ ProjectID = $project.id; MergeRequest = $mr.iid; Title = $mr.title }
}
