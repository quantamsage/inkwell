// Editor: a <textarea> layered over a syntax-highlighted mirror.
//
// The textarea keeps native text input, selection, IME and accessibility;
// its text is transparent and the highlighted <pre> beneath shows through.
// The textarea never scrolls itself — it grows with the mirror and the
// outer container scrolls — so the two layers always line up.
//
// Undo/redo is implemented here because programmatic edits (formatting,
// Tab, list continuation) would otherwise break the browser's native stack.

import { highlight } from './highlight.js';
import { indent, outdent, newline } from './format.js';

const MAX_HISTORY = 300;
const MAX_HISTORY_CHARS = 30_000_000;
const COALESCE_MS = 900;

class History {
  constructor() { this.reset({ value: '', start: 0, end: 0 }); }

  reset(state) {
    this.stack = [state];
    this.index = 0;
    this.lastKind = '';
    this.lastTime = 0;
  }

  record(state, kind) {
    const now = Date.now();
    const top = this.stack[this.index];
    if (top && top.value === state.value) {
      top.start = state.start;
      top.end = state.end;
      return;
    }
    const canMerge = kind !== 'other' && kind === this.lastKind &&
      now - this.lastTime < COALESCE_MS && this.index > 0 &&
      this.index === this.stack.length - 1;
    if (canMerge) {
      this.stack[this.index] = state;
    } else {
      this.stack.length = this.index + 1;
      this.stack.push(state);
      this.index = this.stack.length - 1;
      this.trim();
    }
    this.lastKind = kind;
    this.lastTime = now;
  }

  trim() {
    let total = this.stack.reduce((n, s) => n + s.value.length, 0);
    while (this.stack.length > 2 && (this.stack.length > MAX_HISTORY || total > MAX_HISTORY_CHARS)) {
      total -= this.stack.shift().value.length;
      this.index--;
    }
  }

  // breakGroup forces the next record to start a new undo step.
  breakGroup() { this.lastKind = ''; }

  canUndo() { return this.index > 0; }
  canRedo() { return this.index < this.stack.length - 1; }

  undo() {
    if (!this.canUndo()) return null;
    this.index--;
    this.lastKind = '';
    return this.stack[this.index];
  }

  redo() {
    if (!this.canRedo()) return null;
    this.index++;
    this.lastKind = '';
    return this.stack[this.index];
  }
}

export class Editor extends EventTarget {
  constructor({ textarea, mirror, scroller }) {
    super();
    this.textarea = textarea;
    this.mirror = mirror;
    this.scroller = scroller;
    this.history = new History();
    this.activeLine = -1;
    this.pendingKind = 'other';

    textarea.addEventListener('beforeinput', (e) => this.onBeforeInput(e));
    textarea.addEventListener('input', () => this.onInput());
    textarea.addEventListener('keydown', (e) => this.onKeyDown(e));
    textarea.addEventListener('scroll', () => {
      // The textarea must never scroll internally (the container scrolls).
      if (textarea.scrollTop || textarea.scrollLeft) {
        textarea.scrollTop = 0;
        textarea.scrollLeft = 0;
      }
    });
    const selectionChanged = () => {
      if (document.activeElement === textarea) this.onSelectionChange();
    };
    document.addEventListener('selectionchange', selectionChanged);
    textarea.addEventListener('focus', () => this.onSelectionChange());
    textarea.addEventListener('blur', () => this.history.breakGroup());

    this.render();
  }

  get value() { return this.textarea.value; }

  getState() {
    const t = this.textarea;
    return { value: t.value, start: t.selectionStart, end: t.selectionEnd };
  }

  // setValue loads new content (e.g. when opening a document).
  setValue(text) {
    this.textarea.value = text;
    this.textarea.setSelectionRange(0, 0);
    this.history.reset(this.getState());
    this.render();
    this.scroller.scrollTop = 0;
    this.emitHistory();
    this.onSelectionChange();
  }

  focus() { this.textarea.focus({ preventScroll: true }); }

  // apply performs an edit {from, to, insert, selStart, selEnd} as one undo step.
  apply(edit) {
    if (!edit) return;
    const t = this.textarea;
    this.history.breakGroup();
    t.focus({ preventScroll: true });
    t.setRangeText(edit.insert, edit.from, edit.to, 'end');
    t.setSelectionRange(edit.selStart, edit.selEnd);
    this.history.record(this.getState(), 'other');
    this.history.breakGroup();
    this.afterChange();
  }

  // transform runs a formatting command against the current state.
  transform(fn) {
    this.apply(fn(this.getState()));
  }

  undo() { this.restore(this.history.undo()); }
  redo() { this.restore(this.history.redo()); }

  restore(state) {
    if (!state) return;
    const t = this.textarea;
    t.value = state.value;
    t.focus({ preventScroll: true });
    t.setSelectionRange(Math.min(state.start, state.value.length), Math.min(state.end, state.value.length));
    this.afterChange();
  }

