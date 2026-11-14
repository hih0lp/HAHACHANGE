import assert from 'node:assert/strict';
import { mkdir } from 'node:fs/promises';
import { join } from 'node:path';
import { createServer } from 'vite';
import { chromium } from 'playwright';

const server = await createServer({ server: { host: '127.0.0.1', port: 0 } });
await server.listen();
const { port } = server.httpServer.address();
const base = 'http://127.0.0.1:' + port;
const output = process.env.BOBBY_SCREENSHOTS || 'test/screenshots';
let browser;
const baseScenario = {
  scenario_id: 'scenario-1', revision: 1, goal: 'Первая задача → PR', status: 'needs_input',
  message: 'Для первой задачи нужен доступ к Jira проекта.',
  checks: [{ resource_id: 'jira-demo', capability: 'browse_issues', state: 'missing', checked_at: '2026-11-13T09:30:00Z' }],
  widget: { visible: true, type: 'access_request_form', resource: { id: 'jira-demo', name: 'Jira проекта', url: 'https://example.atlassian.net', capabilities: ['browse_issues'] } },
};
const access = {
  id: 'access-request', title: 'Заявка на доступ', kind: 'access', version: 1, description: 'Запросить право на доступный ресурс',
  schema: { properties: { resource_id: { type: 'string' }, capability: { type: 'string' }, reason: { type: 'string', minLength: 2 } }, required: ['resource_id', 'capability', 'reason'] },
  ui_schema: { labels: { resource_id: 'Ресурс', capability: 'Право', reason: 'Зачем нужен доступ' } },
};
const answer = '### Ваш первый шаг\nПосмотрите задачи проекта в [Jira](https://example.atlassian.net/browse/DEMO-5).\nСверьте рабочий процесс с [Playbook](https://github.com/avito-tech/playbook).\n[Заполнить заявку](/forms/access-request)';

async function fixtures(page) {
  let submits = 0;
  let committed;
  let serial = 0;
  let role = 'intern';
  const sessions = [];
  let loginRequests = 0;
  const jobs = [];
  const keys = [];
  await page.route('**/session/bootstrap', route => route.fulfill({ status: 200, body: '' }));
  await page.routeWebSocket('**/v1/jobs/*/ws*', socket => {
    const id = new URL(socket.url()).pathname.split('/')[3];
    const job = jobs.find(job => job.id === id);
    if (job?.query.includes('Начать')) {
      socket.send(JSON.stringify({ ...job, status: 'running' }));
      setTimeout(() => socket.send(JSON.stringify(job)), 100);
    } else socket.close();
  });
  await page.route('**/v1/**', async route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    const body = request.postDataJSON();
    const respond = (json, status = 200) => route.fulfill({ json, status });
    if (path === '/v1/demo/role') { role = body.role; return respond({ role }); }
    if (path === '/v1/team-access/session') { sessions.push(role); return respond({ access_token: 'demo-' + role, role, expires_in: 3600 }); }
    if (path === '/v1/scenarios/login') { loginRequests++; return respond({ error: 'Unexpected login' }, 500); }
    if (path === '/v1/chat/history') return respond({ turns: [...jobs].reverse() });
    if (path === '/v1/deadline-notifications') return respond({ notifications: [] });
    if (path === '/v1/jobs' && body) {
      const job = { id: String(++serial), query: body.query, status: 'completed', created_at: '2026-11-13T10:00:00Z', answer };
      if (body.query === 'Пустой ответ') job.answer = '';
      if (body.query === 'Проверка тайм-аута') Object.assign(job, { status: 'failed', answer: '', error: 'context deadline exceeded' });
      jobs.push(job);
      return respond({ ...job, status: 'queued', answer: undefined }, 202);
    }
    if (path.startsWith('/v1/jobs/')) return respond(jobs.find(job => job.id === path.split('/').at(-1)));
    if (path === '/v1/forms') {
      assert.equal(request.headers().authorization, 'Bearer demo-' + role);
      return respond({ forms: [access] });
    }
    if (path === '/v1/forms/resources') return respond({ resources: [baseScenario.widget.resource] });
    if (path === '/v1/forms/access-request') return respond(access);
    if (path.endsWith('/validate')) return respond(body.values.reason.length < 5 ? { error: 'invalid form values' } : { valid: true }, body.values.reason.length < 5 ? 400 : 200);
    if (path.endsWith('/submit')) {
      const key = request.headers()['idempotency-key'];
      keys.push(key);
      assert.equal(body.confirmed, true);
      if (!committed) { committed = { id: 'request-42', status: 'pending' }; submits++; return route.abort('connectionreset'); }
      assert.equal(key, keys[0]);
      return respond(committed, 201);
    }
    if (path === '/v1/tasks/jira') return respond({ enabled: true, reminders: [{ id: 'demo-5', key: 'DEMO-5', summary: 'Настроить окружение', due_at: '2026-11-14T00:00:00+03:00', url: 'https://example.atlassian.net/browse/DEMO-5' }] });
    if (path === '/v1/calendar/reminders') return respond({ data: { items: [{ id: 'meeting', summary: 'Встреча с наставником', start: { dateTime: '2026-11-13T11:00:00Z' }, htmlLink: 'https://calendar.google.com/' }] } });
    return respond({ error: 'unknown fixture: ' + path }, 404);
  });
  return { keys, sessions, submits: () => submits, loginRequests: () => loginRequests };
}

async function navigate(page, label) {
  const menu = page.getByRole('button', { name: 'Открыть меню' });
  const mobile = await menu.isVisible();
  if (mobile) await menu.click();
  const scope = mobile ? page.getByRole('dialog', { name: 'Разделы' }) : page.getByRole('complementary', { name: 'Разделы' });
  await scope.getByRole('link', { name: label, exact: true }).click();
}

