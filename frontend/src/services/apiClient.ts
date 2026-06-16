const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? '';
const TOKEN_KEY = 'n2a_admin_token';

export function getAdminToken() {
  return localStorage.getItem(TOKEN_KEY) ?? '';
}

export function setAdminToken(token: string) {
  if (token) {
    localStorage.setItem(TOKEN_KEY, token);
  } else {
    localStorage.removeItem(TOKEN_KEY);
  }
}

function redirectToLoginOnUnauthorized(path: string) {
  if (path === '/admin/login' || window.location.pathname === '/admin/login') {
    return;
  }
  setAdminToken('');
  const currentPath = `${window.location.pathname}${window.location.search}${window.location.hash}`;
  window.location.replace(`/admin/login?redirect=${encodeURIComponent(currentPath)}`);
}

export async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (!headers.has('Content-Type') && init.body) {
    headers.set('Content-Type', 'application/json');
  }
  const token = getAdminToken();
  if (token) {
    headers.set('X-Admin-Token', token);
  }
  const response = await fetch(`${API_BASE_URL}${path}`, {
    ...init,
    headers
  });
  const contentType = response.headers.get('content-type') ?? '';
  const payload = contentType.includes('application/json') ? await response.json() : await response.text();
  if (!response.ok) {
    if (response.status === 401) {
      redirectToLoginOnUnauthorized(path);
    }
    const detail = typeof payload === 'object' && payload !== null && 'detail' in payload ? String(payload.detail) : response.statusText;
    throw new Error(detail || '请求失败');
  }
  return payload as T;
}

export function jsonBody(value: unknown) {
  return JSON.stringify(value);
}
