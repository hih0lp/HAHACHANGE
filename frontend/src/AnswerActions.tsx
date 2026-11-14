import { useEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import { createPortal } from 'react-dom';

function CopyButton({ getText }: { getText: () => string }) {
  const [label, setLabel] = useState('Копировать');
  useEffect(() => {
    if (label === 'Копировать') return;
    const timer = setTimeout(() => setLabel('Копировать'), 2000);
    return () => clearTimeout(timer);
  }, [label]);

  async function copy(button: HTMLButtonElement) {
    const text = getText();
    const doc = button.ownerDocument;
    try {
      const clipboard = doc.defaultView?.navigator.clipboard;
      if (!clipboard) throw new Error();
      await clipboard.writeText(text);
      setLabel('Скопировано');
    } catch {
      // The chat also runs on HTTP, where the Clipboard API is unavailable.
      const selection = doc.getSelection();
      const ranges = selection ? Array.from({ length: selection.rangeCount }, (_, index) => selection.getRangeAt(index).cloneRange()) : [];
      const focused = doc.activeElement as HTMLElement | null;
      const field = doc.createElement('textarea');
      field.value = text;
      field.readOnly = true;
      field.className = 'clipboard-field';
      doc.body.append(field);
      field.select();
      try {
        setLabel(doc.execCommand('copy') ? 'Скопировано' : 'Не удалось скопировать');
      } catch {
        setLabel('Не удалось скопировать');
      } finally {
        field.remove();
        focused?.focus({ preventScroll: true });
        selection?.removeAllRanges();
        ranges.forEach(range => selection?.addRange(range));
      }
    }
  }

  return <button type="button" className="text-button" aria-live="polite" onClick={event => void copy(event.currentTarget)}>{label}</button>;
}

export function AnswerActions({ children, disabled, onReply }: { children: ReactNode; disabled: boolean; onReply: () => void }) {
  const content = useRef<HTMLDivElement>(null);
  return <>
    <div ref={content} data-answer-text>{children}</div>
    <div className="answer-actions">
      <button type="button" className="text-button" disabled={disabled} onClick={onReply}>Ответить</button>
      <CopyButton getText={() => content.current?.innerText.trim() || ''}/>
    </div>
  </>;
}

type SelectedText = { text: string; x: number; y: number; below: boolean };

export function AnswerSelection({ children, active, disabled, onQuote }: { children: ReactNode; active: boolean; disabled: boolean; onQuote: (text: string) => void }) {
  const root = useRef<HTMLDivElement>(null);
  const [selected, setSelected] = useState<SelectedText | null>(null);

  useEffect(() => {
    if (!active) {
      setSelected(null);
      return;
    }
    const container = root.current!;
    const doc = container.ownerDocument;
    const view = doc.defaultView!;
    const viewport = container.closest('.chat-scroll, .bobby-widget-thread');

    function updateSelection() {
      const selection = doc.getSelection();
      const range = selection?.rangeCount && selection.getRangeAt(0);
      if (!range || range.collapsed || !container.contains(range.commonAncestorContainer)) {
        setSelected(null);
        return;
      }
      const node = range.commonAncestorContainer;
      const answer = (node.nodeType === 1 ? node as Element : node.parentElement)?.closest('[data-answer-text]');
      const text = selection.toString().trim();
      const rect = range.getBoundingClientRect();
      const bounds = viewport?.getBoundingClientRect();
      const top = Math.max(rect.top, bounds?.top ?? 0, 0);
      const bottom = Math.min(rect.bottom, bounds?.bottom ?? view.innerHeight, view.innerHeight);
      if (!answer || !text || top >= bottom) {
        setSelected(null);
        return;
      }
      const below = top < 60;
      setSelected({ text, x: (rect.left + rect.right) / 2, y: below ? bottom + 8 : top - 8, below });
    }

    function dismiss(event: KeyboardEvent) {
      if (event.key === 'Escape') setSelected(null);
    }

    doc.addEventListener('selectionchange', updateSelection);
    doc.addEventListener('scroll', updateSelection, true);
    doc.addEventListener('keydown', dismiss);
    view.addEventListener('resize', updateSelection);
    return () => {
      doc.removeEventListener('selectionchange', updateSelection);
      doc.removeEventListener('scroll', updateSelection, true);
      doc.removeEventListener('keydown', dismiss);
      view.removeEventListener('resize', updateSelection);
    };
  }, [active]);

  const doc = root.current?.ownerDocument;
  return <div ref={root}>
    {children}
    {active && selected && doc && createPortal(
      <div className="selection-actions" data-below={selected.below} role="group" aria-label="Действия с выделенным текстом"
        style={{ left: `clamp(min(124px, 50vw), ${selected.x}px, max(50vw, 100vw - 124px))`, top: selected.y }}
        onPointerDown={event => event.preventDefault()}>
        <button type="button" className="text-button" disabled={disabled} onClick={() => {
          doc.getSelection()?.removeAllRanges();
          setSelected(null);
          onQuote(selected.text);
        }}>Цитировать</button>
        <CopyButton key={selected.text} getText={() => selected.text}/>
      </div>, doc.body,
    )}
  </div>;
}
