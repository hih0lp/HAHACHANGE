import { useEffect, useRef, useState } from 'react';
import type { FormEvent } from 'react';
import { createRoot } from 'react-dom/client';
import { api, errorText } from './api';
import type { Role, Session } from './api';
import { Chat } from './Chat';
import { Onboarding } from './Onboarding';
import { Tasks } from './Tasks';
import { Modal, Notice } from './ui';
import { Bobby, Icon } from './visuals';
import type { IconName } from './visuals';
import './styles.css';

const roles: Record<Role, string> = { intern: 'Стажёр', intern2: 'Второй стажёр', mentor: 'Наставник' };
type View = 'chat' | 'onboarding' | 'tasks';
type SignedIn = { token: string; username: string; expires: number };
const nav: { id: View; path: string; label: string; icon: IconName }[] = [
  { id: 'chat', path: '/', label: 'Чат с Бобби', icon: 'chat' },
  { id: 'onboarding', path: '/onboarding', label: 'Адаптация', icon: 'route' },
  { id: 'tasks', path: '/tasks', label: 'Мои задачи', icon: 'tasks' },
];
const readView = () => nav.find(item => item.path === location.pathname)?.id || 'chat';
const readForm = () => {
  const value = new URLSearchParams(location.search).get('form') || '';
  return /^[a-zA-Z0-9_-]+$/.test(value) ? value : '';
};
const readAccount = (): SignedIn | undefined => {
  try { return JSON.parse(sessionStorage.getItem('bobby-account') || 'null') || undefined; }
  catch { return undefined; }
};
const DEV = import.meta.env.DEV;

function App() {
  const [view, setView] = useState(readView);
  const [account, setAccount] = useState(readAccount);
  const [demo, setDemo] = useState<Session>();
  const [demoError, setDemoError] = useState('');
  const [sessionReady, setSessionReady] = useState(!DEV);
  const [apiKey, setApiKey] = useState(() => sessionStorage.getItem('bobby-api-key') || '');
  const [profile, setProfile] = useState(false);
  const [menu, setMenu] = useState(false);
  const [formID, setFormID] = useState(readForm);
  const [catalog, setCatalog] = useState(false);
  const [draft, setDraft] = useState('');
  const [identityError, setIdentityError] = useState('');
  const [sessionVersion, setSessionVersion] = useState(0);
  const profileTrigger = useRef<HTMLButtonElement>(null);
  const content = useRef<HTMLElement>(null);

  useEffect(() => {
    if (!DEV) return;
    void fetch('/session/bootstrap').catch(() => {}).finally(() => setSessionReady(true));
  }, []);

  useEffect(() => {
    if (account || !sessionReady) return;
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
  }, [account, sessionReady, sessionVersion]);

  useEffect(() => {
    if (!account || !sessionReady) return;
    const timer = setTimeout(() => {
      setIdentityError('Срок сессии истёк. Войдите в аккаунт повторно.');
      setProfile(true);
    }, Math.max(0, account.expires - Date.now()));
    return () => clearTimeout(timer);
  }, [account, sessionReady]);

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

  async function changeIdentity(next?: SignedIn) {
    if (next) sessionStorage.setItem('bobby-account', JSON.stringify(next));
    else sessionStorage.removeItem('bobby-account');
    setAccount(next);
    setDemo(undefined);
    setDraft('');
    setFormID('');
    setCatalog(false);
    setIdentityError('');
    if (location.search) history.replaceState({}, '', location.pathname);
    setSessionVersion(value => value + 1);
  }

  const token = account ? (account.expires > Date.now() ? account.token : '') : demo?.access_token || '';
  const role = demo?.role || 'intern';
  const identity = account ? 'account:' + account.username : 'demo:' + role;
  const storageKey = 'bobby-scenario:' + identity;
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

  return <div className="app-shell">
    <a className="skip-link" href="#main">К содержанию</a>
    <aside className="sidebar" aria-label="Разделы">
      <a className="brand" href="/" onClick={event => { if (!event.ctrlKey && !event.metaKey) { event.preventDefault(); navigate('chat'); } }}>
        <span className="brand-mark"><i/><i/><i/><i/></span><span>Бобби</span>
      </a>
      <nav className="navigation" aria-label="Основная навигация">{links()}</nav>
      <div className="sidebar-foot">
        <button className="profile-button" ref={profileTrigger} onClick={openProfile}>
          <span className="profile-icon"><Icon name="user"/></span>
          <span><strong>{account?.username || roles[role]}</strong><small>{account ? 'Демо-аккаунт' : 'Режим прототипа'}</small></span>
          <Icon name="chevron"/>
        </button>
      </div>
    </aside>
    <main id="main" className="main-panel" tabIndex={-1} ref={content}>
      <header className="topbar">
        <div className="topbar-title"><button className="icon-button mobile-menu" aria-label="Открыть меню" aria-expanded={menu} onClick={() => setMenu(true)}><Icon name="menu"/></button>
          <span className="topbar-icon"><Icon name={current.icon}/></span><strong>{current.label}</strong></div>
        <button className="topbar-profile" onClick={openProfile} aria-label="Открыть профиль">
          <span className="account-dot"/><span>{account ? account.username : 'Демо-режим'}</span><Icon name="user"/>
        </button>
      </header>
      {sessionReady && <div className="view" key={identity + ':' + sessionVersion}>
        {view === 'chat' && <Chat token={token} historyToken={account?.token || ''} apiKey={apiKey} isDemo={!account} identity={identity} formID={formID}
          catalog={catalog} draft={draft} onDraft={setDraft} onForm={id => navigate('chat', id)} onCloseForm={() => navigate('chat', '', !!formID && !!history.state?.catalog)}
          onCatalog={() => { navigate('chat', '', true); }}/>}
        {view === 'onboarding' && <Onboarding token={token} storageKey={storageKey} onProfile={openProfile} onChat={text => { setDraft(text); navigate('chat'); }}/>}
        {view === 'tasks' && <Tasks token={token} apiKey={apiKey} onProfile={openProfile}/>}
      </div>}
      {!sessionReady && <div className="section-page"><Notice>Открываем Бобби…</Notice></div>}
    </main>
    {menu && <Modal title="Разделы" className="menu-dialog" onClose={() => setMenu(false)}>
      <nav className="navigation" aria-label="Основная навигация">{links()}</nav>
      <button className="button secondary" onClick={openProfile}><Icon name="user"/>Профиль</button>
    </Modal>}
    {profile && <Modal title="Профиль" onClose={() => {
      setProfile(false);
      requestAnimationFrame(() => { if (document.activeElement === document.body && profileTrigger.current?.offsetWidth) profileTrigger.current.focus(); });
    }}>
      <Profile account={account} demoRole={role} demoError={demoError} identityError={identityError} apiKey={apiKey}
        onKey={key => { sessionStorage.setItem('bobby-api-key', key); setApiKey(key); setSessionVersion(value => value + 1); }}
        onIdentity={changeIdentity} onRetry={() => setSessionVersion(value => value + 1)}/>
    </Modal>}
  </div>;
}

