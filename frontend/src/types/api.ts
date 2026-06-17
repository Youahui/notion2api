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
  api_key: string;
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
  api_key_name?: string;
  api_key: string;
  api_key_masked?: string;
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
  api_key_name?: string;
  api_key_masked?: string;
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
  plan_type: string;
  status: string;
  priority: number;
  hourly_quota: number;
  window_started_at?: string | null;
  window_request_count: number;
  cooldown_until?: string | null;
  max_concurrency: number;
  proxy_mode: string;
  proxy_url: string;
  proxy_http_url: string;
  proxy_https_url: string;
  probe_json: string;
  profile_dir: string;
  storage_state_path: string;
  pending_state_path: string;
  login_message: string;
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

export interface AccountSpace {
  id: string;
  space_id: string;
  space_view_id: string;
  name: string;
  plan_type: string;
  membership_type: string;
  role: string;
  domain: string;
  region: string;
  current: boolean;
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
  plan_type?: string;
  status?: string;
  priority?: number;
  hourly_quota?: number;
  max_concurrency?: number;
  proxy_mode?: string;
  proxy_url?: string;
  proxy_http_url?: string;
  proxy_https_url?: string;
  profile_dir?: string;
  storage_state_path?: string;
  pending_state_path?: string;
  auth_token?: string;
  cookie?: string;
  probe_json?: string;
  clear_auth_token?: boolean;
  clear_cookie?: boolean;
}

export interface ManualImportAccountPayload {
  email?: string;
  user_id?: string;
  user_name?: string;
  space_id?: string;
  space_view_id?: string;
  space_name?: string;
  client_version?: string;
  cookie_header?: string;
  token_v2?: string;
  probe_json_text?: string;
  active?: boolean;
  priority?: number;
  hourly_quota?: number;
  max_concurrency?: number;
}

export interface AccountLoginStatus {
  id: number;
  email: string;
  status: string;
  success: boolean;
  message: string;
  error: string;
  client_version: string;
  device_id: string;
  profile_dir: string;
  storage_state_path: string;
  pending_state_path: string;
  probe_path: string;
  current_url: string;
  final_url: string;
  title: string;
  last_login_at?: string | null;
  expires_at?: string | null;
  created_at: string;
  updated_at: string;
}

export interface AccountLoginResponse {
  success: boolean;
  account: Account;
  status: AccountLoginStatus;
}

export interface AccountTestResponse {
  success: boolean;
  account?: Account;
  notion_request: Record<string, unknown>;
  text: string;
}

export interface NotionConversationAttachment {
  name?: string;
  content_type?: string;
  source?: string;
  url?: string;
}

export interface NotionConversationMessage {
  id: string;
  role: string;
  status: string;
  content: string;
  created_at?: string | null;
  updated_at?: string | null;
  attachments?: NotionConversationAttachment[];
}

export interface NotionConversationSummary {
  id: string;
  title: string;
  origin: string;
  remote_only: boolean;
  preview?: string;
  status: string;
  source: string;
  transport: string;
  account_email: string;
  space_id: string;
  thread_id: string;
  created_by_display_name?: string;
  created_at?: string | null;
  updated_at?: string | null;
  message_count?: number;
}

export interface NotionConversationDetail extends NotionConversationSummary {
  messages: NotionConversationMessage[];
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
