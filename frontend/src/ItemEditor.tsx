import { useEffect, useState } from 'react';
import type { FormEvent } from 'react';
import { api, errorText } from './api';
import { Modal, Notice } from './ui';

export type EditableItem =
  | { kind: 'jira'; key: string; summary: string; dueAt?: string; dateOnly?: boolean }
  | { kind: 'calendar'; id: string; summary: string; start?: string; end?: string }
  | { kind: 'gitlab'; project: number; title: string };

type Props = { item: EditableItem; token: string; apiKey: string; onClose: () => void; onSave: () => void };
const auth = (token: string, key: string) => ({ token, key });

function moscow(value?: string) {
  const time = value ? Date.parse(value) : NaN;
  return Number.isFinite(time) ? new Date(time + 3 * 3600000).toISOString().slice(0, 16) : '';
}
function timestamp(value: string) { return value.length === 16 ? value + ':00+03:00' : value + '+03:00'; }

export function ItemEditor({ item, token, apiKey, onClose, onSave }: Props) {
  if (item.kind === 'jira') return <JiraEditor item={item} token={token} apiKey={apiKey} onClose={onClose} onSave={onSave}/>;
  if (item.kind === 'calendar') return <CalendarEditor item={item} token={token} apiKey={apiKey} onClose={onClose} onSave={onSave}/>;
  return <BranchEditor item={item} token={token} apiKey={apiKey} onClose={onClose} onSave={onSave}/>;
}

function JiraEditor({ item, token, apiKey, onClose, onSave }: Props & { item: Extract<EditableItem, { kind: 'jira' }> }) {
  const original = item.dateOnly ? item.dueAt?.slice(0, 10) || '' : moscow(item.dueAt);
  const [summary, setSummary] = useState(item.summary);
  const [description, setDescription] = useState('');
  const [due, setDue] = useState(original);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const changed = summary !== item.summary || description !== '' || due !== original;

  async function save(event: FormEvent) {
    event.preventDefault();
    if (!changed || busy) return;
    const body: { summary?: string; description?: string; due_date?: string; due_at?: string | null } = {};
    if (summary !== item.summary) body.summary = summary.trim();
    if (description !== '') body.description = description;
    if (due !== original) {
      body.due_date = due.slice(0, 10);
      body.due_at = due.includes('T') ? timestamp(due) : null;
    }
    setBusy(true);
    setError('');
    try { await api.updateJiraIssue(item.key, body, auth(token, apiKey)); onSave(); onClose(); }
    catch (err) { setError(errorText(err, 'Не удалось сохранить задачу.')); }
    finally { setBusy(false); }
  }

  return <Modal title={'Задача ' + item.key} onClose={onClose} className="item-editor">
    {error && <Notice error>{error}</Notice>}
    <form onSubmit={save} className="field-stack">
      <label>Название задачи<input value={summary} required onChange={e => setSummary(e.target.value)}/></label>
      <label>Новое описание<textarea rows={4} value={description} onChange={e => setDescription(e.target.value)} placeholder="Оставьте пустым, если описание менять не нужно"/></label>
      <label>Срок (МСК)<input type={due.includes('T') ? 'datetime-local' : 'date'} value={due} onChange={e => setDue(e.target.value)}/></label>
      <button type="button" className="text-button" onClick={() => setDue(due.includes('T') ? due.slice(0, 10) : (due || new Date().toISOString().slice(0, 10)) + 'T12:00')}>Изменить формат срока: дата / дата и время</button>
      <div className="actions"><button className="button primary" disabled={!changed || busy}>{busy ? 'Сохраняем…' : 'Сохранить'}</button></div>
    </form>
  </Modal>;
}

function CalendarEditor({ item, token, apiKey, onClose, onSave }: Props & { item: Extract<EditableItem, { kind: 'calendar' }> }) {
  const originalStart = moscow(item.start);
  const originalEnd = moscow(item.end);
  const [start, setStart] = useState(originalStart);
  const [end, setEnd] = useState(originalEnd);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const changed = start !== originalStart || end !== originalEnd;

  async function save(event: FormEvent) {
    event.preventDefault();
    if (!changed || busy) return;
    setBusy(true);
    setError('');
    try {
      await api.updateCalendarEvent(item.id, {
        ...(start !== originalStart ? { start: timestamp(start) } : {}),
        ...(end !== originalEnd ? { end: timestamp(end) } : {}),
      }, auth(token, apiKey));
      onSave(); onClose();
    } catch (err) { setError(errorText(err, 'Не удалось сохранить событие.')); }
    finally { setBusy(false); }
  }

  return <Modal title={'Событие · ' + item.summary} onClose={onClose} className="item-editor">
    {error && <Notice error>{error}</Notice>}
    <form onSubmit={save} className="field-stack">
      <label>Начало (МСК)<input type="datetime-local" value={start} required onChange={e => setStart(e.target.value)}/></label>
      <label>Окончание (МСК)<input type="datetime-local" value={end} required onChange={e => setEnd(e.target.value)}/></label>
      <div className="actions"><button className="button primary" disabled={!changed || busy}>{busy ? 'Сохраняем…' : 'Сохранить'}</button></div>
    </form>
  </Modal>;
}

function BranchEditor({ item, token, apiKey, onClose, onSave }: Props & { item: Extract<EditableItem, { kind: 'gitlab' }> }) {
  const [branches, setBranches] = useState<{ name: string }[]>();
  const [current, setCurrent] = useState('');
  const [name, setName] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    let active = true;
    api.gitlabBranches(item.project, auth(token, apiKey)).then(result => {
      if (!active) return;
      setBranches(result.data || []);
      setCurrent(result.data?.[0]?.name || '');
      setName(result.data?.[0]?.name || '');
    }).catch(err => { if (active) setError(errorText(err, 'Не удалось загрузить ветки.')); });
    return () => { active = false; };
  }, [item.project, token, apiKey]);

  async function save(event: FormEvent) {
    event.preventDefault();
    if (!current || !name.trim() || name === current || busy) return;
    setBusy(true);
    setError('');
    try { await api.renameGitlabBranch(item.project, current, name.trim(), auth(token, apiKey)); onSave(); onClose(); }
    catch (err) { setError(errorText(err, 'Не удалось переименовать ветку.')); }
    finally { setBusy(false); }
  }

  return <Modal title={'Ветки · ' + item.title} onClose={onClose} className="item-editor">
    {error && <Notice error>{error}</Notice>}
    {!branches && !error && <Notice>Загружаем ветки…</Notice>}
    {branches && !branches.length && <Notice>В проекте нет веток.</Notice>}
    {!!branches?.length && <form onSubmit={save} className="field-stack">
      <label>Ветка<select value={current} onChange={e => { setCurrent(e.target.value); setName(e.target.value); }}>
        {branches.map(branch => <option key={branch.name} value={branch.name}>{branch.name}</option>)}
      </select></label>
      <label>Новое название<input value={name} required onChange={e => setName(e.target.value)}/></label>
      <div className="actions"><button className="button primary" disabled={name.trim() === current || busy}>{busy ? 'Сохраняем…' : 'Переименовать'}</button></div>
    </form>}
  </Modal>;
}
