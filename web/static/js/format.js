// Markdown formatting commands.
//
// Every command is a pure function taking the editor state
// {value, start, end} and returning an edit:
//   {from, to, insert, selStart, selEnd}
// meaning "replace value[from:to] with insert, then select selStart..selEnd"
// (selection offsets refer to the new text).

const LIST_MARKER = /^(\s*)(?:[-*+][ \t]+\[[ xX]\][ \t]+|[-*+][ \t]+|\d{1,9}[.)][ \t]+)/;

// lineRange returns the start of the first and the end of the last line
// touched by the selection.
export function lineRange(value, start, end) {
  const from = value.lastIndexOf('\n', start - 1) + 1;
  // A selection ending right after a newline does not include the next line.
  let effEnd = end;
  if (end > start && value[end - 1] === '\n') effEnd = end - 1;
  let to = value.indexOf('\n', effEnd);
  if (to === -1) to = value.length;
  return [from, Math.max(from, to)];
}

// wrapInline toggles inline markers such as ** or ` around the selection.
export function wrapInline(state, before, after = before, placeholder = 'text') {
  const { value, start, end } = state;
  const sel = value.slice(start, end);

  // Selection includes the markers: unwrap.
  if (sel.length >= before.length + after.length && sel.startsWith(before) && sel.endsWith(after)) {
    const inner = sel.slice(before.length, sel.length - after.length);
    if (!(before === '*' && inner.startsWith('*') && !inner.startsWith('**'))) {
      return { from: start, to: end, insert: inner, selStart: start, selEnd: start + inner.length };
    }
  }

  // Markers surround the selection: unwrap. Avoid treating **bold** as *italic*.
  const pre = value.slice(start - before.length, start);
  const post = value.slice(end, end + after.length);
  const isBoldAroundItalic = before === '*' &&
    value.slice(start - 2, start) === '**' && value.slice(end, end + 2) === '**' &&
    value.slice(start - 3, start) !== '***';
  if (pre === before && post === after && !isBoldAroundItalic) {
    const from = start - before.length;
    return { from, to: end + after.length, insert: sel, selStart: from, selEnd: from + sel.length };
  }

  if (!sel) {
    const insert = before + placeholder + after;
    return { from: start, to: end, insert, selStart: start + before.length, selEnd: start + before.length + placeholder.length };
  }

  // Keep surrounding whitespace outside the markers ("word " -> "**word** ").
  const lead = sel.length - sel.trimStart().length;
  const trail = sel.length - sel.trimEnd().length;
  const inner = sel.slice(lead, sel.length - trail);
  if (!inner) {
    const insert = before + placeholder + after;
    return { from: end, to: end, insert, selStart: end + before.length, selEnd: end + before.length + placeholder.length };
  }
  const from = start + lead;
  return {
    from,
    to: end - trail,
    insert: before + inner + after,
    selStart: from + before.length,
    selEnd: from + before.length + inner.length,
  };
}

// setHeading sets (or toggles off) a heading level on the selected lines.
// Level 0 turns headings back into paragraphs.
export function setHeading(state, level) {
  const { value, start, end } = state;
  const [from, to] = lineRange(value, start, end);
  const lines = value.slice(from, to).split('\n');
  const re = /^(\s{0,3})(#{1,6})(?:[ \t]+|$)/;
  const allSame = level > 0 && lines.every((l) => {
    const m = re.exec(l);
    return m && m[2].length === level;
  });
  const out = lines.map((l) => {
    const stripped = l.replace(re, '$1');
    if (allSame || level === 0) return stripped;
    if (!stripped.trim() && lines.length > 1) return l;
    const indent = stripped.match(/^\s*/)[0];
    return indent + '#'.repeat(level) + ' ' + stripped.slice(indent.length);
  });
  return lineEdit(state, from, to, out.join('\n'));
}

// lineEdit replaces whole lines, keeping a collapsed caret in place
// relative to the line's text, or selecting the changed block.
function lineEdit(state, from, to, insert) {
  const { start, end } = state;
  const oldLen = to - from;
  if (start === end && !insert.includes('\n')) {
    const delta = insert.length - oldLen;
    const caret = Math.min(from + insert.length, Math.max(from, start + delta));
    return { from, to, insert, selStart: caret, selEnd: caret };
  }
  return { from, to, insert, selStart: from, selEnd: from + insert.length };
}

const PREFIX_TESTS = {
  quote: /^(\s*)>[ \t]?/,
  ul: /^(\s*)[-*+][ \t]+(?!\[[ xX]\][ \t])/,
  ol: /^(\s*)\d{1,9}[.)][ \t]+/,
  task: /^(\s*)[-*+][ \t]+\[[ xX]\][ \t]+/,
};

