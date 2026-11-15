import { useEffect, useState } from 'react';
import type { FormEvent, ReactNode } from 'react';
import { api, ApiError, errorText } from './api';
import { Modal, Notice } from './ui';
import { newId } from './model';
import type { CalendarData, CalendarEntry, GitLabData, GitLabProject, JiraData, MockData, MockSource, Snapshot } from './mockData';

const labels: Record<MockSource, string> = {
  jira: 'Jira', calendar: 'Календарь наставника', 'company-calendar': 'Календарь компании', gitlab: 'GitLab',
};

type Props = { source: MockSource; token: string; apiKey: string; onClose: () => void; onSave: () => void };

export function MockEditor({ source, token, apiKey, onClose, onSave }: Props) {
  const [snapshot, setSnapshot] = useState<Snapshot<MockData>>();
  const [draft, setDraft] = useState<MockData>();
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [saved, setSaved] = useState(false);
  const [reload, setReload] = useState(0);
  const dirty = draft !== snapshot?.data;

  useEffect(() => {
    const abort = new AbortController();
    setMessage('');
    setSnapshot(undefined);
    setDraft(undefined);
    setSaved(false);
    api.mockData(source, { token, key: apiKey, signal: abort.signal }).then(result => {
      if (abort.signal.aborted) return;
      setSnapshot(result);
      setDraft(result.data);
    }).catch(err => {
      if (!abort.signal.aborted) setMessage(errorText(err, 'Не удалось загрузить учебные данные.'));
    });
    return () => abort.abort();
  }, [source, token, apiKey, reload]);

  function close() {
    if (!busy && (!dirty || window.confirm('Закрыть без сохранения изменений?'))) onClose();
  }

  async function save(event: FormEvent) {
    event.preventDefault();
    if (!snapshot || !draft || !dirty || busy) return;
    setBusy(true);
    setMessage('');
    setSaved(false);
    try {
      const result = await api.saveMockData(source, { version: snapshot.version, data: draft }, { token, key: apiKey });
      setSnapshot(result);
      setDraft(result.data);
      setSaved(true);
      onSave();
    } catch (err) {
      setMessage(err instanceof ApiError && (err.status === 400 || err.status === 409)
        ? err.message : errorText(err, 'Не удалось сохранить изменения.'));
    } finally {
      setBusy(false);
    }
  }

  return <Modal className="mock-editor" title={'Редактирование · ' + labels[source]} onClose={close}>
    {message && <Notice error action={<button type="button" className="button secondary" disabled={busy}
      onClick={() => { if (!dirty || window.confirm('Загрузить новую версию и сбросить изменения?')) setReload(n => n + 1); }}>Загрузить заново</button>}>{message}</Notice>}
    {!draft && !message && <Notice>Загружаем учебные данные…</Notice>}
    {draft && <form onSubmit={save}>
      <fieldset disabled={busy} className="mock-editor-fields">
        {source === 'jira' && <JiraFields data={draft as JiraData} change={setDraft}/>}
        {(source === 'calendar' || source === 'company-calendar') && <CalendarFields data={draft as CalendarData} change={setDraft}/>}
        {source === 'gitlab' && <GitLabFields data={draft as GitLabData} change={setDraft}/>}
      </fieldset>
      <div className="actions mock-editor-actions">
        <button className="button primary" disabled={!dirty || busy}>{busy ? 'Сохраняем…' : 'Сохранить изменения'}</button>
        {saved && !dirty && <span role="status">Изменения сохранены</span>}
      </div>
    </form>}
  </Modal>;
}

type Option = [string, string];
type FieldProps = { label: string; value?: string; change: (value: string) => void; required?: boolean };

function Text({ label, value, change, required, type = 'text', multiline }: FieldProps & { type?: string; multiline?: boolean }) {
  return <label>{label}{multiline
    ? <textarea rows={3} value={value ?? ''} onChange={e => change(e.target.value)} required={required}/>
    : <input type={type} step={type === 'datetime-local' ? 1 : undefined} value={value ?? ''} onChange={e => change(e.target.value)} required={required}/>}
  </label>;
}

function Choice({ label, value, change, required, options }: FieldProps & { options: Option[] }) {
  return <label>{label}<select aria-label={label} value={value ?? ''} onChange={e => change(e.target.value)} required={required}>
    {options.map(([id, text]) => <option key={id} value={id}>{text}</option>)}
  </select></label>;
}

