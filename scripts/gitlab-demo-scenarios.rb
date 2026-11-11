require 'json'
require 'net/http'
base = URI('http://127.0.0.1:18929')
tokens = JSON.parse(File.read('/tmp/bobby-demo-tokens.json'))
project = Project.find_by_full_path('root/bobby-team-directory-demo') or raise 'project missing'

def api(base, token, method, path, body = nil)
  uri = base + path
  request = {get: Net::HTTP::Get, post: Net::HTTP::Post, put: Net::HTTP::Put}[method].new(uri)
  request['PRIVATE-TOKEN'] = token
  if body
    request['Content-Type'] = 'application/json'
    request.body = JSON.generate(body)
  end
  response = Net::HTTP.start(uri.host, uri.port) { |http| http.request(request) }
  raise "#{method} #{path}: HTTP #{response.code}" unless response.code.to_i.between?(200, 299)
  JSON.parse(response.body)
end

scenarios = [
  {subject: 'intern', branch: 'demo/intern-member-visibility-v2', title: 'Check team membership visibility', file: 'docs/team-membership-check.md', text: "# Team membership check\n\nVerify that the intern sees only their own team.\n", reviewer: 'approver', assignee: 'intern', draft: false},
  {subject: 'intern2', branch: 'demo/intern2-mentor-handoff-v2', title: 'Draft: Prepare mentor handoff', file: 'docs/mentor-handoff-v2.md', text: "# Mentor handoff\n\nDraft a checklist for the first review conversation.\n", reviewer: 'approver', assignee: 'intern2', draft: true},
  {subject: 'catalog-dev', branch: 'demo/catalog-access-rule', title: 'Clarify catalogue access rule', file: 'docs/catalog-access-rule.md', text: "# Catalogue access\n\nDocument who can see the catalogue during onboarding.\n", reviewer: 'catalog.mentor', assignee: 'catalog.dev', draft: false},
  {subject: 'search-analyst', branch: 'demo/search-feedback-check', title: 'Add search feedback checklist', file: 'docs/search-feedback-check.md', text: "# Search feedback\n\nCheck relevance feedback with the mentor.\n", reviewer: 'bobby-search-mentor', assignee: 'bobby-search-analyst', draft: false},
  {subject: 'platform-dev', branch: 'demo/platform-handoff-draft', title: 'Draft: Platform onboarding handoff', file: 'docs/platform-handoff-draft.md', text: "# Platform handoff\n\nPrepare the initial handoff notes.\n", reviewer: 'bobby-platform-mentor', assignee: 'bobby-platform-dev', draft: true}
]

scenarios.each do |scenario|
  token = tokens.fetch(scenario[:subject])
  reviewer = User.find_by_username(scenario[:reviewer]) or raise "reviewer #{scenario[:reviewer]} missing"
  assignee = User.find_by_username(scenario[:assignee]) or raise "assignee #{scenario[:assignee]} missing"
  next if project.merge_requests.where(source_branch: scenario[:branch]).exists?
  api(base, token, :post, '/api/v4/projects/2/repository/commits', {
    branch: scenario[:branch], start_branch: project.default_branch,
    commit_message: "Add #{scenario[:file]}",
    actions: [{action: 'create', file_path: scenario[:file], content: scenario[:text]}]
  })
  mr = api(base, token, :post, '/api/v4/projects/2/merge_requests', {
    source_branch: scenario[:branch], target_branch: project.default_branch,
    title: scenario[:title], reviewer_ids: [reviewer.id], assignee_id: assignee.id
  })
  puts "created MR !#{mr.fetch('iid')} author=#{scenario[:subject]} reviewer=#{scenario[:reviewer]} draft=#{scenario[:draft]}"
end