// toggleLinePrefix adds or removes block prefixes (quote, ul, ol, task) on
// every selected line. Switching between list types replaces the marker.
export function toggleLinePrefix(state, kind) {
  const { value, start, end } = state;
  const [from, to] = lineRange(value, start, end);
  const lines = value.slice(from, to).split('\n');
  const test = PREFIX_TESTS[kind];
  const content = lines.filter((l) => l.trim());
  const allHave = content.length > 0 && content.every((l) => test.test(l));

  let n = 0;
  const out = lines.map((l) => {
    if (!l.trim() && lines.length > 1) return l;
    if (allHave) return l.replace(test, '$1');
    if (kind === 'quote') return '> ' + l;
    const stripped = l.replace(LIST_MARKER, '$1');
    const indent = stripped.match(/^\s*/)[0];
    const rest = stripped.slice(indent.length);
    n++;
    const marker = kind === 'ul' ? '- ' : kind === 'ol' ? `${n}. ` : '- [ ] ';
    return indent + marker + rest;
  });
  return lineEdit(state, from, to, out.join('\n'));
}

// insertBlock inserts text as its own block, separated by blank lines.
// selOffset/selLength select a part of the inserted text.
export function insertBlock(state, text, selOffset = text.length, selLength = 0, replaceFrom, replaceTo) {
  const { value } = state;
  const from = replaceFrom ?? state.start;
  const to = replaceTo ?? state.end;
  const before = value.slice(0, from);
  const after = value.slice(to);
  let prefix = '';
  if (before.length > 0 && !before.endsWith('\n\n')) prefix = before.endsWith('\n') ? '\n' : '\n\n';
  let suffix = '';
  if (after.length === 0) suffix = '\n';
  else if (!after.startsWith('\n\n')) suffix = after.startsWith('\n') ? '\n' : '\n\n';
  const insert = prefix + text + suffix;
  const selStart = from + prefix.length + selOffset;
  return { from, to, insert, selStart, selEnd: selStart + selLength };
}

export function codeBlock(state) {
  const { value, start, end } = state;
  if (start === end) {
    return insertBlock(state, '```\n\n```', 4, 0);
  }
  const [from, to] = lineRange(value, start, end);
  const body = value.slice(from, to);
  const fenced = /^```[^\n]*\n[\s\S]*\n```$/.test(body);
  if (fenced) {
    const inner = body.replace(/^```[^\n]*\n/, '').replace(/\n```$/, '');
    return { from, to, insert: inner, selStart: from, selEnd: from + inner.length };
  }
  const fence = body.includes('```') ? '~~~' : '```';
  const text = `${fence}\n${body}\n${fence}`;
  return insertBlock(state, text, fence.length + 1, body.length, from, to);
}

const URL_LIKE = /^(?:https?:\/\/|mailto:|www\.)\S+$/i;

export function link(state) {
  const { value, start, end } = state;
  const sel = value.slice(start, end);
  if (sel && URL_LIKE.test(sel.trim())) {
    const url = sel.trim();
    const insert = `[link text](${url})`;
    return { from: start, to: end, insert, selStart: start + 1, selEnd: start + 10 };
  }
  if (sel && !sel.includes('\n')) {
    const insert = `[${sel}](https://)`;
    const urlStart = start + sel.length + 3;
    return { from: start, to: end, insert, selStart: urlStart, selEnd: urlStart + 8 };
  }
  const insert = '[link text](https://)';
  return { from: start, to: end, insert, selStart: start + 1, selEnd: start + 10 };
}

