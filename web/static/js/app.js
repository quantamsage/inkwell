// Inkwell application controller.

import { api, ApiError } from './api.js';
import { Editor } from './editor.js';
import { applyFormat, setHeading } from './format.js';
import { WELCOME_TITLE, WELCOME_CONTENT } from './welcome.js';

// ---------- DOM references (resolved once; preview content may contain ids) ----------

const $ = (id) => document.getElementById(id);
const el = {
  body: document.body,
  root: document.documentElement,
  title: $('doc-title'),
  dirtyDot: $('dirty-dot'),
  saveStatus: $('save-status'),
  saveStatusText: $('save-status-text'),
  importBtn: $('import-btn'),
  importInput: $('import-input'),
  exportMenu: $('export-menu'),
  exportBtn: $('export-btn'),
  exportList: $('export-list'),
  themeToggle: $('theme-toggle'),
  shortcutsBtn: $('shortcuts-btn'),
  settingsBtn: $('settings-btn'),
  sidebarToggle: $('sidebar-toggle'),
  sidebarBackdrop: $('sidebar-backdrop'),
  newDoc: $('new-doc'),
  search: $('search'),
  sort: $('sort'),
  recentSection: $('recent-section'),
  recentList: $('recent-list'),
  docList: $('doc-list'),
  docCount: $('doc-count'),
  allTitle: $('all-title'),
  emptyHint: $('empty-hint'),
  toolbar: $('toolbar'),
  headingSelect: $('heading-select'),
  undoBtn: $('undo-btn'),
  redoBtn: $('redo-btn'),
  focusBtn: $('focus-btn'),
  focusExit: $('focus-exit'),
  panes: $('panes'),
  resizer: $('resizer'),
  editor: $('editor'),
  editorScroll: $('editor-scroll'),
  mirror: $('editor-highlight'),
  previewScroll: $('preview-scroll'),
  preview: $('preview'),
  statWords: $('stat-words'),
  statChars: $('stat-chars'),
  statReading: $('stat-reading'),
  statCursor: $('stat-cursor'),
  toasts: $('toasts'),
  dropOverlay: $('drop-overlay'),
  confirmDialog: $('confirm-dialog'),
  confirmTitle: $('confirm-title'),
  confirmMessage: $('confirm-message'),
  confirmOk: $('confirm-ok'),
  promptDialog: $('prompt-dialog'),
  promptTitle: $('prompt-title'),
  promptInput: $('prompt-input'),
  promptOk: $('prompt-ok'),
  settingsDialog: $('settings-dialog'),
  setTheme: $('set-theme'),
  setFontFamily: $('set-font-family'),
  setFontSize: $('set-font-size'),
  setLineHeight: $('set-line-height'),
  outFontSize: $('out-font-size'),
  outLineHeight: $('out-line-height'),
  settingsReset: $('settings-reset'),
  shortcutsDialog: $('shortcuts-dialog'),
};

const MAX_FILE_BYTES = Number(document.querySelector('meta[name="max-file-bytes"]')?.content) || 5 * 1024 * 1024;
const AUTOSAVE_DELAY = 1000;
const DEFAULT_PREFS = {
  theme: 'light', fontSize: 15, fontFamily: 'mono', lineHeight: 1.6,
  viewMode: 'split', splitRatio: 0.5, sortBy: 'updated-desc',
};
const FONT_STACKS = {
  mono: 'ui-monospace, "SFMono-Regular", "Cascadia Code", "JetBrains Mono", Menlo, Consolas, "Liberation Mono", monospace',
  sans: 'system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif',
  serif: '"Iowan Old Style", "Palatino Linotype", Palatino, Georgia, "Times New Roman", serif',
};

// ---------- State ----------

const state = {
  prefs: { ...DEFAULT_PREFS, ...readInitialPrefs() },
  doc: null,              // {id, title, version, createdAt, updatedAt}
  saved: { title: '', content: '' }, // what the server has for state.doc
  saveTimer: null,
  retryTimer: null,
  retryDelay: 2000,
  savePromise: null,
  saveQueued: false,
  saveError: null,
  documents: [],
  lastNonPreviewView: 'split',
  previewStale: true,
};

function readInitialPrefs() {
  try {
    return JSON.parse($('initial-prefs').textContent) || {};
  } catch {
    return {};
  }
}

const editor = new Editor({ textarea: el.editor, mirror: el.mirror, scroller: el.editorScroll });

// ---------- Utilities ----------

function debounce(fn, ms) {
  let t;
  const wrapped = (...args) => {
    clearTimeout(t);
    t = setTimeout(() => fn(...args), ms);
  };
  wrapped.cancel = () => clearTimeout(t);
  return wrapped;
}

function normalizeTitle(s) {
  // Mirrors the server: control characters become spaces, whitespace collapses.
  const t = s.replace(/[\u0000-\u001f\u007f-\u009f]/g, ' ').replace(/\s+/g, ' ').trim();
  return t || 'Untitled';
}