function Flag({ label, value, change }: { label: string; value: boolean; change: (value: boolean) => void }) {
  return <label className="checkbox-field"><input type="checkbox" checked={value} onChange={e => change(e.target.checked)}/>{label}</label>;
}

type RowsProps<T> = {
  noun: string; objectLabel: string; rows: T[]; describe: (record: T) => string; add: () => T; change: (rows: T[]) => void;
  render: (record: T, update: (record: T) => void, index: number) => ReactNode;
};

function Rows<T>({ noun, objectLabel, rows, describe, add, change, render }: RowsProps<T>) {
  const [index, setIndex] = useState(0);
  const current = Math.min(index, rows.length - 1);
  const selected = rows[current];
  return <>
    <div className="mock-record-tools">
      <label>{noun}<select aria-label={noun} value={current} onChange={e => setIndex(Number(e.target.value))} disabled={!rows.length}>
        {rows.length === 0 && <option value={-1}>Список пуст</option>}
        {rows.map((row, i) => <option key={i} value={i}>{describe(row)}</option>)}
      </select></label>
      <button type="button" className="button secondary" onClick={() => { change([...rows, add()]); setIndex(rows.length); }}>{'Добавить ' + objectLabel}</button>
    </div>
    {selected !== undefined && <div key={current} className="mock-record">
      {render(selected, next => change(rows.map((row, i) => i === current ? next : row)), current)}
      <button type="button" className="button secondary mock-remove" onClick={() => {
        if (window.confirm('Удалить запись «' + describe(selected) + '»? Это вступит в силу после сохранения.')) {
          change(rows.filter((_, i) => i !== current));
          setIndex(Math.max(0, current - 1));
        }
      }}>{'Удалить ' + objectLabel}</button>
    </div>}
  </>;
}

function JiraFields({ data, change }: { data: JiraData; change: (value: JiraData) => void }) {
  const statuses: Option[] = [['new','К выполнению'], ['indeterminate','В работе'], ['done','Готово']];
  const assignees = new Map(data.assignees?.map(({ account_id, name }) => [account_id, name]) ??
    [['demo-intern', 'Стажёр 1'], ['demo-intern2', 'Стажёр 2']]);
  for (const issue of data.issues) if (issue.assignee_account_id) {
    assignees.set(issue.assignee_account_id, issue.assignee_name || assignees.get(issue.assignee_account_id) || issue.assignee_account_id);
  }
  return <Rows noun="Задача" objectLabel="задачу" rows={data.issues} change={issues => change({ ...data, issues })}
    describe={issue => issue.key + ' · ' + (issue.summary || 'Новая задача')}
    add={() => ({ key: data.project_key + '-' + (Math.max(0, ...data.issues.map(i => Number(i.key.split('-').at(-1)) || 0)) + 1),
      summary: '', description: '', status: 'К выполнению', status_category: 'new' })}
    render={(issue, set) => <div className="field-stack">
      <div className="mock-field-pair">
        <Text label="Ключ задачи" value={issue.key} change={key => set({ ...issue, key })} required/>
        <Choice label="Статус" options={statuses} value={issue.status_category} change={status_category => {
          set({ ...issue, status_category, status: statuses.find(([id]) => id === status_category)?.[1] || issue.status });
        }}/>
      </div>
      <Text label="Название задачи" value={issue.summary} change={summary => set({ ...issue, summary })} required/>
      <Text label="Описание" value={issue.description} change={description => set({ ...issue, description })} multiline/>
      <Choice label="Исполнитель" options={[['','Без исполнителя'], ...assignees]} value={issue.assignee_account_id}
        change={assignee_account_id => set({ ...issue, assignee_account_id, assignee_name: assignees.get(assignee_account_id) || '' })}/>
      <div className="mock-field-pair">
        <Text label="Дата начала" type="date" value={issue.start_date} change={start_date => set({ ...issue, start_date })}/>
        <Text label="Срок (дата)" type="date" value={issue.due_date} change={due_date => set({ ...issue, due_date, due_at: "" })}/>
      </div>
      <Text label="Точное время срока (МСК)" type="datetime-local" value={moscow(issue.due_at)}
        change={date => set({ ...issue, due_at: withMoscow(date), due_date: date ? date.slice(0, 10) : issue.due_date })}/>
    </div>}/>;
}

