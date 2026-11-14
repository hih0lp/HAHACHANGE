import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { api, ApiError, completed, errorText, watchJob } from './api';
import type { Job } from './api';
import { AccessForm } from './AccessForm';
import { Answer } from './Answer';
import { timeText, upsert } from './model';
import { External, Notice, Stamp } from './ui';
import { Bobby, Icon, Illustration } from './visuals';

type Props = {
  token: string;
  apiKey: string;
  identity: string;
  formID: string;
  catalog: boolean;
  draft: string;
  onDraft: (text: string) => void;
  onForm: (id: string) => void;
  onCloseForm: () => void;
  onCatalog: () => void;
};

export function Chat({ token, apiKey, identity, formID, catalog, draft, onDraft, onForm, onCloseForm, onCatalog }: Props) {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [revealedAnswers, setRevealedAnswers] = useState<Set<string>>(() => new Set());
  const [waiting, setWaiting] = useState<Record<string, 'reconnect' | 'timeout'>>({});
  const [sending, setSending] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const [editedQuery, setEditedQuery] = useState('');
  const [changing, setChanging] = useState<string | null>(null);
  const [error, setError] = useState('');
  const [historyError, setHistoryError] = useState('');
  const [reload, setReload] = useState(0);
  const [deadlines, setDeadlines] = useState<Awaited<ReturnType<typeof api.deadlines>>['notifications']>([]);
  const dismissedDeadlines = useRef(new Set<string>());
  const deadlineStorageKey = (id: string) => 'bobby:deadline:dismissed:' + identity + ':' + id;

  function dismissDeadline(id: string) {
    dismissedDeadlines.current.add(id);
    localStorage.setItem(deadlineStorageKey(id), '1');
    setDeadlines(current => current.filter(item => item.id !== id));
  }

  const watchers = useRef(new Map<string, () => void>());
  const retired = useRef(new Set<string>());
  const lock = useRef(false);
  const scroll = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLTextAreaElement>(null);
  const inlinePanel = useRef<HTMLDivElement>(null);
  const initialRestore = useRef(true);
  const sourceButton = useRef<HTMLElement | null>(null);

  useEffect(() => {
    let active = true;
    setHistoryError('');
    void api.history(apiKey).then(({ turns }) => {
      if (active) setJobs(current => {
        const latest = new Map([...turns].reverse().map(job => [job.id, job]));
        current.forEach(job => {
          const server = latest.get(job.id);
          if (!server || !completed(server)) latest.set(job.id, job);
        });
        return [...latest.values()];
      });
    }).catch(err => {
      if (active && !(err instanceof ApiError && err.status === 404)) setHistoryError(errorText(err, 'История чата временно недоступна.'));
    });
    return () => { active = false; };
  }, [apiKey, reload]);

  useEffect(() => {
    const active = watchers.current;
    return () => { active.forEach(stop => stop()); active.clear(); };
  }, []);

  useEffect(() => {
    for (const job of jobs) {
      if (completed(job) || watchers.current.has(job.id)) continue;
      const stop = watchJob(job.id, { key: apiKey }, update => {
        if (retired.current.has(update.id)) return;
        if (update.status === 'completed' && update.answer?.trim()) {
          setRevealedAnswers(previous => new Set(previous).add(update.id));
        }
        setJobs(previous => upsert(previous, update));
        setWaiting(previous => {
          if (!previous[update.id]) return previous;
          const next = { ...previous };
          delete next[update.id];
          return next;
        });
      }, state => setWaiting(previous => ({ ...previous, [job.id]: state })));
      watchers.current.set(job.id, stop);
    }
  }, [jobs, apiKey]);

  useEffect(() => {
    setDeadlines([]);
    if (!token) return;
    let active = true;
    const refresh = () => void api.deadlines(apiKey).then(({ notifications }) => {
      if (active) setDeadlines(notifications.filter(item =>
        Date.parse(item.due_at) > Date.now() &&
        !dismissedDeadlines.current.has(item.id) &&
        !localStorage.getItem(deadlineStorageKey(item.id)),
      ));
    }).catch(() => {});
    refresh();
    const timer = setInterval(refresh, 15_000);
    return () => { active = false; clearInterval(timer); };
  }, [apiKey, token, identity]);

  useEffect(() => {
    if (!jobs.length || !scroll.current) return;
    if (initialRestore.current || sending || scroll.current.scrollHeight - scroll.current.scrollTop - scroll.current.clientHeight < 200) {
      scroll.current.scrollTop = scroll.current.scrollHeight;
    }
    initialRestore.current = false;
  }, [jobs, sending]);

  useEffect(() => {
    if (formID || catalog) {
      inlinePanel.current?.scrollIntoView({ block: 'start' });
      inlinePanel.current?.focus({ preventScroll: true });
    }
  }, [formID, catalog]);

  async function send(text: string) {
    const query = text.trim();
    if ([...query].length < 2 || lock.current) return;
    lock.current = true;
    setSending(true);
    setError('');
    try {
      const job = await api.ask(query, { token, key: apiKey });
      if (job.status === 'completed' && job.answer?.trim()) setRevealedAnswers(previous => new Set(previous).add(job.id));
      setJobs(current => upsert(current, { ...job, query, created_at: job.created_at || new Date().toISOString() }));
      onDraft('');
      input.current?.focus();
    } catch (err) {
      onDraft(query);
      setError(errorText(err, 'Не удалось отправить вопрос. Попробуйте ещё раз.'));
    } finally {
      setSending(false);
      lock.current = false;
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    void send(draft);
  }

  async function stopJob(job: Job) {
    if (changing) return;
    setChanging(job.id);
    setError('');
    try {
      const stopped = await api.stop(job.id, { token, key: apiKey });
      retired.current.add(job.id);
      watchers.current.get(job.id)?.();
      watchers.current.delete(job.id);
      setJobs(current => upsert(current, stopped));
    } catch (err) {
      setError(errorText(err, 'Не удалось остановить ответ. Попробуйте ещё раз.'));
    } finally {
      setChanging(null);
    }
  }

  async function saveEdit(job: Job) {
    const query = editedQuery.trim();
    if ([...query].length < 2 || changing) return;
    if (query === job.query) { setEditing(null); return; }
    setChanging(job.id);
    setError('');
    try {
      const replacement = await api.edit(job.id, query, { token, key: apiKey });
      retired.current.add(job.id);
      watchers.current.get(job.id)?.();
      watchers.current.delete(job.id);
      setJobs(current => [...current.filter(item => item.id !== job.id), replacement]);
      setEditing(null);
    } catch (err) {
      setError(errorText(err, 'Не удалось изменить вопрос. Попробуйте ещё раз.'));
    } finally {
      setChanging(null);
    }
  }

  function openForm(id: string) {
    sourceButton.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    onForm(id);
  }

  function closeForm() {
    onCloseForm();
    requestAnimationFrame(() => {
      if (sourceButton.current?.isConnected) sourceButton.current.focus();
      else input.current?.focus();
    });
  }

  const auth = { token, key: apiKey };
  const hasMessages = jobs.length > 0 || !!formID || catalog;
  const composerState = sending ? 'thinking' : draft.trim() ? 'listening' : 'idle';

  return <div className={'chat-page' + (hasMessages ? ' has-messages' : '')}>
    {deadlines.length > 0 && <div className="deadline-notices">
      <div className="chat-inner">
        {deadlines.map(item => <Notice key={item.id} error action={
          <button className="text-button" onClick={() => dismissDeadline(item.id)}>Закрыть уведомление</button>
        }>
          <strong>{item.kind === 'mentor_deadline' ? 'Помогите стажёру' : 'Скоро дедлайн'}</strong>
          <br/><External href={item.url}>{item.key} · {item.summary}</External> · {timeText(item.due_at)}
        </Notice>)}
      </div>
    </div>}
    <div className="chat-scroll" ref={scroll}>
      <div className="chat-inner">
        {historyError && <Notice error action={<button className="text-button" onClick={() => setReload(value => value + 1)}>Повторить</button>}>{historyError}</Notice>}
        {!hasMessages && <div className="welcome">
          <Illustration state={composerState}/>
          <h1>Привет, я Бобби!</h1>
          <p>Помогу освоиться в команде и разобраться с первыми задачами.</p>
        </div>}
        {hasMessages && <div className="messages" aria-label="Сообщения чата">
          {jobs.map((job, index) => <div key={job.id} className="turn">
            <div className="user-message">
              {editing === job.id ? <form className="message-edit" onSubmit={event => { event.preventDefault(); void saveEdit(job); }}>
                <textarea aria-label="Изменённый вопрос" value={editedQuery} onChange={event => setEditedQuery(event.target.value)} rows={3} autoFocus/>
                <div className="message-actions"><button className="button primary" disabled={changing === job.id || [...editedQuery.trim()].length < 2}>Отправить</button><button type="button" className="text-button" onClick={() => setEditing(null)}>Отмена</button></div>
              </form> : <><p>{job.query}</p><div className="message-actions"><button className="message-action" aria-label="Изменить вопрос" title="Изменить вопрос" disabled={!!changing} onClick={() => { setEditing(job.id); setEditedQuery(job.query); }}><Icon name="edit"/></button>{!completed(job) && <button className="message-action" aria-label="Остановить ответ" title="Остановить ответ" disabled={!!changing} onClick={() => void stopJob(job)}><Icon name="stop"/></button>}</div><Stamp value={job.created_at}/></>}
            </div>
            <div className="assistant-message" aria-live="polite">
              <span className="message-avatar"><Bobby state={index < jobs.length - 1 ? 'idle' : job.status === 'completed' && job.answer?.trim() ? 'answering' : !completed(job) && !waiting[job.id] ? 'thinking' : 'idle'}/></span>
              <strong className="message-name">Бобби</strong>
              <div className="message-body">
                {job.status === 'completed' && (job.answer?.trim() ? <Answer text={job.answer} onForm={openForm} reveal={revealedAnswers.has(job.id)}/> : <p>Ответ пуст. Уточните вопрос или попробуйте снова.</p>)}
                {job.status === 'failed' && (job.error === 'Остановлено пользователем' ? <p className="muted">Ответ остановлен.</p> : <Notice error action={<button className="text-button" onClick={() => { onDraft(job.query); input.current?.focus(); }}>Повторить</button>}>
                  {job.error?.toLowerCase().match(/timeout|deadline exceeded/) ? 'Время ожидания ответа истекло.' : 'Не удалось получить ответ.'}
                </Notice>)}
                {!completed(job) && (waiting[job.id] === 'timeout'
                  ? <Notice error action={<button className="text-button" onClick={() => { watchers.current.delete(job.id); setJobs([...jobs]); }}>Проверить ответ</button>}>Ответ пока не получен.</Notice>
                  : <p className="message-status muted"><span className="loading-dot"/>{waiting[job.id] === 'reconnect' ? 'Проверяем статус…' : job.status === 'queued' ? 'Вопрос в очереди…' : 'Готовлю ответ…'}</p>)}
                {completed(job) && <Stamp value={job.completed_at || job.created_at}/>}
              </div>
            </div>
          </div>)}
          {(catalog || formID) && <div className="inline-panel" ref={inlinePanel} tabIndex={-1}>
            {catalog && !formID && <FormCatalog token={token} apiKey={apiKey} onForm={openForm} onClose={closeForm}/>}
            {formID && <AccessForm key={formID} id={formID} token={auth.token} apiKey={auth.key} identity={identity} onClose={closeForm}/>}
          </div>}
        </div>}
      </div>
    </div>
    <div className="chat-footer">
      <div className="chat-inner">
        {!hasMessages && <div className="suggestions">
          <button className="suggestion" disabled={sending} onClick={() => void send('С чего начать адаптацию в команде?')}>
            <span className="suggestion-icon mint"><Icon name="route"/></span><span>С чего начать?</span><Icon className="suggestion-arrow" name="chevron"/>
          </button>
          <button className="suggestion" disabled={sending} onClick={() => void send('Кто мой наставник?')}>
            <span className="suggestion-icon lavender"><Icon name="people"/></span><span>Кто мой наставник?</span><Icon className="suggestion-arrow" name="chevron"/>
          </button>
          <button className="suggestion" onClick={onCatalog}>
            <span className="suggestion-icon peach"><Icon name="key"/></span><span>Запросить доступ</span><Icon className="suggestion-arrow" name="chevron"/>
          </button>
        </div>}
        {error && <Notice error>{error}</Notice>}
        <form className="composer" onSubmit={submit}>
          {hasMessages && composerState !== 'idle' && <Bobby className="composer-bobby" state={composerState}/>}
          <textarea ref={input} value={draft} onChange={event => onDraft(event.target.value)} onKeyDown={event => {
            if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) { event.preventDefault(); void send(draft); }
          }} aria-label="Ваш вопрос" placeholder="Напишите вопрос Бобби" rows={1}/>
          <button className="send-button" aria-label={sending ? 'Отправляем вопрос' : 'Отправить вопрос'} title="Отправить вопрос" disabled={sending || [...draft.trim()].length < 2}><Icon name="send"/></button>
        </form>
        <p className="composer-hint">Бобби может ошибаться. Проверяйте важное по ссылкам в ответах.</p>
      </div>
    </div>
  </div>;
}

