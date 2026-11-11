package main

import (
	"net/http"
	"strings"
)

func webPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(strings.Replace(pageHTML, "</script>", chatFormsJS+"</script>", 1)))
}

const pageHTML = `<!doctype html>
<html lang="ru"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Бобби — AI-buddy</title>
<style>
:root{font-family:system-ui,-apple-system,Arial,sans-serif;color:#222;background:#f6f7fb}*{box-sizing:border-box}
main{max-width:880px;margin:24px auto;padding:0 20px}h1{font-size:30px;margin-bottom:8px}p{color:#59616e}
form,.card{background:white;border:1px solid #e1e5ed;border-radius:14px;padding:20px;box-shadow:0 2px 9px #1b2b4610}
label{display:block;font-weight:600;margin:14px 0 6px}input{width:100%;padding:12px;border:1px solid #bfc8d4;border-radius:8px;font:inherit}
button{background:#005bff;color:white;border:0;border-radius:8px;padding:12px 22px;font:inherit;cursor:pointer;margin-top:16px}
button:disabled{opacity:.55;cursor:wait}.card{margin-top:16px}pre{white-space:pre-wrap;overflow-wrap:anywhere;font:inherit;line-height:1.5}
.meta{color:#586173;font-size:14px}.error{color:#b42318}#results{display:flex;flex-direction:column;gap:12px;min-height:42vh;margin:18px 0}.bubble{max-width:82%;padding:14px 18px;border-radius:16px;background:white;border:1px solid #e1e5ed;overflow-wrap:anywhere}.bubble.user{align-self:flex-end;background:#e9f0ff}.bubble.assistant{align-self:flex-start}.bubble pre{margin:8px 0 0}.bubble h2{font-size:14px;margin:0 0 6px}
.bubble.deadline{background:#FFF3D6;border-left:5px solid #D97706;color:#442500}.bubble.deadline h2{color:#442500}select{padding:8px;border-radius:8px;margin-left:8px}
</style></head><body><main><h1>Бобби · AI-buddy</h1>
<p id="team-state" class="meta">Подключаю сервис команды и доступов…</p>
<label for="demo-role">Демо-роль <select id="demo-role"><option value="intern">Стажёр</option><option value="mentor">Наставник</option></select></label>
<section id="results" aria-label="Сообщения чата" aria-live="polite"></section>
<form id="form"><label for="query">Сообщение</label><input id="query" required minlength="2" placeholder="Спросите о задаче или онбординге">
<label for="key">API-ключ (если задан на сервере)</label><input id="key" type="password" autocomplete="off" placeholder="Необязательно">
<button id="submit" type="submit" value="ask">Отправить</button></form><p id="status" role="status"></p></main>
<script>
const form=document.getElementById('form'),results=document.getElementById('results'),status=document.getElementById('status'),button=document.getElementById('submit'),teamState=document.getElementById('team-state');
async function connectTeamAccess(){try{const response=await fetch('/v1/team-access/session',{method:'POST'});const data=await response.json();if(!response.ok)throw Error(data.error||'Сервис команды и доступов недоступен');document.getElementById('demo-role').value=data.role;sessionStorage.setItem('team-access-token',data.access_token);teamState.textContent='Сервис команды и доступов подключён.';return data.access_token}catch(error){sessionStorage.removeItem('team-access-token');teamState.textContent=error.message;teamState.className='error';return ''}}
const teamSessionReady=connectTeamAccess();
document.getElementById('demo-role').addEventListener('change',async e=>{const response=await fetch('/v1/demo/role',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({role:e.target.value})});if(!response.ok){status.textContent='Не удалось переключить роль';return}sessionStorage.removeItem('team-access-token');location.reload()});
function appendMessage(target,message){const pattern=/\[([^\]]+)\]\((https:\/\/[^\s)]+|\/forms\/[a-zA-Z0-9_-]+)\)/g;let position=0,match;while((match=pattern.exec(message))!==null){target.append(document.createTextNode(message.slice(position,match.index)));const linkPath=match[2];if(linkPath.startsWith('/forms/')){const button=document.createElement('button');button.type='button';button.textContent=match[1];button.onclick=()=>openInlineForm(target.parentElement,linkPath.slice(7));target.append(button)}else{const link=document.createElement('a');link.href=linkPath;link.target='_blank';link.rel='noopener noreferrer';link.textContent=match[1];target.append(link)}position=pattern.lastIndex}target.append(document.createTextNode(message.slice(position)))}
function showJob(card,job){const state=card.querySelector('[data-state]'),body=card.querySelector('[data-body]');
state.textContent=job.status==='queued'?'В очереди':job.status==='running'?'Готовим ответ':job.status==='completed'?'Готово':'Ошибка';
if(job.status==='completed'){body.replaceChildren();const answer=document.createElement('pre');appendMessage(answer,job.answer);body.append(answer)}
if(job.status==='failed'){body.textContent=job.error||'Не удалось получить ответ';body.className='error'}}
function watchJob(job,card,key){let finished=false;const url=(location.protocol==='https:'?'wss://':'ws://')+location.host+'/v1/jobs/'+job.id+'/ws';const socket=new WebSocket(url);
socket.onopen=()=>{if(key)socket.send(JSON.stringify({api_key:key}))};
socket.onmessage=event=>{const update=JSON.parse(event.data);showJob(card,update);if(update.status==='completed'||update.status==='failed'){finished=true;socket.close()}};
socket.onclose=()=>{if(!finished)pollJob(job.id,card,key)};socket.onerror=()=>socket.close()}
async function pollJob(id,card,key){for(;;){await new Promise(resolve=>setTimeout(resolve,2000));try{const headers={};if(key)headers['X-API-Key']=key;const response=await fetch('/v1/jobs/'+id,{headers});if(!response.ok)throw Error('HTTP '+response.status);const job=await response.json();showJob(card,job);if(job.status==='completed'||job.status==='failed')return}catch(error){card.querySelector('[data-state]').textContent='Переподключение…'}}}
form.addEventListener('submit',async event=>{event.preventDefault();const query=document.getElementById('query').value,key=document.getElementById('key').value;
button.disabled=true;status.textContent='Отправляю…';status.className='';
try{const teamToken=await teamSessionReady;if(!teamToken)throw Error('Сервис команды и доступов не подключён. Обновите страницу.');const headers={'Content-Type':'application/json'};if(key)headers['X-API-Key']=key;headers.Authorization='Bearer '+teamToken;
const response=await fetch('/v1/jobs',{method:'POST',headers,body:JSON.stringify({query,scenario:'general'})});
const data=await response.json();if(!response.ok)throw Error(data.error||'HTTP '+response.status);
const user=document.createElement('div');user.className='bubble user';user.textContent=query;results.append(user);const card=document.createElement('article');card.className='bubble assistant';const title=document.createElement('h2');title.textContent='AI-buddy';const state=document.createElement('div');state.className='meta';state.dataset.state='';const body=document.createElement('div');body.dataset.body='';card.append(title,state,body);results.append(card);showJob(card,data);watchJob(data,card,key);document.getElementById('query').value='';status.textContent='Запрос принят: '+data.id
}catch(error){status.textContent=error.message;status.className='error'}finally{button.disabled=false}});
const seenGitLabReminders=new Set();
function showGitLabReminders(data){for(const item of data.reminders||[]){if(seenGitLabReminders.has(item.id))continue;seenGitLabReminders.add(item.id);const card=document.createElement('article');card.className='bubble assistant';const title=document.createElement('h2');title.textContent='AI-buddy · GitLab';const body=document.createElement('p');body.append(document.createTextNode('Открытый MR для проверки ревью: '));const link=document.createElement('a');link.href=item.url;link.target='_blank';link.rel='noopener noreferrer';link.textContent='!'+item.iid+' · '+item.title;body.append(link);card.append(title,body);results.append(card)}}
async function pollGitLabReminders(){try{const token=await teamSessionReady;if(token){const response=await fetch('/v1/gitlab/reminders',{headers:{Authorization:'Bearer '+token}});if(response.ok)showGitLabReminders(await response.json())}}catch(error){}setTimeout(pollGitLabReminders,60000)}
pollGitLabReminders();
const seenCalendarReminders=new Set();
function showCalendarReminders(data){for(const item of data.data?.items||[]){const start=item.start?.dateTime;if(!start)continue;const id=item.id+':'+start;if(seenCalendarReminders.has(id))continue;seenCalendarReminders.add(id);const card=document.createElement('article');card.className='bubble assistant';const title=document.createElement('h2');title.textContent='AI-buddy · календарь';const body=document.createElement('p');body.textContent='Скоро: '+(item.summary||'событие')+' — '+new Date(start).toLocaleString();card.append(title,body);results.append(card)}}
async function pollCalendarReminders(){try{const token=await teamSessionReady;if(token){const response=await fetch('/v1/calendar/reminders',{headers:{Authorization:'Bearer '+token}});if(response.ok)showCalendarReminders(await response.json())}}catch(error){}setTimeout(pollCalendarReminders,60000)}
pollCalendarReminders();
const seenDeadlineNotifications=new Set();
async function loadDeadlineNotifications(){try{await teamSessionReady;const headers={},key=document.getElementById('key').value;if(key)headers['X-API-Key']=key;const response=await fetch('/v1/deadline-notifications',{headers});if(response.ok){const data=await response.json();for(const item of (data.notifications||[]).reverse()){if(seenDeadlineNotifications.has(item.id))continue;seenDeadlineNotifications.add(item.id);const card=document.createElement('article');card.className='bubble assistant deadline';const title=document.createElement('h2');title.textContent=item.kind==='mentor_24h'?'Помогите стажёру':'Скоро дедлайн';const body=document.createElement('p');body.append(document.createTextNode(item.kind==='mentor_24h'?'Срок задачи стажёра скоро истекает. Пожалуйста, помогите ему: ':'Скоро срок вашей задачи: '));const link=document.createElement('a');link.href=item.url;link.target='_blank';link.rel='noopener noreferrer';link.textContent=item.key+' · '+item.summary;body.append(link,document.createTextNode(' — до '+new Date(item.due_at).toLocaleString()));card.append(title,body);results.append(card)}}}catch(error){}setTimeout(loadDeadlineNotifications,60000)}
loadDeadlineNotifications();
async function loadHistory(){try{const headers={},key=document.getElementById('key').value;if(key)headers['X-API-Key']=key;const response=await fetch('/v1/chat/history',{headers});if(!response.ok)return;const data=await response.json();for(const item of (data.turns||[]).reverse()){const user=document.createElement('div');user.className='bubble user';user.textContent=item.query;results.append(user);const card=document.createElement('article');card.className='bubble assistant';const title=document.createElement('h2');title.textContent='AI-buddy';const state=document.createElement('div');state.className='meta';state.dataset.state='';const body=document.createElement('div');body.dataset.body='';card.append(title,state,body);results.append(card);showJob(card,item);if(item.status==='queued'||item.status==='running')watchJob(item,card,key)}}catch(error){status.textContent='История чата временно недоступна'}}
loadHistory();
</script></body></html>`
