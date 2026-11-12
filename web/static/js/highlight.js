// Markdown source highlighter for the editor overlay.
//
// Produces one <div class="hl-line"> per source line so that line numbers
// (CSS counters) stay aligned with soft-wrapped lines. Tokens only change
// colors, never font metrics, so the overlay wraps exactly like the textarea.

export function escapeHTML(s) {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

const span = (cls, html) => `<span class="${cls}">${html}</span>`;

const INLINE_RE = new RegExp(
  [
    String.raw`(?<escape>\\[\\\x60*_{}\[\]()#+\-.!|~<>])`,
    String.raw`(?<code>(?<tk>\x60+)(?:(?!\k<tk>).)+?\k<tk>)`,
    String.raw`(?<link>!?\[[^\]\n]*\]\([^)\n]*\))`,
    String.raw`(?<ref>!?\[[^\]\n]+\]\[[^\]\n]*\])`,
    String.raw`(?<bold>(?<bm>\*\*|__)(?=\S)(?:(?!\k<bm>).)*?\S\k<bm>)`,
    String.raw`(?<strike>~~(?=\S)(?:(?!~~).)*?\S~~)`,
    String.raw`(?<em>(?<![\w*])(?<em1>\*|_)(?=[^\s*_])(?:(?!\k<em1>).)*?[^\s\\]\k<em1>(?![\w*]))`,
    String.raw`(?<url>(?:https?:\/\/|www\.)[^\s<>()\]]+)`,
    String.raw`(?<html><\/?[A-Za-z][A-Za-z0-9-]*(?:\s[^<>]*)?\/?>|<!--.*?-->)`,
  ].join('|'),
  'g',
);

const LINK_PARTS = /^(!?\[)([^\]]*)(\]\()([^)]*)(\))$/;

// highlightInline returns HTML for a fragment of inline Markdown.
export function highlightInline(text, depth = 0) {
  if (!text) return '';
  let out = '';
  let last = 0;
  INLINE_RE.lastIndex = 0;
  const matches = Array.from(text.matchAll(INLINE_RE));
  for (const m of matches) {
    const g = m.groups;
    out += escapeHTML(text.slice(last, m.index));
    last = m.index + m[0].length;
    if (g.escape) {
      out += span('md-escape', escapeHTML(m[0]));
    } else if (g.code) {
      const tk = g.tk.length;
      out += span('md-code', span('md-mark', escapeHTML(g.tk)) + escapeHTML(m[0].slice(tk, -tk)) + span('md-mark', escapeHTML(g.tk)));
    } else if (g.link) {
      const p = LINK_PARTS.exec(m[0]);
      if (p) {
        out += span('md-link',
          span('md-mark', escapeHTML(p[1])) + escapeHTML(p[2]) + span('md-mark', escapeHTML(p[3])) +
          span('md-url', escapeHTML(p[4])) + span('md-mark', escapeHTML(p[5])));
      } else {
        out += span('md-link', escapeHTML(m[0]));
      }
    } else if (g.ref) {
      out += span('md-link', escapeHTML(m[0]));
    } else if (g.bold) {
      out += wrapMarked('md-strong', m[0], g.bm.length, depth);
    } else if (g.strike) {
      out += wrapMarked('md-strike', m[0], 2, depth);
    } else if (g.em) {
      out += wrapMarked('md-em', m[0], 1, depth);
    } else if (g.url) {
      out += span('md-url', escapeHTML(m[0]));
    } else if (g.html) {
      out += span('md-html', escapeHTML(m[0]));
    } else {
      out += escapeHTML(m[0]);
    }
  }
  return out + escapeHTML(text.slice(last));
}

function wrapMarked(cls, s, n, depth) {
  const inner = s.slice(n, -n);
  const body = depth < 3 ? highlightInline(inner, depth + 1) : escapeHTML(inner);
  const mark = span('md-mark', escapeHTML(s.slice(0, n)));
  return span(cls, mark + body + span('md-mark', escapeHTML(s.slice(-n))));
}

