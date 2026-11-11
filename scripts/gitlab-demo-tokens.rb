require 'json'
require 'date'
map = {
  'intern' => 'intern', 'intern2' => 'intern2', 'approver' => 'approver',
  'catalog-lead' => 'catalog.lead', 'catalog-mentor' => 'catalog.mentor', 'catalog-dev' => 'catalog.dev',
  'search-lead' => 'bobby-search-lead', 'search-mentor' => 'bobby-search-mentor', 'search-analyst' => 'bobby-search-analyst',
  'platform-lead' => 'bobby-platform-lead', 'platform-mentor' => 'bobby-platform-mentor', 'platform-dev' => 'bobby-platform-dev'
}
path = '/tmp/bobby-demo-tokens.json'
unless File.exist?(path)
  tokens = {}
  map.each do |directory_id, username|
    user = User.find_by_username(username) or raise "missing #{username}"
    pat = user.personal_access_tokens.create!(name: 'bobby-demo-mcp', scopes: [:api], expires_at: Date.today + 30)
    tokens[directory_id] = pat.token
  end
  File.open(path, 'w', 0600) { |file| file.write(JSON.generate(tokens)) }
  File.chmod(0600, path)
end
puts "token_subjects=#{map.size}"
