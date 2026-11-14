import assert from 'node:assert/strict';
import { mkdir, mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { createServer } from 'vite';
import { chromium } from 'playwright';

const cacheDir = await mkdtemp(join(tmpdir(), 'bobby-vite-'));
const server = await createServer({ cacheDir, server: { host: '127.0.0.1', port: 0 } });
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
    } else if (job) socket.send(JSON.stringify(job));
    else socket.close();
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
      assert.equal(body.thread_id, undefined);
      const job = { id: String(++serial), query: body.query, status: 'completed', created_at: '2026-11-13T10:00:00Z', answer };
      if (body.query === 'Проверка длинного ответа') job.answer = '### Начало длинного ответа\n' + 'Длинный абзац для проверки прокрутки.\n'.repeat(60);
      if (body.query === 'Пустой ответ') job.answer = '';
      if (body.query === 'Проверка тайм-аута') Object.assign(job, { status: 'failed', answer: '', error: 'context deadline exceeded' });
      jobs.push(job);
      return respond({ ...job, status: 'queued', answer: undefined }, 202);
    }
    if (path.startsWith('/v1/jobs/')) assert.fail('Job updates must use WebSocket');
    if (path === '/v1/forms') {
      assert.equal(request.headers().authorization, 'Bearer demo-' + role);
      return respond({ forms: [access] });
    }
    if (path === '/v1/form-resources') return respond({ resources: [baseScenario.widget.resource] });
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

async function selectText(locator) {
  await locator.scrollIntoViewIfNeeded();
  return locator.evaluate(element => {
    const doc = element.ownerDocument;
    const selection = doc.getSelection();
    const range = doc.createRange();
    range.selectNodeContents(element);
    selection.removeAllRanges();
    selection.addRange(range);
    return selection.toString().trim();
  });
}

