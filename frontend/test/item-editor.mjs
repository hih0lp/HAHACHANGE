import assert from 'node:assert/strict';
import { createServer } from 'vite';
import { chromium } from 'playwright';
import { browserCoverage } from './browser-coverage.mjs';

const coverage = browserCoverage();
const server = await createServer({ plugins: coverage.plugins, server: { host: '127.0.0.1', port: 0 } });
await server.listen();
const base = 'http://127.0.0.1:' + server.httpServer.address().port;
const browser = await chromium.launch({ headless: true, executablePath: process.env.BOBBY_CHROME, args: ['--no-sandbox'] });
try {
  const page = await browser.newPage();
  await coverage.track(page);
  let jiraTitle = 'Начальная задача';
  let jiraDescription = 'Исходное описание';
  let start = '2026-11-16T09:00:00+03:00';
  let branch = 'docs/demo';
  let role = 'intern';
  let conflict = true;
  const snapshots = {
    jira: { version: 1, data: { project_key: 'SCRUM', issues: [{ key: 'SCRUM-1', summary: 'Исходная задача', status: 'К выполнению', status_category: 'new' }] } },
    'company-calendar': { version: 1, data: { calendar_id: 'company', time_zone: 'Europe/Moscow', events: [] } },
    gitlab: { version: 1, data: { projects: [{ id: 2, name: 'Проект', path_with_namespace: 'demo/project', default_branch: 'main',
      branches: [{ name: 'main', protected: true, commit_id: '' }, { name: 'docs/demo', protected: false, commit_id: '' }],
      commits: [], merge_requests: [] }] } },
  };
  await page.route('**/session/bootstrap', route => route.fulfill({ status: 200, body: '' }));
  await page.route('**/v1/**', route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    const body = request.postDataJSON();
    const respond = (json, status = 200) => route.fulfill({ json, status });
    if (path === '/v1/team-access/session') return respond({ access_token: role + '-token', role });
    if (path === '/v1/demo/role') {
      role = body.role;
      return respond({ role });
    }
    if (path === '/v1/chat/history') return respond({ turns: [] });
    if (path === '/v1/tasks/jira') return respond({ enabled: true, reminders: [{ id: 'SCRUM-1', key: 'SCRUM-1', summary: jiraTitle, due_at: '2026-11-20T17:00:00+03:00' }] });
    if (path === '/v1/calendar/events') return respond({ data: { items: [{ id: 'event-1', summary: 'Встреча', start: { dateTime: start }, end: { dateTime: '2026-11-16T10:00:00+03:00' } }] } });
    if (path === '/v1/tasks/gitlab') return respond({ data: [{ id: 2, name: 'Проект', path_with_namespace: 'demo/project', default_branch: 'main' }] });
    if (path === '/v1/tasks/gitlab/2/branches') return respond({ data: [{ name: branch }] });
    const snapshot = path.match(/^\/v1\/mock-data\/(jira|company-calendar|gitlab)$/);
    if (snapshot) {
      const name = snapshot[1];
      if (request.method() === 'GET') return respond(snapshots[name]);
      assert.equal(request.method(), 'PUT');
      assert.equal(body.version, snapshots[name].version);
      if (name === 'jira' && conflict) {
        conflict = false;
        snapshots.jira.version++;
        return respond({ error: 'Версия изменилась' }, 409);
      }
      snapshots[name] = { version: body.version + 1, data: body.data };
      return respond(snapshots[name]);
    }
    if (path === '/v1/mock-data/jira/issues/SCRUM-1') {
      assert.equal(request.headers().authorization, 'Bearer intern-token');
      if (request.method() === 'GET') return respond({ key: 'SCRUM-1', summary: jiraTitle, description: jiraDescription, status: 'В работе', status_category: 'indeterminate', due_at: '2026-11-20T17:00:00+03:00' });
      assert.equal(request.method(), 'PUT');
      if ('summary' in body) jiraTitle = body.summary;
      if ('description' in body) jiraDescription = body.description;
      return respond({ saved: true, version: 2 });
    }
    if (path === '/v1/mock-data/calendar/events/event-1') {
      assert.equal(request.method(), 'PUT');
      start = body.start;
      return respond({ saved: true, version: 2 });
    }
    if (path === '/v1/mock-data/gitlab/projects/2/branches/' + branch) {
      assert.equal(request.method(), 'PUT');
      branch = body.name;
      return respond({ saved: true, version: 2 });
    }
    return respond({ error: 'unused fixture' }, 404);
  });
  await page.goto(base + '/tasks');
  await page.getByText('SCRUM-1 · Начальная задача').waitFor();
  await page.getByRole('button', { name: 'Изменить · SCRUM-1' }).click();
  const jira = page.getByRole('dialog', { name: 'Задача SCRUM-1' });
  await jira.getByRole('textbox', { name: 'Описание задачи' }).waitFor();
  assert.equal(await jira.getByRole('textbox', { name: 'Описание задачи' }).inputValue(), 'Исходное описание');
  await jira.getByRole('textbox', { name: 'Название задачи' }).fill('Новая задача');
  await jira.getByRole('button', { name: 'Сохранить' }).click();
  await page.getByText('SCRUM-1 · Новая задача').waitFor();
  await page.getByRole('button', { name: 'Изменить · SCRUM-1' }).click();
  const description = page.getByRole('dialog', { name: 'Задача SCRUM-1' }).getByRole('textbox', { name: 'Описание задачи' });
  await description.waitFor();
  assert.equal(await description.inputValue(), 'Исходное описание');
  await description.fill('Сохранённое описание');
  await page.getByRole('dialog', { name: 'Задача SCRUM-1' }).getByRole('button', { name: 'Сохранить' }).click();
  await page.getByRole('button', { name: 'Изменить · SCRUM-1' }).click();
  await description.waitFor();
  assert.equal(await description.inputValue(), 'Сохранённое описание');
  await description.fill('');
  await page.getByRole('dialog', { name: 'Задача SCRUM-1' }).getByRole('button', { name: 'Сохранить' }).click();
  assert.equal(jiraDescription, '');

  await page.getByRole('button', { name: 'Изменить · Встреча' }).click();
  const calendar = page.getByRole('dialog', { name: 'Событие · Встреча' });
  await calendar.getByLabel('Начало (МСК)').fill('2026-11-16T09:30');
  await calendar.getByRole('button', { name: 'Сохранить' }).click();
  await page.getByText(/09:30 МСК/).waitFor();

  await page.getByRole('button', { name: 'Переименовать ветку · Проект' }).click();
  const gitlab = page.getByRole('dialog', { name: 'Ветки · Проект' });
  await gitlab.getByRole('textbox', { name: 'Новое название' }).fill('docs/new');
  await gitlab.getByRole('button', { name: 'Переименовать' }).click();
  await gitlab.waitFor({ state: 'hidden' });
  assert.equal(branch, 'docs/new');

  await page.getByRole('button', { name: 'Открыть профиль' }).click();
  const profile = page.getByRole('dialog', { name: 'Профиль' });
  await profile.getByRole('button', { name: 'Наставник' }).click();
  await profile.getByRole('button', { name: 'Наставник', pressed: true }).waitFor();
  await profile.getByRole('button', { name: 'Закрыть' }).click();

  await page.getByRole('button', { name: 'Редактировать весь список · Jira' }).click();
  const issues = page.getByRole('dialog', { name: 'Редактирование · Jira' });
  const addIssue = async () => {
    await issues.getByRole('button', { name: 'Добавить задачу' }).click();
    assert.equal(await issues.getByLabel('Ключ задачи').inputValue(), 'SCRUM-2');
    await issues.getByLabel('Название задачи').fill('Учебная задача');
    await issues.getByLabel('Описание', { exact: true }).fill('Проверить форму');
    await issues.getByLabel('Статус').selectOption('indeterminate');
    await issues.getByLabel('Исполнитель').selectOption('demo-intern');
    await issues.getByLabel('Дата начала').fill('2026-11-16');
    await issues.getByLabel('Срок (дата)').fill('2026-11-21');
    await issues.getByLabel('Точное время срока (МСК)').fill('2026-11-21T17:00');
    await issues.getByRole('button', { name: 'Сохранить изменения' }).click();
  };
  await addIssue();
  await issues.getByText('Версия изменилась').waitFor();
  page.once('dialog', alert => alert.accept());
  await issues.getByRole('button', { name: 'Загрузить заново' }).click();
  await issues.getByLabel('Ключ задачи').waitFor();
  await addIssue();
  await issues.getByText('Изменения сохранены').waitFor();
  assert.deepEqual(snapshots.jira.data.issues[1], {
    key: 'SCRUM-2', summary: 'Учебная задача', description: 'Проверить форму', status: 'В работе',
    status_category: 'indeterminate', assignee_account_id: 'demo-intern', assignee_name: 'Стажёр 1',
    start_date: '2026-11-16', due_date: '2026-11-21', due_at: '2026-11-21T17:00:00+03:00',
  });
  await issues.getByRole('button', { name: 'Закрыть' }).click();

  await page.getByRole('button', { name: 'Редактировать календарь компании' }).click();
  const events = page.getByRole('dialog', { name: 'Редактирование · Календарь компании' });
  await events.getByRole('button', { name: 'Добавить событие' }).click();
  await events.getByLabel('Название события').fill('Открытый микрофон');
  await events.getByLabel('Описание').fill('Вопросы стажёров');
  await events.getByLabel('Начало (МСК)').fill('2026-11-18T15:00');
  await events.getByLabel('Окончание (МСК)').fill('2026-11-18T16:00');
  await events.getByLabel('Статус события').selectOption('tentative');
  await events.getByRole('button', { name: 'Сохранить изменения' }).click();
  await events.getByText('Изменения сохранены').waitFor();
  assert.equal(snapshots['company-calendar'].data.events[0].start.dateTime, '2026-11-18T15:00:00+03:00');
  await events.getByRole('button', { name: 'Закрыть' }).click();

  await page.getByRole('button', { name: 'Редактировать весь список · GitLab' }).click();
  const projects = page.getByRole('dialog', { name: 'Редактирование · GitLab' });
  await projects.getByLabel('Название проекта').fill('Проект стажировки');
  await projects.getByLabel('Описание', { exact: true }).fill('Учебный репозиторий');
  await projects.getByLabel('Кому доступен проект').selectOption('intern');
  await projects.getByLabel('Раздел GitLab').selectOption('branches');
  page.once('dialog', confirm => {
    page.once('dialog', alert => {
      assert.match(alert.message(), /смените основную ветку/);
      return alert.accept();
    });
    return confirm.accept();
  });
  await projects.getByRole('button', { name: 'Удалить ветку' }).click();
  await projects.getByLabel('Ветка', { exact: true }).selectOption('1');
  await projects.getByLabel('Имя ветки').fill('docs/review');
  await projects.getByLabel('Защищённая ветка').check();
  await projects.getByLabel('Раздел GitLab').selectOption('commits');
  await projects.getByRole('button', { name: 'Добавить коммит' }).click();
  await projects.getByLabel('Название коммита').fill('Add guide');
  await projects.getByLabel('Сообщение коммита').fill('Onboarding notes');
  await projects.getByLabel('Автор').fill('Стажёр');
  await projects.getByLabel('Дата коммита (МСК)').fill('2026-11-18T15:00');
  await projects.getByLabel('Раздел GitLab').selectOption('merge-requests');
  await projects.getByRole('button', { name: 'Добавить merge request' }).click();
  await projects.getByLabel('Название merge request').fill('Guide review');
  await projects.getByLabel('Описание', { exact: true }).fill('Проверить документацию');
  await projects.getByLabel('Исходная ветка').selectOption('docs/review');
  await projects.getByLabel('Статус merge request').selectOption('opened');
  await projects.getByLabel('Ревьюер').fill('Наставник');
  await projects.getByLabel('Черновик').check();
  await projects.getByLabel('Одобрен').check();
  await projects.getByRole('button', { name: 'Сохранить изменения' }).click();
  await projects.getByText('Изменения сохранены').waitFor();
  assert.equal(snapshots.gitlab.data.projects[0].merge_requests[0].source_branch, 'docs/review');
  assert.equal(snapshots.gitlab.data.projects[0].merge_requests[0].approved, true);
  await projects.getByRole('button', { name: 'Закрыть' }).click();
  console.log('Passed Jira, calendar, and GitLab edit dialogs');
  await coverage.capture(page);
  await coverage.write(server);
} finally {
  await browser.close();
  await server.close();
}