async function assertFits(page, width) {
  const overflows = await page.evaluate(() => [...document.querySelectorAll('.main-panel, .view, .chat-scroll, .section-page, .dialog-body')]
    .filter(el => el.scrollWidth > el.clientWidth + 2).map(el => el.className));
  assert.deepEqual(overflows, [], 'horizontal overflow at ' + width);
  assert.equal(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), true);
}

async function shot(page, label, width) {
  await assertFits(page, width);
  await page.screenshot({ path: join(output, label + '-' + width + '.png'), animations: 'disabled' });
}

try {
  await mkdir(output, { recursive: true });
  const launch = { headless: true, args: ['--no-sandbox'] };
  if (process.env.BOBBY_CHROME) launch.executablePath = process.env.BOBBY_CHROME;
  browser = await chromium.launch(launch);
  for (const width of [360, 768, 1440]) {
    const context = await browser.newContext({ viewport: { width, height: width === 360 ? 780 : 900 } });
    const page = await context.newPage();
    page.setDefaultTimeout(8_000);
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    const form = await fixtures(page);
    await page.goto(base);
    await page.getByRole('heading', { name: 'Привет, я Бобби!' }).waitFor();
    await shot(page, 'welcome', width);

    const profile = page.getByRole('button', { name: 'Открыть профиль' });
    await profile.click();
    const dialog = page.getByRole('dialog', { name: 'Профиль' });
    await dialog.waitFor();
    await page.keyboard.press('Escape');
    await dialog.waitFor({ state: 'hidden' });
    assert.equal(await profile.evaluate(element => document.activeElement === element), true);
    await profile.click();
    assert.equal(await dialog.getByLabel('Логин').count(), 0);
    assert.equal(await dialog.getByLabel('Пароль').count(), 0);
    for (const [label, role] of [['Наставник', 'mentor'], ['Второй стажёр', 'intern2'], ['Стажёр', 'intern']]) {
      const newSession = page.waitForResponse(response =>
        response.url().endsWith('/v1/team-access/session') && response.ok());
      const button = dialog.getByRole('button', { name: label, exact: true });
      await button.click();
      assert.equal((await (await newSession).json()).access_token, 'demo-' + role);
      await dialog.getByRole('button', { name: label, exact: true, pressed: true }).waitFor();
    }
    assert.deepEqual(form.sessions.slice(0, 4), ['intern', 'mentor', 'intern2', 'intern']);
    assert.equal(form.loginRequests(), 0);
    await page.keyboard.press('Escape');
    await page.reload();
    await page.getByRole('heading', { name: 'Привет, я Бобби!' }).waitFor();
    assert.equal(await page.getByRole('link', { name: 'Адаптация', includeHidden: true }).count(), 0);

    await page.getByRole('button', { name: 'С чего начать?' }).click();
    await page.getByRole('link', { name: 'Данные Jira Jira' }).waitFor();
    await page.getByRole('link', { name: 'Материалы Playbook' }).waitFor();
    await shot(page, 'answer', width);
    const formTrigger = page.getByRole('button', { name: 'Заполнить заявку' });
    await formTrigger.click();
    await page.getByLabel('Ресурс').selectOption('jira-demo');
    await page.getByLabel('Право').selectOption('browse_issues');
    await page.getByLabel('Зачем нужен доступ').fill('Нет');
    await page.getByRole('button', { name: 'Проверить заявку' }).click();
    await page.getByText('Не удалось проверить форму', { exact: false }).waitFor();
    await page.getByLabel('Зачем нужен доступ').fill('Доступ для первой задачи');
    await page.getByRole('button', { name: 'Проверить заявку' }).click();
    await page.getByRole('heading', { name: 'Проверьте перед отправкой' }).waitFor();
    await shot(page, 'confirm', width);
    const confirm = page.getByRole('button', { name: 'Подтвердить отправку' });
    await confirm.evaluate(element => { element.click(); element.click(); });
    await page.getByText('Нет связи с Бобби', { exact: false }).waitFor();
    assert.equal(form.keys.length, 1);
    if (width === 360) await page.reload();
    await confirm.click();
    await page.getByText('Номер заявки request-42').waitFor();
    assert.deepEqual(form.keys, [form.keys[0], form.keys[0]]);
    assert.equal(form.submits(), 1);
    await shot(page, 'receipt', width);
    await page.getByRole('button', { name: 'Закрыть форму' }).click();
    await page.getByRole('heading', { name: 'Заявка на доступ' }).waitFor({ state: 'hidden' });
    if (width !== 360) assert.equal(await formTrigger.evaluate(element => document.activeElement === element), true);

    const input = page.getByRole('textbox', { name: 'Ваш вопрос' });
    await input.fill('Пустой ответ');
    await input.press('Enter');
    await page.getByText('Ответ пуст.', { exact: false }).waitFor();
    await input.fill('Проверка тайм-аута');
    await input.press('Enter');
    await page.getByText('Время ожидания ответа истекло.').waitFor();

    await navigate(page, 'Мои задачи');
    await page.getByText('DEMO-5 · Настроить окружение').waitFor();
    if (await page.getByRole('heading', { name: 'GitLab' }).count()) {
      throw new Error('Tasks must only show Jira and Calendar');
    }
    await page.getByText('Встреча с наставником').waitFor();
    await page.getByText(/14:00 МСК/).waitFor();
    await shot(page, 'tasks', width);
    assert.deepEqual(errors, []);
    await context.close();
    console.log('Passed demo role, chat, form, and tasks checks at ' + width + ' px');
  }
} finally {
  await browser?.close();
  await server.close();
}