const relativeFormatter = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' });
const dateFormatter = new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric', year: 'numeric' });

function relativeTime(iso) {
  const date = new Date(iso);
  const diff = (date.getTime() - Date.now()) / 1000;
  const abs = Math.abs(diff);
  if (abs < 45) return 'just now';
  if (abs < 3600) return relativeFormatter.format(Math.round(diff / 60), 'minute');
  if (abs < 86400) return relativeFormatter.format(Math.round(diff / 3600), 'hour');
  if (abs < 86400 * 7) return relativeFormatter.format(Math.round(diff / 86400), 'day');
  return dateFormatter.format(date);
}

function toast(message, { error = false, timeout = 3500 } = {}) {
  const node = document.createElement('div');
  node.className = 'toast' + (error ? ' error' : '');
  node.setAttribute('role', error ? 'alert' : 'status');
  node.textContent = message;
  el.toasts.append(node);
  setTimeout(() => node.remove(), timeout);
}

function icon(name) {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('class', 'icon');
  svg.setAttribute('aria-hidden', 'true');
  const use = document.createElementNS('http://www.w3.org/2000/svg', 'use');
  use.setAttribute('href', `#i-${name}`);
  svg.append(use);
  return svg;
}

// ---------- Dialogs ----------

function confirmDialog({ title = 'Are you sure?', message, okLabel = 'OK', cancelLabel = 'Cancel', danger = false }) {
  return new Promise((resolve) => {
    el.confirmTitle.textContent = title;
    el.confirmMessage.textContent = message;
    el.confirmOk.textContent = okLabel;
    el.confirmDialog.querySelector('#confirm-cancel').textContent = cancelLabel;
    el.confirmOk.className = 'btn ' + (danger ? 'btn-danger' : 'btn-primary');
    el.confirmDialog.returnValue = '';
    el.confirmDialog.addEventListener('close', () => resolve(el.confirmDialog.returnValue === 'ok'), { once: true });
    el.confirmDialog.showModal();
    el.confirmOk.focus();
  });
}

function promptDialog({ title, value = '', okLabel = 'Save' }) {
  return new Promise((resolve) => {
    el.promptTitle.textContent = title;
    el.promptOk.textContent = okLabel;
    el.promptInput.value = value;
    el.promptDialog.returnValue = '';
    el.promptDialog.addEventListener('close', () => {
      resolve(el.promptDialog.returnValue === 'ok' ? el.promptInput.value : null);
    }, { once: true });
    el.promptDialog.showModal();
    el.promptInput.select();
  });
}

// ---------- Preferences ----------

const persistPrefs = debounce(async () => {
  try {
    await api.savePreferences(state.prefs);
  } catch (err) {
    toast(`Could not save settings: ${err.message}`, { error: true });
  }
}, 400);

function setPrefs(patch, { persist = true } = {}) {
  Object.assign(state.prefs, patch);
  applyPrefs();
  if (persist) persistPrefs();
}

