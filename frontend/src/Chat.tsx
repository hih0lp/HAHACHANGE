import { useEffect, useRef, useState } from 'react';
import type { Dispatch, FormEvent, SetStateAction } from 'react';
import { api, ApiError, completed, errorText, watchJob } from './api';
import type { Job } from './api';
import { AccessForm } from './AccessForm';
import { Answer } from './Answer';
import { AnswerActions, AnswerSelection } from './AnswerActions';
import { BobbyWidget } from './BobbyWidget';
import type { useBobbyWidget } from './BobbyWidget';
import { timeText, upsert } from './model';
import { External, Notice, Stamp } from './ui';
import { useDictation } from './useDictation';
import { Bobby, Icon, Illustration } from './visuals';

type Props = {
  hidden: boolean;
  token: string;
  apiKey: string;
  identity: string;
  formID: string;
  catalog: boolean;
  draft: string;
  onDraft: Dispatch<SetStateAction<string>>;
  onForm: (id: string) => void;
  onCloseForm: () => void;
  onCatalog: () => void;
  widget: ReturnType<typeof useBobbyWidget>;
  onOpenChat: () => void;
};

type ReplyContext = { id: number; kind: 'reply' | 'quote'; text: string };

export function Chat({ hidden, token, apiKey, identity, formID, catalog, draft, onDraft, onForm, onCloseForm, onCatalog, widget, onOpenChat }: Props) {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [revealedAnswers, setRevealedAnswers] = useState<Set<string>>(() => new Set());
  const [waiting, setWaiting] = useState<Record<string, 'reconnect' | 'timeout'>>({});
  const [sending, setSending] = useState(false);
  const [edit, setEdit] = useState<{ id: string; query: string } | null>(null);
  const [changing, setChanging] = useState(false);
  const [replyContext, setReplyContext] = useState<ReplyContext[]>([]);
  const [error, setError] = useState('');
  const [historyError, setHistoryError] = useState('');
  const [reload, setReload] = useState(0);
  const [deadlines, setDeadlines] = useState<Awaited<ReturnType<typeof api.deadlines>>['notifications']>([]);
  const dismissedDeadlines = useRef(new Set<string>());
  const deadlineStoragePrefix = 'bobby:deadline:dismissed:' + identity + ':';
  const watchers = useRef(new Map<string, () => void>());
  const editMessage = useRef<HTMLElement | null>(null);
  const replaced = useRef(new Set<string>());
  const lock = useRef(false);
  const scroll = useRef<HTMLDivElement>(null);
  const latestTurn = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLTextAreaElement>(null);
  const widgetInput = useRef<HTMLTextAreaElement>(null);
  const nextContextID = useRef(0);
  const inlinePanel = useRef<HTMLDivElement>(null);
  const initialRestore = useRef(true);
  const scrollAfterSend = useRef(false);
  const followLatest = useRef(true);
  const lastAnimatedReply = useRef('');
  const sourceButton = useRef<HTMLElement | null>(null);
  const dictation = useDictation(onDraft, !hidden, !!widget.target);

  useEffect(() => {
    let active = true;
    setHistoryError('');
    void api.history(apiKey).then(({ turns }) => {
      if (active) setJobs(current => {
        const latest = new Map([...turns].reverse().filter(job => !replaced.current.has(job.id)).map(job => [job.id, job]));
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
    if (edit && !jobs.some(job => job.id === edit.id && !completed(job))) setEdit(null);
  }, [edit, jobs]);

  useEffect(() => {
    for (const job of jobs) {
      if (completed(job) || replaced.current.has(job.id) || watchers.current.has(job.id)) continue;
      const stop = watchJob(job.id, { key: apiKey }, update => {
        if (replaced.current.has(update.id)) return;
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
    if (!token) return;
    let active = true;
    const refresh = () => void api.deadlines(apiKey).then(({ notifications }) => {
      if (active) setDeadlines(notifications.filter(item =>
        Date.parse(item.due_at) > Date.now() &&
        !dismissedDeadlines.current.has(item.id) &&
        !localStorage.getItem(deadlineStoragePrefix + item.id),
      ));
    }).catch(() => {});
    refresh();
    const timer = setInterval(refresh, 15_000);
    return () => { active = false; clearInterval(timer); };
  }, [apiKey, token, deadlineStoragePrefix]);

  function dismissDeadline(id: string) {
    dismissedDeadlines.current.add(id);
    localStorage.setItem(deadlineStoragePrefix + id, '1');
    setDeadlines(current => current.filter(item => item.id !== id));
  }

  useEffect(() => {
    if (hidden || !jobs.length || !scroll.current) return;
    const viewport = scroll.current;
    const latest = jobs.at(-1);
    const newReply = latest && revealedAnswers.has(latest.id) && lastAnimatedReply.current !== latest.id;
    if (initialRestore.current || sending || scrollAfterSend.current || followLatest.current) {
      viewport.scrollTop = viewport.scrollHeight;
      if (newReply) {
        const answer = latestTurn.current?.querySelector('.answer-reveal');
        if (answer && answer.getBoundingClientRect().top < viewport.getBoundingClientRect().top) {
          viewport.scrollTop += answer.getBoundingClientRect().top - viewport.getBoundingClientRect().top;
        }
      }
    }
    if (newReply) lastAnimatedReply.current = latest.id;
    initialRestore.current = false;
    if (!sending) scrollAfterSend.current = false;
  }, [hidden, jobs, revealedAnswers, sending]);

  useEffect(() => {
    if (formID || catalog) {
      inlinePanel.current?.scrollIntoView({ block: 'start' });
      inlinePanel.current?.focus({ preventScroll: true });
    }
  }, [formID, catalog]);

  async function send(text: string, fromWidget = false) {
    const question = text.trim();
    if ([...question].length < 2 || lock.current || activeJob) return;
    const context = replyContext.map(({ kind, text }) => {
      const label = kind === 'reply' ? 'Ответ Бобби целиком' : 'Цитата из ответа Бобби';
      return `${label}\n${text.split(/\r?\n/).map(line => '> ' + line).join('\n')}`;
    });
    const query = [question, ...context].join('\n\n');
    dictation.cancel();
    lock.current = true;
    setSending(true);
    setError('');
    try {
      const job = await api.ask(query, { token, key: apiKey });
      scrollAfterSend.current = true;
      if (job.status === 'completed' && job.answer?.trim()) setRevealedAnswers(previous => new Set(previous).add(job.id));
      setJobs(current => upsert(current, { ...job, query, created_at: job.created_at || new Date().toISOString() }));
      onDraft('');
      setReplyContext([]);
      if (!fromWidget) input.current?.focus();
    } catch (err) {
      onDraft(question);
      setError(errorText(err, 'Не удалось отправить вопрос. Попробуйте ещё раз.'));
    } finally {
      setSending(false);
      lock.current = false;
    }
  }

  function submit(event: FormEvent<HTMLFormElement>, fromWidget: boolean) {
    event.preventDefault();
    if (edit) {
      const job = jobs.find(item => item.id === edit.id);
      if (job && !completed(job)) void changeJob(job, edit.query.trim());
    } else {
      void send(draft, fromWidget);
    }
  }

  async function changeJob(job: Job, query?: string) {
    if (lock.current) return;
    if (query !== undefined) {
      if ([...query].length < 2) return;
      if (query === job.query.trim()) { cancelEdit(); return; }
    }
    lock.current = true;
    setChanging(true);
    setError('');
    try {
      if (query === undefined) {
        const stopped = await api.stop(job.id, auth);
        setJobs(current => upsert(current, stopped));
      } else {
        const replacement = await api.edit(job.id, query, auth);
        replaced.current.add(job.id);
        scrollAfterSend.current = true;
        setJobs(current => upsert(current.filter(item => item.id !== job.id), replacement));
        setEdit(null);
      }
      watchers.current.get(job.id)?.();
      watchers.current.delete(job.id);
      setWaiting(current => {
        const next = { ...current };
        delete next[job.id];
        return next;
      });
    } catch (err) {
      setError(errorText(err, query === undefined ? 'Не удалось остановить ответ.' : 'Не удалось изменить вопрос.'));
    } finally {
      setChanging(false);
      lock.current = false;
    }
  }

  function cancelEdit(compact = false) {
    setEdit(null);
    requestAnimationFrame(() => {
      const target = compact ? widgetInput.current : editMessage.current?.querySelector<HTMLButtonElement>('.answer-action');
      target?.focus({ preventScroll: true });
    });
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
  const composerState = sending ? 'thinking' : dictation.active || draft.trim() ? 'listening' : 'idle';
  const latest = jobs.at(-1);
  const activeJob = [...jobs].reverse().find(job => !completed(job));
  const preparingReply = !!latest && !completed(latest) && !waiting[latest.id];
  const widgetState = sending || preparingReply ? 'thinking'
    : dictation.active || draft.trim() ? 'listening'
    : formID || catalog ? 'waiting-form'
    : latest?.status === 'completed' && latest.answer?.trim() ? 'answering' : 'idle';

  function openFromWidget(id?: string) {
    window.focus();
    widget.close();
    if (id) openForm(id);
    else {
      onOpenChat();
      requestAnimationFrame(() => input.current?.focus());
    }
  }

  function addReplyContext(kind: ReplyContext['kind'], text: string, compact: boolean) {
    if (lock.current) return;
    const id = nextContextID.current++;
    setReplyContext(current => [...current, { id, kind, text }]);
    (compact ? widgetInput : input).current?.focus({ preventScroll: true });
  }

  function reply(job: Job, compact = false) {
    const answer = job.answer;
    if (job.status === 'completed') return answer?.trim()
      ? <AnswerActions disabled={sending} onReply={() => addReplyContext('reply', answer, compact)}>
          <Answer text={answer} onForm={compact ? openFromWidget : openForm} reveal={!compact && revealedAnswers.has(job.id)}/>
        </AnswerActions>
      : <p>Ответ пуст. Уточните вопрос или попробуйте снова.</p>;
    if (job.status === 'failed' && job.error === 'Остановлено пользователем') return <p className="message-status muted">Ответ остановлен.</p>;
    if (job.status === 'failed') return <Notice error action={<button className="text-button" onClick={event => {
      onDraft(job.query);
      setReplyContext([]);
      if (compact) event.currentTarget.closest('.bobby-widget')?.querySelector('textarea')?.focus();
      else input.current?.focus();
    }}>Повторить</button>}>
      {job.error?.toLowerCase().match(/timeout|deadline exceeded/) ? 'Время ожидания ответа истекло.' : 'Не удалось получить ответ.'}
    </Notice>;
    if (waiting[job.id] === 'timeout') return <Notice error action={<button className="text-button" onClick={() => {
      watchers.current.delete(job.id);
      setJobs(current => [...current]);
    }}>Проверить ответ</button>}>Ответ пока не получен.</Notice>;
    return <p className="message-status muted"><span className="loading-dot"/>{waiting[job.id] === 'reconnect' ? 'Проверяем статус…' : job.status === 'queued' ? 'Вопрос в очереди…' : 'Готовлю ответ…'}</p>;
  }

  function composer(compact = false) {
    const surface = compact ? 'widget' : 'chat';
    const value = edit?.query ?? draft;
    return <>{dictation.message?.surface === surface && <Notice error>{dictation.message.text}</Notice>}
    <form className={edit ? 'compose-panel is-editing' : 'compose-panel'} onSubmit={event => submit(event, compact)}
      onKeyDown={event => {
        if (edit && event.key === 'Escape' && !changing && !event.nativeEvent.isComposing) {
          event.preventDefault();
          event.stopPropagation();
          cancelEdit(compact);
        }
      }}>
      {edit && <div className="composer-edit-header">
        <span role="status">Редактирование вопроса</span>
        <button type="button" className="answer-action" aria-label="Отменить редактирование" title="Отменить редактирование"
          disabled={changing} onClick={() => cancelEdit(compact)}><Icon name="close"/></button>
      </div>}
      {!edit && !!replyContext.length && <ul className="reply-context" aria-label="Контекст ответа">
        {replyContext.map(({ id, kind, text }) => {
          const removeLabel = kind === 'reply' ? 'Убрать ответ' : 'Убрать цитату';
          return <li key={id}>
            <blockquote aria-label={kind === 'reply' ? 'Ответ Бобби целиком' : 'Цитата'} title={text}>{text}</blockquote>
            <button type="button" className="answer-action" aria-label={removeLabel} title={removeLabel}
              disabled={sending} onClick={() => {
                setReplyContext(current => current.filter(item => item.id !== id));
                (compact ? widgetInput : input).current?.focus();
              }}><Icon name="close"/></button>
          </li>;
        })}
      </ul>}
      <div className="composer">
        <textarea ref={compact ? widgetInput : input} value={value} readOnly={sending || !!edit && changing} onChange={event => {
          if (edit) setEdit({ ...edit, query: event.target.value });
          else onDraft(event.target.value);
        }} onKeyDown={event => {
          if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
            event.preventDefault();
            event.currentTarget.form?.requestSubmit();
          }
        }} aria-label={edit ? 'Изменённый вопрос' : 'Ваш вопрос'} placeholder="Напишите вопрос Бобби" rows={1}/>
        {!edit && microphone(surface)}
        {activeJob && <button type="button" className="send-button stop-button" aria-label="Остановить ответ" title="Остановить ответ"
          disabled={sending || changing} onClick={() => void changeJob(activeJob)}><Icon name="stop"/></button>}
        {(!activeJob || edit) && <button className="send-button" aria-label={edit ? 'Отправить изменённый вопрос' : sending ? 'Отправляем вопрос' : 'Отправить вопрос'}
              title={edit ? 'Отправить изменённый вопрос' : 'Отправить вопрос'}
              disabled={sending || changing || [...value.trim()].length < 2}><Icon name="send"/></button>}
      </div>
    </form></>;
  }

  function microphone(surface: 'chat' | 'widget', reveal?: () => void) {
    const recording = dictation.active === surface;
    const label = recording ? 'Остановить диктовку' : 'Голосовой ввод';
    return <button type="button" className="icon-button mic-button" title={label} aria-label={label}
      aria-pressed={recording} disabled={!!edit || sending || !!dictation.active && !recording}
      onClick={() => {
        reveal?.();
        dictation.toggle(surface);
      }}><Icon name={recording ? 'stop' : 'mic'}/></button>;
  }

  return <><div className={'chat-page' + (hasMessages ? ' has-messages' : '')} hidden={hidden}>
    {deadlines.length > 0 && <div className="deadline-notices">
      <div className="chat-inner">
        {deadlines.map(item => <Notice key={item.id} error action={
          <button type="button" className="text-button" onClick={() => dismissDeadline(item.id)}>Закрыть уведомление</button>
        }>
          <strong>{item.kind === 'mentor_deadline' || item.kind === 'mentor_24h' ? 'Помогите стажёру' : 'Скоро дедлайн'}</strong>
          <br/><External href={item.url}>{item.key} · {item.summary}</External> · {timeText(item.due_at)}
        </Notice>)}
      </div>
    </div>}
    <div className="chat-scroll" ref={scroll} onScroll={event => {
      const viewport = event.currentTarget;
      followLatest.current = viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight < 64;
    }}>
      <div className="chat-inner">
        {historyError && <Notice error action={<button className="text-button" onClick={() => setReload(value => value + 1)}>Повторить</button>}>{historyError}</Notice>}
        {!hasMessages && <div className="welcome">
          <Illustration state={composerState}/>
          <h1>Привет, я Бобби!</h1>
          <p>Помогу освоиться в команде и разобраться с первыми задачами.</p>
        </div>}
        {hasMessages && <AnswerSelection active={!hidden} disabled={sending} onQuote={text => addReplyContext('quote', text, false)}>
        <div className="messages" aria-label="Сообщения чата">
          {jobs.map((job, index) => <div key={job.id} className="turn" ref={job.id === latest?.id ? latestTurn : undefined}>
            <div className="user-message">
              <p>{job.query}</p>
              <div className="message-actions">
                <Stamp value={job.created_at}/>
                {!completed(job) && <button type="button" className="answer-action" aria-label="Изменить вопрос" title="Изменить вопрос"
                  disabled={sending || changing} onClick={event => {
                    dictation.cancel();
                    editMessage.current = event.currentTarget.closest('.user-message');
                    if (edit?.id !== job.id) setEdit({ id: job.id, query: job.query });
                    input.current?.focus({ preventScroll: true });
                  }}><Icon name="edit"/></button>}
              </div>
            </div>
            <div className="assistant-message" aria-live="polite">
              <span className="message-avatar"><Bobby state={index < jobs.length - 1 ? 'idle'
                : composerState === 'listening' ? 'listening'
                : job.status === 'completed' && job.answer?.trim() ? 'answering'
                : !completed(job) && !waiting[job.id] ? 'thinking' : 'idle'}/></span>
              <strong className="message-name">Бобби</strong>
              <div className="message-body">
                {reply(job)}
                {completed(job) && <Stamp value={job.completed_at || job.created_at}/>}
              </div>
            </div>
          </div>)}
          {(catalog || formID) && <div className="inline-panel" ref={inlinePanel} tabIndex={-1}>
            {catalog && !formID && <FormCatalog token={token} apiKey={apiKey} onForm={openForm} onClose={closeForm}/>}
            {formID && <AccessForm key={formID} id={formID} token={auth.token} apiKey={auth.key} identity={identity} onClose={closeForm}/>}
          </div>}
        </div></AnswerSelection>}
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
        {jobs.length > 0 && !catalog && !formID && <div className="chat-shortcuts">
          <button className="button secondary" onClick={onCatalog}><Icon name="key"/>Запросить доступ</button>
        </div>}
        {error && <Notice error>{error}</Notice>}
        {composer()}
        <p className="composer-hint">Бобби может ошибаться. Проверяйте важное по ссылкам в ответах.</p>
      </div>
    </div>
  </div>
    {widget.target && <BobbyWidget target={widget.target} fallback={widget.fallback} state={widgetState}
      jobID={latest?.id} query={latest?.query} reply={expanded => latest &&
        <AnswerSelection active={expanded} disabled={sending} onQuote={text => addReplyContext('quote', text, true)}>
          {reply(latest, true)}
        </AnswerSelection>} error={error}
      onClose={widget.close} onChat={openFromWidget} renderMicrophone={reveal => microphone('widget', reveal)}>
      {composer(true)}
    </BobbyWidget>}
  </>;
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
