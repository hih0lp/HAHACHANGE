require 'securerandom'
project = Project.find_by_full_path('root/bobby-team-directory-demo') or raise 'demo project missing'
accounts = {
  'intern' => ['Стажёр', 30],
  'intern2' => ['Второй стажёр', 30],
  'approver' => ['Наставник', 40],
  'catalog.lead' => ['Алексей Морозов', 30],
  'catalog.mentor' => ['Вера Соколова', 30],
  'catalog.dev' => ['Ирина Белова', 30],
  'bobby-search-lead' => ['Павел Орлов', 30],
  'bobby-search-mentor' => ['Мария Котова', 30],
  'bobby-search-analyst' => ['Денис Романов', 30],
  'bobby-platform-lead' => ['Ольга Крылова', 30],
  'bobby-platform-mentor' => ['Сергей Лебедев', 30],
  'bobby-platform-dev' => ['Наталья Волкова', 30]
}
credentials = []
initialize_credentials = !File.exist?('/tmp/bobby-demo-credentials.txt')
accounts.each do |username, (name, access)|
  user = User.find_by_username(username)
  unless user
    user = User.new(username: username, name: name, email: "#{username.tr('.', '-')}@bobby-demo.invalid")
    user.build_namespace(name: name, path: username, organization_id: project.organization_id)
    user.skip_confirmation!
  end
  if initialize_credentials
    password = SecureRandom.base58(24)
    user.password = password
    user.password_confirmation = password
    credentials << "#{username}: #{password}"
  end
  user.save!
  project.add_member(user, access) unless project.team.max_member_access(user.id) >= access
end
if credentials.any?
  path = '/tmp/bobby-demo-credentials.txt'
  File.open(path, 'w', 0600) { |file| file.puts(credentials) }
  File.chmod(0600, path)
end
puts "users=#{accounts.size}, credentials_created=#{credentials.size}"