try {
  await mkdir(output, { recursive: true });
  const launch = { headless: true, args: ['--no-sandbox'] };
  if (process.env.BOBBY_CHROME) launch.executablePath = process.env.BOBBY_CHROME;
  browser = await chromium.launch(launch);
  {
    const page = await browser.newPage();
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await fixtures(page);
    await page.addInitScript(() => {
      window.SpeechRecognition = class {
        start() { window.currentRecognition = this; }
        stop() { this.onend(); }
        abort() { this.onend(); }
      };
    });
    const finishDictation = async text => {
      await page.evaluate(transcript => {
        const recognition = window.currentRecognition;
        if (recognition.lang !== 'ru-RU') throw new Error('Wrong dictation language');
        recognition.onresult({ results: [[{ transcript }]] });
        recognition.onend();
      }, text);
    };
    await page.goto(base);
    const trigger = page.getByRole('button', { name: 'Мини-чат с Бобби' });
    if (await page.evaluate(() => !!window.documentPictureInPicture)) {
      await trigger.click();
      await page.waitForFunction(() => window.documentPictureInPicture.window?.document.querySelector('textarea'));
      assert.equal(await page.evaluate(() => {
        const floating = window.documentPictureInPicture.window;
        const pet = floating.document.querySelector('.bobby-widget-pet');
        return pet.dataset.bobbyState === 'idle' && floating.getComputedStyle(pet).width === '64px';
      }), true);
      await page.evaluate(() => window.documentPictureInPicture.window.document.querySelector('.mic-button').click());
      await finishDictation('Привет');
      await page.waitForFunction(() => window.documentPictureInPicture.window.document.querySelector('textarea').value === 'Привет');
      await page.evaluate(() => {
        const floating = window.documentPictureInPicture.window;
        const input = floating.document.querySelector('textarea');
        Object.getOwnPropertyDescriptor(floating.HTMLTextAreaElement.prototype, 'value').set.call(input, 'Бобби, ты здесь?');
        input.dispatchEvent(new floating.Event('input', { bubbles: true }));
      });
      await page.waitForFunction(() => document.querySelector('.chat-page textarea').value === 'Бобби, ты здесь?');
      await page.evaluate(() => window.documentPictureInPicture.window.document.querySelector('.send-button').click());
      await page.waitForFunction(() => window.documentPictureInPicture.window?.document.querySelector('.answer h3')?.textContent === 'Ваш первый шаг');
      assert.equal(await page.locator('.chat-page .user-message p').textContent(), 'Бобби, ты здесь?');
      await page.evaluate(() => {
        const doc = window.documentPictureInPicture.window.document;
        const heading = doc.querySelector('.answer h3');
        heading.scrollIntoView();
        const range = doc.createRange();
        range.selectNodeContents(heading);
        doc.getSelection().removeAllRanges();
        doc.getSelection().addRange(range);
      });
      await page.waitForFunction(() => window.documentPictureInPicture.window.document.querySelector('.selection-actions'));
      await page.evaluate(() => window.documentPictureInPicture.window.document.querySelector('.selection-actions button').click());
      await page.locator('.chat-page .reply-context').getByText('Ваш первый шаг').waitFor();
      await page.locator('.chat-page').getByRole('button', { name: 'Убрать цитату' }).click();
      await page.evaluate(() => window.documentPictureInPicture.window.document.querySelector('[aria-label="Свернуть мини-чат"]').click());
      await page.waitForFunction(() => {
        const floating = window.documentPictureInPicture.window;
        return floating.document.querySelector('.bobby-widget-only-pet [data-bobby-state="idle"]') && floating.document.querySelector('.bobby-widget-compose').hidden;
      });
      await page.evaluate(() => window.documentPictureInPicture.window.document.querySelector('.bobby-widget-only-pet').click());
      await page.waitForFunction(() => {
        const floating = window.documentPictureInPicture.window;
        return !floating.document.querySelector('.bobby-widget-compose').hidden;
      });
      await page.evaluate(() => window.documentPictureInPicture.window.close());
      await page.waitForFunction(() => !document.querySelector('.widget-trigger[aria-pressed="true"]'));
      console.log('Passed native picture-in-picture send, reply, and close checks');
    }
    await page.addInitScript(() => Object.defineProperty(window, 'documentPictureInPicture', { value: undefined }));
    await page.reload();
    await page.locator('.chat-page').getByRole('button', { name: 'Голосовой ввод' }).click();
    await finishDictation('Черновик из чата');
    assert.equal(await page.locator('.chat-page textarea').inputValue(), 'Черновик из чата');
    await trigger.click();
    const widget = page.getByRole('region', { name: 'Мини-чат с Бобби' });
    await widget.waitFor();
    assert.equal(await widget.locator('.bobby-widget-fallback').count(), 0);
    const input = widget.getByRole('textbox', { name: 'Ваш вопрос' });
    assert.equal(await input.evaluate(element => document.activeElement === element), true);
    await input.fill('Начать работу через мини-чат');
    await widget.getByText('Слушаю').waitFor();
    assert.equal(await page.locator('.chat-page textarea').inputValue(), 'Начать работу через мини-чат');
    await widget.getByRole('button', { name: 'Свернуть мини-чат' }).click();
    const pet = widget.getByRole('button', { name: 'Развернуть мини-чат' });
    await pet.locator('[data-bobby-state="idle"]').waitFor();
    assert.equal(await widget.evaluate(element => getComputedStyle(element).backgroundColor), 'rgba(0, 0, 0, 0)');
    assert.equal(await widget.evaluate(element => getComputedStyle(element).boxShadow), 'none');
    assert.equal(await input.isVisible(), false);
    await widget.getByRole('button', { name: 'Голосовой ввод' }).click();
    await widget.getByRole('button', { name: 'Остановить диктовку' }).waitFor();
    await finishDictation('голосом');
    assert.equal(await input.inputValue(), 'Начать работу через мини-чат голосом');
    await widget.getByText('Слушаю').waitFor();
    const scrollStyle = await page.addStyleTag({ content: '.bobby-widget-thread { flex: none; height: 50px; } .bobby-widget-query { min-height: 120px; }' });
    await page.getByRole('link', { name: 'Мои задачи' }).click();
    await input.press('Enter');
    await widget.getByText('Думаю').waitFor();
    assert.ok(await widget.locator('.bobby-widget-thread').evaluate(element => element.scrollTop > 0));
    await widget.getByText('Ответ готов').waitFor();
    await widget.getByRole('heading', { name: 'Ваш первый шаг' }).waitFor();
    await scrollStyle.evaluate(element => element.remove());
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);
    const selection = page.getByRole('group', { name: 'Действия с выделенным текстом' });
    const firstQuote = await selectText(widget.locator('.answer h3'));
    await selection.getByRole('button', { name: 'Копировать' }).click();
    await selection.getByRole('button', { name: 'Скопировано' }).waitFor();
    assert.equal(await page.evaluate(() => navigator.clipboard.readText()), firstQuote);
    await selection.getByRole('button', { name: 'Цитировать' }).click();
    const secondQuote = await selectText(widget.locator('.answer p').first());
    await selection.getByRole('button', { name: 'Цитировать' }).click();
    await widget.getByRole('button', { name: 'Ответить' }).click();
    await widget.locator('.answer-actions').getByRole('button', { name: 'Копировать' }).click();
    await widget.locator('.answer-actions').getByRole('button', { name: 'Скопировано' }).waitFor();
    assert.equal((await page.evaluate(() => navigator.clipboard.readText())).replace(/\r\n/g, '\n'),
      (await widget.locator('[data-answer-text]').innerText()).trim());
    assert.equal(await widget.locator('.reply-context li').count(), 3);
    await widget.getByRole('button', { name: 'Убрать цитату' }).first().click();
    await input.fill('Уточни детали');
    const sent = page.waitForRequest(request => request.url().endsWith('/v1/jobs') && request.method() === 'POST');
    await input.press('Enter');
    assert.equal((await sent).postDataJSON().query,
      `Уточни детали\n\nЦитата из ответа Бобби\n> ${secondQuote}\n\nОтвет Бобби целиком\n${answer.split('\n').map(line => '> ' + line).join('\n')}`);
    await widget.locator('.reply-context').waitFor({ state: 'hidden' });
    await widget.getByText('Ответ готов').waitFor();
    await input.fill('Следующий вопрос');
    await widget.locator('.bobby-widget-header [data-bobby-state="listening"]').waitFor();
    const listeningAvatar = page.locator('.message-avatar [data-bobby-state="listening"]');
    assert.equal(await listeningAvatar.count(), 1);
    assert.equal(await listeningAvatar.evaluate(element => getComputedStyle(element).width), '68px');
    assert.equal(await page.locator('.user-message [data-bobby-state="listening"], .bobby-widget-query [data-bobby-state="listening"]').count(), 0);
    assert.equal(await page.locator('.composer [data-bobby-state="listening"]').count(), 0);
    await input.clear();
    await widget.getByRole('button', { name: 'Заполнить заявку' }).click();
    await widget.waitFor({ state: 'hidden' });
    await page.getByLabel('Ресурс').waitFor();
    await trigger.click();
    await widget.getByText('Жду заполнения формы').waitFor();
    await widget.getByRole('textbox').press('Escape');
    await widget.waitFor({ state: 'hidden' });
    assert.equal(await trigger.evaluate(element => document.activeElement === element), true);
    const main = page.locator('.chat-page');
    await selectText(main.locator('.answer h3').last());
    await selection.getByRole('button', { name: 'Цитировать' }).click();
    await main.locator('.reply-context').getByText('Ваш первый шаг').waitFor();
    await main.getByRole('button', { name: 'Убрать цитату' }).click();
    await page.emulateMedia({ reducedMotion: 'reduce' });
    assert.equal(await main.locator('.bobby-body').last().evaluate(element => getComputedStyle(element).transitionDuration), '0s');
    assert.deepEqual(errors, []);
    await page.close();
    console.log('Passed Bobby widget checks');
  }
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
    assert.equal(await page.locator('.answer-reveal').count(), 1);
    assert.equal(await page.locator('.message-avatar [data-bobby-state="answering"]').count(), 1);
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
    assert.equal(await page.locator('.message-avatar [data-bobby-state="answering"]').count(), 0);
    await input.fill('Проверка тайм-аута');
    await input.press('Enter');
    await page.getByText('Время ожидания ответа истекло.').waitFor();
    await page.reload();
    await page.getByText('С чего начать адаптацию в команде?', { exact: true }).waitFor();
    await page.getByText('Время ожидания ответа истекло.').waitFor();
    assert.equal(await page.locator('.answer-reveal').count(), 0);
    if (width === 360) {
      await page.emulateMedia({ reducedMotion: 'no-preference' });
      await page.evaluate(() => {
        document.addEventListener('animationstart', event => {
          if (event.animationName === 'reveal-answer' && event.target.textContent === 'Начало длинного ответа') {
            document.body.dataset.answerAnimated = 'true';
          }
        });
      });
      await input.fill('Проверка длинного ответа');
      await input.press('Enter');
      await page.waitForFunction(() => document.body.dataset.answerAnimated === 'true');
      const heading = page.getByRole('heading', { name: 'Начало длинного ответа' });
      assert.equal(await heading.evaluate(element => {
        const viewport = element.closest('.chat-scroll').getBoundingClientRect();
        const top = element.getBoundingClientRect().top;
        return top >= viewport.top - 1 && top < viewport.bottom;
      }), true);
      await page.emulateMedia({ reducedMotion: 'reduce' });
      assert.equal(await heading.evaluate(element => getComputedStyle(element).animationName), 'none');
    }
    assert.equal(await page.getByRole('button', { name: 'Новый чат' }).count(), 0);
    assert.equal(await page.getByRole('combobox', { name: 'Открыть беседу' }).count(), 0);

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
  await rm(cacheDir, { recursive: true, force: true });
}
