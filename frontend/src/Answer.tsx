import type { ReactNode } from 'react';
import { linkedForm, linkSource, safeLink } from './model';
import { Icon } from './visuals';

function inline(value: string, onForm: (id: string) => void): ReactNode[] {
  const pattern = /\[([^\]]+)\]\(([^\s)]+)\)|\*\*([^*]+)\*\*|\*([^*]+)\*|`([^`]+)`/g;
  const nodes: ReactNode[] = [];
  let cursor = 0;
  for (const match of value.matchAll(pattern)) {
    const index = match.index ?? 0;
    nodes.push(value.slice(cursor, index));
    const [, label, href, bold, emphasis, code] = match;
    if (href) {
      const form = linkedForm(href);
      const url = safeLink(href);
      if (form) nodes.push(<button key={index} className="button outline inline-action" onClick={() => onForm(form)}>{label}<Icon name="arrow"/></button>);
      else if (url) {
        const source = linkSource(url);
        nodes.push(<a key={index} className="answer-source" href={url} target="_blank" rel="noopener noreferrer">
          {source && <span className={source === 'Материалы' ? 'materials' : ''}>{source === label ? 'Данные ' + source : source}</span>}{label}<Icon name="external"/>
        </a>);
      } else nodes.push(label);
    } else if (bold) nodes.push(<strong key={index}>{bold}</strong>);
    else if (emphasis) nodes.push(<em key={index}>{emphasis}</em>);
    else nodes.push(<code key={index}>{code}</code>);
    cursor = index + match[0].length;
  }
  nodes.push(value.slice(cursor));
  return nodes;
}

export function Answer({ text, onForm }: { text: string; onForm: (id: string) => void }) {
  const lines = text.replace(/\\([*()[\]])/g, '$1').split(/\r?\n/).map(line => line.trim());
  const blocks: ReactNode[] = [];
  const listPattern = /^([-*]|\d+[.)])\s+(.+)$/;
  for (let index = 0; index < lines.length;) {
    const key = index;
    const line = lines[index++];
    if (!line) continue;
    const heading = /^#{1,6}\s+(.+)$/.exec(line);
    const item = listPattern.exec(line);
    if (heading) blocks.push(<h3 key={key}>{inline(heading[1], onForm)}</h3>);
    else if (item) {
      const ordered = /^\d/.test(item[1]);
      const Tag = ordered ? 'ol' : 'ul';
      const items = [item[2]];
      for (let next = listPattern.exec(lines[index] ?? ''); next && /^\d/.test(next[1]) === ordered; next = listPattern.exec(lines[index] ?? '')) {
        items.push(next[2]);
        index++;
      }
      blocks.push(<Tag key={key}>{items.map((entry, i) => <li key={i}>{inline(entry, onForm)}</li>)}</Tag>);
    } else blocks.push(<p key={key}>{inline(line, onForm)}</p>);
  }
  return <div className="answer">{blocks}</div>;
}
