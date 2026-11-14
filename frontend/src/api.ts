export type Role = 'intern' | 'intern2' | 'mentor';
export type Session = { access_token: string; expires_in: number; role?: Role };
export type ChatThread = { id: string; title: string };
export type Job = {
  id: string;
  thread_id?: string;
  query: string;
  status: 'queued' | 'running' | 'completed' | 'failed';
  answer?: string;
  error?: string;
  created_at?: string;
  completed_at?: string;
};
export type Resource = { id: string; name: string; url: string; capabilities: string[] };
export type Field = {
  type?: string;
  title?: string;
  format?: string;
  enum?: (string | number | boolean)[];
  minLength?: number;
  maxLength?: number;
};
export type FormDefinition = {
  id: string;
  version: number;
  title: string;
  description?: string;
  kind: string;
  schema: { properties?: Record<string, Field>; required?: string[] };
  ui_schema?: { order?: string[]; labels?: Record<string, string> };
};
export type Values = Record<string, unknown>;
export type Receipt = { id: string; status: string };
export type Reminder = { id: string; key: string; summary: string; url: string; due_at: string };
export type CalendarEvent = {
  id: string; summary?: string; start?: { dateTime?: string; date?: string };
  htmlLink?: string; status?: string;
};
type Auth = { token?: string; key?: string };
type Options = Auth & { body?: unknown; idem?: string; signal?: AbortSignal; account?: boolean };

export class ApiError extends Error {
  readonly status: number;
  constructor(message: string, status: number) {
    super(message);
    this.status = status;
  }
}

export async function request<T>(path: string, options: Options = {}): Promise<T> {
  const { token, key, body, idem, signal, account } = options;
  const headers = new Headers({ Accept: 'application/json' });
  if (token) headers.set('Authorization', 'Bearer ' + token);
  if (key) headers.set('X-API-Key', key);
  if (idem) headers.set('Idempotency-Key', idem);
  if (account) headers.set('X-Bobby-Account', '1');
  if (body !== undefined) headers.set('Content-Type', 'application/json');
  const timeout = AbortSignal.timeout(18_000);
  const response = await fetch(path, {
    method: body === undefined ? 'GET' : 'POST',
    headers,
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: signal ? AbortSignal.any([signal, timeout]) : timeout,
    cache: 'no-store',
  });
  const data = await response.json().catch(() => null);
  if (!response.ok || data === null) {
    const details = data?.error;
    throw new ApiError(typeof details === 'string' ? details : details?.message || '', response.status);
  }
  return data as T;
}

const formPath = (id: string) => '/v1/forms/' + encodeURIComponent(id);
export const api = {
  demo: () => request<Session>('/v1/team-access/session', { body: {} }),
  role: (role: Role) => request('/v1/demo/role', { body: { role } }),
  threads: (key: string) => request<{ threads: ChatThread[] }>('/v1/chat/threads', { key }),
  history: (key: string, thread: string) => request<{ turns: Job[] }>('/v1/chat/history?thread_id=' + encodeURIComponent(thread), { key }),
  ask: (query: string, auth: Auth, thread?: string) =>
    request<Job>('/v1/jobs', { ...auth, body: { query, scenario: 'general', thread_id: thread } }),
  job: (id: string, auth: Auth, signal: AbortSignal) => request<Job>('/v1/jobs/' + encodeURIComponent(id), { ...auth, account: !!auth.token, signal }),
  forms: (auth: Auth) => request<{ forms: FormDefinition[] | null }>('/v1/forms', auth),
  form: (id: string, auth: Auth) => request<FormDefinition>(formPath(id), auth),
  resources: (auth: Auth) => request<{ resources: Resource[] | null }>('/v1/forms/resources', auth),
  validate: (id: string, version: number, values: Values, auth: Auth) =>
    request<{ valid: boolean }>(formPath(id) + '/validate', { ...auth, body: { version, values } }),
  submit: (id: string, version: number, values: Values, idem: string, auth: Auth) =>
    request<Receipt>(formPath(id) + '/submit', { ...auth, idem, body: { version, values, confirmed: true } }),
  jira: (auth: Auth) => request<{ enabled: boolean; reminders: Reminder[] }>('/v1/tasks/jira', auth),
  calendar: (auth: Auth) => request<{ data?: { items?: CalendarEvent[] } }>('/v1/calendar/reminders', auth),
  deadlines: (key: string) => request<{ notifications: (Reminder & { kind: string })[] }>('/v1/deadline-notifications', { key }),
};

export function errorText(error: unknown, fallback: string) {
  if (error instanceof ApiError && error.status === 401) {
    return error.message === 'invalid API key' ? 'Проверьте API-ключ в профиле.' : 'Сессия недоступна. Попробуйте обновить страницу.';
  }
  if (error instanceof ApiError && error.status === 403) return 'У этой сессии нет доступа к действию.';
  if (error instanceof TypeError) return 'Нет связи с Бобби. Проверьте подключение и попробуйте снова.';
  if (error instanceof DOMException && error.name === 'TimeoutError') return 'Сервис не успел ответить. Попробуйте ещё раз.';
  return fallback;
}

export const completed = (job: Job) => job.status === 'completed' || job.status === 'failed';

export function watchJob(id: string, auth: Auth, onJob: (job: Job) => void, onState: (state: 'reconnect' | 'timeout') => void) {
  const abort = new AbortController();
  let socket: WebSocket | undefined;
  let pollTimer: ReturnType<typeof setTimeout>;
  let socketTimer: ReturnType<typeof setTimeout>;
  const deadline = setTimeout(() => { stop(); onState('timeout'); }, 210_000);
  function stop() {
    abort.abort();
    clearTimeout(deadline);
    clearTimeout(pollTimer);
    clearTimeout(socketTimer);
    socket?.close();
  }
  function receive(job: Job) {
    if (abort.signal.aborted || job.id !== id) return;
    onJob(job);
    if (completed(job)) stop();
  }
  async function poll() {
    if (abort.signal.aborted) return;
    try {
      receive(await api.job(id, auth, abort.signal));
    } catch {
      if (!abort.signal.aborted) onState('reconnect');
    }
    if (!abort.signal.aborted) pollTimer = setTimeout(poll, 2_000);
  }
  const scheme = location.protocol === 'https:' ? 'wss:' : 'ws:';
  socket = new WebSocket(scheme + '//' + location.host + '/v1/jobs/' + encodeURIComponent(id) + '/ws' + (auth.token ? '?account=1' : ''));
  socketTimer = setTimeout(() => socket?.close(), 8_000);
  socket.onopen = () => { if (auth.key || auth.token) socket?.send(JSON.stringify({ api_key: auth.key, access_token: auth.token })); };
  socket.onmessage = ({ data }) => {
    try {
      receive(JSON.parse(data) as Job);
      clearTimeout(socketTimer);
    } catch {
      socket?.close();
    }
  };
  socket.onclose = () => {
    clearTimeout(socketTimer);
    if (!abort.signal.aborted) { onState('reconnect'); void poll(); }
  };
  socket.onerror = () => socket?.close();
  return stop;
}
