import { useEffect, useRef, useState } from 'react';
import { createRoot } from 'react-dom/client';
import { api, errorText } from './api';
import type { Role, Session } from './api';
import { useBobbyWidget } from './BobbyWidget';
import { Chat } from './Chat';
import { Tasks } from './Tasks';
import { Modal, Notice } from './ui';
import { Bobby, Icon } from './visuals';
import type { IconName } from './visuals';
import avitoAnnouncement from './assets/avito-announcement.jpg';
import './styles.css';

const roles: Record<Role, string> = { intern: 'Стажёр', intern2: 'Второй стажёр', mentor: 'Наставник' };
type View = 'chat' | 'tasks';
const nav: { id: View; path: string; label: string; icon: IconName }[] = [
  { id: 'chat', path: '/', label: 'Чат с Бобби', icon: 'chat' },
  { id: 'tasks', path: '/tasks', label: 'Мои задачи', icon: 'tasks' },
];
const readView = () => nav.find(item => item.path === location.pathname)?.id || 'chat';
const readForm = () => {
  const value = new URLSearchParams(location.search).get('form') || '';
  return /^[a-zA-Z0-9_-]+$/.test(value) ? value : '';
};

function App() {
  const [view, setView] = useState(readView);
  const [demo, setDemo] = useState<Session>();
  const [demoError, setDemoError] = useState('');
  const [sessionReady, setSessionReady] = useState(false);
  const [apiKey, setApiKey] = useState(() => sessionStorage.getItem('bobby-api-key') || '');
  const [profile, setProfile] = useState(false);
  const [menu, setMenu] = useState(false);
  const [formID, setFormID] = useState(readForm);
  const [catalog, setCatalog] = useState(false);
  const [draft, setDraft] = useState('');
  const [selectedRole, setSelectedRole] = useState<Role>();
  const [sessionVersion, setSessionVersion] = useState(0);
  const profileTrigger = useRef<HTMLButtonElement>(null);
  const content = useRef<HTMLElement>(null);
  const widget = useBobbyWidget();

  useEffect(() => {
    void fetch('/session/bootstrap', { cache: 'no-store' }).catch(() => {}).finally(() => setSessionReady(true));
  }, []);

  useEffect(() => {
    if (!sessionReady) return;
    let active = true;
    setDemoError('');
    const refresh = async () => {
      try {
        const session = await api.demo();
        if (active) { setDemo(session); setDemoError(''); }
      } catch (err) {
        if (active) setDemoError(errorText(err, 'Демо-сессия команды сейчас недоступна.'));
      }
    };
    void refresh();
    const timer = setInterval(() => void refresh(), 120_000);
    return () => { active = false; clearInterval(timer); };
  }, [sessionReady, sessionVersion]);

  useEffect(() => {
    function sync() { setView(readView()); setFormID(readForm()); setCatalog(false); setMenu(false); }
    window.addEventListener('popstate', sync);
    return () => window.removeEventListener('popstate', sync);
  }, []);

  function navigate(next: View, id = '', showCatalog = false) {
    const path = nav.find(item => item.id === next)?.path || '/';
    const url = id ? path + '?form=' + encodeURIComponent(id) : path;
    if (url !== location.pathname + location.search) history.pushState({ catalog: !!id && catalog }, '', url);
    setView(next);
    setFormID(id);
    setCatalog(showCatalog);
    if (menu) {
      setMenu(false);
      requestAnimationFrame(() => content.current?.focus());
    }
  }

  async function changeRole(role: Role) {
    await api.role(role);
    setSelectedRole(role);
    setDemo(undefined);
    setDraft('');
    setFormID('');
    setCatalog(false);
    if (location.search) history.replaceState({}, '', location.pathname);
    setSessionVersion(value => value + 1);
  }

  const token = demo?.access_token || '';
  const role = demo?.role || selectedRole;
  const identity = 'demo:' + (role || 'intern');
  const current = nav.find(item => item.id === view) || nav[0];

  function links() {
    return nav.map(item => <a key={item.id} href={item.path} className={'nav-link' + (view === item.id ? ' selected' : '')}
      aria-current={view === item.id ? 'page' : undefined} onClick={event => {
        if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
        event.preventDefault();
        navigate(item.id);
      }}><Icon name={item.icon}/>{item.label}</a>);
  }
  function openProfile() { setMenu(false); setProfile(true); }
  function brand(className: string) {
    return <a className={'brand ' + className} href="/" onClick={event => {
      if (event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
      event.preventDefault();
      navigate('chat');
    }}>
      {/* Cyrillic lockup from Avito's launch announcement: https://t.me/avito/7613 */}
      <span className="brand-logo"><img src={avitoAnnouncement} alt="Авито" width="800" height="800"/></span>
      <span className="brand-name">Бобби</span>
    </a>;
  }

  return <div className="app-shell">
    <a className="skip-link" href="#main">К содержанию</a>
    <aside className="sidebar" aria-label="Разделы">
      {brand('sidebar-brand')}
      <nav className="navigation" aria-label="Основная навигация">{links()}</nav>
      <div className="sidebar-foot">
        <button className="profile-button" ref={profileTrigger} onClick={openProfile}>
          <span className="profile-icon"><Icon name="user"/></span>
          <span><strong>{role ? roles[role] : 'Демо-режим'}</strong><small>Демо-роль</small></span>
          <Icon name="chevron"/>
        </button>
      </div>
    </aside>
    <main id="main" className="main-panel" tabIndex={-1} ref={content}>
      <header className="topbar">
        <div className="topbar-title"><button className="icon-button mobile-menu" aria-label="Открыть меню" aria-expanded={menu} onClick={() => setMenu(true)}><Icon name="menu"/></button>
          {brand('mobile-brand')}
          <span className="topbar-icon"><Icon name={current.icon}/></span><strong className="topbar-section">{current.label}</strong></div>
        <div className="topbar-actions">
          <button className="icon-button widget-trigger" ref={widget.trigger} aria-label="Мини-чат с Бобби"
            title="Мини-чат с Бобби" aria-pressed={!!widget.target} disabled={!token || widget.opening}
            onClick={() => void widget.open()}><Icon name="widget"/></button>
          <button className="topbar-profile" onClick={openProfile} aria-label="Открыть профиль">
            <span className="account-dot"/><span>{role ? roles[role] : 'Демо-режим'}</span><Icon name="user"/>
          </button>
        </div>
      </header>
      {token && <div className="view" key={identity + ':' + sessionVersion}>
        <Chat hidden={view !== 'chat'} token={token} apiKey={apiKey} identity={identity} formID={formID}
          catalog={catalog} draft={draft} onDraft={setDraft} onForm={id => navigate('chat', id)} onCloseForm={() => navigate('chat', '', !!formID && !!history.state?.catalog)}
          onCatalog={() => { navigate('chat', '', true); }} widget={widget}
          onOpenChat={() => { if (view !== 'chat') navigate('chat'); }}/>
        {view === 'tasks' && <Tasks token={token} apiKey={apiKey} canEdit={role === "mentor"} onProfile={openProfile}/>}
      </div>}
      {!token && <div className="section-page">
        <Notice error={!!demoError} action={demoError && <button className="text-button" onClick={() => {
          setDemoError('');
          setSessionVersion(value => value + 1);
        }}>Повторить</button>}>{demoError || 'Подключаем демо-сессию…'}</Notice>
      </div>}
    </main>
    {menu && <Modal title="Разделы" className="menu-dialog" onClose={() => setMenu(false)}>
      <nav className="navigation" aria-label="Основная навигация">{links()}</nav>
      <button className="button secondary" onClick={openProfile}><Icon name="user"/>Профиль</button>
    </Modal>}
    {profile && <Modal title="Профиль" onClose={() => {
      setProfile(false);
      requestAnimationFrame(() => { if (document.activeElement === document.body && profileTrigger.current?.offsetWidth) profileTrigger.current.focus(); });
    }}>
      <Profile demoRole={role} demoError={demoError} apiKey={apiKey}
        onKey={key => { sessionStorage.setItem('bobby-api-key', key); setApiKey(key); setSessionVersion(value => value + 1); }}
        onRole={changeRole} onRetry={() => setSessionVersion(value => value + 1)}/>
    </Modal>}
  </div>;
}

function Profile({ demoRole, demoError, apiKey, onKey, onRole, onRetry }: {
  demoRole?: Role; demoError: string; apiKey: string;
  onKey: (key: string) => void; onRole: (role: Role) => Promise<void>; onRetry: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);

  async function selectRole(role: Role) {
    setError('');
    setBusy(true);
    try { await onRole(role); }
    catch (err) { setError(errorText(err, 'Не удалось переключить демо-роль.')); }
    finally { setBusy(false); }
  }

  return <div className="profile-content">
    <div className="profile-intro"><Bobby/><div><strong>Выберите роль</strong></div></div>
    {demoError && <Notice error action={<button className="text-button" onClick={onRetry}>Повторить</button>}>{demoError}</Notice>}
    {error && <Notice error>{error}</Notice>}
    <div className="field-stack" role="group" aria-label="Демо-роль">
      {Object.entries(roles).map(([value, label]) => <button key={value} type="button"
        className={'button ' + (value === demoRole ? 'primary' : 'secondary')}
        aria-pressed={value === demoRole} disabled={busy || value === demoRole}
        onClick={() => void selectRole(value as Role)}>{label}</button>)}
    </div>
    <details><summary>Настройки подключения</summary>
      <form className="field-stack" onSubmit={event => {
        event.preventDefault();
        onKey(String(new FormData(event.currentTarget).get('api-key') || '').trim());
        setSaved(true);
      }}>
        <label>API-ключ, если задан на сервере<input name="api-key" type="password" defaultValue={apiKey} autoComplete="off" onChange={() => setSaved(false)}/></label>
        <button className="button secondary">Сохранить ключ</button>{saved && <span role="status">Ключ сохранён для этой вкладки.</span>}
      </form>
    </details>
  </div>;
}

createRoot(document.getElementById('root')!).render(<App/>);
