import type { SVGProps } from 'react';

const icons = {
  plus: <path d="M12 5v14M5 12h14"/>,
  chat: <path d="M20 11.5a7.5 7.5 0 0 1-7.5 7.5H6l-2 2v-6A7.5 7.5 0 1 1 20 11.5Z"/>,
  route: <><rect x="5" y="4" width="14" height="17" rx="2"/><path d="m8.5 12 2.5 2.5 4.5-5M9 4V3h6v1"/></>,
  tasks: <><rect x="3.5" y="5" width="17" height="15.5" rx="3"/><path d="M7.5 3.5V7m9-3.5V7M4 10h16m-12 5h3"/></>,
  arrow: <path d="M4.5 12h14m-6-6 6 6-6 6"/>,
  send: <path d="M12 19V5m-6 6 6-6 6 6"/>,
  chevron: <path d="m9 6 6 6-6 6"/>,
  user: <><circle cx="12" cy="8" r="3.5"/><path d="M5 21v-2a7 7 0 0 1 14 0v2"/></>,
  menu: <path d="M4 6h16M4 12h16M4 18h16"/>,
  close: <path d="m6 6 12 12M18 6 6 18"/>,
  external: <path d="M13 4h7v7m0-7-9 9m7 1v6H4V6h6"/>,
  key: <><circle cx="8.5" cy="10.5" r="4"/><path d="m11.4 13.3 6.6 6.7 2-2-2-2 1.5-1.5"/></>,
  book: <path d="M12 6c-3-2-5-2-9-1v14c4-1 6-1 9 1 3-2 5-2 9-1V5c-4-1-6-1-9 1Zm0 0v14"/>,
  check: <path d="m5 12 4 4L19 6"/>,
  clock: <><circle cx="12" cy="12" r="8"/><path d="M12 7v5l3 2"/></>,
  people: <><circle cx="9" cy="8" r="3"/><path d="M3.5 19v-1a5.5 5.5 0 0 1 11 0v1m1-14a3 3 0 0 1 0 6m1 3a4.5 4.5 0 0 1 4 4.5v.5"/></>,
} as const;

export type IconName = keyof typeof icons;

export function Icon({ name, ...props }: SVGProps<SVGSVGElement> & { name: IconName }) {
  return <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true" {...props}>{icons[name]}</svg>;
}

type BobbyState = 'idle' | 'thinking' | 'answering' | 'listening' | 'waiting-form';

export function Bobby({ className, state = 'idle' }: { className?: string; state?: BobbyState }) {
  return <svg className={className} data-bobby-state={state} viewBox="30 10 1240 1170" aria-hidden="true">
    <g transform={state === 'thinking' ? 'translate(0 70)' : undefined}>
      <g stroke="var(--bobby-ink)" strokeWidth="24" strokeLinecap="round" strokeLinejoin="round">
        <path fill="var(--bobby-blue)" d="M463 907c-12 28-20 56-43 78-24 17-48 44-35 74 12 29 49 28 77 26 58-3 96-10 105-60l15-93Zm222 21c-8 30-19 64-17 92 2 52 48 61 104 61 54 0 108-6 107-47-1-37-33-58-60-68l16-54Z"/>
        <path fill="white" d="M288 794c-48-52-76-106-77-170-9-202 179-400 397-453 178-44 317 10 398 142 80 130 95 292 8 430-121 196-390 266-607 166-55 35-106 54-145 52-37-2-54-28-38-59 18-36 47-66 64-108Z"/>
      </g>
      <ellipse cx="885" cy="315" rx="84" ry="54" fill="var(--bobby-blue)" transform="rotate(42 885 315)"/>
      <g fill="var(--bobby-ink)">
        <ellipse cx="572" cy="550" rx="53" ry="56" transform="rotate(12 572 550)"/>
        <ellipse cx="865" cy="482" rx="50" ry="54" transform="rotate(12 865 482)"/>
      </g>
      <g fill="white" transform={state === 'thinking' ? 'translate(4 -9)' : state === 'listening' ? 'translate(-29 7)' : undefined}>
        <circle cx="586" cy="519" r="14"/><circle cx="877" cy="454" r="14"/>
      </g>
      <path d={state === 'answering' ? 'M690 573l94-19c4 88-77 107-94 19Z' : state === 'listening' ? 'M709 585l49-10' : 'M695 581c28 23 68 10 80-22'}
        fill={state === 'answering' ? 'var(--bobby-blue)' : 'none'} stroke="var(--bobby-ink)" strokeWidth="25" strokeLinecap="round" strokeLinejoin="round"/>
    </g>
    {state === 'thinking' && <g fill="var(--bobby-blue)"><circle cx="856" cy="176" r="20"/><circle cx="923" cy="126" r="29"/><circle cx="1003" cy="65" r="40"/></g>}
    {state === 'answering' && <path d="m1117 517 84-38m-84 109 90 27" fill="none" stroke="var(--bobby-blue)" strokeWidth="27" strokeLinecap="round"/>}
    {state === 'listening' && <path d="M173 512q-57 71-14 145M114 471Q32 572 92 682" fill="none" stroke="var(--bobby-blue)" strokeWidth="26" strokeLinecap="round"/>}
    {state === 'waiting-form' && <g fill="none" stroke="var(--bobby-blue)" strokeWidth="30" strokeLinecap="round">
      <path d="M1090 342c0-75 130-79 130-4 0 54-66 58-66 112"/><circle cx="1154" cy="507" r="16" fill="var(--bobby-blue)" stroke="none"/>
    </g>}
  </svg>;
}

export function Illustration({ state = 'idle' }: { state?: BobbyState }) {
  return <div className="illustration" aria-hidden="true">
    <svg viewBox="0 0 332 216" fill="none">
      <path d="M38 161C13 93 129 18 219 39c93 23 93 118 30 150C174 225 67 210 38 161Z" fill="var(--hero-blue)"/>
      <path d="M43 153c-33 6-34 30-3 31 64 3 192-34 243-66 29-18 27-33-3-34" stroke="var(--orbit)" strokeWidth="2" strokeDasharray="4 6" strokeLinecap="round"/>
      <circle cx="261" cy="58" r="34" fill="var(--brand-purple)"/>
      <circle cx="55" cy="151" r="28" fill="var(--brand-green)"/>
      <circle cx="286" cy="155" r="15" fill="var(--brand-red)"/>
      <circle cx="93" cy="44" r="9" fill="var(--brand-blue)"/>
      <path d="M37 84v16m-8-8h16M288 31v12m-6-6h12" stroke="var(--orbit)" strokeWidth="2" strokeLinecap="round"/>
    </svg>
    <Bobby className="hero-bobby" state={state}/>
  </div>;
}
