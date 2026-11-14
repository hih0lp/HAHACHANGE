import type { Field, Job } from './api.ts';

export function safeLink(value?: string) {
  if (!value) return undefined;
  try {
    const url = new URL(value);
    return url.protocol === 'https:' || url.protocol === 'http:' ? url.href : undefined;
  } catch {
    return undefined;
  }
}

export function linkedForm(href: string) {
  return /^\/forms\/([a-zA-Z0-9_-]+)$/.exec(href)?.[1];
}

export function linkSource(href: string) {
  const url = new URL(href);
  const host = url.hostname.toLowerCase();
  if (host.endsWith('.atlassian.net') || /(^|\.)jira\./.test(host)) return 'Jira';
  if (/(^|\.)gitlab\./.test(host) || url.pathname.includes('/-/merge_requests/')) return 'GitLab';
  if (host === 'calendar.google.com') return 'Календарь';
  if (host === 'github.com' && /^\/avito-tech\/playbook(?:\/|$)/.test(url.pathname)) return 'Материалы';
  return '';
}

export function timeText(value?: string, dateOnly = false) {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  return new Intl.DateTimeFormat('ru-RU', {
    dateStyle: 'medium', ...(dateOnly ? {} : { timeStyle: 'short' }), timeZone: 'Europe/Moscow',
  }).format(date) + (dateOnly ? '' : ' МСК');
}

export function upsert(jobs: Job[], job: Job) {
  const previous = jobs.find(item => item.id === job.id);
  if (!previous) return [...jobs, job];
  return jobs.map(item => item.id === job.id ? { ...item, ...job } : item);
}

export function fieldValue(raw: string, field: Field) {
  if (field.enum) return field.enum[Number(raw)];
  if (field.type === 'boolean') return raw === 'on';
  if (field.type === 'number' || field.type === 'integer') return Number(raw);
  if (field.type === 'object' || field.type === 'array') return JSON.parse(raw) as unknown;
  return raw.trim();
}

// Works on the existing HTTP demo gateway as well as on HTTPS.
export function newId() {
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), byte => byte.toString(16).padStart(2, '0')).join('');
}