function applyPrefs() {
  const p = state.prefs;
  el.root.dataset.theme = p.theme;
  const rootStyle = el.root.style;
  rootStyle.setProperty('--editor-font-size', `${p.fontSize}px`);
  rootStyle.setProperty('--editor-line-height', String(p.lineHeight));
  rootStyle.setProperty('--editor-font', FONT_STACKS[p.fontFamily] || FONT_STACKS.mono);
  el.panes.style.setProperty('--split', `${(p.splitRatio * 100).toFixed(2)}%`);
  el.resizer.setAttribute('aria-valuenow', String(Math.round(p.splitRatio * 100)));
  el.sort.value = p.sortBy;
  setView(p.viewMode, { persist: false });

  el.setTheme.value = p.theme;
  el.setFontFamily.value = p.fontFamily;
  el.setFontSize.value = String(p.fontSize);
  el.setLineHeight.value = String(p.lineHeight);
  el.outFontSize.textContent = `${p.fontSize}px`;
  el.outLineHeight.textContent = Number(p.lineHeight).toFixed(1);
  el.themeToggle.setAttribute('aria-label', p.theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme');
}

// ---------- View modes ----------

function setView(mode, { persist = true } = {}) {
  if (!['editor', 'split', 'preview'].includes(mode)) mode = 'split';
  if (mode !== 'preview') state.lastNonPreviewView = mode;
  el.body.dataset.view = mode;
  for (const b of el.toolbar.querySelectorAll('.segmented button')) {
    b.setAttribute('aria-pressed', String(b.dataset.view === mode));
  }
  if (state.prefs.viewMode !== mode) {
    state.prefs.viewMode = mode;
    if (persist) persistPrefs();
  }
  if (mode !== 'editor' && state.previewStale) renderPreview();
}

function isPreviewVisible() {
  const mode = el.body.dataset.view;
  if (el.body.classList.contains('focus-mode')) return mode === 'preview';
  if (mode === 'preview') return true;
  return mode === 'split' && window.matchMedia('(min-width: 769px)').matches;
}

function togglePreview() {
  setView(el.body.dataset.view === 'preview' ? (state.lastNonPreviewView || 'split') : 'preview');
}

function setFocusMode(on) {
  el.body.classList.toggle('focus-mode', on);
  el.focusBtn.setAttribute('aria-pressed', String(on));
  if (on) {
    closeSidebar();
    if (el.body.dataset.view !== 'preview') editor.focus();
  } else {
    if (state.previewStale && isPreviewVisible()) renderPreview();
  }
}

// ---------- Resizable split ----------

function initResizer() {
  let dragging = false;
  const update = (clientX) => {
    const rect = el.panes.getBoundingClientRect();
    const ratio = Math.min(0.8, Math.max(0.2, (clientX - rect.left) / rect.width));
    setPrefs({ splitRatio: Math.round(ratio * 1000) / 1000 }, { persist: false });
  };
  el.resizer.addEventListener('pointerdown', (e) => {
    dragging = true;
    el.resizer.setPointerCapture(e.pointerId);
    el.body.classList.add('resizing');
    e.preventDefault();
  });
  el.resizer.addEventListener('pointermove', (e) => { if (dragging) update(e.clientX); });
  const stop = () => {
    if (!dragging) return;
    dragging = false;
    el.body.classList.remove('resizing');
    persistPrefs();
  };
  el.resizer.addEventListener('pointerup', stop);
  el.resizer.addEventListener('pointercancel', stop);
  el.resizer.addEventListener('dblclick', () => setPrefs({ splitRatio: 0.5 }));
  el.resizer.addEventListener('keydown', (e) => {
    const step = e.shiftKey ? 0.1 : 0.02;
    let r = state.prefs.splitRatio;
    if (e.key === 'ArrowLeft') r -= step;
    else if (e.key === 'ArrowRight') r += step;
    else if (e.key === 'Home') r = 0.2;
    else if (e.key === 'End') r = 0.8;
    else return;
    e.preventDefault();
    setPrefs({ splitRatio: Math.round(Math.min(0.8, Math.max(0.2, r)) * 1000) / 1000 });
  });
}

// ---------- Live preview ----------

let previewController = null;
let previewSeq = 0;

async function renderPreview() {
  if (!isPreviewVisible()) {
    state.previewStale = true;
    return;
  }
  state.previewStale = false;
  const content = editor.value;
  const seq = ++previewSeq;
  previewController?.abort();
  previewController = new AbortController();
  if (!content.trim()) {
    el.preview.replaceChildren();
    const p = document.createElement('p');
    p.className = 'preview-empty';
    p.textContent = 'Nothing to preview yet.';
    el.preview.append(p);
    return;
  }
  try {
    const res = await api.render(content, previewController.signal);
    // Ignore out-of-order responses.
    if (seq !== previewSeq) return;
    // The HTML was sanitized server-side against a strict allow-list.
    el.preview.innerHTML = res.html;
  } catch (err) {
    if (err.name === 'AbortError' || seq !== previewSeq) return;
    state.previewStale = true;
    if (err instanceof ApiError && err.status === 413) {
      el.preview.textContent = 'This document is too large to preview.';
    }
  }
}

const schedulePreview = debounce(renderPreview, 120);
const scheduleLargePreview = debounce(renderPreview, 450);

function onContentForPreview() {
  if (editor.value.length > 200_000) scheduleLargePreview(); else schedulePreview();
}

function syncPreviewScroll() {
  if (el.body.dataset.view !== 'split' || !isPreviewVisible()) return;
  const src = el.editorScroll;
  const max = src.scrollHeight - src.clientHeight;
  if (max <= 0) return;
  const ratio = src.scrollTop / max;
  const dst = el.previewScroll;
  dst.scrollTop = ratio * (dst.scrollHeight - dst.clientHeight);
}

// In-document anchor links (e.g. footnotes, headings) scroll the preview.
el.preview.addEventListener('click', (e) => {
  const a = e.target.closest('a[href^="#"]');
  if (!a) return;
  e.preventDefault();
  const id = decodeURIComponent(a.getAttribute('href').slice(1));
  const target = el.preview.querySelector(`[id="${CSS.escape(id)}"]`);
  target?.scrollIntoView({ block: 'start' });
});

// ---------- Statistics ----------

const WORD_RE = /[\p{L}\p{N}]+(?:['’\-_.][\p{L}\p{N}]+)*/gu;

function updateStats() {
  const text = editor.value;
  const words = (text.match(WORD_RE) || []).length;
  let chars = 0;
  for (let i = 0; i < text.length; i++) {
    const c = text.charCodeAt(i);
    if (c < 0xdc00 || c > 0xdfff) chars++; // count code points, not UTF-16 units
  }
  const minutes = words === 0 ? 0 : Math.max(1, Math.round(words / 225));
  el.statWords.textContent = `${words.toLocaleString()} ${words === 1 ? 'word' : 'words'}`;
  el.statChars.textContent = `${chars.toLocaleString()} ${chars === 1 ? 'character' : 'characters'}`;
  el.statReading.textContent = `${minutes} min read`;
}
const scheduleStats = debounce(updateStats, 150);

editor.addEventListener('cursor', (e) => {
  const { line, column, selected } = e.detail;
  el.statCursor.textContent = `Ln ${line}, Col ${column}` + (selected ? ` (${selected} selected)` : '');
});

editor.addEventListener('history', (e) => {
  el.undoBtn.disabled = !e.detail.canUndo;
  el.redoBtn.disabled = !e.detail.canRedo;
});

// ---------- Saving ----------

function currentSnapshot() {
  return { title: normalizeTitle(el.title.value), content: editor.value };
}

function isDirty() {
  if (!state.doc) return false;
  const s = currentSnapshot();
  return s.title !== state.saved.title || s.content !== state.saved.content;
}

function setSaveStatus(kind, text) {
  el.saveStatus.dataset.state = kind;
  el.saveStatusText.textContent = text;
  el.saveStatus.title = kind === 'error' ? `${text} — click to retry` : 'Save now (Ctrl+S)';
}

function refreshSaveStatus() {
  const dirty = isDirty();
  el.dirtyDot.hidden = !dirty;
  document.title = `${dirty ? '• ' : ''}${state.doc ? normalizeTitle(el.title.value) : 'Inkwell'} — Inkwell`;
  if (state.savePromise) setSaveStatus('saving', 'Saving…');
  else if (state.saveError) setSaveStatus('error', state.saveError);
  else if (dirty) setSaveStatus('unsaved', 'Unsaved changes');
  else setSaveStatus('saved', 'Saved');
}

function scheduleSave() {
  clearTimeout(state.saveTimer);
  if (!state.doc) return;
  state.saveTimer = setTimeout(() => { saveNow(); }, AUTOSAVE_DELAY);
}

// saveNow persists the current document. Saves are serialized: while one is
// in flight, further requests are queued and coalesced into a single follow-up
// save of the latest content, and every save carries the version it is based
// on, so an older snapshot can never overwrite newer content.
function saveNow() {
  clearTimeout(state.saveTimer);
  if (state.savePromise) {
    state.saveQueued = true;
    return state.savePromise;
  }
  state.savePromise = (async () => {
    let ok = true;
    try {
      do {
        state.saveQueued = false;
        ok = await saveOnce();
      } while (ok && state.saveQueued);
    } finally {
      state.savePromise = null;
      refreshSaveStatus();
    }
    return ok;
  })();
  refreshSaveStatus();
  return state.savePromise;
}

async function saveOnce() {
  const doc = state.doc;
  if (!doc) return true;
  const snap = currentSnapshot();
  if (snap.title === state.saved.title && snap.content === state.saved.content) {
    state.saveError = null;
    return true; // nothing changed: no request, no database write
  }
  setSaveStatus('saving', 'Saving…');
  try {
    const res = await api.save(doc.id, { ...snap, version: doc.version });
    if (state.doc !== doc) return true;
    Object.assign(doc, res.document);
    state.saved = { title: res.document.title, content: snap.content };
    state.saveError = null;
    state.retryDelay = 2000;
    clearTimeout(state.retryTimer);
    if (document.activeElement !== el.title && el.title.value !== res.document.title && normalizeTitle(el.title.value) === res.document.title) {
      el.title.value = res.document.title;
    }
    scheduleListRefresh();
    return true;
  } catch (err) {
    if (state.doc !== doc) return true;
    if (err instanceof ApiError && err.status === 409 && err.body?.document) {
      return resolveConflict(doc, err.body.document);
    }
    if (err instanceof ApiError && err.status === 404) {
      state.saveError = 'Document was deleted';
      toast('This document no longer exists on the server. Copy your text or create a new document.', { error: true, timeout: 8000 });
      return false;
    }
    state.saveError = err instanceof ApiError && err.status === 413 ? 'Too large to save' : 'Save failed';
    if (err.status !== 413 && err.status !== 422) scheduleRetry();
    else toast(err.message, { error: true });
    return false;
  }
}

function scheduleRetry() {
  clearTimeout(state.retryTimer);
  state.retryTimer = setTimeout(() => saveNow(), state.retryDelay);
  state.retryDelay = Math.min(state.retryDelay * 2, 60000);
}

async function resolveConflict(doc, server) {
  const overwrite = await confirmDialog({
    title: 'Document changed elsewhere',
    message: `“${server.title}” was modified in another window or tab after you opened it. Keep your version (overwriting theirs) or load the latest saved version?`,
    okLabel: 'Keep my version',
    cancelLabel: 'Load latest',
  });
  if (state.doc !== doc) return true;
  if (overwrite) {
    doc.version = server.version;
    state.saved = { title: server.title, content: server.content };
    state.saveQueued = true; // loop saves again with the new base version
  } else {
    loadIntoEditor(server);
  }
  return true;
}

// flushBeforeLeaving saves pending edits; returns false if the user cancels.
async function flushBeforeLeaving() {
  if (!state.doc) return true;
  if (!isDirty() && !state.savePromise) return true;
  const ok = await saveNow();
  if (ok && !isDirty()) return true;
  return confirmDialog({
    title: 'Unsaved changes',
    message: 'Your latest changes could not be saved. Leave this document and discard them?',
    okLabel: 'Discard changes',
    danger: true,
  });
}

window.addEventListener('beforeunload', (e) => {
  if (isDirty() || state.savePromise) {
    saveNow();
    e.preventDefault();
    e.returnValue = '';
  }
});

document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'hidden' && isDirty()) saveNow();
});

