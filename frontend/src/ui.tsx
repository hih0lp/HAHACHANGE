import { useEffect, useRef } from 'react';
import type { ReactNode } from 'react';
import { Icon } from './visuals';
import { safeLink, timeText } from './model';

export function Notice({ children, error = false, action }: { children: ReactNode; error?: boolean; action?: ReactNode }) {
  return <div className={error ? 'notice notice-error' : 'notice'} role={error ? 'alert' : 'status'}>
    <span>{children}</span>{action}
  </div>;
}

export function External({ href, children }: { href?: string; children: ReactNode }) {
  const url = safeLink(href);
  return url ? <a className="external" href={url} target="_blank" rel="noopener noreferrer">{children}<Icon name="external"/></a> : <span>{children}</span>;
}

export function Stamp({ value, dateOnly }: { value?: string; dateOnly?: boolean }) {
  const label = timeText(value, dateOnly);
  return label ? <time dateTime={value}>{label}</time> : null;
}

export function Modal({ title, children, onClose, className = '' }: {
  title: string; children: ReactNode; onClose: () => void; className?: string;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const dialog = ref.current;
    const opener = document.activeElement;
    dialog?.showModal();
    return () => {
      dialog?.close();
      if (opener instanceof HTMLElement && opener.isConnected) opener.focus();
    };
  }, []);
  return <dialog className={className} ref={ref} aria-label={title} onCancel={event => { event.preventDefault(); onClose(); }}
    onClick={event => { if (event.target === event.currentTarget) onClose(); }}>
    <div className="dialog-body">
      <div className="dialog-heading"><h2>{title}</h2><button className="icon-button" type="button" aria-label="Закрыть" onClick={onClose}><Icon name="close"/></button></div>
      {children}
    </div>
  </dialog>;
}

const states: Record<string, string> = {
  pending: 'На согласовании', submitted: 'Отправлена', approved: 'Одобрена', rejected: 'Отклонена',
  granted: 'Доступ подтверждён', missing: 'Нужен доступ', unknown: 'Требуется проверка',
  ready_for_task: 'Можно выбрать задачу', waiting_access: 'Ожидаем доступ', needs_input: 'Требуется действие',
  check_unavailable: 'Проверка недоступна', needs_refresh: 'Требуется проверка', discovering: 'Проверка доступа',
};
export const statusText = (value: string) => states[value] || value;
