import type {
  AdminUser,
  Account,
  AccountSpace,
  AccountTestResponse,
  AccountLoginResponse,
  AccountLoginStatus,
  AccountPayload,
  ApiKey,
  CreatedApiKey,
  HealthResponse,
  ListResponse,
  LoginResponse,
  ManualImportAccountPayload,
  ModelRow,
  NotionConversationDetail,
  NotionConversationSummary,
  RequestLog,
  UsageRow
} from '../types/api';
import { jsonBody, request, setAdminToken } from './apiClient';

export const adminApi = {
  async login(username: string, password: string) {
    const response = await request<LoginResponse>('/admin/login', {
      method: 'POST',
      body: jsonBody({ username, password })
    });
    setAdminToken(response.token);
    return response;
  },

  verify() {
    return request<{ admin: AdminUser }>('/admin/verify');
  },

  async logout() {
    try {
      return await request<{ ok: boolean }>('/admin/logout', { method: 'POST' });
    } finally {
      setAdminToken('');
    }
  },

  health() {
    return request<HealthResponse>('/healthz');
  },

  admins() {
    return request<ListResponse<AdminUser>>('/admin/admins');
  },

  createAdmin(payload: { username: string; password: string; role?: string; enabled?: boolean }) {
    return request<AdminUser>('/admin/admins', { method: 'POST', body: jsonBody(payload) });
  },

  updateAdmin(id: number, payload: { password?: string; role?: string; enabled?: boolean }) {
    return request<AdminUser>(`/admin/admins/${id}`, { method: 'PUT', body: jsonBody(payload) });
  },

  deleteAdmin(id: number) {
    return request<{ ok: boolean }>(`/admin/admins/${id}`, { method: 'DELETE' });
  },

  apiKeys() {
    return request<ListResponse<ApiKey>>('/admin/api-keys');
  },

  createApiKey(payload: { name: string; enabled?: boolean; token_quota_total?: number; token_quota_daily?: number; expires_at?: string }) {
    return request<CreatedApiKey>('/admin/api-keys', { method: 'POST', body: jsonBody(payload) });
  },

  updateApiKey(id: number, payload: { name?: string; token_quota_total?: number; token_quota_daily?: number; expires_at?: string | null }) {
    return request<ApiKey>(`/admin/api-keys/${id}`, { method: 'PUT', body: jsonBody(payload) });
  },

  enableApiKey(id: number) {
    return request<ApiKey>(`/admin/api-keys/${id}/enable`, { method: 'POST' });
  },

  disableApiKey(id: number) {
    return request<ApiKey>(`/admin/api-keys/${id}/disable`, { method: 'POST' });
  },

  deleteApiKey(id: number) {
    return request<{ ok: boolean }>(`/admin/api-keys/${id}`, { method: 'DELETE' });
  },

  models() {
    return request<ListResponse<ModelRow>>('/admin/models');
  },

  usage(query = '') {
    return request<ListResponse<UsageRow>>(`/admin/usage${query}`);
  },

  apiKeyUsage(id: number) {
    return request<ListResponse<UsageRow>>(`/admin/api-keys/${id}/usage`);
  },

  logs() {
    return request<ListResponse<RequestLog>>('/admin/logs');
  },

  accounts() {
    return request<ListResponse<Account>>('/admin/accounts?include_disabled=true');
  },

  createAccount(payload: AccountPayload) {
    return request<Account>('/admin/accounts', { method: 'POST', body: jsonBody(payload) });
  },

  manualImportAccount(payload: ManualImportAccountPayload) {
    return request<AccountLoginResponse>('/admin/accounts/manual', { method: 'POST', body: jsonBody(payload) });
  },

  startAccountLogin(email: string) {
    return request<AccountLoginResponse>('/admin/accounts/login/start', { method: 'POST', body: jsonBody({ email }) });
  },

  verifyAccountLogin(email: string, code: string) {
    return request<AccountLoginResponse>('/admin/accounts/login/verify', { method: 'POST', body: jsonBody({ email, code }) });
  },

  accountLoginStatus(email?: string) {
    const query = email ? `?email=${encodeURIComponent(email)}` : '';
    return request<ListResponse<AccountLoginStatus>>(`/admin/accounts/login/status${query}`);
  },

  accountSpaces(id: number) {
    return request<ListResponse<AccountSpace>>(`/admin/accounts/${id}/spaces`);
  },

  updateAccount(id: number, payload: AccountPayload) {
    return request<Account>(`/admin/accounts/${id}`, { method: 'PUT', body: jsonBody(payload) });
  },

  activateAccount(id: number) {
    return request<Account>(`/admin/accounts/${id}/activate`, { method: 'POST' });
  },

  refreshAccount(id: number) {
    return request<Account>(`/admin/accounts/${id}/refresh`, { method: 'POST' });
  },

  deleteAccount(id: number) {
    return request<{ ok: boolean }>(`/admin/accounts/${id}`, { method: 'DELETE' });
  },

  testAccount(id: number, payload: { prompt?: string; model?: string }) {
    return request<AccountTestResponse>(`/admin/accounts/${id}/test`, {
      method: 'POST',
      body: jsonBody(payload)
    });
  },

  notionConversations(params: { account_id: number; space_id: string; space_view_id?: string; space_name?: string; limit?: number }) {
    return request<ListResponse<NotionConversationSummary>>(`/admin/notion-conversations${queryString(params)}`);
  },

  notionConversation(threadID: string, params: { account_id: number; space_id: string; space_view_id?: string; space_name?: string }) {
    return request<NotionConversationDetail>(`/admin/notion-conversations/${encodeURIComponent(threadID)}${queryString(params)}`);
  },

  deleteNotionConversation(threadID: string, params: { account_id: number; space_id: string; space_view_id?: string; space_name?: string }) {
    return request<{ ok: boolean }>(`/admin/notion-conversations/${encodeURIComponent(threadID)}${queryString(params)}`, { method: 'DELETE' });
  }
};

function queryString(params: Record<string, string | number | undefined>) {
  const query = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== '') {
      query.set(key, String(value));
    }
  });
  const text = query.toString();
  return text ? `?${text}` : '';
}
