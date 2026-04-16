// API client for ShiguangHub operator dashboard.
// All requests add Authorization header from stored JWT.
// Includes: auto-retry on transient errors, 429 backoff, network detection.

const BASE = '/api/v1';

let token = localStorage.getItem('hub_token') || '';

// Network status tracking — components can subscribe
let _online = navigator.onLine;
const _listeners = new Set();

window.addEventListener('online', () => { _online = true; _notify() });
window.addEventListener('offline', () => { _online = false; _notify() });

function _notify() { _listeners.forEach(fn => fn(_online)) }

export function onNetworkChange(fn) {
  _listeners.add(fn);
  return () => _listeners.delete(fn);
}
export function isOnline() { return _online }

export function setToken(t) {
  token = t;
  localStorage.setItem('hub_token', t);
}

export function getToken() {
  return token;
}

export function clearToken() {
  token = '';
  localStorage.removeItem('hub_token');
}

// Transient error detection: network failures or 5xx server errors
function isTransient(err, status) {
  if (!status) return true; // network error (no response)
  return status >= 500 && status < 600;
}

async function request(method, path, body, { retries = 2, retryDelay = 800 } = {}) {
  const opts = {
    method,
    headers: { 'Content-Type': 'application/json' },
  };
  if (token) {
    opts.headers['Authorization'] = `Bearer ${token}`;
  }
  if (body !== undefined) {
    opts.body = JSON.stringify(body);
  }

  let lastErr;
  for (let attempt = 0; attempt <= retries; attempt++) {
    try {
      const resp = await fetch(BASE + path, opts);

      // Rate limited — respect Retry-After or back off
      if (resp.status === 429) {
        const retryAfter = parseInt(resp.headers.get('Retry-After') || '5', 10);
        const err = new Error('Too many requests. Please wait a moment and try again.');
        err.status = 429;
        err.retryAfter = retryAfter;
        throw err;
      }

      // Handle empty responses (204, etc.)
      const text = await resp.text();
      let data = null;
      if (text) {
        try { data = JSON.parse(text); } catch { data = null; }
      }

      if (!resp.ok) {
        const msg = data?.message || data?.error || `Request failed (HTTP ${resp.status})`;
        const err = new Error(msg);
        err.status = resp.status;
        // Retry on transient server errors for idempotent requests
        if (isTransient(null, resp.status) && method === 'GET' && attempt < retries) {
          lastErr = err;
          await new Promise(r => setTimeout(r, retryDelay * (attempt + 1)));
          continue;
        }
        throw err;
      }
      return data;
    } catch (err) {
      // Network error (fetch failed) — retry if possible
      if (!err.status && attempt < retries) {
        lastErr = err;
        await new Promise(r => setTimeout(r, retryDelay * (attempt + 1)));
        continue;
      }
      throw err;
    }
  }
  throw lastErr;
}

// Public endpoints
export const login = (email, password) =>
  request('POST', '/onboard/login', { email, password });

export const register = (name, slug, email, password) =>
  request('POST', '/onboard/register', { name, slug, email, password });

// Protected operator endpoints
export const getMe = () => request('GET', '/operator/me');
export const getLines = () => request('GET', '/operator/me/lines');
export const createLine = (line) => request('POST', '/operator/me/lines', line);
export const updateLine = (id, line) => request('PUT', `/operator/me/lines/${id}`, line);
export const deleteLine = (id) => request('DELETE', `/operator/me/lines/${id}`);
export const getTheme = () => request('GET', '/operator/me/theme').catch(() => null);
export const updateTheme = (theme) => request('PUT', '/operator/me/theme', theme);
export const getCodes = () => request('GET', '/operator/me/codes');
export const createCode = (code) => request('POST', '/operator/me/codes', { code });
export const deleteCode = (code) => request('DELETE', `/operator/me/codes/${code}`);
export const getAgents = () => request('GET', '/operator/me/agents');
export const getStats = (days = 7) => request('GET', `/operator/me/stats?days=${days}`);
export const rotateAgentKey = (agentId) => request('POST', `/operator/me/agents/${agentId}/rotate-key`);
export const changePassword = (oldPassword, newPassword) =>
  request('PUT', '/operator/me/password', { old_password: oldPassword, new_password: newPassword });
