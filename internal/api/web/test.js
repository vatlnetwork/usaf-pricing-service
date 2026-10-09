'use strict';
(() => {
  const $ = id => document.getElementById(id);
  const {el,button,field,grid,choices,render,localDate,programPayload} = PricingForms;
  let program, order, examples=[], saved=[], offset=0, hasMore=true, sequence=0, busy=false;
  const dollars = v => new Intl.NumberFormat('en-US',{style:'currency',currency:'USD'}).format(v);
  async function request(path,method='GET',body) {const response=await fetch(path,{method,headers:body?{'Content-Type':'application/json'}:{},body:body?JSON.stringify(body):undefined});const data=await response.json();if(!response.ok)throw new Error(data.error || 'Request failed');return data;}
  function message(text,error=false) {const n=$('test-message');n.textContent=text;n.hidden=!text;n.className='status-note'+(error?' error':'');}
  function dirty() { sequence++; if(program) $('test-results').replaceChildren(el('p','Inputs changed. Calculate again to update the result.','status-note')); }
  function attributes(text) {const result={};for(const raw of text.split('\n')){if(!raw.trim())continue;const p=raw.indexOf('=');if(p<1)throw new Error('Attributes must use name=value, one per line.');const key=raw.slice(0,p).trim();if(Object.hasOwn(result,key))throw new Error('Duplicate attribute: '+key);result[key]=raw.slice(p+1).trim();}return result;}
  function writeAttributes(values) {return Object.entries(values || {}).map(([k,v])=>`${k}=${v}`).join('\n');}
  function defaultLine() {return {line_id:crypto.randomUUID(),product_id:'',quantity:1,price_unit:'each',list_price:1000};}
  function selectRules() {
    const container=$('selected-scenarios');container.replaceChildren();
    if(!program.scenarios?.length){container.append(el('p','This program uses existing discount options. Enter option names on each order line.','help'));return;}
    for(const s of program.scenarios) container.append(field(`${s.name || s.id} (${s.id})`,(order.selected_scenario_ids || []).includes(s.id),checked=>{order.selected_scenario_ids ||= [];if(checked)order.selected_scenario_ids.push(s.id);else order.selected_scenario_ids=order.selected_scenario_ids.filter(id=>id!==s.id);dirty();},{type:'checkbox'}));
  }
  function lines() {
    const container=$('order-lines');container.replaceChildren();
    order.lines.forEach((l,i)=>{
      const card=el('div',undefined,'test-line');const heading=el('div',undefined,'section-heading');heading.append(el('h4',`Product ${i+1}`),button('Remove',()=>{order.lines.splice(i,1);dirty();lines();}));card.append(heading);
      const g=grid();const set=k=>v=>{if(v===undefined || v==='')delete l[k];else l[k]=v;dirty();};
      g.append(field('Line ID',l.line_id,set('line_id'),{required:true}),field('Product ID',l.product_id,set('product_id'),{required:true}),field('Product group',l.group_name,set('group_name')),field('Configuration',l.configuration,set('configuration')),field('Quantity',l.quantity,set('quantity'),{type:'number',integer:true,min:1,required:true}),field('Unit',l.price_unit,set('price_unit'),{values:choices('each','case','section')}));
      for(const [key,title] of [['list_price','List price ($)'],['net_price','Verified net price ($)'],['quote_price','Quote price ($)']])g.append(field(title,l[key],set(key),{type:'number',min:0}));
      card.append(g,field('Quote is approved',l.quote_approved,set('quote_approved'),{type:'checkbox'}));
      if(!program.scenarios?.length) card.append(field('Selected discount option names (one per line)',(l.discount_options || []).join('\n'),v=>{l.discount_options=v.split('\n').filter(Boolean);dirty();},{multiline:true}));
      const details=el('details',undefined,'scenario-details');details.append(el('summary','Product attributes'),field('Attributes (one name=value per line)',l._attributes ?? writeAttributes(l.attributes),v=>{l._attributes=v;dirty();},{multiline:true}));card.append(details);container.append(card);
    });
  }
  function context() {
    const g=grid();const set=k=>v=>{order[k]=v;dirty();};
    g.append(field('Order date and time · local timezone',localDate(order.at),v=>{order.at=v?new Date(v).toISOString():undefined;dirty();},{type:'datetime-local',help:'Start is inclusive; end is exclusive. Blank uses the current instant.'}),field('Fulfillment',order.fulfillment || '',set('fulfillment'),{values:choices(['','Choose…'],['pickup','Warehouse pickup'],['ship','Vendor shipment'])}),field('Order channel',order.channel || '',set('channel'),{values:choices(['','Choose…'],['online','Online'],['direct','Direct / email'])}),field('Dealer class',order.dealer_class,set('dealer_class')),field('Account ID',order.account_id,set('account_id')),field('Order use',order.order_use,set('order_use')),field('Shipment',order.shipment,set('shipment'),{help:'Match the rule value, e.g. immediate_single_destination.'}));
    $('order-context').replaceChildren(g);$('order-attributes').value=writeAttributes(order.attributes);
  }
  function load(p,o,description) {
    program=structuredClone(p);order=structuredClone(o || {at:new Date().toISOString(),lines:[defaultLine()]});
    $('program-description').textContent=description; $('test-form').hidden=false;context();lines();selectRules();
    render($('test-scenarios'),program,()=>{dirty();selectRules();});dirty();message('');
  }
  function results(data) {
    const container=$('test-results');container.replaceChildren(el('p','PRICING RESULT','eyebrow'));
    if(data.available){container.append(el('h2',dollars(data.total)),el('p','Eligible merchandise total','muted'));const table=el('table');const head=el('tr');for(const t of ['Line / unit','Qty','Total'])head.append(el('th',t));const thead=el('thead');thead.append(head);table.append(thead);const body=el('tbody');
      for(const l of data.lines){const row=el('tr');row.append(el('td',`${l.line_id} · ${l.price_unit}`),el('td',String(l.quantity)),el('td',dollars(l.total)));body.append(row);}table.append(body);container.append(table);
      for(const l of data.lines){const d=el('details',undefined,'result-detail');d.append(el('summary',`${l.line_id}: ${l.scenario_ids.join(' → ') || 'Existing discount pricing'}`));const list=el('ul');for(const step of l.explanation)list.append(el('li',step));d.append(el('p',`Average price per ${l.price_unit}: ${dollars(l.unit_price)}`,'help'),list);container.append(d);}
    } else {container.append(el('h2','Unavailable'));for(const reason of data.reasons)container.append(el('p',reason,'status-note error'));}
    if(data.instructions.length){container.append(el('h4','PO / cart instructions'));const list=el('ul');for(const s of data.instructions)list.append(el('li',s));container.append(list);}
    if(data.sources.length){const d=el('details',undefined,'result-detail');d.append(el('summary','Source and approval evidence'));for(const s of data.sources)d.append(el('p',s,'help'));container.append(d);}
    const evaluations=el('div',undefined,'test-result-list');for(const evaluation of data.evaluations){const d=el('details',undefined,'result-detail');d.open=!data.available;d.append(el('summary',`${evaluation.name} · ${evaluation.eligible_line_ids.length ? 'Eligible for '+evaluation.eligible_line_ids.join(', ') : 'Not eligible'}`));for(const [id,reason] of Object.entries(evaluation.rejections))d.append(el('p',`${id}: ${reason}`,'help'));evaluations.append(d);}container.append(el('h4','Rule eligibility'),evaluations);
  }
  async function calculate(event) {
    event?.preventDefault();if(!program || busy)return;
    if(!$('test-form').reportValidity())return;
    busy=true;$('calculate').disabled=true;const version=sequence;
    try{const clean=structuredClone(order);clean.attributes=attributes($('order-attributes').value);for(const l of clean.lines){if(l._attributes!==undefined)l.attributes=attributes(l._attributes);delete l._attributes;}
      message('');const data=await request('/vendor-programs/preview','POST',{program:programPayload(program),order:clean});if(version===sequence)results(data);
    }catch(error){if(version===sequence){message(error.message,true);$('test-results').replaceChildren(el('p','Could not calculate. Correct the reported input and try again.','status-note error'));}}finally{busy=false;$('calculate').disabled=false;}
  }
  async function loadPrograms(reset=false) {
    $('more-programs').disabled=true;$('reload-programs').disabled=true;
    try{if(reset){offset=0;saved=[];}const page=(await request(`/vendor-programs?limit=100&offset=${offset}`)) || [];offset+=page.length;saved.push(...page);hasMore=page.length===100;
      const selected=$('saved-program').value;const first=el('option','Select a saved program…');first.value='';$('saved-program').replaceChildren(first);for(const p of saved){const option=el('option',`${p.vendor}${p.vendor_code ? ' · ' + p.vendor_code : ''} · ${p.id}`);option.value=p.id;$('saved-program').append(option);}$('saved-program').value=selected;$('more-programs').hidden=!hasMore;
    }catch(error){message('Saved programs could not be loaded: '+error.message+'. Tutorial examples remain available.',true);}finally{$('more-programs').disabled=false;$('reload-programs').disabled=false;}
  }

  document.getElementById('test-form').addEventListener('invalid', event => {
    for (let parent = event.target.parentElement; parent; parent = parent.parentElement) {
      if (parent.tagName === 'DETAILS') parent.open = true;
    }
  }, true);
  $('test-form').onsubmit=calculate;$('order-attributes').oninput=dirty;
  $('add-line').onclick=()=>{order.lines.push(defaultLine());dirty();lines();};
  $('saved-program').onchange=async()=>{const id=$('saved-program').value;if(!id)return;const selection=++sequence;try{const p=await request('/vendor-programs/'+encodeURIComponent(id));if(selection!==sequence)return;$('example').value='';load(p,null,`${p.vendor} · testing a copy of the saved program. Changes here are not saved.`);}catch(error){message(error.message,true);}};
  $('example').onchange=()=>{const example=examples.find(x=>x.id===$('example').value);if(!example)return;$('saved-program').value='';load(example.program,example.order,example.description+' Example data is temporary.');calculate();};
  $('more-programs').onclick=()=>loadPrograms();$('reload-programs').onclick=()=>loadPrograms(true);
  request('/assets/examples.json').then(data=>{examples=data;for(const x of examples){const o=el('option',x.name);o.value=x.id;$('example').append(o);}}).catch(error=>message(error.message,true));
  loadPrograms();
  if(new URLSearchParams(location.search).has('draft')){try{const draft=sessionStorage.getItem('usaf-pricing-draft');if(draft)load(JSON.parse(draft),null,'Testing your unsaved editor draft. Changes here do not update the original draft.');else message('Draft is not available. Open this page using Test this draft in the program editor.',true);}catch(error){message('Could not load draft: '+error.message,true);}}
})();
