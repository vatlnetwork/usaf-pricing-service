'use strict';

const $ = (id) => document.getElementById(id);
const state = { programs: [], offset: 0, hasMore: false, selected: null, draft: null, dirty: false, busy: false };
const pageSize = 100;
let nextID = 0;

function node(tag, className, text) {
  const element = document.createElement(tag);
  if (className) element.className = className;
  if (text !== undefined) element.textContent = text;
  return element;
}
function button(text, action, className = '') {
  const element = node('button', className, text);
  element.type = 'button';
  element.addEventListener('click', action);
  return element;
}
function field(title, value, change, { type = 'text', required = false, min, max } = {}) {
  const label = node('label', '', title);
  const input = document.createElement('input');
  input.id = `field-${++nextID}`;
  input.type = type;
  input.required = required;
  if (type === 'checkbox') { label.className = 'check'; input.checked = Boolean(value); }
  else input.value = value ?? '';
  if (type === 'number') input.step = 'any';
  if (min !== undefined) input.min = min;
  if (max !== undefined) input.max = max;
  input.addEventListener('input', () => {
    input.setCustomValidity('');
    change(type === 'checkbox' ? input.checked : type === 'number' ? input.valueAsNumber : input.value);
    markDirty();
  });
  label.append(input);
  return { label, input };
}
function markDirty() {
  state.dirty = true;
  $('save-state').textContent = 'Unsaved changes';
}
function notice(message = '', error = false) {
  $('notice').textContent = message;
  $('notice').hidden = !message;
  $('notice').classList.toggle('error', error);
}
async function request(path, method = 'GET', body) {
  const response = await fetch(path, {
    method, headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (response.status === 204) return null;
  const data = await response.json().catch(() => null);
  if (!response.ok) throw new Error(data?.error || `Request failed (${response.status}). Please retry.`);
  if (data === null) {
    // Older stores can represent an empty program list as null.
    if (path.startsWith('/vendor-programs?')) return [];
    throw new Error('The server returned an empty response. Please reload and retry.');
  }
  return data;
}
async function run(action) {
  if (state.busy) return;
  state.busy = true;
  $('editor-fields').disabled = true;
  const controls = ['save', 'discard', 'delete-program', 'new-program', 'refresh', 'load-more'];
  controls.forEach((id) => $(id).disabled = true);
  $('editor').setAttribute('aria-busy', 'true');
  try { await action(); }
  catch (error) { notice(error.message, true); $('notice').scrollIntoView({ block: 'nearest' }); }
  finally {
    state.busy = false;
    $('editor-fields').disabled = false;
    controls.forEach((id) => $(id).disabled = false);
    $('editor').setAttribute('aria-busy', 'false');
  }
}
function confirmAction(title, message, accept) {
  $('confirm-title').textContent = title;
  $('confirm-message').textContent = message;
  $('confirm-accept').textContent = accept;
  const dialog = $('confirm-dialog');
  dialog.returnValue = '';
  dialog.showModal();
  $('confirm-cancel').focus();
  return new Promise((resolve) => dialog.addEventListener('close', () => resolve(dialog.returnValue === 'accept'), { once: true }));
}
$('confirm-cancel').onclick = () => $('confirm-dialog').close('cancel');
$('confirm-accept').onclick = () => $('confirm-dialog').close('accept');
const mayDiscard = () => !state.dirty || confirmAction('Discard unsaved changes?', 'Your changes have not been saved. Discard them to continue?', 'Discard changes');
function expired(program) { return program.expires_at && new Date(program.expires_at) <= new Date(); }
function renderList() {
  const query = $('search').value.toLowerCase();
  const matches = state.programs.filter((program) => `${program.vendor} ${program.id}`.toLowerCase().includes(query));
  $('program-list').replaceChildren();
  for (const program of matches) {
    const item = button('', () => run(async () => {
      if (!await mayDiscard()) return;
      const loaded = await request(`/vendor-programs/${encodeURIComponent(program.id)}`);
      openProgram(loaded);
      notice();
    }), 'program-item');
    item.append(node('span', '', program.vendor), node('small', '', `${expired(program) ? 'Expired' : 'Active'} · ${(program.scenarios || []).length} scenarios · ${(program.discount_options || []).length} vendor options`));
    item.setAttribute('aria-current', String(state.selected?.id === program.id));
    $('program-list').append(item);
  }
  $('list-status').textContent = `${state.programs.length} loaded${query ? ` · ${matches.length} matching` : ''}`;
  if (!matches.length) $('program-list').append(node('p', 'empty', query ? 'No loaded programs match your search.' : 'No programs yet. Create your first program above.'));
  $('load-more').hidden = !state.hasMore;
}
async function loadPrograms(reset = false) {
  const offset = reset ? 0 : state.offset;
  const programs = await request(`/vendor-programs?limit=${pageSize}&offset=${offset}`);
  state.programs = reset ? programs : [...state.programs, ...programs.filter((p) => !state.programs.some((existing) => existing.id === p.id))];
  state.offset = offset + programs.length;
  state.hasMore = programs.length === pageSize;
  renderList();
}
function optionsEditor(container, options) {
  container.replaceChildren();
  if (!options.length) container.append(node('p', 'empty', 'No discount options in this scope.'));
  options.forEach((option, index) => {
    const card = node('div', 'option-card');
    const heading = node('div', 'option-heading');
    heading.append(field('Option name', option.name, (value) => option.name = value, { required: true }).label,
      button('Remove option', () => { options.splice(index, 1); markDirty(); optionsEditor(container, options); }, 'remove'));
    card.append(heading, node('p', 'path-heading', 'DISCOUNT STEPS · applied from top to bottom'));
    const path = node('div');
    const renderPath = () => {
      path.replaceChildren();
      if (!option.discount_path.length) path.append(node('p', 'help', 'No steps. This option applies no discount.'));
      option.discount_path.forEach((step, stepIndex) => {
        const row = node('div', 'path-row');
        const typeLabel = node('label', '', 'Discount type');
        const select = document.createElement('select');
        for (const [value, text] of [['percentage', 'Percentage (%)'], ['dollar_amount', 'Dollar amount ($)']]) {
          const choice = node('option', '', text); choice.value = value; select.append(choice);
        }
        select.value = step.type;
        const amount = field('Amount', step.amount, (value) => step.amount = value, { type: 'number', required: true, min: 0, max: step.type === 'percentage' ? 100 : undefined });
        select.addEventListener('change', () => {
          step.type = select.value;
          if (step.type === 'percentage') amount.input.max = 100;
          else amount.input.removeAttribute('max');
          markDirty();
        });
        typeLabel.append(select);
        row.append(node('span', 'step-number', `${stepIndex + 1}.`), typeLabel, amount.label,
          button('Remove step', () => { option.discount_path.splice(stepIndex, 1); markDirty(); renderPath(); }, 'remove'));
        path.append(row);
      });
    };
    renderPath();
    card.append(path, button('+ Add step', () => { option.discount_path.push({ type: 'percentage', amount: 0 }); markDirty(); renderPath(); path.querySelector('.path-row:last-child select').focus(); }, 'small-button'));
    container.append(card);
  });
  container.append(button('+ Add discount option', () => {
    options.push({ name: '', discount_path: [] }); markDirty(); optionsEditor(container, options);
    container.querySelector('.option-card:last-of-type input').focus();
  }, 'small-button'));
}
function renderOverrides(kind) {
  const product = kind === 'products';
  const overrides = product ? state.draft.product_overrides : state.draft.product_group_overrides;
  const container = $(kind);
  container.replaceChildren();
  if (!overrides.length) container.append(node('p', 'empty', product ? 'No product overrides. Vendor and group discounts apply.' : 'No group overrides. Vendor discounts apply.'));
  overrides.forEach((override, index) => {
    const card = node('div', 'override-card');
    const heading = node('div', 'option-heading');
    const key = product ? 'product_id' : 'group_name';
    heading.append(field(product ? 'Product ID' : 'Group name', override[key], (value) => override[key] = value, { required: true }).label,
      button(product ? 'Remove product' : 'Remove group', () => { overrides.splice(index, 1); markDirty(); renderOverrides(kind); }, 'remove'));
    card.append(heading, field(product ? 'Allow quote pricing for this product' : 'Allow quote pricing for this group', override.quote_enabled, (value) => override.quote_enabled = value, { type: 'checkbox' }).label);
    const options = node('fieldset');
    if (product) {
      const note = node('p', 'net-note', 'Fixed net price is active. All discount options are ignored. Set the price to 0 to use discounts again.');
      const updateNet = () => { const active = override.net_price > 0; options.disabled = active; note.hidden = !active; };
      card.append(field('Fixed net price ($) · 0 means no fixed price', override.net_price || 0, (value) => { override.net_price = value; updateNet(); }, { type: 'number', required: true, min: 0 }).label, note);
      updateNet();
    }
    optionsEditor(options, override.discount_options);
    card.append(options);
    container.append(card);
  });
}
function localDate(iso) {
  if (!iso) return '';
  const date = new Date(iso);
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, -1);
}
function openProgram(program) {
  state.selected = program ? structuredClone(program) : null;
  state.draft = program ? structuredClone(program) : { vendor: '', quote_enabled: false, expires_at: null, discount_options: [], product_overrides: [], product_group_overrides: [] };
  const draft = state.draft;
  draft.discount_options ||= [];
  draft.product_overrides ||= [];
  draft.product_group_overrides ||= [];
  for (const scope of [draft, ...draft.product_overrides, ...draft.product_group_overrides]) {
    scope.discount_options ||= [];
    scope.discount_options.forEach((option) => option.discount_path ||= []);
  }
  state.dirty = false;
  $('welcome').hidden = true;
  $('editor').hidden = false;
  $('editor-title').textContent = program ? program.vendor : 'New vendor program';
  $('editor-label').textContent = program ? 'EDIT PROGRAM' : 'CREATE PROGRAM';
  $('metadata').textContent = program ? `ID ${program.id} · Updated ${new Date(program.updated_at).toLocaleString()}` : 'Set up shared discounts, then add more specific overrides.';
  $('save-state').textContent = program ? 'Saved' : 'Draft';
  $('vendor').value = draft.vendor;
  $('expiry').value = localDate(draft.expires_at);
  $('vendor-quote').checked = draft.quote_enabled;
  $('expiry-status').textContent = expired(draft) ? 'Expired' : draft.expires_at ? 'Expiry scheduled' : 'No expiry';
  $('save').textContent = program ? 'Save changes' : 'Create program';
  $('delete-program').hidden = !program;
  optionsEditor($('vendor-options'), draft.discount_options);
  renderOverrides('groups');
  renderOverrides('products');
  PricingForms.render($('scenarios'), draft, markDirty);
  renderList();
}
function validateDraft() {
  const draft = state.draft;
  const unique = (values, label) => {
    if (values.some((value) => !value.trim())) throw new Error(`${label} cannot be blank.`);
    if (new Set(values).size !== values.length) throw new Error(`${label} must be unique within their scope.`);
  };
  if (!draft.vendor.trim()) throw new Error('Vendor name cannot be blank.');
  unique(draft.product_overrides.map((p) => p.product_id), 'Product IDs');
  unique(draft.product_group_overrides.map((g) => g.group_name), 'Group names');
  for (const scope of [draft, ...draft.product_group_overrides, ...draft.product_overrides]) {
    if (scope.net_price > 0) continue;
    unique(scope.discount_options.map((option) => option.name), 'Option names');
  }
}
$('vendor').addEventListener('input', () => { state.draft.vendor = $('vendor').value; markDirty(); });
$('vendor-quote').addEventListener('change', () => { state.draft.quote_enabled = $('vendor-quote').checked; markDirty(); });
$('expiry').addEventListener('input', () => {
  state.draft.expires_at = $('expiry').value ? new Date($('expiry').value).toISOString() : null;
  $('expiry-status').textContent = expired(state.draft) ? 'Expired' : state.draft.expires_at ? 'Expiry scheduled' : 'No expiry';
  markDirty();
});
$('search').addEventListener('input', renderList);
$('refresh').onclick = () => run(async () => { await loadPrograms(true); notice('Program list refreshed.'); });
$('load-more').onclick = () => run(() => loadPrograms());
$('new-program').onclick = () => run(async () => { if (await mayDiscard()) { openProgram(null); notice(); $('vendor').focus(); } });
$('discard').onclick = () => run(async () => {
  if (!await mayDiscard()) return;
  const program = state.selected ? await request(`/vendor-programs/${encodeURIComponent(state.selected.id)}`) : null;
  openProgram(program); notice();
});
for (const kind of ['groups', 'products']) {
  $(kind === 'groups' ? 'add-group' : 'add-product').onclick = () => {
    const product = kind === 'products';
    (product ? state.draft.product_overrides : state.draft.product_group_overrides).push(product
      ? { product_id: '', quote_enabled: false, net_price: 0, discount_options: [] }
      : { group_name: '', quote_enabled: false, discount_options: [] });
    markDirty(); renderOverrides(kind);
    $(kind).querySelector('.override-card:last-child input').focus();
  };
}
$('editor').addEventListener('submit', (event) => {
  event.preventDefault();
  run(async () => {
    validateDraft();
    const payload = PricingForms.programPayload(state.draft);
    const creating = !state.selected;
    if (!creating) payload.expected_updated_at = state.selected.updated_at;
    const saved = await request(creating ? '/vendor-programs' : `/vendor-programs/${encodeURIComponent(state.selected.id)}`, creating ? 'POST' : 'PUT', payload);
    const index = state.programs.findIndex((p) => p.id === saved.id);
    if (index < 0) state.programs.unshift(saved); else state.programs[index] = saved;
    openProgram(saved);
    notice(creating ? 'Vendor program created.' : 'All program changes saved.');
    $('notice').scrollIntoView({ block: 'nearest' });
  });
});
$('delete-program').onclick = () => run(async () => {
  if (!await confirmAction('Delete vendor program?', `Delete “${state.selected.vendor}” and all of its discount options and overrides? This cannot be undone.`, 'Delete program')) return;
  await request(`/vendor-programs/${encodeURIComponent(state.selected.id)}`, 'DELETE');
  state.programs = state.programs.filter((p) => p.id !== state.selected.id);
  state.offset = Math.max(0, state.offset - 1);
  state.selected = null; state.draft = null; state.dirty = false;
  $('editor').hidden = true; $('welcome').hidden = false;
  renderList(); notice('Vendor program deleted.');
});
window.addEventListener('beforeunload', (event) => { if (state.dirty) { event.preventDefault(); event.returnValue = ''; } });
$('list-status').textContent = 'Loading programs…';
run(() => loadPrograms(true));

$('test-draft').onclick = () => {
  try { sessionStorage.setItem('usaf-pricing-draft', JSON.stringify(PricingForms.programPayload(state.draft))); window.open('/test?draft=1', '_blank'); }
  catch (error) { notice('Could not open draft preview: ' + error.message, true); }
};

  document.getElementById('editor').addEventListener('invalid', event => {
    for (let parent = event.target.parentElement; parent; parent = parent.parentElement) {
      if (parent.tagName === 'DETAILS') parent.open = true;
    }
  }, true);