// ---------- Documents ----------

function loadIntoEditor(d) {
  state.doc = { id: d.id, title: d.title, version: d.version, createdAt: d.createdAt, updatedAt: d.updatedAt };
  state.saved = { title: d.title, content: d.content };
  state.saveError = null;
  clearTimeout(state.saveTimer);
  clearTimeout(state.retryTimer);
  el.title.value = d.title;
  editor.setValue(d.content);
  el.previewScroll.scrollTop = 0;
  state.previewStale = true;
  renderPreview();
  updateStats();
  refreshSaveStatus();
  markActiveInLists();
  const url = new URL(window.location.href);
  url.searchParams.set('doc', d.id);
  history.replaceState(null, '', url);
}

async function openDocument(id, { skipFlush = false } = {}) {
  if (state.doc?.id === id) {
    closeSidebar();
    return;
  }
  if (!skipFlush && !(await flushBeforeLeaving())) return;
  try {
    const d = await api.get(id);
    loadIntoEditor(d);
    closeSidebar();
  } catch (err) {
    toast(`Could not open document: ${err.message}`, { error: true });
    if (err.status === 404) refreshLists();
  }
}

async function newDocument() {
  if (!(await flushBeforeLeaving())) return;
  try {
    const d = await api.create('Untitled', '');
    loadIntoEditor(d);
    await refreshLists();
    closeSidebar();
    el.title.focus();
    el.title.select();
  } catch (err) {
    toast(`Could not create document: ${err.message}`, { error: true });
  }
}