export function image(state) {
  const { value, start, end } = state;
  const sel = value.slice(start, end);
  if (sel && URL_LIKE.test(sel.trim())) {
    const insert = `![alt text](${sel.trim()})`;
    return { from: start, to: end, insert, selStart: start + 2, selEnd: start + 10 };
  }
  const alt = sel && !sel.includes('\n') ? sel : 'alt text';
  const insert = `![${alt}](https://)`;
  if (sel) {
    const urlStart = start + alt.length + 4;
    return { from: start, to: end, insert, selStart: urlStart, selEnd: urlStart + 8 };
  }
  return { from: start, to: end, insert, selStart: start + 2, selEnd: start + 2 + alt.length };
}

export function table(state) {
  const text = [
    '| Column 1 | Column 2 | Column 3 |',
    '| -------- | -------- | -------- |',
    '| Cell     | Cell     | Cell     |',
    '| Cell     | Cell     | Cell     |',
  ].join('\n');
  return insertBlock(state, text, 2, 8);
}

export function horizontalRule(state) {
  return insertBlock(state, '---', 3, 0);
}

// applyFormat maps toolbar command names to edits.
export function applyFormat(name, state) {
  switch (name) {
    case 'bold': return wrapInline(state, '**', '**', 'bold text');
    case 'italic': return wrapInline(state, '*', '*', 'italic text');
    case 'strike': return wrapInline(state, '~~', '~~', 'strikethrough');
    case 'code': return wrapInline(state, '`', '`', 'code');
    case 'codeblock': return codeBlock(state);
    case 'quote': return toggleLinePrefix(state, 'quote');
    case 'ul': return toggleLinePrefix(state, 'ul');
    case 'ol': return toggleLinePrefix(state, 'ol');
    case 'task': return toggleLinePrefix(state, 'task');
    case 'link': return link(state);
    case 'image': return image(state);
    case 'table': return table(state);
    case 'hr': return horizontalRule(state);
    default: return null;
  }
}

// ---- Indentation and list continuation ----

const LIST_LINE = /^(\s*)([-*+]|\d{1,9}[.)])([ \t]+)(\[[ xX]\][ \t]+)?/;
const INDENT = '    ';

function listIndentUnit(value, lineStart) {
  // Indent a list item to the content column of the previous item at the
  // same level, so nesting works for both "- " and "10. " markers.
  const line = value.slice(lineStart, value.indexOf('\n', lineStart) === -1 ? value.length : value.indexOf('\n', lineStart));
  const m = LIST_LINE.exec(line);
  if (!m) return INDENT;
  const indent = m[1].length;
  let pos = lineStart - 1;
  while (pos > 0) {
    const prevStart = value.lastIndexOf('\n', pos - 1) + 1;
    const prev = value.slice(prevStart, pos);
    const pm = LIST_LINE.exec(prev);
    if (pm && pm[1].length === indent) return ' '.repeat(pm[2].length + pm[3].length);
    if (pm && pm[1].length < indent) break;
    if (!prev.trim()) break;
    pos = prevStart - 1;
  }
  return ' '.repeat(Math.max(2, m[2].length + m[3].length));
}

// indent handles Tab: indents selected lines, list items, or inserts spaces.
export function indent(state) {
  const { value, start, end } = state;
  const [from, to] = lineRange(value, start, end);
  const multi = value.slice(start, end).includes('\n');
  const line = value.slice(from, to);
  const isList = LIST_LINE.test(line);
  if (!multi && !isList) {
    const col = start - from;
    const spaces = 4 - (col % 4);
    const insert = ' '.repeat(spaces);
    return { from: start, to: end, insert, selStart: start + spaces, selEnd: start + spaces };
  }
  const lines = line.split('\n');
  let offset = from;
  const units = lines.map((l) => {
    const u = LIST_LINE.test(l) ? listIndentUnit(value, offset) : INDENT;
    offset += l.length + 1;
    return u;
  });
  const out = lines.map((l, i) => (l.trim() ? units[i] + l : l)).join('\n');
  if (!multi) {
    const delta = units[0].length;
    return { from, to, insert: out, selStart: start + delta, selEnd: end + delta };
  }
  return { from, to, insert: out, selStart: from, selEnd: from + out.length };
}

