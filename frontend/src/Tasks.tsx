import { useEffect, useState } from 'react';
import { api, errorText } from './api';
import { External, Notice, Stamp } from './ui';
import { Icon } from './visuals';

type Task = { id: string; title: string; href?: string; status: string; date?: string; dateOnly?: boolean };
type Source = 'Jira' | 'GitLab' | 'Календарь';
type Result = { state: 'loading' } | { state: 'error'; message: string } | { state: 'ready'; items: Task[] };
const sources: Source[] = ['Jira', 'GitLab', 'Календарь'];
const initials = { Jira: 'J', GitLab: 'G', Календарь: 'К' };
const colors = { Jira: 'sky', GitLab: 'peach', Календарь: 'mint' };
const defaultResult = (): Record<Source, Result> => ({ Jira: { state: 'loading' }, GitLab: { state: 'loading' }, Календарь: { state: 'loading' } });

export function Tasks({ token, apiKey, onProfile }: { token: string; apiKey: string; onProfile: () => void }) {
  const [data, setData] = useState(defaultResult);
  const [reload, setReload] = useState(0);
  useEffect(() => {
    if (!token) return;
    let active = true;
    const auth = { token, key: apiKey };
    const loaders = {
      Jira: async () => {
        const result = await api.jira(auth);
        if (!result.enabled) return { state: 'error', message: 'Jira не подключена для этого профиля.' } as const;
        return { state: 'ready', items: result.reminders.map(item => ({
          id: item.id, title: item.key + ' · ' + item.summary, href: item.url, status: 'Скоро дедлайн',
          date: new Date(Date.parse(item.due_at) - 1).toISOString(), dateOnly: true,
        })) } as const;
      },
      GitLab: async () => {
        const result = await api.gitlab(auth);
        return { state: 'ready', items: (result.reminders || []).map(item => ({
          id: item.id, title: '!' + item.iid + ' · ' + item.title, href: item.url, status: 'Открытый MR', date: item.updated_at,
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
          if (active) setData(previous => ({ ...previous, [source]: result }));
        } catch (err) {
          if (active) setData(previous => ({ ...previous, [source]: { state: 'error', message: errorText(err, 'Источник временно недоступен.') } }));
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
      <p>Ближайшие дедлайны, открытые MR и события календаря.</p>
    </header>
    {!token ? <Notice action={<button className="button secondary" onClick={onProfile}>Открыть профиль</button>}>Войдите, чтобы увидеть задачи своей команды.</Notice> : <>
      <div className="tasks-toolbar"><span>Время событий указано по Москве</span><button className="button secondary" onClick={() => setReload(value => value + 1)}>Обновить</button></div>
      <div className="task-sources">
        {sources.map(source => {
          const result = data[source];
          return <section className="card source-card" key={source}>
            <div className="section-heading"><h2><span className={'source-icon ' + colors[source]} aria-hidden="true">{initials[source]}</span>{source}</h2></div>
            {result.state === 'loading' && <Notice>Проверяем источник…</Notice>}
            {result.state === 'error' && <Notice error>{result.message}</Notice>}
            {result.state === 'ready' && (result.items.length ? <ul className="task-list">{result.items.map(item => <li key={item.id}>
              <External href={item.href}>{item.title}</External>
              <span className="pill">{item.status}</span>
              <div className="muted"><Stamp value={item.date} dateOnly={item.dateOnly}/></div>
            </li>)}</ul> : <p className="muted">Ближайших действий нет.</p>)}
          </section>;
        })}
      </div>
    </>}
  </div>;
}