async function renameDocument(id, currentTitle) {
  const value = await promptDialog({ title: 'Rename document', value: currentTitle, okLabel: 'Rename' });
  if (value === null) return;
  const title = value.trim();
  if (!title) {
    toast('Title cannot be empty', { error: true });
    return;
  }
  if (state.doc?.id === id) {
    // The open document is renamed through the save queue so its version
    // stays consistent with in-flight autosaves.
    el.title.value = title;
    refreshSaveStatus();
    await saveNow();
    return;
  }
  try {
    await api.rename(id, title);
    await refreshLists();
  } catch (err) {
    toast(`Could not rename: ${err.message}`, { error: true });
  }
}

async function duplicateDocument(id) {
  if (state.doc?.id === id) {
    const ok = await saveNow();
    if (!ok) {
      toast('Save the document before duplicating it', { error: true });
      return;
    }
  }
  try {
    const copy = await api.duplicate(id);
    await refreshLists();
    await openDocument(copy.id);
    toast(`Created “${copy.title}”`);
  } catch (err) {
    toast(`Could not duplicate: ${err.message}`, { error: true });
  }
}

async function deleteDocument(id, title) {
  const ok = await confirmDialog({
    title: 'Delete document?',
    message: `“${title}” will be permanently deleted. This cannot be undone.`,
    okLabel: 'Delete',
    danger: true,
  });
  if (!ok) return;
  const isCurrent = state.doc?.id === id;
  if (isCurrent && state.savePromise) await state.savePromise.catch(() => {});
  try {
    await api.remove(id);
  } catch (err) {
    if (err.status !== 404) {
      toast(`Could not delete: ${err.message}`, { error: true });
      return;
    }
  }
  toast(`Deleted “${title}”`);
  if (isCurrent) {
    clearTimeout(state.saveTimer);
    clearTimeout(state.retryTimer);
    state.doc = null;
    const list = await refreshLists();
    if (list.length) await openDocument(list[0].id, { skipFlush: true });
    else await createWelcomeOrBlank(false);
  } else {
    await refreshLists();
  }
}

async function createWelcomeOrBlank(welcome) {
  const d = welcome ? await api.create(WELCOME_TITLE, WELCOME_CONTENT) : await api.create('Untitled', '');
  loadIntoEditor(d);
  await refreshLists();
}

// ---------- Sidebar lists ----------