// outdent handles Shift+Tab.
export function outdent(state) {
  const { value, start, end } = state;
  const [from, to] = lineRange(value, start, end);
  const lines = value.slice(from, to).split('\n');
  let firstRemoved = 0;
  const out = lines.map((l, i) => {
    let n = 0;
    if (l.startsWith('\t')) n = 1;
    else {
      const lead = l.match(/^ */)[0].length;
      const m = LIST_LINE.exec(l);
      n = m ? Math.min(lead, Math.max(2, lead % 4 || 4)) : Math.min(lead, 4);
      if (m && lead > 0) {
        // Snap list items back to the previous shallower item's indentation.
        let pos = from - 1;
        let target = 0;
        while (pos > 0) {
          const ps = value.lastIndexOf('\n', pos - 1) + 1;
          const pm = LIST_LINE.exec(value.slice(ps, pos));
          if (pm && pm[1].length < lead) { target = pm[1].length; break; }
          if (!value.slice(ps, pos).trim()) break;
          pos = ps - 1;
        }
        n = lead - target;
      }
    }
    if (i === 0) firstRemoved = n;
    return l.slice(n);
  });
  const insert = out.join('\n');
  if (start === end) {
    const caret = Math.max(from, start - firstRemoved);
    return { from, to, insert, selStart: caret, selEnd: caret };
  }
  return { from, to, insert, selStart: from, selEnd: from + insert.length };
}

// newline handles Enter: keeps indentation and continues lists and quotes.
// An empty list item or quote line ends the list instead.
export function newline(state, { plain = false } = {}) {
  const { value, start, end } = state;
  const lineStart = value.lastIndexOf('\n', start - 1) + 1;
  const before = value.slice(lineStart, start);
  let lineEnd = value.indexOf('\n', end);
  if (lineEnd === -1) lineEnd = value.length;
  const restOfLine = value.slice(end, lineEnd);

  const indentStr = before.match(/^[ \t]*/)[0];
  if (plain) {
    const insert = '\n' + indentStr;
    return { from: start, to: end, insert, selStart: start + insert.length, selEnd: start + insert.length };
  }

  // Inside a fenced code block only keep indentation.
  const fences = (value.slice(0, lineStart).match(/^\s{0,3}(```|~~~)/gm) || []).length;
  if (fences % 2 === 1) {
    const insert = '\n' + indentStr;
    return { from: start, to: end, insert, selStart: start + insert.length, selEnd: start + insert.length };
  }

  const lm = LIST_LINE.exec(before);
  if (lm) {
    const markerEnd = lm[0].length;
    const itemText = before.slice(markerEnd) + restOfLine;
    if (!itemText.trim()) {
      // Empty item: remove the marker and end the list.
      return { from: lineStart, to: lineEnd, insert: lm[1], selStart: lineStart + lm[1].length, selEnd: lineStart + lm[1].length };
    }
    let marker = lm[2];
    const num = /^(\d+)([.)])$/.exec(marker);
    if (num) marker = String(Number(num[1]) + 1) + num[2];
    const task = lm[4] ? '[ ] ' : '';
    const insert = '\n' + lm[1] + marker + lm[3] + task;
    return { from: start, to: end, insert, selStart: start + insert.length, selEnd: start + insert.length };
  }

  const qm = /^(\s*(?:>[ \t]?)+)/.exec(before);
  if (qm) {
    const text = before.slice(qm[0].length) + restOfLine;
    if (!text.trim()) {
      return { from: lineStart, to: lineEnd, insert: '', selStart: lineStart, selEnd: lineStart };
    }
    const prefix = qm[1].endsWith(' ') || qm[1].endsWith('\t') ? qm[1] : qm[1] + ' ';
    const insert = '\n' + prefix;
    return { from: start, to: end, insert, selStart: start + insert.length, selEnd: start + insert.length };
  }

  const insert = '\n' + indentStr;
  return { from: start, to: end, insert, selStart: start + insert.length, selEnd: start + insert.length };
}
