import assert from 'node:assert/strict';
import { chromium } from 'playwright';
import { createServer } from 'vite';
import { browserCoverage } from './browser-coverage.mjs';

const coverage = browserCoverage();
const server = await createServer({ plugins: coverage.plugins, server: { host: '127.0.0.1', port: 0 } });
await server.listen();
const browser = await chromium.launch({ executablePath: process.env.BOBBY_CHROME, args: ['--no-sandbox'] });
try {
  const page = await browser.newPage();
  await coverage.track(page);
  await page.addInitScript(() => {
    Object.defineProperty(window, 'documentPictureInPicture', {
      configurable: true,
      value: { requestWindow: () => Promise.reject(new Error('window unavailable')) },
    });
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined });
    document.execCommand = command => {
      document.documentElement.dataset.testCopy = command;
      return true;
    };
  });
  let sessions = 0;
  let definitions = 0;
  let validations = 0;
  let submissions = 0;
  let roleFailed = false;
  const form = {
    id: 'equipment', kind: 'general', title: 'Учебная заявка на оборудование', version: 2,
    schema: {
      properties: {
        category: { title: 'Категория', enum: ['Клавиатура', 'Монитор'] },
        priority: { title: 'Приоритет', type: 'integer' },
        remote: { title: 'Удалённо', type: 'boolean' },
        details: { title: 'Параметры', type: 'object' },
        email: { title: 'Почта', format: 'email' },
      },
      required: ['category', 'priority', 'details'],
    },
  };
  await page.route('**/session/bootstrap', route => route.fulfill({ status: 204 }));
  await page.route('**/v1/**', route => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    const respond = (json, status = 200) => route.fulfill({ status, json });
    if (path === '/v1/team-access/session') {
      if (++sessions === 1) return respond({ error: 'temporary failure' }, 503);
      return respond({ access_token: 'form-sample', role: 'intern' });
    }
    if (path === '/v1/demo/role') {
      roleFailed = true;
      return respond({ error: 'forbidden' }, 403);
    }
    if (path === '/v1/chat/history') return respond({ turns: [{
      id: 'verified-sources', query: 'Как начать работу?', status: 'completed',
      answer: '### Ссылки\n1. **Проверьте** [Материалы](https://github.com/avito-tech/playbook).\n2. *Запросите* `ревью` [Подпись](javascript:alert).\n- Первая задача\n- Проверка результата',
    }] });
    if (path === '/v1/forms/equipment') {
      return ++definitions === 1 ? respond({ error: 'retry' }, 503) : respond(form);
    }
    if (path === '/v1/forms/equipment/validate') {
      assert.deepEqual(request.postDataJSON(), { version: 2, values: {
        category: 'Монитор', priority: 2, remote: true, details: { size: 24 }, email: 'intern@example.test',
      } });
      return respond({ valid: ++validations > 1 });
    }
    if (path === '/v1/forms/equipment/submit') {
      assert.equal(request.postDataJSON().confirmed, true);
      return ++submissions === 1 ? respond({ error: 'Заявка уже открыта' }, 409) : respond({ id: 'test-42', status: 'submitted' }, 201);
    }
    return respond({ error: 'unknown fixture' }, 404);
  });
  await page.goto('http://127.0.0.1:' + server.httpServer.address().port + '/?form=equipment');
  const retry = page.getByRole('button', { name: 'Повторить', exact: true });
  await retry.click();
  await retry.click();
  const fill = async () => {
    await page.getByLabel('Категория').selectOption('1');
    await page.getByLabel('Приоритет').fill('2');
    await page.getByLabel('Удалённо').check();
    await page.getByLabel('Параметры').fill('{"size":24}');
    await page.getByLabel('Почта').fill('intern@example.test');
    await page.getByRole('button', { name: 'Проверить заявку' }).click();
  };
  await fill();
  await page.getByText('Не удалось проверить форму.', { exact: false }).waitFor();
  await page.getByRole('button', { name: 'Проверить заявку' }).click();
  const submit = page.getByRole('button', { name: 'Подтвердить отправку' });
  await submit.click();
  await page.getByText('Заявка уже открыта').waitFor();
  await page.getByRole('button', { name: 'Изменить', exact: true }).click();
  await page.getByRole('button', { name: 'Проверить заявку' }).click();
  await submit.click();
  await page.getByText('Номер заявки test-42').waitFor();
  await page.getByRole('button', { name: 'Заполнить новую заявку' }).click();
  await page.getByRole('button', { name: 'Проверить заявку' }).waitFor();
  await page.getByRole('button', { name: 'Открыть профиль' }).click();
  const profile = page.getByRole('dialog', { name: 'Профиль' });
  await profile.getByRole('button', { name: 'Наставник' }).click();
  await profile.getByText('У этой сессии нет доступа к действию.').waitFor();
  assert.equal(roleFailed, true);
  await profile.getByText('Настройки подключения').click();
  await profile.getByLabel('API-ключ, если задан на сервере').fill('test-api-key');
  await profile.getByRole('button', { name: 'Сохранить ключ' }).click();
  assert.equal(await page.evaluate(() => sessionStorage.getItem('bobby-api-key')), 'test-api-key');
  await profile.getByRole('button', { name: 'Закрыть' }).click();
  await page.getByRole('button', { name: 'Закрыть форму' }).click();
  await page.getByRole('heading', { name: 'Ссылки' }).waitFor();
  assert.equal(await page.locator('.answer ol li').count(), 2);
  assert.equal(await page.locator('.answer ul li').count(), 2);
  assert.equal(await page.getByRole('link', { name: /Подпись/ }).count(), 0);
  await page.getByRole('button', { name: 'Копировать' }).click();
  await page.getByRole('button', { name: 'Скопировано' }).waitFor();
  assert.equal(await page.evaluate(() => document.documentElement.dataset.testCopy), 'copy');
  await page.getByRole('button', { name: 'Мини-чат с Бобби' }).click();
  await page.getByText('Браузер не открыл отдельное окно.', { exact: false }).waitFor();
  await page.getByRole('button', { name: 'Свернуть мини-чат' }).click();
  await page.getByRole('button', { name: 'Развернуть мини-чат' }).press('Escape');
  await page.getByRole('region', { name: 'Мини-чат с Бобби' }).waitFor({ state: 'hidden' });
  await coverage.capture(page);
  await coverage.write(server);
  console.log('Passed session recovery, form validation, conflict, receipt and profile settings');
} finally {
  await browser.close();
  await server.close();
}