function renderDocItem(d, { showMeta = true, showSnippet = false } = {}) {
  const li = document.createElement('li');
  li.className = 'doc-item';
  li.dataset.id = d.id;
  if (state.doc?.id === d.id) li.classList.add('active');

  const open = document.createElement('button');
  open.type = 'button';
  open.className = 'doc-open';
  open.dataset.action = 'open';
  open.title = d.title;
  const title = document.createElement('span');
  title.className = 'doc-item-title';
  title.textContent = d.title;
  open.append(title);
  if (showMeta) {
    const meta = document.createElement('span');
    meta.className = 'doc-item-meta';
    meta.textContent = `Edited ${relativeTime(d.updatedAt)}`;
    meta.title = `Created ${new Date(d.createdAt).toLocaleString()}\nModified ${new Date(d.updatedAt).toLocaleString()}`;
    open.append(meta);
  }
  if (showSnippet && d.snippet) {
    const snip = document.createElement('span');
    snip.className = 'doc-item-snippet';
    snip.textContent = d.snippet;
    open.append(snip);
  }

  const actions = document.createElement('div');
  actions.className = 'doc-actions';
  for (const [action, iconName, label] of [['rename', 'edit', 'Rename'], ['duplicate', 'copy', 'Duplicate'], ['delete', 'trash', 'Delete']]) {
    const b = document.createElement('button');
    b.type = 'button';
    b.className = 'icon-btn' + (action === 'delete' ? ' danger' : '');
    b.dataset.action = action;
    b.title = label;
    b.setAttribute('aria-label', `${label} “${d.title}”`);
    b.append(icon(iconName));
    actions.append(b);
  }
  li.append(open, actions);
  return li;
}

async function refreshLists() {
  const q = el.search.value.trim();
  try {
    const [all, recent] = await Promise.all([
      api.list(q, state.prefs.sortBy),
      q ? Promise.resolve(null) : api.recent(),
    ]);
    state.documents = all.documents;
    el.docList.replaceChildren(...all.documents.map((d) => renderDocItem(d, { showSnippet: !!q })));
    el.docCount.textContent = String(all.documents.length);
    el.allTitle.textContent = q ? 'Search results' : 'All documents';
    el.recentSection.hidden = !!q || !recent || recent.documents.length === 0;
    if (recent) el.recentList.replaceChildren(...recent.documents.map((d) => renderDocItem(d, { showMeta: false })));
    el.emptyHint.hidden = all.documents.length > 0;
    el.emptyHint.textContent = q ? `No documents match “${q}”.` : 'No documents yet.';
    return all.documents;
  } catch (err) {
    toast(`Could not load documents: ${err.message}`, { error: true });
    return state.documents;
  }
}

const scheduleListRefresh = debounce(refreshLists, 1200);

function markActiveInLists() {
  for (const li of document.querySelectorAll('.doc-item')) {
    li.classList.toggle('active', li.dataset.id === state.doc?.id);
  }
}

function onListClick(e) {
  const button = e.target.closest('button[data-action]');
  if (!button) return;
  const li = button.closest('.doc-item');
  const id = li?.dataset.id;
  if (!id) return;
  const title = li.querySelector('.doc-item-title')?.textContent ?? '';
  switch (button.dataset.action) {
    case 'open': openDocument(id); break;
    case 'rename': renameDocument(id, title); break;
    case 'duplicate': duplicateDocument(id); break;
    case 'delete': deleteDocument(id, title); break;
  }
}

function openSidebar() {
  el.body.classList.add('sidebar-open');
  el.sidebarBackdrop.hidden = false;
  el.sidebarToggle.setAttribute('aria-expanded', 'true');
}

function closeSidebar() {
  el.body.classList.remove('sidebar-open');
  el.sidebarBackdrop.hidden = true;
  el.sidebarToggle.setAttribute('aria-expanded', 'false');
}

// ---------- Import / export ----------

function validateImportFile(file) {
  if (!/\.(md|markdown)$/i.test(file.name)) return 'Only .md and .markdown files can be imported.';
  if (file.size > MAX_FILE_BYTES) return `The file is larger than ${Math.round(MAX_FILE_BYTES / 1024 / 1024)} MB.`;
  return null;
}

async function importFiles(files) {
  const list = Array.from(files);
  if (!list.length) return;
  if (!(await flushBeforeLeaving())) return;
  let last = null;
  for (const file of list) {
    const problem = validateImportFile(file);
    if (problem) {
      toast(`${file.name}: ${problem}`, { error: true });
      continue;
    }
    try {
      last = await api.importFile(file);
      toast(`Imported “${last.title}”`);
    } catch (err) {
      toast(`${file.name}: ${err.message}`, { error: true });
    }
  }
  if (last) {
    loadIntoEditor(last);
    await refreshLists();
  }
}

function downloadBlob(blob, filename) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 10000);
}

async function exportDocument(format) {
  closeExportMenu();
  if (!state.doc) return;
  const ok = await saveNow();
  if (!ok) {
    toast('Save failed — exporting the last saved version', { error: true });
  }
  const id = state.doc.id;
  if (format === 'pdf') {
    printDocument(id);
    return;
  }
  try {
    const { blob, filename } = await api.exportFile(id, format);
    downloadBlob(blob, filename || `document.${format}`);
  } catch (err) {
    toast(`Export failed: ${err.message}`, { error: true });
  }
}