const FENCE_RE = /^(\s{0,3})(`{3,}|~{3,})(.*)$/;
const HEADING_RE = /^(\s{0,3})(#{1,6})(?=\s|$)(.*)$/;
const HR_RE = /^\s{0,3}([-*_])(?:[ \t]*\1){2,}[ \t]*$/;
const QUOTE_RE = /^(\s*(?:>[ \t]?)+)(.*)$/;
const LIST_RE = /^(\s*)([-*+]|\d{1,9}[.)])([ \t]+)(\[[ xX]\](?=[ \t]))?(.*)$/;
const TABLE_SEP_RE = /^\s*\|?\s*:?-{1,}:?\s*(\|\s*:?-{1,}:?\s*)+\|?\s*$/;
const FOOTNOTE_DEF_RE = /^(\[\^[^\]]+\]:)(.*)$/;

function highlightTableRow(line) {
  // Split on unescaped pipes, highlighting the pipes and cell content.
  let out = '';
  let cell = '';
  for (let i = 0; i < line.length; i++) {
    const c = line[i];
    if (c === '\\' && i + 1 < line.length) {
      cell += c + line[i + 1];
      i++;
    } else if (c === '|') {
      out += highlightInline(cell) + span('md-table', '|');
      cell = '';
    } else {
      cell += c;
    }
  }
  return out + highlightInline(cell);
}

function highlightBlockLine(line) {
  let m;
  if ((m = HEADING_RE.exec(line))) {
    return span('md-heading', escapeHTML(m[1]) + span('md-mark', m[2]) + highlightInline(m[3]));
  }
  if (HR_RE.test(line)) {
    return span('md-hr', escapeHTML(line));
  }
  if (TABLE_SEP_RE.test(line) && line.includes('|')) {
    return span('md-table', escapeHTML(line));
  }
  if ((m = QUOTE_RE.exec(line))) {
    return span('md-quote', span('md-mark', escapeHTML(m[1])) + highlightBlockLine(m[2]));
  }
  if ((m = LIST_RE.exec(line))) {
    let html = escapeHTML(m[1]) + span('md-list', escapeHTML(m[2])) + m[3];
    if (m[4]) html += span('md-task', escapeHTML(m[4]));
    return html + highlightInline(m[5]);
  }
  if ((m = FOOTNOTE_DEF_RE.exec(line))) {
    return span('md-link', escapeHTML(m[1])) + highlightInline(m[2]);
  }
  if (/^\s*\|/.test(line) || (/\|/.test(line) && /\|\s*$/.test(line))) {
    return highlightTableRow(line);
  }
  return highlightInline(line);
}

// highlight converts a whole document into overlay HTML.
export function highlight(text) {
  const lines = text.split('\n');
  const out = new Array(lines.length);
  let fence = null; // {char, len}
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    let cls = 'hl-line';
    let html;
    const fm = FENCE_RE.exec(line);
    if (fence) {
      cls += ' md-fenced';
      if (fm && fm[2][0] === fence.char && fm[2].length >= fence.len && fm[3].trim() === '') {
        fence = null;
        html = span('md-fence', escapeHTML(line));
      } else {
        html = span('md-codeblock', escapeHTML(line));
      }
    } else if (fm && !(fm[2][0] === '`' && fm[3].includes('`'))) {
      fence = { char: fm[2][0], len: fm[2].length };
      cls += ' md-fenced';
      html = span('md-fence', escapeHTML(fm[1] + fm[2])) + span('md-lang', escapeHTML(fm[3]));
    } else {
      html = highlightBlockLine(line);
    }
    // Empty lines need content to keep their height.
    // (An empty <span> would collapse the line to zero height.)
    out[i] = `<div class="${cls}">${line === '' ? '<br>' : html}</div>`;
  }
  return out.join('');
}
