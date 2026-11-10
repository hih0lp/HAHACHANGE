package main

import "net/http"

func webPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(pageHTML))
}

const pageHTML = `<!doctype html>
<html lang="ru"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>AI-buddy — база знаний</title>
<style>
:root{font-family:system-ui,-apple-system,Arial,sans-serif;color:#222;background:#f6f7fb}*{box-sizing:border-box}
main{max-width:880px;margin:56px auto;padding:0 20px}h1{font-size:30px;margin-bottom:8px}p{color:#59616e}
form,.card{background:white;border:1px solid #e1e5ed;border-radius:14px;padding:20px;box-shadow:0 2px 9px #1b2b4610}
label{display:block;font-weight:600;margin:14px 0 6px}input{width:100%;padding:12px;border:1px solid #bfc8d4;border-radius:8px;font:inherit}
button{background:#005bff;color:white;border:0;border-radius:8px;padding:12px 22px;font:inherit;cursor:pointer;margin-top:16px}
button:disabled{opacity:.55;cursor:wait}.card{margin-top:16px}pre{white-space:pre-wrap;overflow-wrap:anywhere;font:inherit;line-height:1.5}
.meta{color:#586173;font-size:14px}.source{display:inline-block;margin:8px 0;color:#005bff}.error{color:#b42318}.empty{padding:20px 0}
</style></head><body><main><h1>AI-buddy: база знаний</h1>
<p>Поиск по открытому AvitoTech Playbook и чек-листу стажёра. Ответ модели показывает источники; внутренний порядок действий Авито здесь не представлен.</p>
<form id="form"><label for="query">Вопрос</label><input id="query" required minlength="2" placeholder="Как проходит ревью технического дизайна?">
<label for="key">API-ключ (если задан на сервере)</label><input id="key" type="password" autocomplete="off" placeholder="Необязательно">
<button id="submit" type="submit" value="ask">Спросить Qwen</button></form><p id="status" role="status"></p><section id="results" aria-live="polite"></section>
<section class="card"><h2>Тестовая заявка на доступ</h2><p>Проект SCRUM в Jira. Сначала проверьте содержание заявки, затем подтвердите создание. В демо режиме задача сохранится только локально.</p>
<form id="access-form"><label for="resource">К какому ресурсу нужен доступ</label><input id="resource" required minlength="2" maxlength="120" placeholder="Тестовое пространство команды">
<label for="reason">Зачем нужен доступ</label><input id="reason" required minlength="2" maxlength="500" placeholder="Для учебного онбординга">
<label><input id="confirmed" type="checkbox" required style="width:auto"> Подтверждаю создание тестовой задачи</label><button type="submit">Создать тестовую заявку</button></form>
<p id="access-status" role="status"></p><div id="access-list"></div></section></main>
<script>
const form=document.getElementById('form'),results=document.getElementById('results'),status=document.getElementById('status'),button=document.getElementById('submit');
function appendMessage(target,message){const pattern=/\[([^\]]+)\]\((https:\/\/[^\s)]+)\)/g;let position=0,match;while((match=pattern.exec(message))!==null){target.append(document.createTextNode(message.slice(position,match.index)));const link=document.createElement('a');link.href=match[2];link.target='_blank';link.rel='noopener noreferrer';link.textContent=match[1];target.append(link);position=pattern.lastIndex}target.append(document.createTextNode(message.slice(position)))}
function showJob(card,job){const state=card.querySelector('[data-state]'),body=card.querySelector('[data-body]');
state.textContent=job.status==='queued'?'В очереди':job.status==='running'?'Готовим ответ':job.status==='completed'?'Готово':'Ошибка';
if(job.status==='completed'){body.replaceChildren();const answer=document.createElement('pre');appendMessage(answer,job.answer);body.append(answer);if(/доступ|access/i.test(card.dataset.query||'')){const action=document.createElement('button');action.type='button';action.textContent='Оформить тестовый запрос в Jira';action.addEventListener('click',()=>{document.getElementById('resource').focus();document.getElementById('access-form').scrollIntoView({behavior:'smooth'})});body.append(action)}}
if(job.status==='failed'){body.textContent=job.error||'Не удалось получить ответ';body.className='error'}}
function watchJob(job,card,key){let finished=false;const url=(location.protocol==='https:'?'wss://':'ws://')+location.host+'/v1/jobs/'+job.id+'/ws';const socket=new WebSocket(url);
socket.onopen=()=>{if(key)socket.send(JSON.stringify({api_key:key}))};
socket.onmessage=event=>{const update=JSON.parse(event.data);showJob(card,update);if(update.status==='completed'||update.status==='failed'){finished=true;socket.close()}};
socket.onclose=()=>{if(!finished)pollJob(job.id,card,key)};socket.onerror=()=>socket.close()}
async function pollJob(id,card,key){for(;;){await new Promise(resolve=>setTimeout(resolve,2000));try{const headers={};if(key)headers['X-API-Key']=key;const response=await fetch('/v1/jobs/'+id,{headers});if(!response.ok)throw Error('HTTP '+response.status);const job=await response.json();showJob(card,job);if(job.status==='completed'||job.status==='failed')return}catch(error){card.querySelector('[data-state]').textContent='Переподключение…'}}}
form.addEventListener('submit',async event=>{event.preventDefault();const query=document.getElementById('query').value,key=document.getElementById('key').value;
button.disabled=true;status.textContent='Отправляю…';status.className='';
try{const headers={'Content-Type':'application/json'};if(key)headers['X-API-Key']=key;
const response=await fetch('/v1/jobs',{method:'POST',headers,body:JSON.stringify({query})});
const data=await response.json();if(!response.ok)throw Error(data.error||'HTTP '+response.status);
const card=document.createElement('article');card.className='card';card.dataset.query=query;const title=document.createElement('h2');title.textContent=query;const state=document.createElement('div');state.className='meta';state.dataset.state='';const body=document.createElement('div');body.dataset.body='';card.append(title,state,body);results.prepend(card);showJob(card,data);watchJob(data,card,key);status.textContent='Запрос принят: '+data.id
}catch(error){status.textContent=error.message;status.className='error'}finally{button.disabled=false}});
const accessForm=document.getElementById('access-form'),accessList=document.getElementById('access-list'),accessStatus=document.getElementById('access-status');
function accessHeaders(){const headers={'Content-Type':'application/json'},key=document.getElementById('key').value;if(key)headers['X-API-Key']=key;return headers}
function renderAccess(item){const card=document.createElement('article');card.className='card';const title=document.createElement('h3');title.textContent=item.key+' · '+item.resource;const meta=document.createElement('p');meta.className='meta';meta.textContent='Статус: '+item.status+' · режим: '+item.mode+' · '+new Date(item.created_at).toLocaleString();const reason=document.createElement('p');reason.textContent=item.reason;card.append(title,meta,reason);if(item.url){const link=document.createElement('a');link.href=item.url;link.target='_blank';link.rel='noopener noreferrer';link.textContent='Открыть задачу в Jira ↗';card.append(link)}const next=document.createElement('p');next.textContent=item.next_step;card.append(next);return card}
async function loadAccess(){try{const response=await fetch('/v1/access-requests',{headers:accessHeaders()});if(!response.ok)return;const data=await response.json();accessList.replaceChildren(...data.requests.map(renderAccess))}catch(error){accessStatus.textContent=error.message}}
accessForm.addEventListener('submit',async event=>{event.preventDefault();const resource=document.getElementById('resource').value.trim(),reason=document.getElementById('reason').value.trim(),confirmed=document.getElementById('confirmed').checked;const headers=accessHeaders();headers['Idempotency-Key']=crypto.randomUUID();const button=accessForm.querySelector('button');button.disabled=true;accessStatus.textContent='Создаю задачу…';try{const response=await fetch('/v1/access-requests',{method:'POST',headers,body:JSON.stringify({resource,reason,confirmed,role:'intern'})});const data=await response.json();if(!response.ok)throw Error(data.error||'HTTP '+response.status);accessStatus.textContent='Создана '+data.key+' ('+data.mode+')';accessForm.reset();await loadAccess()}catch(error){accessStatus.textContent=error.message;accessStatus.className='error'}finally{button.disabled=false}});
loadAccess();
</script></body></html>`