// printDocument opens the print-optimized page in a hidden frame and invokes
// the browser's print dialog, where "Save as PDF" produces the PDF file.
function printDocument(id) {
  document.querySelector('iframe.print-frame')?.remove();
  const frame = document.createElement('iframe');
  frame.className = 'print-frame';
  frame.title = 'Print preview';
  frame.setAttribute('aria-hidden', 'true');
  frame.style.position = 'fixed';
  frame.style.width = '0';
  frame.style.height = '0';
  frame.style.border = '0';
  frame.style.right = '0';
  frame.style.bottom = '0';
  frame.src = `/documents/${encodeURIComponent(id)}/print`;
  frame.addEventListener('load', () => {
    try {
      frame.contentWindow.focus();
      frame.contentWindow.print();
    } catch {
      window.open(`/documents/${encodeURIComponent(id)}/print?autoprint=1`, '_blank', 'noopener');
    }
    setTimeout(() => frame.remove(), 60000);
  }, { once: true });
  document.body.append(frame);
  toast('Choose “Save as PDF” in the print dialog');
}

function openExportMenu() {
  el.exportList.hidden = false;
  el.exportBtn.setAttribute('aria-expanded', 'true');
  el.exportList.querySelector('button')?.focus();
}

function closeExportMenu() {
  el.exportList.hidden = true;
  el.exportBtn.setAttribute('aria-expanded', 'false');
}

// ---------- Formatting ----------

function format(name) {
  if (el.body.dataset.view === 'preview') setView(state.lastNonPreviewView || 'split');
  editor.transform((s) => applyFormat(name, s));
}

// ---------- Keyboard shortcuts ----------

function onGlobalKeyDown(e) {
  const mod = e.ctrlKey || e.metaKey;
  const key = e.key.toLowerCase();
  const inEditor = e.target === el.editor;

  if (e.key === 'Escape') {
    if (!el.exportList.hidden) { closeExportMenu(); el.exportBtn.focus(); return; }
    if (el.body.classList.contains('focus-mode')) { e.preventDefault(); setFocusMode(false); return; }
    if (el.body.classList.contains('sidebar-open')) { closeSidebar(); return; }
    return;
  }
  if (!mod) return;

  if (key === 's' && !e.shiftKey && !e.altKey) {
    e.preventDefault();
    saveNow().then((ok) => { if (ok && !isDirty()) setSaveStatus('saved', 'Saved'); });
    return;
  }
  if (key === 'p' && e.shiftKey && !e.altKey) {
    e.preventDefault();
    togglePreview();
    return;
  }
  if (key === 'f' && e.shiftKey && !e.altKey) {
    e.preventDefault();
    setFocusMode(!el.body.classList.contains('focus-mode'));
    return;
  }
  if (key === 'n' && !e.shiftKey) {
    e.preventDefault();
    newDocument();
    return;
  }
  if (key === '/' || e.code === 'Slash') {
    e.preventDefault();
    el.shortcutsDialog.showModal();
    return;
  }
  if (inEditor && !e.shiftKey && !e.altKey) {
    if (key === 'b') { e.preventDefault(); format('bold'); return; }
    if (key === 'i') { e.preventDefault(); format('italic'); return; }
    if (key === 'k') { e.preventDefault(); format('link'); return; }
  }
}

// ---------- Wiring ----------

