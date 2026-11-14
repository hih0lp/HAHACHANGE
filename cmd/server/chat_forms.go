package main

// Kept in the chat page: the bearer token stays in the same browser tab and
// every form uses the current server-published JSON Schema.
const chatFormsJS = `
function newIdempotencyKey(){const bytes=new Uint8Array(16);crypto.getRandomValues(bytes);return Array.from(bytes,byte=>byte.toString(16).padStart(2,'0')).join('')}
async function openInlineForm(container,formId){
  if(container.querySelector('[data-inline-form]'))return;
  const panel=document.createElement('section');panel.dataset.inlineForm='';panel.className='card';container.append(panel);
  const state=document.createElement('p');state.textContent='Загружаю форму…';panel.append(state);
  try{
    const token=await teamSessionReady;
    if(!token)throw Error('Сессия команды недоступна. Обновите чат.');
    const key=document.getElementById('key').value;
    const authHeaders={Authorization:'Bearer '+token};if(key)authHeaders['X-API-Key']=key;
    const response=await fetch('/v1/forms/'+encodeURIComponent(formId),{headers:authHeaders});
    if(!response.ok)throw Error('Не удалось загрузить форму. Обновите чат и попробуйте снова.');
    const definition=await response.json();
    const schema=definition.schema||{},ui=definition.ui_schema||{};
    const order=ui.order||Object.keys(schema.properties||{});
    let resources=[];
    if(schema.properties?.resource_id&&schema.properties?.capability){
      const catalog=await fetch('/v1/form-resources',{headers:authHeaders});
      if(!catalog.ok)throw Error('Не удалось загрузить доступные ресурсы. Попробуйте позже.');
      const data=await catalog.json();resources=Array.isArray(data.resources)?data.resources:[];
      if(!resources.length)throw Error('Для вашей команды пока нет доступных ресурсов для заявки.');
    }
    panel.replaceChildren();
    const title=document.createElement('h3');title.textContent=definition.title;panel.append(title);
    if(definition.description){const description=document.createElement('p');description.textContent=definition.description;panel.append(description)}
    const inlineForm=document.createElement('form');inlineForm.className='inline-form';panel.append(inlineForm);
    let resourceField=null,capabilityField=null;
    for(const name of order){
      const fieldSchema=(schema.properties||{})[name];if(!fieldSchema)continue;
      const label=document.createElement('label');label.textContent=(ui.labels||{})[name]||fieldSchema.title||name;inlineForm.append(label);
      let field;
      if(name==='resource_id'&&resources.length){field=document.createElement('select');const empty=document.createElement('option');empty.value='';empty.textContent='Выберите ресурс';field.append(empty);for(const resource of resources){const option=document.createElement('option');option.value=resource.id;option.textContent=resource.name||resource.id;field.append(option)}resourceField=field}
      else if(name==='capability'&&resources.length){field=document.createElement('select');capabilityField=field}
      else if(fieldSchema.enum){field=document.createElement('select');for(const value of fieldSchema.enum){const option=document.createElement('option');option.value=String(value);option.textContent=String(value);field.append(option)}}
      else if(fieldSchema.type==='boolean'){field=document.createElement('input');field.type='checkbox'}
      else if(fieldSchema.type==='object'||fieldSchema.type==='array'){field=document.createElement('textarea');field.placeholder='JSON'}
      else{field=document.createElement('input');field.type=fieldSchema.format==='date'?'date':fieldSchema.format==='email'?'email':fieldSchema.type==='integer'||fieldSchema.type==='number'?'number':'text';if(fieldSchema.type==='integer')field.step='1';if(fieldSchema.minLength)field.minLength=fieldSchema.minLength;if(fieldSchema.maxLength)field.maxLength=fieldSchema.maxLength}
      field.dataset.name=name;field.dataset.type=fieldSchema.type||'string';field.required=(schema.required||[]).includes(name);inlineForm.append(field);
    }
    if(resourceField&&capabilityField){const updateCapabilities=()=>{capabilityField.replaceChildren();const empty=document.createElement('option');empty.value='';empty.textContent='Выберите право';capabilityField.append(empty);const selected=resources.find(resource=>resource.id===resourceField.value);for(const value of selected?.capabilities||[]){const option=document.createElement('option');option.value=value;option.textContent=value;capabilityField.append(option)}};resourceField.addEventListener('change',updateCapabilities);updateCapabilities()}
    const review=document.createElement('button');review.type='submit';review.textContent='Проверить заявку';inlineForm.append(review);
    const result=document.createElement('div');result.setAttribute('role','status');panel.append(result);
    inlineForm.onsubmit=event=>{
      event.preventDefault();result.replaceChildren();
      try{
        const values={};for(const field of inlineForm.querySelectorAll('[data-name]')){let value=field.type==='checkbox'?field.checked:field.value;if(field.dataset.type==='integer'||field.dataset.type==='number')value=Number(value);if(field.dataset.type==='object'||field.dataset.type==='array')value=JSON.parse(value);values[field.dataset.name]=value}
        const preview=document.createElement('pre');preview.textContent=JSON.stringify(values,null,2);result.append(preview);
        const confirmButton=document.createElement('button');confirmButton.type='button';confirmButton.textContent='Подтвердить отправку';result.append(confirmButton);
        const idem=newIdempotencyKey();
        confirmButton.onclick=async()=>{
          confirmButton.disabled=true;
          try{
            const headers={...authHeaders,'Content-Type':'application/json','Idempotency-Key':idem};
            const sent=await fetch('/v1/forms/'+encodeURIComponent(formId)+'/submit',{method:'POST',headers,body:JSON.stringify({version:definition.version,values,confirmed:true})});
            const data=await sent.json();if(!sent.ok)throw Error(typeof data.error==='string'?data.error:'Не удалось отправить заявку. Проверьте значения полей.');
            result.replaceChildren();const message=document.createElement('p');message.textContent='Заявка успешно отправлена!';result.append(message);inlineForm.remove();
          }catch(error){confirmButton.disabled=false;const message=document.createElement('p');message.className='error';message.textContent=error.message;result.append(message)}
        };
      }catch(error){result.textContent='Проверьте значения полей: '+error.message;result.className='error'}
    };
  }catch(error){panel.replaceChildren();const message=document.createElement('p');message.textContent=error.message;message.className='error';panel.append(message)}
}
const requestedForm=new URLSearchParams(location.search).get('form');
if(requestedForm&&/^[a-zA-Z0-9_-]+$/.test(requestedForm)){
  const card=document.createElement('article');card.className='bubble assistant';results.append(card);openInlineForm(card,requestedForm);history.replaceState(null,'','/');
}
`
