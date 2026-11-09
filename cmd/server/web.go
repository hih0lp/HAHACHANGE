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
<button id="submit" type="submit" value="ask">Спросить Qwen</button> <button type="submit" value="search">Только поиск</button></form><p id="status" role="status"></p><section id="results" aria-live="polite"></section></main>
<script>
const form=document.getElementById('form'),results=document.getElementById('results'),status=document.getElementById('status'),button=document.getElementById('submit');
form.addEventListener('submit',async event=>{event.preventDefault();const ask=event.submitter.value==='ask';button.disabled=true;status.textContent=ask?'Qwen готовит ответ…':'Ищу…';status.className='';results.replaceChildren();
try{const headers={'Content-Type':'application/json'},key=document.getElementById('key').value;if(key)headers['X-API-Key']=key;
const response=await fetch(ask?'/v1/ask':'/v1/knowledge/search',{method:'POST',headers,body:JSON.stringify({query:document.getElementById('query').value,limit:5})});
const data=await response.json();if(!response.ok)throw Error(data.error||'HTTP '+response.status);
const hits=ask?[]:data.hits;status.textContent=ask?'Ответ Qwen':'Найдено фрагментов: '+hits.length;
if(ask){const card=document.createElement('article');card.className='card';const title=document.createElement('h2');title.textContent='Ответ Qwen';const text=document.createElement('pre');const pattern=/\[([^\]]+)\]\((https:\/\/[^\s)]+)\)/g;let position=0,match;while((match=pattern.exec(data.answer))!==null){text.append(document.createTextNode(data.answer.slice(position,match.index)));const link=document.createElement('a');link.href=match[2];link.target='_blank';link.rel='noopener noreferrer';link.textContent=match[1];text.append(link);position=pattern.lastIndex}text.append(document.createTextNode(data.answer.slice(position)));card.append(title,text);results.append(card)}
if(!ask)for(const hit of hits){const card=document.createElement('article');card.className='card';const title=document.createElement('h2');title.textContent=hit.title;
const meta=document.createElement('div');meta.className='meta';meta.textContent=hit.path+' · фрагмент '+hit.chunk_index+' · оценка '+hit.score.toFixed(3);
const text=document.createElement('pre');text.textContent=hit.text;card.append(title,meta);if(hit.source_url){const link=document.createElement('a');link.className='source';link.href=hit.source_url;link.target='_blank';link.rel='noopener noreferrer';link.textContent=hit.path.startsWith('onboarding/')?'Открыть исходный чек-лист ↗':'Открыть документ Playbook ↗';card.append(link)}card.append(text);results.append(card)}
if(!ask&&!hits.length){const empty=document.createElement('p');empty.className='empty';empty.textContent='Ничего не найдено.';results.append(empty)}
}catch(error){status.textContent=error.message;status.className='error'}finally{button.disabled=false}});
</script></body></html>`
