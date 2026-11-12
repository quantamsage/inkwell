// Thin wrapper around fetch for the JSON API. Adds the CSRF token to every
// state-changing request and normalizes errors.

const csrfToken = document.querySelector('meta[name="csrf-token"]')?.content ?? '';

export class ApiError extends Error {
  constructor(status, body) {
    super(body?.error || (status ? `Request failed (${status})` : 'Network error — check your connection'));
    this.status = status;
    this.body = body;
  }
}

export async function request(method, url, { json, body, signal } = {}) {
  const headers = { Accept: 'application/json' };
  if (method !== 'GET' && method !== 'HEAD') headers['X-CSRF-Token'] = csrfToken;
  if (json !== undefined) {
    headers['Content-Type'] = 'application/json';
    body = JSON.stringify(json);
  }
  let res;
  try {
    res = await fetch(url, { method, headers, body, signal, credentials: 'same-origin', cache: 'no-store' });
  } catch (err) {
    if (err.name === 'AbortError') throw err;
    throw new ApiError(0, null);
  }
  if (res.status === 204) return null;
  let data = null;
  if ((res.headers.get('Content-Type') || '').includes('application/json')) {
    data = await res.json().catch(() => null);
  }
  if (!res.ok) throw new ApiError(res.status, data);
  return data;
}

const doc = (id) => `/api/documents/${encodeURIComponent(id)}`;

export const api = {
  list: (q, sort) => request('GET', `/api/documents?${new URLSearchParams({ q, sort })}`),
  recent: () => request('GET', '/api/documents/recent'),
  get: (id) => request('GET', doc(id)),
  create: (title, content) => request('POST', '/api/documents', { json: { title, content } }),
  save: (id, { title, content, version }) => request('PUT', doc(id), { json: { title, content, version } }),
  rename: (id, title) => request('PATCH', doc(id), { json: { title } }),
  remove: (id) => request('DELETE', doc(id)),
  duplicate: (id) => request('POST', `${doc(id)}/duplicate`),
  importFile(file) {
    const form = new FormData();
    form.append('file', file, file.name);
    return request('POST', '/api/documents/import', { body: form });
  },
  render: (content, signal) => request('POST', '/api/render', { json: { content }, signal }),
  savePreferences: (prefs) => request('PUT', '/api/preferences', { json: prefs }),

  // exportFile downloads an export as a Blob along with its file name.
  async exportFile(id, format) {
    let res;
    try {
      res = await fetch(`${doc(id)}/export?format=${encodeURIComponent(format)}`, { credentials: 'same-origin', cache: 'no-store' });
    } catch {
      throw new ApiError(0, null);
    }
    if (!res.ok) {
      const data = await res.json().catch(() => null);
      throw new ApiError(res.status, data);
    }
    const blob = await res.blob();
    return { blob, filename: filenameFromDisposition(res.headers.get('Content-Disposition')) };
  },
};

function filenameFromDisposition(header) {
  if (!header) return null;
  const star = /filename\*=(?:UTF-8|utf-8)''([^;]+)/.exec(header);
  if (star) {
    try { return decodeURIComponent(star[1]); } catch { /* fall through */ }
  }
  const plain = /filename="?([^";]+)"?/.exec(header);
  return plain ? plain[1] : null;
}
