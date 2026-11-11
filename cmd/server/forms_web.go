package main

import "net/http"

func formPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(formPageHTML))
}

const formPageHTML = `<!doctype html><html lang="ru"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Форма — Бобби</title>
<style>:root{font-family:system-ui;color:#222;background:#f6f7fb}*{box-sizing:border-box}main{max-width:680px;margin:32px auto;padding:20px}form{background:white;border:1px solid #ddd;border-radius:14px;padding:24px}label{display:block;margin:16px 0 5px;font-weight:600}input,select,textarea{width:100%;padding:10px;font:inherit;border:1px solid #aaa;border-radius:7px}input[type=checkbox]{width:auto}textarea{min-height:100px}button{margin-top:20px;padding:12px 20px;border:0;border-radius:8px;background:#005bff;color:white;font:inherit}pre{white-space:pre-wrap}.error{color:#b42318}</style></head><body><main><p><a href="/">← К чату</a></p><h1 id="title">Загрузка формы…</h1><p id="description"></p><form id="dynamic-form" hidden><div id="fields"></div><label for="key">API-ключ, если требуется</label><input id="key" type="password"><button type="submit">Проверить и отправить</button></form><p id="status" role="status"></p></main>
<script>
const form=document.getElementById('dynamic-form'),fields=document.getElementById('fields'),status=document.getElementById('status');let definition;
const formId=decodeURIComponent(location.pathname.split('/').pop());
function headers(){const value=sessionStorage.getItem('team-access-token');if(!value)throw Error('Сессия недоступна. Вернитесь в чат и обновите страницу.');const result={'Authorization':'Bearer '+value};const key=document.getElementById('key').value;if(key)result['X-API-Key']=key;return result}
function renderField(name,schema,ui,required){const label=document.createElement('label');label.textContent=(ui.labels&&ui.labels[name])||schema.title||name;label.htmlFor='field-'+name;fields.append(label);let field;
if(schema.enum){field=document.createElement('select');for(const choice of schema.enum){const option=document.createElement('option');option.value=String(choice);option.textContent=String(choice);field.append(option)}}
else if(schema.type==='boolean'){field=document.createElement('input');field.type='checkbox'}
else if(schema.type==='object'||schema.type==='array'){field=document.createElement('textarea');field.placeholder='JSON'}
else{field=document.createElement('input');field.type=(schema.type==='number'||schema.type==='integer')?'number':schema.format==='date'?'date':schema.format==='email'?'email':'text';if(schema.type==='integer')field.step='1';if(schema.minLength)field.minLength=schema.minLength;if(schema.maxLength)field.maxLength=schema.maxLength}
field.id='field-'+name;field.dataset.name=name;field.dataset.type=schema.type||'string';field.required=required;fields.append(field)}
async function load(){try{const response=await fetch('/v1/forms/'+encodeURIComponent(formId),{headers:headers()});if(!response.ok)throw Error('Форма недоступна');definition=await response.json();document.getElementById('title').textContent=definition.title;document.getElementById('description').textContent=definition.description||'';const schema=definition.schema,ui=definition.ui_schema||{};const order=ui.order||Object.keys(schema.properties||{});for(const name of order){if(schema.properties&&schema.properties[name])renderField(name,schema.properties[name],ui,(schema.required||[]).includes(name))}form.hidden=false}catch(error){status.textContent=error.message;status.className='error'}}
form.addEventListener('submit',async event=>{event.preventDefault();try{const values={};for(const field of fields.querySelectorAll('[data-name]')){let value=field.type==='checkbox'?field.checked:field.value;if(field.dataset.type==='number'||field.dataset.type==='integer')value=Number(value);if(field.dataset.type==='object'||field.dataset.type==='array')value=JSON.parse(value);values[field.dataset.name]=value}if(!confirm('Отправить форму «'+definition.title+'» с указанными данными?'))return;const requestHeaders=headers();requestHeaders['Content-Type']='application/json';requestHeaders['Idempotency-Key']=crypto.randomUUID();const response=await fetch('/v1/forms/'+encodeURIComponent(formId)+'/submit',{method:'POST',headers:requestHeaders,body:JSON.stringify({version:definition.version,values,confirmed:true})});const result=await response.json();if(!response.ok)throw Error(result.error||'Не удалось отправить форму');status.textContent='Форма отправлена. Номер: '+result.id+'. Статус: '+result.status;status.className='';form.querySelector('button').disabled=true}catch(error){status.textContent=error.message;status.className='error'}});
load();
</script></body></html>`
