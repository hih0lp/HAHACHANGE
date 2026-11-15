import assert from 'node:assert/strict';
import { createServer } from 'vite';
import { chromium } from 'playwright';

const server = await createServer({ server: { host: '127.0.0.1', port: 0 } });
await server.listen();
const base = 'http://127.0.0.1:' + server.httpServer.address().port;
const browser = await chromium.launch({ headless: true, executablePath: process.env.BOBBY_CHROME, args: ['--no-sandbox'] });
try {
  const page = await browser.newPage();
  let jiraTitle = 'Начальная задача';
  let start = '2026-11-16T09:00:00+03:00';
  let branch = 'docs/demo';
  await page.route('**/session/bootstrap', route => route.fulfill({ status: 200, body: '' }));
  await page.route('**/v1/**', route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    const body = request.postDataJSON();
    const respond = (json, status = 200) => route.fulfill({ json, status });
    if (path === '/v1/team-access/session') return respond({ access_token: 'intern-token', role: 'intern' });
    if (path === '/v1/chat/history') return respond({ turns: [] });
    if (path === '/v1/tasks/jira') return respond({ enabled: true, reminders: [{ id: 'SCRUM-1', key: 'SCRUM-1', summary: jiraTitle, due_at: '2026-11-20T17:00:00+03:00' }] });
    if (path === '/v1/calendar/events') return respond({ data: { items: [{ id: 'event-1', summary: 'Встреча', start: { dateTime: start }, end: { dateTime: '2026-11-16T10:00:00+03:00' } }] } });
    if (path === '/v1/tasks/gitlab') return respond({ data: [{ id: 2, name: 'Проект', path_with_namespace: 'demo/project', default_branch: 'main' }] });
    if (path === '/v1/tasks/gitlab/2/branches') return respond({ data: [{ name: branch }] });
    if (path === '/v1/mock-data/jira/issues/SCRUM-1') {
      assert.equal(request.method(), 'PUT');
      assert.equal(request.headers().authorization, 'Bearer intern-token');
      jiraTitle = body.summary;
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
  await jira.getByRole('textbox', { name: 'Название задачи' }).fill('Новая задача');
  await jira.getByRole('button', { name: 'Сохранить' }).click();
  await page.getByText('SCRUM-1 · Новая задача').waitFor();

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
  console.log('Passed Jira, calendar, and GitLab edit dialogs');
} finally {
  await browser.close();
  await server.close();
}
