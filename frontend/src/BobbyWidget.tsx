import { useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { Bobby, Icon } from './visuals';
import type { BobbyState } from './visuals';

type PictureInPicture = { requestWindow(options: { width: number; height: number }): Promise<Window> };
type WidgetTarget = Window | 'inline';

const moods: Record<BobbyState, string> = {
  idle: 'На связи',
  listening: 'Слушаю',
  thinking: 'Думаю',
  answering: 'Ответ готов',
  'waiting-form': 'Жду заполнения формы',
};

export function useBobbyWidget() {
  const [target, setTarget] = useState<WidgetTarget | null>(null);
  const [opening, setOpening] = useState(false);
  const [fallback, setFallback] = useState('');
  const trigger = useRef<HTMLButtonElement>(null);

  function close() {
    if (target && target !== 'inline') target.close();
    setTarget(null);
    trigger.current?.focus();
  }

  async function open() {
    if (opening) return;
    if (target) {
      if (target === 'inline') close();
      else target.focus();
      return;
    }
    const pip = (window as Window & { documentPictureInPicture?: PictureInPicture }).documentPictureInPicture;
    if (!pip) {
      setFallback('');
      setTarget('inline');
      return;
    }
    setOpening(true);
    let floating: Window | undefined;
    try {
      floating = await pip.requestWindow({ width: 360, height: 410 });
      const doc = floating.document;
      doc.title = 'Бобби';
      doc.documentElement.lang = 'ru';
      doc.body.className = 'bobby-widget-window';
      // Vite injects style elements in development and links a stylesheet in production.
      for (const stylesheet of document.querySelectorAll<HTMLStyleElement | HTMLLinkElement>('style, link[rel="stylesheet"]')) {
        const copy = stylesheet.cloneNode(true) as HTMLStyleElement | HTMLLinkElement;
        if (stylesheet instanceof HTMLLinkElement) (copy as HTMLLinkElement).href = stylesheet.href;
        doc.head.append(copy);
      }
      floating.addEventListener('pagehide', () => {
        setTarget(current => current === floating ? null : current);
      }, { once: true });
      setTarget(floating);
    } catch {
      floating?.close();
      setFallback('Браузер не открыл отдельное окно. Мини-чат открыт на странице.');
      setTarget('inline');
    } finally {
      setOpening(false);
    }
  }

  return { target, opening, fallback, trigger, open, close };
}

type WidgetProps = {
  target: WidgetTarget;
  fallback: string;
  state: BobbyState;
  query?: string;
  reply?: ReactNode;
  error?: string;
  onClose: () => void;
  onChat: () => void;
  children: ReactNode;
};

export function BobbyWidget({ target, fallback, state, query, reply, error, onClose, onChat, children }: WidgetProps) {
  const panel = useRef<HTMLElement>(null);
  const thread = useRef<HTMLDivElement>(null);
  useEffect(() => {
    panel.current?.querySelector('textarea')?.focus();
  }, [target]);
  useEffect(() => {
    if (thread.current) thread.current.scrollTop = 0;
  }, [query]);

  return createPortal(
    <section ref={panel} className="bobby-widget" aria-label="Мини-чат с Бобби"
      onKeyDown={event => {
        if (event.key === 'Escape' && !event.nativeEvent.isComposing) {
          event.stopPropagation();
          onClose();
        }
      }}>
      <header className="bobby-widget-header">
        <Bobby className="bobby-widget-pet" state={state}/>
        <div className="bobby-widget-name"><strong>Бобби</strong><small role="status">{moods[state]}</small></div>
        <button type="button" className="icon-button" aria-label="Открыть полный чат" title="Открыть полный чат" onClick={onChat}><Icon name="external"/></button>
        <button type="button" className="icon-button" aria-label="Закрыть мини-чат" title="Закрыть мини-чат" onClick={onClose}><Icon name="close"/></button>
      </header>
      {target === 'inline' && fallback && <p className="bobby-widget-fallback">{fallback}</p>}
      <div className="bobby-widget-thread" ref={thread} aria-live="polite" tabIndex={0}>
        {query && <p className="bobby-widget-query">{query}</p>}
        {reply || (!query && <p className="muted">Напишите мне, если нужна помощь.</p>)}
      </div>
      <div className="bobby-widget-compose">
        {error && <p className="bobby-widget-error" role="alert">{error}</p>}
        {children}
      </div>
    </section>,
    target === 'inline' ? document.body : target.document.body,
  );
}