function moscow(date?: string) {
  const time = date ? Date.parse(date) : NaN;
  return Number.isFinite(time) ? new Date(time + 3 * 3600000).toISOString().slice(0, 19) : '';
}
function withMoscow(date: string) {
  return date ? date + (date.length === 16 ? ':00' : '') + '+03:00' : '';
}

function CalendarFields({ data, change }: { data: CalendarData; change: (value: CalendarData) => void }) {
  function add(): CalendarEntry {
    const start = Math.ceil(Date.now() / 1800000) * 1800000;
    return {
      id: newId(), summary: '', description: '', status: 'confirmed',
      start: { dateTime: new Date(start).toISOString(), timeZone: data.time_zone },
      end: { dateTime: new Date(start + 1800000).toISOString(), timeZone: data.time_zone },
    };
  }
  return <Rows noun="Событие" objectLabel="событие" rows={data.events} change={events => change({ ...data, events })} add={add}
    describe={entry => (entry.summary || 'Новое событие') + ' · ' + moscow(entry.start.dateTime).slice(0, 16).replace('T', ' ')}
    render={(entry, set) => <div className="field-stack">
      <Text label="Название события" value={entry.summary} change={summary => set({ ...entry, summary })} required/>
      <Text label="Описание" value={entry.description} change={description => set({ ...entry, description })} multiline/>
      <div className="mock-field-pair">
        <Text label="Начало (МСК)" type="datetime-local" value={moscow(entry.start.dateTime)}
          change={date => set({ ...entry, start: { ...entry.start, dateTime: withMoscow(date) } })} required/>
        <Text label="Окончание (МСК)" type="datetime-local" value={moscow(entry.end.dateTime)}
          change={date => set({ ...entry, end: { ...entry.end, dateTime: withMoscow(date) } })} required/>
      </div>
      <Choice label="Статус события" value={entry.status || 'confirmed'} change={status => set({ ...entry, status })}
        options={[['confirmed','Подтверждено'], ['tentative','Предварительно'], ['cancelled','Отменено']]}/>
    </div>}/>;
}

function GitLabFields({ data, change }: { data: GitLabData; change: (value: GitLabData) => void }) {
  return <Rows noun="Проект GitLab" objectLabel="проект" rows={data.projects} change={projects => change({ ...data, projects })}
    describe={p => p.path_with_namespace || p.name || 'Новый проект'}
    add={() => {
      const id = Math.max(0, ...data.projects.map(p => p.id)) + 1;
      return { id, name: '', path_with_namespace: 'demo/project-' + id, default_branch: 'main',
        description: '', owner: '', branches: [{ name: 'main', protected: true, commit_id: '' }], commits: [], merge_requests: [] };
    }}
    render={(project, set) => <GitLabProjectFields key={project.id} project={project} change={set}/>}/>;
}

