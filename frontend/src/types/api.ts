export interface AdminUser {
  id: number;
  username: string;
  role: string;
  enabled: boolean;
  last_login_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface ApiKey {
  id: number;
  name: string;
  key_prefix: string;
  key_suffix: string;
  enabled: boolean;
  owner_admin_id?: number | null;
  expires_at?: string | null;
  last_used_at?: string | null;
  token_quota_total: number;
  token_quota_daily: number;
  total_requests: number;
  total_tokens: number;
  created_at: string;
  updated_at: string;
}

export interface CreatedApiKey extends ApiKey {
  secret: string;
}

export interface ModelRow {
  id: number;
  public_id: string;
  name: string;
  notion_model: string;
  family: string;
  group: string;
  beta: boolean;
  enabled: boolean;
  aliases: string;
}

export interface UsageRow {
  id: number;
  api_key_id: number;
  date: string;
  model: string;
  request_count: number;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  estimated_tokens: number;
}

export interface RequestLog {
  id: number;
  api_key_id?: number | null;
  method: string;
  route: string;
  status_code: number;
  latency_ms: number;
  model: string;
  prompt_tokens: number;
  completion_tokens: number;
  total_tokens: number;
  estimated: boolean;
  error_summary: string;
  created_at: string;
}

export interface Account {
  id: number;
  email: string;
  enabled: boolean;
  active: boolean;
  user_id: string;
  user_name: string;
  space_id: string;
  space_view_id: string;
  space_name: string;
  client_version: string;
  status: string;
  priority: number;
  max_concurrency: number;
  proxy_mode: string;
  proxy_url: string;
  proxy_http_url: string;
  proxy_https_url: string;
  probe_json: string;
  has_auth_token: boolean;
  has_cookie: boolean;
  last_error: string;
  last_login_at?: string | null;
  last_used_at?: string | null;
  last_success_at?: string | null;
  last_refresh_at?: string | null;
  consecutive_failures: number;
  total_successes: number;
  total_failures: number;
  created_at: string;
  updated_at: string;
}

export interface AccountPayload {
  email: string;
  enabled?: boolean;
  active?: boolean;
  user_id?: string;
  user_name?: string;
  space_id?: string;
  space_view_id?: string;
  space_name?: string;
  client_version?: string;
  status?: string;
  priority?: number;
  max_concurrency?: number;
  proxy_mode?: string;
  proxy_url?: string;
  proxy_http_url?: string;
  proxy_https_url?: string;
  auth_token?: string;
  cookie?: string;
  probe_json?: string;
  clear_auth_token?: boolean;
  clear_cookie?: boolean;
}

export interface ListResponse<T> {
  data: T[];
}

export interface LoginResponse {
  token: string;
  expires_at: string;
  admin: AdminUser;
}

export interface HealthResponse {
  ok: boolean;
  service: string;
  database_driver: string;
}