  onBeforeInput(e) {
    switch (e.inputType) {
      case 'historyUndo':
        e.preventDefault();
        this.undo();
        return;
      case 'historyRedo':
        e.preventDefault();
        this.redo();
        return;
      case 'insertText':
      case 'insertCompositionText':
        this.pendingKind = e.data && /\s/.test(e.data) ? 'space' : 'type';
        return;
      default:
        this.pendingKind = e.inputType && e.inputType.startsWith('delete') && !e.inputType.includes('ByCut') ? 'delete' : 'other';
    }
  }

  onInput() {
    // Typing a space after a word closes the current undo group so that
    // undo removes text word by word.
    const kind = this.pendingKind === 'space' ? 'type' : this.pendingKind;
    if (this.pendingKind === 'space') this.history.breakGroup();
    this.history.record(this.getState(), kind);
    if (this.pendingKind === 'space') this.history.breakGroup();
    this.pendingKind = 'other';
    this.afterChange();
  }

  onKeyDown(e) {
    const mod = e.ctrlKey || e.metaKey;
    if (e.isComposing) return;

    if (mod && !e.altKey) {
      const key = e.key.toLowerCase();
      if (key === 'z') {
        e.preventDefault();
        if (e.shiftKey) this.redo(); else this.undo();
        return;
      }
      if (key === 'y' && !e.shiftKey) {
        e.preventDefault();
        this.redo();
        return;
      }
    }

    if (e.key === 'Tab' && !mod && !e.altKey) {
      e.preventDefault();
      this.transform(e.shiftKey ? outdent : indent);
      return;
    }

    if (e.key === 'Enter' && !mod && !e.altKey) {
      e.preventDefault();
      this.transform((s) => newline(s, { plain: e.shiftKey }));
      return;
    }

    if (e.key === 'Escape' && !document.body.classList.contains('focus-mode')) {
      // Let keyboard users leave the editor (Tab is captured for indentation).
      this.textarea.blur();
    }
  }

  afterChange() {
    this.render();
    this.ensureCaretVisible();
    this.emitHistory();
    this.onSelectionChange();
    this.dispatchEvent(new Event('change'));
  }

  emitHistory() {
    this.dispatchEvent(new CustomEvent('history', {
      detail: { canUndo: this.history.canUndo(), canRedo: this.history.canRedo() },
    }));
  }

  render() {
    this.mirror.innerHTML = highlight(this.textarea.value);
    this.activeLine = -1;
    this.markActiveLine();
  }

  caretLineIndex() {
    const { value, selectionStart } = this.textarea;
    let n = 0;
    let i = -1;
    while ((i = value.indexOf('\n', i + 1)) !== -1 && i < selectionStart) n++;
    return n;
  }

  markActiveLine() {
    const idx = this.caretLineIndex();
    if (idx === this.activeLine) return;
    const lines = this.mirror.children;
    if (this.activeLine >= 0 && lines[this.activeLine]) lines[this.activeLine].classList.remove('active');
    if (lines[idx]) lines[idx].classList.add('active');
    this.activeLine = idx;
  }

  onSelectionChange() {
    this.markActiveLine();
    const { value, selectionStart, selectionEnd } = this.textarea;
    const lineStart = value.lastIndexOf('\n', selectionStart - 1) + 1;
    this.dispatchEvent(new CustomEvent('cursor', {
      detail: {
        line: this.activeLine + 1,
        column: selectionStart - lineStart + 1,
        selected: selectionEnd - selectionStart,
      },
    }));
  }

  // caretRect measures the caret position using the mirror's text nodes.
  caretRect() {
    const idx = this.caretLineIndex();
    const lineEl = this.mirror.children[idx];
    if (!lineEl) return null;
    const { value, selectionStart } = this.textarea;
    let col = selectionStart - (value.lastIndexOf('\n', selectionStart - 1) + 1);
    const walker = document.createTreeWalker(lineEl, NodeFilter.SHOW_TEXT);
    let node;
    while ((node = walker.nextNode())) {
      if (col <= node.length) {
        const range = document.createRange();
        range.setStart(node, col);
        range.setEnd(node, col);
        const rects = range.getClientRects();
        if (rects.length) return rects[rects.length - 1];
        break;
      }
      col -= node.length;
    }
    return lineEl.getBoundingClientRect();
  }

  ensureCaretVisible() {
    const rect = this.caretRect();
    if (!rect) return;
    const box = this.scroller.getBoundingClientRect();
    const margin = Math.min(80, box.height / 4);
    if (rect.bottom > box.bottom - margin) {
      this.scroller.scrollTop += rect.bottom - (box.bottom - margin);
    } else if (rect.top < box.top + margin / 2) {
      this.scroller.scrollTop -= (box.top + margin / 2) - rect.top;
    }
  }
}