function FormCatalog({ token, apiKey, onForm, onClose }: { token: string; apiKey: string; onForm: (id: string) => void; onClose: () => void }) {
  const [forms, setForms] = useState<Awaited<ReturnType<typeof api.forms>>['forms']>();
  const [error, setError] = useState('');
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    if (!token) return;
    let active = true;
    setError('');
    void api.forms({ token, key: apiKey }).then(result => { if (active) setForms(result.forms || []); })
      .catch(err => { if (active) setError(errorText(err, 'Каталог форм сейчас недоступен.')); });
    return () => { active = false; };
  }, [token, apiKey, retry]);
  return <section className="card inline-form" aria-label="Каталог заявок">
    <div className="section-heading"><h3>Каталог заявок</h3><button className="icon-button" aria-label="Закрыть каталог" onClick={onClose}><Icon name="close"/></button></div>
    {!token ? <Notice error>Для каталога нужна демо-сессия. Проверьте подключение в профиле.</Notice> : error ? <Notice error action={<button className="text-button" onClick={() => setRetry(value => value + 1)}>Повторить</button>}>{error}</Notice> :
      !forms ? <Notice>Открываем каталог…</Notice> : forms.length ? <ul className="catalog-list">{forms.map(form =>
        <li key={form.id}><button onClick={() => onForm(form.id)}><span><strong>{form.title}</strong>{form.description && <small>{form.description}</small>}</span><Icon name="chevron"/></button></li>)}</ul>
        : <p className="muted">В каталоге пока нет доступных форм.</p>}
  </section>;
}
