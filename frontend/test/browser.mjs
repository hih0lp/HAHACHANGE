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

async function fixtures(page, { deadline = false, beforeSession = async () => {} } = {}) {
  let submits = 0;
  let committed;
  let serial = 0;
  let role = 'intern';
  const sessions = [];
  let loginRequests = 0;
  let deadlineSocket;
  let extraDeadline = false;
  const jobs = [];
  const keys = [];
  await page.route('**/session/bootstrap', route => route.fulfill({ status: 200, body: '' }));
  await page.routeWebSocket('**/v1/deadline-notifications/ws', socket => {
    socket.onMessage(message => {
      const auth = JSON.parse(message);
      assert.equal(auth.access_token, 'demo-' + role);
      deadlineSocket = socket;
      const notifications = deadline ? [{
        id: 1, kind: 'intern_deadline', key: 'DEMO-5', summary: 'Тестовый срок',
        url: 'https://example.atlassian.net/browse/DEMO-5', due_at: new Date(Date.now() + 60_000).toISOString(),
      }] : [];
      if (extraDeadline) notifications.push({ ...notifications[0], id: 2, key: 'DEMO-6', summary: 'Новый срок' });
      socket.send(JSON.stringify({ notifications }));
    });
  });
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
    if (path === '/v1/team-access/session') {
      await beforeSession();
      sessions.push(role);
      return respond({ access_token: 'demo-' + role, role, expires_in: 3600 });
    }
    if (path === '/v1/scenarios/login') { loginRequests++; return respond({ error: 'Unexpected login' }, 500); }
    assert.equal(request.headers().authorization, 'Bearer demo-' + role, path);
    if (path === '/v1/chat/history') return respond({ turns: [...jobs].reverse() });
    if (path === '/v1/deadline-notifications') assert.fail('Deadline notifications must use WebSocket');
    const action = /^\/v1\/jobs\/([^/]+)\/(edit|stop)$/.exec(path);
    if (action) {
      const index = jobs.findIndex(job => job.id === action[1]);
      if (index === -1) return respond({ error: 'Job not found' }, 404);
      if (action[2] === 'stop') {
        const job = jobs[index];
        if (job.status === 'running') Object.assign(job, { status: 'failed', error: 'Остановлено пользователем' });
        return respond(job);
      }
      if (!['queued', 'running'].includes(jobs[index].status)) return respond({ error: 'Job not found' }, 404);
      const replacement = { ...jobs[index], id: String(++serial), query: body.query, status: 'completed', answer: 'Ответ на изменённый вопрос' };
      jobs.splice(index, 1);
      jobs.push(replacement);
      return respond({ ...replacement, status: 'queued', answer: undefined }, 202);
    }
    if (path === '/v1/jobs' && body) {
      assert.equal(body.thread_id, undefined);
      const job = { id: String(++serial), query: body.query, status: 'completed', created_at: '2026-11-13T10:00:00Z', answer };
      if (body.query === 'Проверка длинного ответа') job.answer = '### Начало длинного ответа\n' + 'Длинный абзац для проверки прокрутки.\n'.repeat(60);
      if (body.query === 'Пустой ответ') job.answer = '';
      if (body.query === 'Вопрос для остановки') Object.assign(job, { status: 'running', answer: undefined });
      if (body.query === 'Проверка тайм-аута') Object.assign(job, { status: 'failed', answer: '', error: 'context deadline exceeded' });
      jobs.push(job);
      return respond({ ...job, status: 'queued', answer: undefined }, 202);
    }
    if (path.startsWith('/v1/jobs/')) assert.fail('Job updates must use WebSocket');
    if (path === '/v1/forms') return respond({ forms: [access] });
    if (path === '/v1/form-resources') return respond({ resources: [baseScenario.widget.resource] });
    if (path === '/v1/forms/access-request') return respond(access);
    if (path === '/v1/forms/access-request/access-request') return respond({ receipt: committed ?? null });
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
  return {
    keys, sessions, submits: () => submits, loginRequests: () => loginRequests,
    reconnectDeadlines: async () => {
      extraDeadline = true;
      await deadlineSocket.close();
    },
    approveAccess: () => { committed = { ...committed, status: 'approved' }; },
  };
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
    const session = Promise.withResolvers();
    await fixtures(page, { beforeSession: () => session.promise });
    await page.goto(base);
    await page.getByText('Подключаем демо-сессию…').waitFor();
    assert.equal(await page.getByRole('textbox', { name: 'Ваш вопрос' }).count(), 0);
    assert.equal(await page.getByRole('button', { name: 'Мини-чат с Бобби' }).isDisabled(), true);
    session.resolve();
    await page.getByRole('button', { name: 'С чего начать?' }).click();
    await page.getByRole('heading', { name: 'Ваш первый шаг' }).waitFor();
    await page.close();
  }
  {
    const page = await browser.newPage();
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    const mock = await fixtures(page, { deadline: true });
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
    await page.getByRole('button', { name: 'Закрыть уведомление' }).click();
    await page.locator('.deadline-notices').waitFor({ state: 'hidden' });
    await mock.reconnectDeadlines();
    await page.locator('.deadline-notices').getByText('DEMO-6 · Новый срок').waitFor();
    assert.equal(await page.locator('.deadline-notices').getByText('DEMO-5 · Тестовый срок').count(), 0);
    await page.getByRole('button', { name: 'Закрыть уведомление' }).click();
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
    await page.locator('.deadline-notices').waitFor({ state: 'hidden' });
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
    await page.emulateMedia({ reducedMotion: 'no-preference' });
    await widget.locator('.bobby-widget-header .bobby-motion').evaluate(async body => {
      const motion = body.getAnimations().find(animation => animation.animationName === 'bobby-breathe');
      if (!motion) throw new Error('Active Bobby does not breathe');
      body.getAnimations().find(animation => animation.animationName === 'bobby-nod')?.finish();
      await motion.ready;
      motion.pause();
      try {
        motion.currentTime = 0;
        const bottom = body.getBoundingClientRect().bottom;
        const midpoint = motion.effect.getTiming().duration / 2;
        motion.currentTime = midpoint / 2;
        const ascent = bottom - body.getBoundingClientRect().bottom;
        motion.currentTime = midpoint;
        const peak = bottom - body.getBoundingClientRect().bottom;
        motion.currentTime = midpoint * 1.5;
        const descent = bottom - body.getBoundingClientRect().bottom;
        if (peak < 1 || peak > 3 || ascent <= 0 || descent <= 0 || ascent >= peak || descent >= peak) {
          throw new Error(`Unexpected breathing path: ${ascent}, ${peak}, ${descent}`);
        }
      } finally {
        motion.play();
      }
    });
    await widget.locator('.bobby-widget-header .bobby-eye').first().evaluate(async eye => {
      const blink = eye.getAnimations().find(animation => animation.animationName === 'bobby-blink');
      if (!blink) throw new Error('Active Bobby does not blink');
      await blink.ready;
      blink.pause();
      try {
        blink.currentTime = 0;
        const openHeight = eye.getBoundingClientRect().height;
        blink.currentTime = 928;
        if (eye.getBoundingClientRect().height > openHeight * .35) {
          throw new Error('Bobby’s eye does not close visibly');
        }
      } finally {
        blink.play();
      }
    });
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
    const cards = widget.locator('.reply-context li');
    assert.equal(await cards.count(), 3);
    const cardRects = await cards.evaluateAll(elements => elements.map(element => {
      const card = element.getBoundingClientRect();
      const list = element.parentElement.getBoundingClientRect();
      return { top: card.top, width: card.width, listWidth: list.width };
    }));
    assert.ok(cardRects.every(card => card.width < card.listWidth));
    assert.ok(cardRects.at(-1).top > cardRects[0].top, 'quote tiles wrap in the mini-chat');
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
    const completedTurn = main.locator('.turn').last();
    assert.equal(await main.getByRole('button', { name: 'Остановить ответ' }).count(), 0);
    assert.equal(await completedTurn.getByRole('button', { name: 'Изменить вопрос' }).count(), 0);
    const composer = main.locator('.composer');
    const draftField = composer.getByRole('textbox', { name: 'Ваш вопрос' });
    const editField = composer.getByRole('textbox', { name: 'Изменённый вопрос' });
    await draftField.fill('Вопрос для остановки');
    await draftField.press('Enter');
    const editButton = main.locator('.turn').last().getByRole('button', { name: 'Изменить вопрос' });
    await editButton.waitFor();
    await draftField.fill('Сохранённый черновик');
    await editButton.click();
    assert.equal(await completedTurn.getByRole('textbox').count(), 0);
    assert.equal(await editField.inputValue(), 'Вопрос для остановки');
    await editField.press('Escape');
    assert.equal(await draftField.inputValue(), 'Сохранённый черновик');
    await page.waitForFunction(() => document.activeElement?.getAttribute('aria-label') === 'Изменить вопрос');
    await editButton.click();
    await editField.fill('Вопрос после редактирования');
    await editField.press('Enter');
    await main.getByText('Ответ на изменённый вопрос', { exact: true }).waitFor();
    assert.equal(await draftField.inputValue(), 'Сохранённый черновик');
    assert.equal(await main.locator('.user-message').getByText('Вопрос для остановки', { exact: true }).count(), 0);
    assert.equal(await main.locator('.turn').last().getByRole('button', { name: 'Изменить вопрос' }).count(), 0);
    await main.getByRole('textbox', { name: 'Ваш вопрос' }).fill('Вопрос для остановки');
    await main.getByRole('textbox', { name: 'Ваш вопрос' }).press('Enter');
    const stop = main.locator('.composer').getByRole('button', { name: 'Остановить ответ' });
    await stop.waitFor();
    assert.equal(await main.getByRole('button', { name: 'Отправить вопрос' }).count(), 0);
    await main.getByRole('textbox', { name: 'Ваш вопрос' }).fill('Следующий черновик');
    await main.locator('.turn').last().getByRole('button', { name: 'Изменить вопрос' }).click();
    await stop.click();
    await main.getByText('Ответ остановлен.').waitFor();
    await main.getByRole('button', { name: 'Отправить вопрос' }).waitFor();
    assert.equal(await main.getByRole('textbox', { name: 'Ваш вопрос' }).inputValue(), 'Следующий черновик');
    assert.equal(await main.locator('.turn').last().getByRole('button', { name: 'Изменить вопрос' }).count(), 0);
    assert.equal(await page.locator('.deadline-notices').count(), 0);
    const avatars = main.locator('.message-avatar');
    await page.emulateMedia({ reducedMotion: 'no-preference' });
    assert.equal(await avatars.last().locator('.bobby-eye').first().evaluate(element => getComputedStyle(element).animationName), 'bobby-blink');
    await avatars.first().evaluate(async avatar => {
      const body = avatar.querySelector('.bobby-motion');
      const eyelids = avatar.querySelector('.bobby-eyelids');
      const breath = body.getAnimations().find(animation => animation.animationName === 'bobby-breathe');
      const blink = eyelids.getAnimations().find(animation => animation.animationName === 'bobby-eyelids');
      if (!breath || !blink) throw new Error('Earlier chat avatars must breathe and blink');
      await Promise.all([breath.ready, blink.ready]);
      breath.pause();
      blink.pause();
      try {
        breath.currentTime = 0;
        const bottom = body.getBoundingClientRect().bottom;
        breath.currentTime = breath.effect.getTiming().duration / 2;
        const frame = body.getBoundingClientRect();
        const icon = avatar.getBoundingClientRect();
        assertMotion(bottom - frame.bottom >= 1, 'Bobby does not breathe in the chat');
        assertMotion(frame.top >= icon.top && frame.bottom <= icon.bottom, 'Bobby breathes outside the avatar box');
        blink.currentTime = 0;
        assertMotion(getComputedStyle(eyelids).opacity === '0', 'Eyelids cover open eyes');
        blink.currentTime = blink.effect.getTiming().duration * .29;
        assertMotion(getComputedStyle(eyelids).opacity === '1', 'Closed eyelids are not visible');
      } finally {
        breath.play();
        blink.play();
      }
      function assertMotion(condition, message) {
        if (!condition) throw new Error(message);
      }
    });
    await page.emulateMedia({ reducedMotion: 'reduce' });
    assert.equal(await avatars.last().locator('.bobby-body').evaluate(element => getComputedStyle(element).transitionDuration), '0s');
    assert.equal(await avatars.last().locator('.bobby-eye').first().evaluate(element => getComputedStyle(element).animationName), 'none');
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
    const brand = page.getByRole('link', { name: 'Авито Бобби' });
    assert.equal(await brand.getByRole('img').evaluate(img => img.decode().then(() => img.naturalWidth > 0)), true);
    assert.equal(await page.evaluate(async () => (await document.fonts.load('400 16px Manrope', 'Бобби')).length), 1);
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
    await page.getByRole('button', { name: 'Запросить доступ' }).click();
    await page.getByRole('heading', { name: 'Каталог заявок' }).waitFor();
    await page.getByRole('button', { name: 'Закрыть каталог' }).click();
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
    await page.locator('dd').getByText('Просмотр задач', { exact: true }).waitFor();
    await shot(page, 'confirm', width);
    const confirm = page.getByRole('button', { name: 'Подтвердить отправку' });
    await confirm.evaluate(element => { element.click(); element.click(); });
    await page.getByText('Нет связи с Бобби', { exact: false }).waitFor();
    assert.equal(form.keys.length, 1);
    if (width === 360) await page.reload();
    else await confirm.click();
    await page.getByText('Номер заявки request-42').waitFor();
    await page.getByText('На согласовании', { exact: false }).waitFor();
    await shot(page, 'receipt', width);
    form.approveAccess();
    const statusCheck = page.waitForResponse(response => response.url().endsWith('/v1/forms/access-request/access-request') && response.ok());
    await page.getByRole('button', { name: 'Проверить статус заявки' }).click();
    await statusCheck;
    await page.getByText('Одобрена', { exact: false }).waitFor();
    assert.equal(await page.getByRole('button', { name: 'Проверить статус заявки' }).count(), 0);
    assert.deepEqual(form.keys, width === 360 ? [form.keys[0]] : [form.keys[0], form.keys[0]]);
    assert.equal(form.submits(), 1);
    assert.equal(await page.getByRole('button', { name: 'Заполнить новую заявку' }).count(), 0);
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