function bindEvents() {
  editor.addEventListener('change', () => {
    refreshSaveStatus();
    scheduleSave();
    onContentForPreview();
    scheduleStats();
  });
  el.editorScroll.addEventListener('scroll', syncPreviewScroll, { passive: true });

  el.title.addEventListener('input', () => {
    refreshSaveStatus();
    scheduleSave();
  });
  el.title.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') {
      e.preventDefault();
      editor.focus();
    }
  });
  el.title.addEventListener('blur', () => {
    if (!el.title.value.trim() && state.doc) el.title.value = 'Untitled';
    if (isDirty()) saveNow();
  });

  el.saveStatus.addEventListener('click', () => saveNow());

  el.toolbar.addEventListener('click', (e) => {
    const b = e.target.closest('button');
    if (!b) return;
    if (b.dataset.format) format(b.dataset.format);
    else if (b.dataset.view) setView(b.dataset.view);
  });
  el.toolbar.addEventListener('mousedown', (e) => {
    // Keep the editor selection when clicking toolbar buttons.
    if (e.target.closest('button[data-format]')) e.preventDefault();
  });
  el.headingSelect.addEventListener('change', () => {
    const level = Number(el.headingSelect.value);
    el.headingSelect.value = '';
    if (el.body.dataset.view === 'preview') setView(state.lastNonPreviewView || 'split');
    editor.transform((s) => setHeading(s, level));
  });
  el.undoBtn.addEventListener('click', () => editor.undo());
  el.redoBtn.addEventListener('click', () => editor.redo());
  el.focusBtn.addEventListener('click', () => setFocusMode(true));
  el.focusExit.addEventListener('click', () => setFocusMode(false));

  el.newDoc.addEventListener('click', () => newDocument());
  el.search.addEventListener('input', debounce(refreshLists, 200));
  el.sort.addEventListener('change', () => {
    setPrefs({ sortBy: el.sort.value });
    refreshLists();
  });
  el.docList.addEventListener('click', onListClick);
  el.recentList.addEventListener('click', onListClick);
  el.sidebarToggle.addEventListener('click', () => {
    if (el.body.classList.contains('sidebar-open')) closeSidebar(); else openSidebar();
  });
  el.sidebarBackdrop.addEventListener('click', closeSidebar);

  el.importBtn.addEventListener('click', () => el.importInput.click());
  el.importInput.addEventListener('change', () => {
    importFiles(el.importInput.files);
    el.importInput.value = '';
  });
  el.exportBtn.addEventListener('click', () => {
    if (el.exportList.hidden) openExportMenu(); else closeExportMenu();
  });
  el.exportList.addEventListener('click', (e) => {
    const b = e.target.closest('button[data-export]');
    if (b) exportDocument(b.dataset.export);
  });
  el.exportList.addEventListener('keydown', (e) => {
    const items = Array.from(el.exportList.querySelectorAll('button'));
    const i = items.indexOf(document.activeElement);
    if (e.key === 'ArrowDown') { e.preventDefault(); items[(i + 1) % items.length].focus(); }
    if (e.key === 'ArrowUp') { e.preventDefault(); items[(i - 1 + items.length) % items.length].focus(); }
  });
  document.addEventListener('click', (e) => {
    if (!el.exportMenu.contains(e.target)) closeExportMenu();
  });

  el.themeToggle.addEventListener('click', () => setPrefs({ theme: state.prefs.theme === 'dark' ? 'light' : 'dark' }));
  el.shortcutsBtn.addEventListener('click', () => el.shortcutsDialog.showModal());
  el.settingsBtn.addEventListener('click', () => el.settingsDialog.showModal());
  el.setTheme.addEventListener('change', () => setPrefs({ theme: el.setTheme.value }));
  el.setFontFamily.addEventListener('change', () => setPrefs({ fontFamily: el.setFontFamily.value }));
  el.setFontSize.addEventListener('input', () => setPrefs({ fontSize: Number(el.setFontSize.value) }));
  el.setLineHeight.addEventListener('input', () => setPrefs({ lineHeight: Math.round(Number(el.setLineHeight.value) * 10) / 10 }));
  el.settingsReset.addEventListener('click', () => {
    const { viewMode, splitRatio, sortBy } = state.prefs;
    setPrefs({ ...DEFAULT_PREFS, viewMode, splitRatio, sortBy });
  });

  document.addEventListener('keydown', onGlobalKeyDown);

  // Drag and drop import.
  let dragDepth = 0;
  const hasFiles = (e) => Array.from(e.dataTransfer?.types || []).includes('Files');
  window.addEventListener('dragenter', (e) => {
    if (!hasFiles(e)) return;
    dragDepth++;
    el.dropOverlay.hidden = false;
  });
  window.addEventListener('dragleave', (e) => {
    if (!hasFiles(e)) return;
    dragDepth = Math.max(0, dragDepth - 1);
    if (!dragDepth) el.dropOverlay.hidden = true;
  });
  window.addEventListener('dragover', (e) => {
    if (hasFiles(e)) e.preventDefault();
  });
  window.addEventListener('drop', (e) => {
    if (!hasFiles(e)) return;
    e.preventDefault();
    dragDepth = 0;
    el.dropOverlay.hidden = true;
    importFiles(e.dataTransfer.files);
  });

  window.matchMedia('(min-width: 769px)').addEventListener('change', () => {
    if (state.previewStale && isPreviewVisible()) renderPreview();
  });

  // Keep relative timestamps fresh.
  setInterval(() => { if (!document.hidden) refreshLists(); }, 60000);
}

// ---------- Startup ----------

async function init() {
  applyPrefs();
  initResizer();
  bindEvents();
  updateStats();

  const list = await refreshLists();
  const requested = new URL(window.location.href).searchParams.get('doc');
  if (requested && /^[0-9a-f]{32}$/.test(requested)) {
    try {
      loadIntoEditor(await api.get(requested));
      return;
    } catch {
      toast('That document could not be found', { error: true });
    }
  }
  if (list.length) {
    const recent = list.slice().sort((a, b) => new Date(b.updatedAt) - new Date(a.updatedAt))[0];
    await openDocument(recent.id, { skipFlush: true });
  } else {
    try {
      await createWelcomeOrBlank(true);
    } catch (err) {
      toast(`Could not create a document: ${err.message}`, { error: true });
    }
  }
}

init();