type Section = 'project' | 'branches' | 'commits' | 'merge-requests';
function GitLabProjectFields({ project, change }: { project: GitLabProject; change: (project: GitLabProject) => void }) {
  const [section, setSection] = useState<Section>('project');
  const branches: Option[] = project.branches.map(b => [b.name, b.name]);
  const commits: Option[] = project.commits.map(c => [c.id, c.id.slice(0, 8) + ' · ' + c.title]);
  return <div className="field-stack">
    <Choice label="Раздел GitLab" value={section} change={value => setSection(value as Section)}
      options={[['project','Проект'], ['branches','Ветки'], ['commits','Коммиты'], ['merge-requests','Merge requests']]}/>
    {section === 'project' && <>
      <Text label="Название проекта" value={project.name} change={name => change({ ...project, name })} required/>
      <Text label="Путь проекта (группа/проект)" value={project.path_with_namespace}
        change={path_with_namespace => change({ ...project, path_with_namespace })} required/>
      <Text label="Описание" value={project.description} change={description => change({ ...project, description })} multiline/>
      <Choice label="Основная ветка" value={project.default_branch} options={branches}
        change={default_branch => change({ ...project, default_branch })} required/>
      <Choice label="Кому доступен проект" value={project.owner} change={owner => change({ ...project, owner })}
        options={[['','Обоим стажёрам и наставнику'], ['intern','Первому стажёру и наставнику'], ['intern2','Второму стажёру и наставнику']]}/>
    </>}
    {section === 'branches' && <Rows noun="Ветка" objectLabel="ветку" rows={project.branches} change={rows => {
      const removed = project.branches.find(branch => !rows.some(row => row.name === branch.name));
      if (removed && rows.length === project.branches.length - 1 &&
          (removed.name === project.default_branch || project.merge_requests.some(mr => mr.source_branch === removed.name || mr.target_branch === removed.name))) {
        window.alert('Сначала смените основную ветку или обновите merge requests, использующие эту ветку.');
        return;
      }
      change({ ...project, branches: rows });
    }} describe={branch => branch.name || 'Новая ветка'} add={() => ({ name: '', protected: false, commit_id: '' })}
      render={(branch, set) => <div className="field-stack">
        <Text label="Имя ветки" value={branch.name} change={name => {
          change({ ...project,
            branches: project.branches.map(item => item === branch ? { ...item, name } : item),
            default_branch: project.default_branch === branch.name ? name : project.default_branch,
            merge_requests: project.merge_requests.map(mr => ({ ...mr,
              source_branch: mr.source_branch === branch.name ? name : mr.source_branch,
              target_branch: mr.target_branch === branch.name ? name : mr.target_branch })),
          });
        }} required/>
        <Choice label="Коммит ветки" value={branch.commit_id} options={[['','Без коммита'], ...commits]}
          change={commit_id => set({ ...branch, commit_id })}/>
        <Flag label="Защищённая ветка" value={branch.protected} change={value => set({ ...branch, protected: value })}/>
      </div>}/>}
    {section === 'commits' && <Rows noun="Коммит" objectLabel="коммит" rows={project.commits} change={rows => change({
      ...project, commits: rows, branches: project.branches.map(branch => rows.some(c => c.id === branch.commit_id) ? branch : { ...branch, commit_id: '' }),
    })} describe={commit => commit.id.slice(0,8) + ' · ' + (commit.title || 'Новый коммит')}
      add={() => ({ id: Array.from(crypto.getRandomValues(new Uint8Array(20)), b => b.toString(16).padStart(2,'0')).join(''),
        title: '', message: '', author_name: '', created_at: new Date().toISOString() })}
      render={(commit, set) => <div className="field-stack">
        <Text label="Название коммита" value={commit.title} change={title => set({ ...commit, title })} required/>
        <Text label="Сообщение коммита" value={commit.message} change={message => set({ ...commit, message })} multiline/>
        <Text label="Автор" value={commit.author_name} change={author_name => set({ ...commit, author_name })}/>
        <Text label="Дата коммита (МСК)" type="datetime-local" value={moscow(commit.created_at)}
          change={date => set({ ...commit, created_at: withMoscow(date) })} required/>
      </div>}/>}
    {section === 'merge-requests' && <Rows noun="Merge request" objectLabel="merge request" rows={project.merge_requests}
      change={merge_requests => change({ ...project, merge_requests })}
      describe={mr => '!' + mr.iid + ' · ' + (mr.title || 'Новый merge request')}
      add={() => ({ iid: Math.max(0, ...project.merge_requests.map(mr => mr.iid)) + 1, title: '', description: '', state: 'opened',
        source_branch: project.branches.find(b => b.name !== project.default_branch)?.name || '', target_branch: project.default_branch,
        updated_at: new Date().toISOString(), reviewer: '', draft: false, approved: false })}
      render={(mr, update) => {
        const set = (next: typeof mr) => update({ ...next, updated_at: new Date().toISOString() });
        return <div className="field-stack">
          <Text label="Название merge request" value={mr.title} change={title => set({ ...mr, title })} required/>
          <Text label="Описание" value={mr.description} change={description => set({ ...mr, description })} multiline/>
          <div className="mock-field-pair">
            <Choice label="Исходная ветка" value={mr.source_branch} options={[['','Выберите ветку'], ...branches]}
              change={source_branch => set({ ...mr, source_branch })} required/>
            <Choice label="Ветка назначения" value={mr.target_branch} options={branches} change={target_branch => set({ ...mr, target_branch })} required/>
          </div>
          <Choice label="Статус merge request" value={mr.state} change={state => set({ ...mr, state })}
            options={[['opened','Открыт'], ['closed','Закрыт'], ['merged','Объединён']]}/>
          <Text label="Ревьюер" value={mr.reviewer} change={reviewer => set({ ...mr, reviewer })}/>
          <Flag label="Черновик" value={mr.draft} change={draft => set({ ...mr, draft })}/>
          <Flag label="Одобрен" value={mr.approved} change={approved => set({ ...mr, approved })}/>
        </div>;
      }}/>}
  </div>;
}
