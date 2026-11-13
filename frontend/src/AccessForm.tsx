import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { api, errorText } from './api';
import type { Field, FormDefinition, Receipt, Resource, Values } from './api';
import { fieldValue, newId } from './model';
import { Notice, statusText } from './ui';
import { Icon } from './visuals';

type Props = { id: string; token: string; apiKey: string; identity: string; onClose: () => void };
type Review = { version: number; values: Values; idem: string; sent?: boolean; receipt?: Receipt };

export function AccessForm({ id, token, apiKey, identity, onClose }: Props) {
  const storageKey = 'bobby-form:' + identity + ':' + id;
  const [form, setForm] = useState<FormDefinition>();
  const [resources, setResources] = useState<Resource[]>([]);
  const [resourceID, setResourceID] = useState('');
  const [review, setReview] = useState<Review | undefined>(() => {
    const saved = sessionStorage.getItem(storageKey);
    return saved ? JSON.parse(saved) as Review : undefined;
  });
  const receipt = review?.receipt;
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const [reload, setReload] = useState(0);
  const pending = useRef(false);
  const result = useRef<HTMLDivElement>(null);
  const auth = { token, key: apiKey };

  function remember(value?: Review) {
    if (value) sessionStorage.setItem(storageKey, JSON.stringify(value));
    else sessionStorage.removeItem(storageKey);
    setReview(value);
  }

  useEffect(() => {
    let active = true;
    if (!token) return;
    setError('');
    async function load() {
      const definition = await api.form(id, { token, key: apiKey });
      const catalog = definition.kind === 'access' ? await api.resources({ token, key: apiKey }) : null;
      if (active) {
        setResources(catalog?.resources || []);
        setForm(definition);
      }
    }
    void load().catch(err => { if (active) setError(errorText(err, 'Форма или каталог ресурсов сейчас недоступны.')); });
    return () => { active = false; };
  }, [id, token, apiKey, reload]);

  useEffect(() => { if (review || receipt) result.current?.focus(); }, [review, receipt]);

  if (!token) return <section className="card inline-form"><Notice error>Для заявки нужна сессия команды. Войдите через профиль.</Notice><button className="button secondary" onClick={onClose}>Закрыть форму</button></section>;
  if (!form) return <section className="card inline-form">
    <Notice error={!!error} action={error && <button className="button secondary" onClick={() => setReload(value => value + 1)}>Повторить</button>}>
      {error || 'Загружаем форму…'}
    </Notice>
    <button className="button secondary" onClick={onClose}>Закрыть форму</button>
  </section>;

  const definition = form;
  const fields = definition.schema.properties || {};
  const labels = definition.ui_schema?.labels || {};
  const order = [...new Set([...(definition.ui_schema?.order || []), ...Object.keys(fields)])].filter(name => fields[name]);
  const selected = resources.find(item => item.id === resourceID);
  const blocked = definition.kind === 'access' && resources.length === 0;

  async function verify(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending.current) return;
    pending.current = true;
    setBusy(true);
    setError('');
    try {
      const data = new FormData(event.currentTarget);
      const values = Object.fromEntries(order.flatMap(name => {
        const field = fields[name];
        const raw = String(data.get(name) ?? '');
        if (!raw && field.type !== 'boolean' && !definition.schema.required?.includes(name)) return [];
        return [[name, fieldValue(raw, field)]];
      }));
      const checked = await api.validate(id, definition.version, values, auth);
      if (!checked.valid) throw new Error('invalid');
      remember({ version: definition.version, values, idem: newId() });
    } catch (err) {
      setError(errorText(err, 'Не удалось проверить форму. Проверьте значения полей и повторите.'));
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }

  async function submit() {
    if (!review || pending.current || receipt) return;
    pending.current = true;
    setBusy(true);
    setError('');
    try {
      const attempt = { ...review, sent: true };
      remember(attempt);
      const submitted = await api.submit(id, review.version, review.values, review.idem, auth);
      if (!submitted.id || !submitted.status) throw new Error('Missing receipt');
      remember({ ...attempt, receipt: submitted });
    } catch (err) {
      setError(errorText(err, 'Подтверждение не получено. Повторите отправку этой заявки или проверьте права доступа.'));
    } finally {
      pending.current = false;
      setBusy(false);
    }
  }

  function renderField(name: string, field: Field) {
    const required = definition.schema.required?.includes(name);
    const props = { name, required };
    if (definition.kind === 'access' && name === 'resource_id') return <select {...props} value={resourceID} onChange={event => setResourceID(event.target.value)}>
      <option value="">Выберите ресурс</option>{resources.map(resource => <option key={resource.id} value={resource.id}>{resource.name}</option>)}
    </select>;
    if (definition.kind === 'access' && name === 'capability') return <select key={resourceID} {...props} disabled={!selected} defaultValue="">
      <option value="">Выберите право</option>{selected?.capabilities.map(value => <option key={value} value={value}>{value === 'browse_issues' ? 'Просмотр задач' : value}</option>)}
    </select>;
    if (field.enum) return <select {...props} defaultValue=""><option value="">Выберите значение</option>{field.enum.map((value, i) => <option key={i} value={i}>{String(value)}</option>)}</select>;
    if (field.type === 'boolean') return <input type="checkbox" name={name}/>;
    if (field.type === 'object' || field.type === 'array' || name === 'reason') return <textarea {...props} minLength={field.minLength} maxLength={field.maxLength} rows={3}/>;
    const numeric = field.type === 'number' || field.type === 'integer';
    const type = numeric ? 'number' : field.format === 'email' || field.format === 'date' ? field.format : 'text';
    return <input {...props} type={type} step={numeric ? field.type === 'integer' ? 1 : 'any' : undefined}
      minLength={field.minLength} maxLength={field.maxLength}/>;
  }

  return <section className="card inline-form" aria-label={definition.title}>
    <div className="section-heading"><h3>{definition.title}</h3><button className="icon-button" disabled={busy} aria-label="Закрыть форму" onClick={onClose}><Icon name="close"/></button></div>
    {definition.description && <p className="muted">{definition.description}</p>}
    {blocked && <Notice>В каталоге нет ресурсов для вашей команды.</Notice>}
    {!receipt && <form hidden={!!review || blocked} onSubmit={verify} className="field-stack">
      {order.map(name => <label key={name}>
        <span>{labels[name] || fields[name].title || name}{definition.schema.required?.includes(name) && <span aria-hidden="true"> *</span>}</span>
        {renderField(name, fields[name])}
      </label>)}
      <button className="button primary" disabled={busy}>{busy ? 'Проверяем…' : 'Проверить заявку'}</button>
    </form>}
    <div ref={result} tabIndex={-1}>
      {review && !receipt && <>
        <h4>Проверьте перед отправкой</h4>
        <dl className="review-values">{Object.entries(review.values).map(([name, value]) => <div key={name}>
          <dt>{labels[name] || fields[name]?.title || name}</dt><dd>{name === 'resource_id' && selected ? selected.name : typeof value === 'string' ? value : JSON.stringify(value)}</dd>
        </div>)}</dl>
        <div className="actions">
          <button className="button primary" disabled={busy} onClick={() => void submit()}>{busy ? 'Отправляем…' : 'Подтвердить отправку'}</button>
          {!review.sent && <button className="button secondary" disabled={busy} onClick={() => remember()}>Изменить</button>}
        </div>
      </>}
      {receipt && <>
        <Notice><strong>{statusText(receipt.status)}</strong><br/>Номер заявки {receipt.id}</Notice>
        <button className="button secondary" onClick={() => remember()}>Заполнить новую заявку</button>
      </>}
      {error && <Notice error>{error}</Notice>}
    </div>
  </section>;
}
