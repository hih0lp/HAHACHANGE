import { useEffect, useState } from 'react';
import { api, ApiError, errorText } from './api';
import { MockEditor } from './MockEditor';
import type { MockSource } from './mockData';
import { External, Notice, Stamp } from './ui';
import { Icon } from './visuals';

type Task = { id: string; title: string; href?: string; status: string; date?: string; dateOnly?: boolean; detail?: string };
type Source = 'Jira' | 'Календарь' | 'GitLab';
type Result = { state: 'loading' } | { state: 'error'; message: string } | { state: 'ready'; items: Task[] };
const sources: Source[] = ['Jira', 'Календарь', 'GitLab'];
const initials = { Jira: 'J', Календарь: 'К', GitLab: 'G' };
const colors = { Jira: 'sky', Календарь: 'mint', GitLab: 'lavender' };
const defaultResult = (): Record<Source, Result> => ({ Jira: { state: 'loading' }, Календарь: { state: 'loading' }, GitLab: { state: 'loading' } });

export function Tasks({ token, apiKey, canEdit, onProfile }: { token: string; apiKey: string; canEdit: boolean; onProfile: () => void }) {
  const [data, setData] = useState(defaultResult);
  const [reload, setReload] = useState(0);
  const [mockAvailable, setMockAvailable] = useState(false);
  const [editor, setEditor] = useState<MockSource>();
  useEffect(() => {
    if (!token) return;
    let active = true;
    const auth = { token, key: apiKey };
    const loaders = {
      GitLab: async () => {
        const result = await api.gitlab(auth);
        return { state: 'ready', items: result.data.map(project => ({
          id: String(project.id), title: project.name + ' · ' + project.path_with_namespace,
          status: project.default_branch, detail: project.description,
        })) } as const;
      },
      Jira: async () => {
        const result = await api.jira(auth);
        if (!result.enabled) return { state: 'error', message: 'Jira не подключена для этого профиля.' } as const;
        return { state: 'ready', items: result.reminders.map(item => ({
          id: item.id, title: item.key + ' · ' + item.summary, href: item.url, status: item.status || 'Задача',
          date: item.due_at && item.date_only ? new Date(Date.parse(item.due_at) - 1).toISOString() : item.due_at,
          dateOnly: item.date_only,
        })) } as const;
      },
      Календарь: async () => {
        const result = await api.calendar(auth);
        if (!result.data || !Array.isArray(result.data.items) && result.data.items !== undefined) {
          return { state: 'error', message: 'Календарь не вернул события для этого профиля.' } as const;
        }
        return { state: 'ready', items: (result.data.items || []).filter(item => item.status !== 'cancelled').map(item => ({
          id: item.id, title: item.summary || 'Событие', href: item.htmlLink, status: 'Запланировано',
          date: item.start?.dateTime || item.start?.date, dateOnly: !item.start?.dateTime,
        })) } as const;
      },
    };
    setData(defaultResult());
    let running = false;
    async function refresh() {
      if (running) return;
      running = true;
      await Promise.all(sources.map(async source => {
        try {
          const result = await loaders[source]();
          if (active) {
            if (source === 'GitLab') setMockAvailable(true);
            setData(previous => ({ ...previous, [source]: result }));
          }
        } catch (err) {
          if (active) {
            if (source === 'GitLab') setMockAvailable(!(err instanceof ApiError && err.status === 404));
            setData(previous => ({ ...previous, [source]: { state: 'error', message: errorText(err, 'Источник временно недоступен.') } }));
          }
        }
      }));
      running = false;
    }
    void refresh();
    const timer = setInterval(() => void refresh(), 60_000);
    return () => { active = false; clearInterval(timer); };
  }, [token, apiKey, reload]);

  return <div className="section-page">
    <header className="page-intro"><div className="page-symbol lavender"><Icon name="tasks"/></div><h1>Мои задачи</h1>
      <p>{mockAvailable ? 'Назначенные задачи Jira, события календаря и проекты GitLab.' : 'Назначенные задачи Jira и события календаря.'}</p>
    </header>
    {!token ? <Notice action={<button className="button secondary" onClick={onProfile}>Открыть профиль</button>}>Для просмотра задач нужна демо-сессия.</Notice> : <>
      <div className="tasks-toolbar"><span>Время событий указано по Москве</span><button className="button secondary" onClick={() => setReload(value => value + 1)}>Обновить</button></div>
      <div className={"task-sources" + (mockAvailable ? " task-sources-with-gitlab" : "")}>
        {sources.filter(source => source !== "GitLab" || mockAvailable).map(source => {
          const result = data[source];
          return <section className="card source-card" key={source}>
            <div className="section-heading"><h2><span className={'source-icon ' + colors[source]} aria-hidden="true">{initials[source]}</span>{source}</h2>
              {canEdit && mockAvailable && <button type="button" className="button secondary" onClick={() => setEditor(
                source === 'Jira' ? 'jira' : source === 'Календарь' ? 'calendar' : 'gitlab'
              )} aria-label={'Редактировать · ' + source}>Редактировать</button>}
            </div>
            {canEdit && mockAvailable && source === 'Календарь' && <button type="button" className="button secondary"
              onClick={() => setEditor('company-calendar')}>Редактировать календарь компании</button>}
            {result.state === 'loading' && <Notice>Проверяем источник…</Notice>}
            {result.state === 'error' && <Notice error>{result.message}</Notice>}
            {result.state === 'ready' && (result.items.length ? <ul className="task-list">{result.items.map(item => <li key={item.id}>
              <External href={item.href}>{item.title}</External>
              <span className="pill">{item.status}</span>
              {item.date && <div className="muted"><Stamp value={item.date} dateOnly={item.dateOnly}/></div>}
              {item.detail && <div className="muted">{item.detail}</div>}
            </li>)}</ul> : <p className="muted">Список пуст.</p>)}
          </section>;
        })}
      </div>
      {editor && <MockEditor key={editor} source={editor} token={token} apiKey={apiKey}
        onClose={() => setEditor(undefined)} onSave={() => setReload(n => n + 1)}/>}
    </>}
  </div>;
}
