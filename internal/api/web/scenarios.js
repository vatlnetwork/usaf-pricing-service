'use strict';
// Shared form controls for the program editor and the order test workspace.
window.PricingForms = (() => {
  const el = (tag, text, cls) => { const n = document.createElement(tag); if (text !== undefined) n.textContent = text; if (cls) n.className = cls; return n; };
  const button = (text, action) => { const b = el('button', text); b.type = 'button'; b.onclick = action; return b; };
  const grid = () => el('div', undefined, 'scenario-grid');
  function field(title, value, change, options = {}) {
    const label = el('label', title);
    const input = document.createElement(options.values ? 'select' : options.multiline ? 'textarea' : 'input');
    if (options.values) for (const [v, t] of options.values) { const o = el('option', t); o.value = v; input.append(o); }
    else if (!options.multiline) input.type = options.type || 'text';
    if (input.type === 'checkbox') { label.className = 'check'; input.checked = Boolean(value); }
    else input.value = value ?? '';
    if (input.type === 'number') { input.step = options.integer ? '1' : 'any'; if (options.min !== undefined) input.min = options.min; }
    if (input.type === 'datetime-local') input.step = '0.001';
    if (options.required) input.required = true;
    input.oninput = () => change(input.type === 'checkbox' ? input.checked : input.type === 'number' ? (input.value === '' ? undefined : input.valueAsNumber) : input.value);
    label.append(input);
    if (options.help) label.append(el('span', options.help, 'help'));
    return label;
  }
  const choices = (...values) => values.map(v => Array.isArray(v) ? v : [v, v.replaceAll('_', ' ')]);
  const list = value => value.split('\n').map(s => s.trim()).filter(Boolean);
  const listField = (title, values, change) => field(title, (values || []).join('\n'), v => change(list(v)), { multiline: true, help: 'One exact value per line.' });
  function section(title) { const d = el('details', undefined, 'scenario-details'); d.append(el('summary', title)); return d; }
  function selector(title, target, changed) {
    const d = section(title); const g = grid();
    for (const [key, label] of [['product_ids', 'Product IDs'], ['group_names', 'Product groups'], ['configurations', 'Configurations'], ['excluded_product_ids', 'Excluded product IDs'], ['excluded_group_names', 'Excluded groups']]) g.append(listField(label, target[key], v => { target[key] = v; changed(); }));
    d.append(el('p', 'Blank IDs and groups match all products. IDs and groups match either list; configurations and exclusions narrow the match.', 'help'), g); return d;
  }
  const policies = choices(['lowest_price', 'Lowest permitted order total'], ['highest_tier', 'Highest qualifying tier'], ['priority', 'Highest approved priority'], ['require_review', 'Require review when alternatives qualify']);
  function render(container, program, changed) {
    const openIDs = new Set([...container.querySelectorAll(".scenario-card[open]")].map(n => n.dataset.scenarioId));
    container.replaceChildren(); program.scenarios ||= [];
    const update = () => { changed(); };
    container.append(field('When multiple scenarios qualify', program.selection_policy || 'lowest_price', v => { program.selection_policy = v; update(); }, { values: policies, help: 'Tiers and priorities use larger numbers first. Ties use the lower total.' }));
    for (const [index, s] of program.scenarios.entries()) {
      const card = el('details', undefined, 'scenario-card'); card.dataset.scenarioId = s.id; card.open = !s.name || openIDs.has(s.id);
      const summary = el('summary', `${s.name || 'New scenario'} · ${s.id || 'ID required'}`); card.append(summary);
      const set = key => v => { if (key === 'id') card.dataset.scenarioId = v; if (v === undefined || v === '') delete s[key]; else s[key] = v; update(); if (key === 'name' || key === 'id') summary.textContent = `${s.name || 'New scenario'} · ${s.id || 'ID required'}`; };
      const g = grid();
      g.append(field('Scenario ID', s.id, set('id'), {required: true, help: 'Stable ID used by base rules and combinations.'}), field('Scenario name', s.name, set('name'), {required: true}), field('Program', s.program || 'regular', set('program'), {values: choices('regular', 'special')}), field('Rule role', s.role || 'price', set('role'), {values: choices(['price','Complete purchase price'], ['adjustment','Additional adjustment'], ['replacement','Replacement price'])}));
      for (const [key,label] of [['starts_at','Starts at · local time'],['ends_at','Ends at · local time']]) g.append(field(label, localDate(s[key]), v => { if (v) s[key] = new Date(v).toISOString(); else delete s[key]; update(); }, {type: 'datetime-local'}));
      s.scope ||= {};card.append(g, selector('Eligible products and exclusions', s.scope, update));
      const pricing = grid();
      pricing.append(field('Calculation method', s.method || 'steps', v => {
        s.method = v;
        if (!['fixed','bundle'].includes(v)) delete s.fixed_price;
        if (v !== 'count') { delete s.count_scope; delete s.percent_per_unit; delete s.maximum_percent; }
        if (v !== 'bundle') { delete s.bundle_components; if (s.price_unit === 'bundle') s.price_unit = 'each'; }
        if (v !== 'free') { delete s.buy_quantity; delete s.free_quantity; delete s.free_value_limit; }
        if (['bundle','free','review'].includes(v)) s.adjustments = [];
        if (['fixed','bundle','quote','review'].includes(v)) { s.starting_price = 'list'; delete s.base_scenario_id; if (s.role === 'adjustment') s.role = 'price'; }
        if (v === 'bundle') { s.price_unit = 'bundle'; s.bundle_components ||= []; }
        update(); render(container, program, changed);
      }, {values: choices(['steps','Sequential adjustments'],['fixed','Fixed net with adjustments'],['quote','Approved quote'],['count','Discount per qualifying unit'],['bundle','Complete bundle net'],['free','Buy X, get Y free'],['review','Manual pricing review'])}));
      if (!['fixed','bundle','quote','review'].includes(s.method)) {
        pricing.append(field('Starting price', s.starting_price || 'list', v => { s.starting_price = v; if (v !== 'rule') delete s.base_scenario_id; update(); render(container, program, changed); }, {values: choices(['list','List price'],['net','Verified net price from order'],['quote','Approved quote from order'],['rule','Another scenario'])}));
        if (s.starting_price === 'rule') pricing.append(field('Base scenario ID', s.base_scenario_id, set('base_scenario_id'), {required:true, help:'The base and every ancestor must explicitly allow this scenario ID.'}));
      }
      if (['fixed','bundle'].includes(s.method)) pricing.append(field('Fixed price ($)', s.fixed_price, set('fixed_price'), {type:'number', min:0, required:true}));
      pricing.append(field('Price unit', s.price_unit || 'each', set('price_unit'), {values: choices('each','case','section','bundle')}));
      card.append(el('h4','Price calculation'), pricing);
      if (!['bundle','free','review'].includes(s.method)) {
        s.adjustments ||= [];
        s.adjustments.forEach((a, i) => { const row = el('div', undefined, 'scenario-row'); row.append(field(`Step ${i+1}`, a.type, v => {a.type=v; update();}, {values: choices(['percentage','Discount (%)'],['dollar_amount','Deduct dollars ($)'],['multiplier','Price multiplier'],['surcharge','Add dollars ($)'])}), field('Amount', a.amount, v => {a.amount=v; update();}, {type:'number', min:0, required:true}), button('↑', () => { if (i) { [s.adjustments[i-1],s.adjustments[i]]=[s.adjustments[i],s.adjustments[i-1]]; update(); render(container,program,changed); } }), button('Remove', () => {s.adjustments.splice(i,1);update();render(container,program,changed);})); card.append(row); });
        card.append(button('+ Add adjustment', () => {s.adjustments.push({type:'percentage',amount:0});update();render(container,program,changed);}));
      }
      if (s.method === 'count') {
        s.count_scope ||= {};
        const g = grid();g.append(field('Discount per qualifying unit (%)',s.percent_per_unit,set('percent_per_unit'),{type:'number',min:0,required:true}),field('Maximum discount (%)',s.maximum_percent,set('maximum_percent'),{type:'number',min:0,required:true}));
        card.append(g,selector('Count these products',s.count_scope,update),el('p','The capped percentage applies to products in this scenario’s eligible scope. Quantities are counted across the order.', 'help'));
      }
      if (s.method === 'bundle') {
        card.append(el('p','Include every required product and accessory. Quantities must match exactly; bundles are never split between rules.', 'help'));
        s.bundle_components ||= [];
        for (const [i,c] of s.bundle_components.entries()) {const g=grid();g.append(field('Component product ID',c.product_id,v=>{c.product_id=v;update();},{required:true}),field('Exact configuration (blank for none)',c.configuration,v=>{c.configuration=v;update();}),field('Required quantity',c.quantity,v=>{c.quantity=v;update();},{type:'number',integer:true,min:1,required:true}),button('Remove component',()=>{s.bundle_components.splice(i,1);update();render(container,program,changed);}));card.append(g);}
        card.append(button('+ Add component',()=>{s.bundle_components.push({product_id:'',quantity:1});update();render(container,program,changed);}));
      }
      if (s.method === 'free') {const g=grid();g.append(field('Paid quantity',s.buy_quantity,set('buy_quantity'),{type:'number',integer:true,min:1,required:true}),field('Free quantity',s.free_quantity,set('free_quantity'),{type:'number',integer:true,min:1,required:true}),field('Maximum credit per free unit ($) · optional',s.free_value_limit,set('free_value_limit'),{type:'number',min:0}));card.append(g,el('p','Exactly paid + free units must be present. The cheapest eligible units receive the credit; repeat groups are not assumed.', 'help'));}
      const eligibility = section('Eligibility conditions');
      eligibility.append(field('Subtotal threshold basis',s.subtotal_basis || 'list',set('subtotal_basis'),{values:choices('list','net','quote')}));
      s.conditions ||= [];
      const fields = choices(['fulfillment','Fulfillment'],['channel','Order channel'],['eligible_subtotal','Eligible merchandise subtotal'],['order_subtotal','Entire merchandise subtotal'],['eligible_quantity','Eligible unit count'],['qualifying_quantity','Counted units (count method)'],['line_quantity','Units on this line'],['dealer_class','Dealer class'],['account_id','Account ID'],['order_use','Order use'],['shipment','Shipment'],['configuration','Configuration']);
      for (const [i,c] of s.conditions.entries()) {const row=el('div',undefined,'scenario-row');const choicesWithCustom=[...fields];if (c.field && !choicesWithCustom.some(([v])=>v===c.field)) choicesWithCustom.push([c.field,c.field]);row.append(field('Field',c.field,v=>{c.field=v;update();render(container,program,changed);},{values:[...choicesWithCustom,['attribute.verified','Custom order attribute'],['line_attribute.verified','Custom line attribute']]}),field('Operator',c.operator,v=>{c.operator=v;update();},{values:choices('=','!=','>=','>','<=','<')}),field('Value',c.value,v=>{c.value=v;update();},{required:true}),button('Remove',()=>{s.conditions.splice(i,1);update();render(container,program,changed);}));eligibility.append(row);
        if (c.field?.includes('attribute.')) eligibility.append(field('Attribute field (attribute.stock_verified or line_attribute.domestic)',c.field,v=>{c.field=v;update();}));
      }
      eligibility.append(button('+ Add condition',()=>{s.conditions.push({field:'fulfillment',operator:'=',value:'pickup'});update();render(container,program,changed);}));
      eligibility.append(field('Use a separate product scope for threshold counts / subtotals',Boolean(s.qualification_scope),v=>{if(v)s.qualification_scope={};else delete s.qualification_scope;update();render(container,program,changed);},{type:'checkbox'}));
      if(s.qualification_scope) eligibility.append(selector('Products counted toward eligibility',s.qualification_scope,update));
      card.append(eligibility);
      const combinations = section('Combinations, priority, and approval');combinations.open=true;const cg=grid();
      cg.append(field('Combines with',s.combination || 'included_only',v=>{s.combination=v;if(v!=='compatible')delete s.compatible_scenario_ids;update();render(container,program,changed);},{values:choices(['included_only','Only steps in this rule'],['compatible','Explicitly allowed dependent rules'],['exclusive','No other discounts or promotions'])}),field('Tier rank',s.tier || 0,set('tier'),{type:'number',integer:true,min:0}),field('Priority',s.priority || 0,set('priority'),{type:'number',integer:true,min:0}));
      if(s.combination==='compatible') cg.append(listField('Allowed dependent scenario IDs',s.compatible_scenario_ids,set('compatible_scenario_ids')));
      combinations.append(cg,field('Upfront price and combination verified',s.approved,set('approved'),{type:'checkbox'}));
      const evidence=grid();evidence.append(field('Approval evidence',s.approval_evidence,set('approval_evidence')),field('Review requirement',s.review_note,set('review_note')),field('Required promo code',s.promo_code,set('promo_code')),field('PO / cart instruction',s.instruction,set('instruction')),field('Source document / cells',s.source,set('source')));combinations.append(evidence);card.append(combinations);
      card.append(button('Remove scenario',()=>{program.scenarios.splice(index,1);update();render(container,program,changed);}));container.append(card);
    }
    container.append(button('+ Add pricing scenario',()=>{program.scenarios.push({id:crypto.randomUUID(),name:'',method:'steps',price_unit:'each',approved:false,scope:{},conditions:[],adjustments:[]});changed();render(container,program,changed);container.lastElementChild.previousElementSibling.scrollIntoView({block:'nearest'});}));
  }
  function localDate(value) { if (!value) return ''; const d=new Date(value);return new Date(d.getTime()-d.getTimezoneOffset()*60000).toISOString().slice(0,19); }
  function programPayload(p) { const keys=['vendor','vendor_code','expires_at','quote_enabled','discount_options','product_overrides','product_group_overrides','scenarios','selection_policy'];return Object.fromEntries(keys.filter(k=>p[k]!==undefined).map(k=>[k,p[k]])); }
  return {el,button,field,grid,choices,render,localDate,programPayload};
})();
