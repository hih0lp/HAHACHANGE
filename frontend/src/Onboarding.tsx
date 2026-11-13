import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { api, ApiError, errorText } from './api';
import type { Scenario } from './api';
import { newId } from './model';
import { External, Notice, Stamp, statusText } from './ui';
import { Icon } from './visuals';

type Props = { token: string; storageKey: string; onProfile: () => void; onChat: (query: string) => void };
type Pending = { reason: string; idem: string; revision: number };

export function Onboarding({ token, storageKey, onProfile, onChat }: Props) {
  const [state, setState] = useState<Scenario>();
  const [error, setError] = useState('');
  const [restoring, setRestoring] = useState(!!localStorage.getItem(storageKey) && !!token);
  const [busy, setBusy] = useState(false);
  const [unrestored, setUnrestored] = useState(false);
  const [retry, setRetry] = useState(0);
  const lock = useRef(false);
  const pendingKey = storageKey + ':pending';
  const pending = useRef<Pending | undefined>(JSON.parse(sessionStorage.getItem(pendingKey) || 'null') || undefined);
  const submitHeading = useRef<HTMLHeadingElement>(null);

  function save(next: Scenario) {
    localStorage.setItem(storageKey, next.scenario_id);
    setState(next);
  }

  useEffect(() => {
    const id = localStorage.getItem(storageKey);
    if (!id || !token) { setRestoring(false); return; }
    let active = true;
    setRestoring(true);
    setUnrestored(false);
    setError('');
    void api.scenario(id, token).then(next => { if (active) setState(next); }).catch(err => {
      if (!active) return;
      if (err instanceof ApiError && err.status === 404) localStorage.removeItem(storageKey);
      else {
        setUnrestored(true);
        setError(errorText(err, 'Не удалось восстановить прогресс. Попробуйте ещё раз.'));
      }
    }).finally(() => { if (active) setRestoring(false); });
    return () => { active = false; };
  }, [storageKey, token, retry]);

  useEffect(() => {
    if (!state || state.status !== 'waiting_access' || !token) return;
    let active = true;
    let running = false;
    const interval = setInterval(async () => {
      if (running || lock.current) return;
      running = true;
      try {
        const next = await api.scenario(state.scenario_id, token);
        if (active && !lock.current) setState(current => current && current.revision <= next.revision ? next : current);
      } catch {
        // The explicit refresh action reports errors without announcing each background retry.
      } finally {
        running = false;
      }
    }, 5_000);
    return () => { active = false; clearInterval(interval); };
  }, [state?.scenario_id, state?.status, token]);

  async function perform(action: () => Promise<Scenario>) {
    if (lock.current) return;
    lock.current = true;
    setBusy(true);
    setError('');
    try {
      save(await action());
      pending.current = undefined;
      sessionStorage.removeItem(pendingKey);
    } catch (err) {
      if (err instanceof ApiError && err.status === 409 && state) {
        try {
          const next = await api.scenario(state.scenario_id, token);
          save(next);
          if (next.revision !== state.revision) {
            pending.current = undefined;
            sessionStorage.removeItem(pendingKey);
          }
          if (!next.widget.request_id) setError('Состояние изменилось. Проверьте текущий шаг.');
        } catch (refreshError) {
          setError(errorText(refreshError, 'Обновите состояние перед повторной отправкой.'));
        }
      } else setError(errorText(err, 'Действие не удалось подтвердить. Проверьте состояние или попробуйте снова.'));
    } finally {
      lock.current = false;
      setBusy(false);
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!state || lock.current) return;
    const data = new FormData(event.currentTarget);
    const reason = String(data.get('reason') || '').trim();
    if ([...reason].length < 2) { setError('Укажите, зачем нужен доступ.'); return; }
    const current = pending.current?.revision === state.revision ? pending.current : { reason, idem: newId(), revision: state.revision };
    pending.current = current;
    sessionStorage.setItem(pendingKey, JSON.stringify(current));
    void perform(() => api.scenarioRequest(state, current.reason, current.idem, token));
  }

  const widget = state?.widget;
  useEffect(() => { if (widget?.request_id) submitHeading.current?.focus(); }, [widget?.request_id]);

  return <div className="section-page">
    <header className="page-intro"><div className="page-symbol mint"><Icon name="route"/></div><h1>Адаптация</h1>
      <p>Проверьте доступы и подготовьтесь к первой задаче.</p>
    </header>
    <section className="card workflow">
      <div className="section-heading"><h2>{state ? state.goal : 'Первая задача'}</h2>{state && <span className="pill">{statusText(state.status)}</span>}</div>
      {!token && <><p>Войдите в демо-аккаунт, чтобы увидеть свой прогресс.</p><button className="button primary" onClick={onProfile}>Открыть профиль</button></>}
      {restoring && <Notice>Восстанавливаем прогресс…</Notice>}
      {token && !state && !restoring && !unrestored && <>
        <p>Сценарий ещё не начат. Бобби проверит первый шаг по данным команды.</p>
        <button className="button primary" disabled={busy} onClick={() => void perform(() => api.start(token))}>{busy ? 'Проверяем…' : 'Начать адаптацию'}<Icon name="arrow"/></button>
      </>}
      {state && <>
        <p>{state.message}</p>
        <h3>Чек-лист</h3>
        {state.checks?.length ? <ul className="check-list">{state.checks.map(check => <li key={check.resource_id + ':' + check.capability}>
          <span className={'check-icon ' + (check.state === 'granted' ? 'mint' : 'sky')}><Icon name={check.state === 'granted' ? 'check' : 'key'}/></span>
          <div><strong>{widget?.resource?.id === check.resource_id ? widget.resource.name : check.resource_id}</strong>
            <div className="muted">{statusText(check.state)} · <Stamp value={check.checked_at}/></div></div>
        </li>)}</ul> : <p className="muted">Подтверждённых пунктов пока нет.</p>}
        {widget?.visible && <div className="next-step">
          <h3 ref={submitHeading} tabIndex={-1}>Следующий шаг</h3>
          {widget.type === 'access_request_form' && <form className="field-stack" onSubmit={submit}>
            <label>Зачем нужен доступ к {widget.resource?.name}<textarea name="reason" rows={3} required minLength={2} maxLength={1000}
              defaultValue={pending.current?.revision === state.revision ? pending.current.reason : ''} readOnly={busy || pending.current?.revision === state.revision}/></label>
            <label className="checkbox-field"><input type="checkbox" name="confirmed" required disabled={busy}/>Подтверждаю отправку заявки</label>
            <button className="button primary" disabled={busy}>{busy ? 'Отправляем…' : pending.current ? 'Повторить отправку' : 'Отправить заявку'}</button>
          </form>}
          {widget.request_id && <Notice><strong>Заявка на согласовании</strong><br/>Номер заявки {widget.request_id}</Notice>}
          {widget.type === 'task_selection' && <div className="actions">
            {widget.resource && <External href={widget.resource.url}>Открыть {widget.resource.name}</External>}
            <button className="button secondary" onClick={() => onChat('Как выбрать первую задачу?')}>Спросить Бобби</button>
          </div>}
          {widget.type !== 'task_selection' && <button className="button secondary" disabled={busy} onClick={() => void perform(() => api.advance(state, token))}>
            Проверить текущий доступ
          </button>}
        </div>}
      </>}
      {error && <Notice error action={unrestored && <button className="button secondary" onClick={() => setRetry(value => value + 1)}>Повторить</button>}>{error}</Notice>}
    </section>
  </div>;
}
