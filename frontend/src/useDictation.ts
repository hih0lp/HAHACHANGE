import { useEffect, useRef, useState } from 'react';
import type { Dispatch, SetStateAction } from 'react';

type Recognition = {
  lang: string;
  start(): void;
  stop(): void;
  abort(): void;
  onresult: ((event: { results: ArrayLike<ArrayLike<{ transcript: string }>> }) => void) | null;
  onerror: ((event: { error: string }) => void) | null;
  onend: (() => void) | null;
};

type SpeechWindow = Window & {
  SpeechRecognition?: new () => Recognition;
  webkitSpeechRecognition?: new () => Recognition;
};

type Surface = 'chat' | 'widget';

export function useDictation(onDraft: Dispatch<SetStateAction<string>>, showChat: boolean, showWidget: boolean) {
  const [active, setActive] = useState<Surface>();
  const [message, setMessage] = useState<{ surface: Surface; text: string }>();
  const recognition = useRef<Recognition | null>(null);

  function cancel() {
    const session = recognition.current;
    recognition.current = null;
    session?.abort();
    setActive(undefined);
  }

  useEffect(() => () => {
    const session = recognition.current;
    recognition.current = null;
    session?.abort();
  }, []);

  useEffect(() => {
    if ((active === 'chat' && !showChat) || (active === 'widget' && !showWidget)) cancel();
  }, [active, showChat, showWidget]);

  function toggle(surface: Surface) {
    if (recognition.current) {
      recognition.current.stop();
      return;
    }

    const browser = window as SpeechWindow;
    const Constructor = browser.SpeechRecognition || browser.webkitSpeechRecognition;
    const report = (text: string) => setMessage({ surface, text });
    if (!Constructor) {
      report('Этот браузер не поддерживает голосовой ввод.');
      return;
    }
    setMessage(undefined);
    try {
      const session = new Constructor();
      recognition.current = session;
      session.lang = 'ru-RU';
      session.onresult = event => {
        if (recognition.current !== session) return;
        const text = Array.from(event.results, result => result[0]?.transcript.trim()).filter(Boolean).join(' ');
        if (text) onDraft(current => current && !/\s$/.test(current) ? current + ' ' + text : current + text);
      };
      session.onerror = ({ error }) => {
        if (recognition.current !== session) return;
        if (error === 'not-allowed' || error === 'service-not-allowed') {
          report(window.isSecureContext ? 'Разрешите доступ к микрофону в браузере или проверьте доступность распознавания.'
            : 'Для доступа к микрофону откройте Бобби по HTTPS или через localhost.');
        } else if (error === 'audio-capture') report('Микрофон недоступен.');
        else if (error === 'no-speech') report('Речь не распознана. Попробуйте ещё раз.');
        else if (error !== 'aborted') report('Распознавание речи недоступно. Попробуйте ещё раз.');
      };
      session.onend = () => {
        if (recognition.current !== session) return;
        recognition.current = null;
        setActive(undefined);
      };
      session.start();
      setActive(surface);
    } catch {
      recognition.current = null;
      report('Не удалось запустить голосовой ввод.');
    }
  }

  return { active, message, toggle, cancel };
}