function Profile({ account, demoRole, demoError, identityError, apiKey, onKey, onIdentity, onRetry }: {
  account?: SignedIn; demoRole: Role; demoError: string; identityError: string; apiKey: string;
  onKey: (key: string) => void; onIdentity: (value?: SignedIn) => Promise<void>; onRetry: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);

  async function perform(action: () => Promise<void>) {
    setError('');
    setBusy(true);
    try { await action(); }
    catch (err) { setError(errorText(err, 'Не удалось изменить сессию. Проверьте логин, пароль и доступность сервиса.')); }
    finally { setBusy(false); }
  }
  function login(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const data = new FormData(event.currentTarget);
    const username = String(data.get('username') || '').trim();
    void perform(async () => {
      const session = await api.login(username, String(data.get('password') || ''));
      await onIdentity({ token: session.access_token, username, expires: Date.now() + session.expires_in * 1_000 });
    });
  }

  return <div className="profile-content">
    <div className="profile-intro"><Bobby/><div><strong>{account?.username || roles[demoRole]}</strong><p>Аккаунты и роли относятся к учебному прототипу.</p></div></div>
    {identityError && <Notice error>{identityError}</Notice>}
    {demoError && !account && <Notice error action={<button className="text-button" onClick={onRetry}>Повторить</button>}>{demoError}</Notice>}
    {error && <Notice error>{error}</Notice>}
    {account && !identityError ? <button className="button secondary" disabled={busy} onClick={() => void perform(async () => {
      await onIdentity();
    })}>Выйти из аккаунта</button> : <form className="field-stack" onSubmit={login}>
      <label>Логин<input name="username" autoComplete="username" required/></label>
      <label>Пароль<input name="password" type="password" autoComplete="current-password" required/></label>
      <button className="button primary" disabled={busy}>{busy ? 'Подключаем…' : 'Войти в демо-аккаунт'}</button>
      {account && <button className="button secondary" type="button" disabled={busy} onClick={() => void perform(async () => {
        await onIdentity();
      })}>Вернуться в демо-режим</button>}
    </form>}
    {!account && <label className="field-stack">Демо-роль<select value={demoRole} disabled={busy} onChange={event => {
      const role = event.target.value as Role;
      void perform(async () => { await api.role(role); await onIdentity(); });
    }}>{Object.entries(roles).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>}
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
