import assert from 'node:assert/strict';
import { test } from 'node:test';
import { ApiError, api, completed, errorText, request } from '../src/api.ts';
import { fieldValue, linkedForm, linkSource, newId, safeLink, timeText, upsert } from '../src/model.ts';

test('links reject unsafe protocols and only open valid form IDs', () => {
  assert.equal(safeLink('javascript:alert(1)'), undefined);
  assert.equal(safeLink('//example.org'), undefined);
  assert.equal(safeLink(undefined), undefined);
  assert.equal(safeLink('https://example.org'), 'https://example.org/');
  assert.equal(safeLink('http://127.0.0.1:8080/item'), 'http://127.0.0.1:8080/item');
  assert.equal(linkedForm('/forms/access-request'), 'access-request');
  assert.equal(linkedForm('/forms/../access-request'), undefined);
  assert.equal(linkSource('https://team.atlassian.net/browse/AA-1'), 'Jira');
  assert.equal(linkSource('https://gitlab.example.org/project'), 'GitLab');
  assert.equal(linkSource('https://calendar.google.com/calendar/event'), 'Календарь');
  assert.equal(linkSource('https://github.com/avito-tech/playbook'), 'Материалы');
  assert.equal(linkSource('https://example.org/resource'), '');
});

test('timestamps indicate Moscow time and do not render invalid dates', () => {
  assert.match(timeText('2026-11-13T10:00:00Z'), /13:00 МСК$/);
  assert.doesNotMatch(timeText('2026-11-13', true), /МСК/);
  assert.equal(timeText(), '');
  assert.equal(timeText('unknown'), '');
});

test('job updates preserve local fields and do not duplicate a turn', () => {
  const initial = { id: 'abc', query: 'Вопрос', status: 'queued' } as const;
  const jobs = upsert([], initial);
  const result = upsert(jobs, { ...initial, status: 'completed', answer: 'Ответ' });
  assert.equal(result.length, 1);
  assert.equal(result[0].answer, 'Ответ');
  assert.equal(completed(result[0]), true);
  assert.equal(completed(initial), false);
  assert.equal(completed({ ...initial, status: 'failed' }), true);
});

test('form values keep schema types and generate keys on HTTP origins', () => {
  assert.equal(fieldValue('1', { enum: [7, true] }), true);
  assert.equal(fieldValue('on', { type: 'boolean' }), true);
  assert.equal(fieldValue('', { type: 'boolean' }), false);
  assert.equal(fieldValue('12.5', { type: 'number' }), 12.5);
  assert.equal(fieldValue('  note ', {}), 'note');
  assert.deepEqual(fieldValue('[1]', { type: 'array' }), [1]);
  assert.deepEqual(fieldValue('{"a":1}', { type: 'object' }), { a: 1 });
  assert.throws(() => fieldValue('wrong', { type: 'object' }));
  assert.match(newId(), /^[a-f0-9]{32}$/);
  assert.notEqual(newId(), newId());
});

test('API client preserves account headers and the same idempotency key on retry', async t => {
  const calls: { url: string; method?: string; headers: Headers; body?: BodyInit | null }[] = [];
  t.mock.method(globalThis, 'fetch', async (url: string, options: RequestInit) => {
    calls.push({ url, ...options, headers: new Headers(options.headers) });
    return Response.json({ id: 'request-42', status: 'pending' });
  });
  const auth = { token: 'sample', key: 'sample-key' };
  await api.ask('Где задачи?', auth, true);
  const first = await api.submit('access', 2, { reason: 'Первая задача' }, 'idem-test', auth);
  const second = await api.submit('access', 2, { reason: 'Первая задача' }, 'idem-test', auth);
  assert.equal(calls[0].headers.get('X-Bobby-Account'), '1');
  assert.equal(calls[0].headers.get('Authorization'), 'Bearer sample');
  assert.equal(calls[0].headers.get('X-API-Key'), 'sample-key');
  assert.equal(calls[1].headers.get('Idempotency-Key'), calls[2].headers.get('Idempotency-Key'));
  assert.equal(calls[1].body, calls[2].body);
  assert.deepEqual(first, second);
  assert.equal(JSON.parse(String(calls[1].body)).confirmed, true);
});

test('empty, rejected and timed-out requests have distinct outcomes', async t => {
  const mock = t.mock.method(globalThis, 'fetch', async () => Response.json({ error: 'invalid API key' }, { status: 401 }));
  await assert.rejects(request('/v1/jobs'), (err: unknown) => err instanceof ApiError && /API-ключ/.test(errorText(err, 'fallback')));
  mock.mock.mockImplementation(async () => new Response('not JSON', { status: 503 }));
  await assert.rejects(request('/v1/jobs'), (err: unknown) => err instanceof ApiError && err.status === 503);
  assert.match(errorText(new ApiError('', 403), 'fallback'), /нет доступа/);
  assert.match(errorText(new ApiError('', 401), 'fallback'), /Войдите/);
  assert.match(errorText(new TypeError(), 'fallback'), /Нет связи/);
  assert.match(errorText(new DOMException('', 'TimeoutError'), 'fallback'), /не успел/);
  assert.equal(errorText(new Error(), 'fallback'), 'fallback');
});
