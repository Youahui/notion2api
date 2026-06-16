import {
  CheckCircleOutlined,
  CloudSyncOutlined,
  DeleteOutlined,
  ExperimentOutlined,
  ImportOutlined,
  LoginOutlined,
  PlusOutlined,
  ReloadOutlined,
  SaveOutlined,
  ThunderboltOutlined
} from '@ant-design/icons';
import {
  Alert,
  App as AntApp,
  Button,
  Card,
  Col,
  Collapse,
  Descriptions,
  Empty,
  Flex,
  Form,
  Input,
  InputNumber,
  Pagination,
  Popconfirm,
  Radio,
  Row,
  Select,
  Space,
  Statistic,
  Switch,
  Table,
  Tabs,
  Tag,
  Typography
} from 'antd';
import { useEffect, useMemo, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { adminApi } from '../../services/adminApi';
import type { Account, AccountLoginStatus, AccountPayload, AccountSpace, ManualImportAccountPayload, ModelRow } from '../../types/api';

const { Text, Title } = Typography;

type TabKey = 'list' | 'add' | 'test' | 'logs';
type ManualImportMode = 'probe' | 'token' | 'cookie';

const accountTabKeys: TabKey[] = ['list', 'add', 'test', 'logs'];

interface AccountTestPanelResult {
  success: boolean;
  status: string;
  text: string;
  error: string;
  elapsedMs: number;
  accountEmail: string;
  model: string;
}

interface AccountLogRow {
  key: string;
  time: string;
  account: string;
  action: string;
  status: string;
  error: string;
}

function fmtTime(value?: string | null) {
  return value ? new Date(value).toLocaleString() : '-';
}

function statusText(status?: string | null) {
  const value = status || '';
  const map: Record<string, string> = {
    ready: '可用',
    active: '当前活跃',
    enabled: '已启用',
    error: '异常',
    disabled: '已停用',
    pending_code: '等待验证码',
    inactive: '未激活'
  };
  if (map[value]) return map[value];
  if (value.includes('failed')) return '异常';
  return value || '未知';
}

function statusColor(status?: string | null) {
  const value = status || '';
  if (value === 'ready' || value === 'enabled') return 'green';
  if (value === 'active' || value === 'pending_code') return 'blue';
  if (value.includes('failed') || value === 'error') return 'red';
  if (value === 'disabled' || value === 'inactive') return 'default';
  return 'purple';
}

function StatusTag({ status }: { status?: string | null }) {
  return <Tag color={statusColor(status)}>{statusText(status)}</Tag>;
}

function EnabledTag({ enabled }: { enabled: boolean }) {
  return <StatusTag status={enabled ? 'enabled' : 'disabled'} />;
}

function quotaText(value?: number) {
  return value && value > 0 ? `${value} / 小时` : '不限';
}

function spaceLabel(space: AccountSpace) {
  const name = space.name || space.space_id;
  const suffix = [space.plan_type, space.role].filter(Boolean).join(' · ');
  return suffix ? `${name} (${suffix})` : name;
}

function currentAccountSpace(account: Account): AccountSpace | null {
  if (!account.space_id) return null;
  return {
    id: account.space_id,
    space_id: account.space_id,
    space_view_id: account.space_view_id,
    name: account.space_name,
    plan_type: account.plan_type,
    membership_type: '',
    role: '',
    domain: '',
    region: '',
    current: true
  };
}

function accountToPayload(row: Account): AccountPayload {
  return {
    email: row.email,
    enabled: row.enabled,
    active: row.active,
    user_id: row.user_id,
    user_name: row.user_name,
    space_id: row.space_id,
    space_view_id: row.space_view_id,
    space_name: row.space_name,
    client_version: row.client_version,
    plan_type: row.plan_type,
    status: row.status,
    priority: row.priority,
    hourly_quota: row.hourly_quota,
    max_concurrency: row.max_concurrency,
    proxy_mode: row.proxy_mode || '',
    proxy_url: row.proxy_url,
    proxy_http_url: row.proxy_http_url,
    proxy_https_url: row.proxy_https_url,
    profile_dir: row.profile_dir,
    storage_state_path: row.storage_state_path,
    pending_state_path: row.pending_state_path,
    probe_json: row.probe_json
  };
}

function cleanObject<T extends object>(value: T): Partial<T> {
  return Object.fromEntries(Object.entries(value as Record<string, unknown>).filter(([, item]) => item !== undefined && item !== '')) as Partial<T>;
}

function accountLogAction(status: AccountLoginStatus) {
  if (status.status === 'pending_code') return '请求验证码';
  if (status.success) return '登录或刷新完成';
  if (status.error || status.status.includes('failed')) return '登录或刷新异常';
  return status.message || '账号状态更新';
}

function isAccountTab(value?: string): value is TabKey {
  return accountTabKeys.includes(value as TabKey);
}

export function AccountsPage() {
  const { message } = AntApp.useApp();
  const navigate = useNavigate();
  const { tab } = useParams();
  const activeTab = isAccountTab(tab) ? tab : 'list';
  const [rows, setRows] = useState<Account[]>([]);
  const [statuses, setStatuses] = useState<AccountLoginStatus[]>([]);
  const [models, setModels] = useState<ModelRow[]>([]);
  const [spaceOptions, setSpaceOptions] = useState<AccountSpace[]>([]);
  const [selectedID, setSelectedID] = useState<number>();
  const [manualMode, setManualMode] = useState<ManualImportMode>('probe');
  const [lastLoadedAt, setLastLoadedAt] = useState<string | null>(null);
  const [accountPage, setAccountPage] = useState(1);
  const [accountPageSize, setAccountPageSize] = useState(8);
  const [loginEmail, setLoginEmail] = useState('');
  const [codeCountdown, setCodeCountdown] = useState(0);
  const [testResult, setTestResult] = useState<AccountTestPanelResult | null>(null);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [loginBusy, setLoginBusy] = useState<'start' | 'verify' | ''>('');
  const [refreshingAccountID, setRefreshingAccountID] = useState<number>();
  const [importing, setImporting] = useState(false);
  const [testing, setTesting] = useState(false);
  const [loadingSpaces, setLoadingSpaces] = useState(false);
  const [spaceLoadError, setSpaceLoadError] = useState('');
  const [detailForm] = Form.useForm<AccountPayload>();
  const [startForm] = Form.useForm<{ email: string }>();
  const [verifyForm] = Form.useForm<{ code: string }>();
  const [manualForm] = Form.useForm<ManualImportAccountPayload>();
  const [testForm] = Form.useForm<{ account_id: number; model: string; prompt: string }>();

  const selected = useMemo(() => rows.find((row) => row.id === selectedID) ?? rows[0] ?? null, [rows, selectedID]);
  const selectedStatus = statuses.find((status) => status.email === selected?.email) ?? statuses[0];
  const readyCount = rows.filter((row) => row.enabled && row.status === 'ready').length;
  const errorCount = rows.filter((row) => row.status === 'error' || Boolean(row.last_error)).length;
  const accountTotalPages = Math.max(1, Math.ceil(rows.length / accountPageSize));
  const currentAccountPage = Math.min(accountPage, accountTotalPages);
  const pagedRows = useMemo(() => {
    const start = (currentAccountPage - 1) * accountPageSize;
    return rows.slice(start, start + accountPageSize);
  }, [rows, currentAccountPage, accountPageSize]);

  const modelOptions = useMemo(() => {
    const options = models
      .filter((row) => row.enabled)
      .map((row) => ({ value: row.public_id, label: row.name ? `${row.name} (${row.public_id})` : row.public_id }));
    return [{ value: 'auto', label: '自动选择' }, ...options];
  }, [models]);

  const logRows = useMemo<AccountLogRow[]>(() => {
    const loginLogs = statuses.map((status) => ({
      key: `login-${status.id}-${status.updated_at}`,
      time: status.updated_at || status.created_at,
      account: status.email,
      action: accountLogAction(status),
      status: status.success ? 'ready' : status.status,
      error: status.error || ''
    }));
    const accountFallbackLogs = rows
      .filter((row) => row.last_error || row.updated_at)
      .map((row) => ({
        key: `account-${row.id}-${row.updated_at}`,
        time: row.last_refresh_at || row.updated_at,
        account: row.email,
        action: row.last_error ? '账号异常' : '账号记录更新',
        status: row.last_error ? 'error' : row.status,
        error: row.last_error || ''
      }));
    return [...loginLogs, ...accountFallbackLogs]
      .sort((a, b) => new Date(b.time).getTime() - new Date(a.time).getTime())
      .slice(0, 80);
  }, [rows, statuses]);

  const load = async () => {
    setLoading(true);
    try {
      const [accountRes, statusRes, modelRes] = await Promise.all([
        adminApi.accounts(),
        adminApi.accountLoginStatus(),
        adminApi.models().catch(() => ({ data: [] as ModelRow[] }))
      ]);
      setRows(accountRes.data);
      setStatuses(statusRes.data);
      setModels(modelRes.data);
      setSelectedID((current) => current && accountRes.data.some((row) => row.id === current) ? current : accountRes.data[0]?.id);
      setLastLoadedAt(new Date().toISOString());
    } catch (error) {
      message.error(error instanceof Error ? error.message : '加载账号池失败');
    } finally {
      setLoading(false);
    }
  };

  const goTab = (key: TabKey) => {
    navigate(`/admin/accounts/${key}`);
  };

  useEffect(() => {
    void load();
  }, []);

  useEffect(() => {
    if (tab && !isAccountTab(tab)) {
      navigate('/admin/accounts/list', { replace: true });
    }
  }, [tab, navigate]);

  useEffect(() => {
    if (accountPage === currentAccountPage) return;
    setAccountPage(currentAccountPage);
  }, [accountPage, currentAccountPage]);

  useEffect(() => {
    if (!selectedID) return;
    const selectedIndex = rows.findIndex((row) => row.id === selectedID);
    if (selectedIndex < 0) return;
    const selectedPage = Math.floor(selectedIndex / accountPageSize) + 1;
    setAccountPage((current) => current === selectedPage ? current : selectedPage);
  }, [rows, selectedID, accountPageSize]);

  useEffect(() => {
    if (codeCountdown <= 0) return;
    const timer = window.setInterval(() => {
      setCodeCountdown((current) => Math.max(0, current - 1));
    }, 1000);
    return () => window.clearInterval(timer);
  }, [codeCountdown]);

  useEffect(() => {
    if (activeTab !== 'list' || !selected) return;
    detailForm.setFieldsValue(accountToPayload(selected));
  }, [activeTab, selected, detailForm]);

  useEffect(() => {
    if (activeTab !== 'list' || !selected) {
      setSpaceOptions([]);
      setSpaceLoadError('');
      return;
    }
    let ignore = false;
    const fallback = currentAccountSpace(selected);
    setSpaceOptions(fallback ? [fallback] : []);
    setSpaceLoadError('');
    setLoadingSpaces(true);
    adminApi.accountSpaces(selected.id)
      .then((res) => {
        if (ignore) return;
        setSpaceOptions(res.data.length > 0 ? res.data : fallback ? [fallback] : []);
      })
      .catch((error) => {
        if (ignore) return;
        setSpaceLoadError(error instanceof Error ? error.message : '空间列表加载失败');
      })
      .finally(() => {
        if (!ignore) setLoadingSpaces(false);
      });
    return () => {
      ignore = true;
    };
  }, [activeTab, selected]);

  useEffect(() => {
    if (activeTab !== 'test' || !selected) return;
    const currentTestValues = testForm.getFieldsValue();
    testForm.setFieldsValue({
      account_id: selected.id,
      model: currentTestValues.model || 'auto',
      prompt: currentTestValues.prompt || 'Reply with NOTION2API_ACCOUNT_OK only.'
    });
  }, [activeTab, selected, testForm]);

  useEffect(() => {
    if (activeTab !== 'add' || !loginEmail) return;
    startForm.setFieldsValue({ email: loginEmail });
    verifyForm.resetFields();
  }, [activeTab, loginEmail, startForm, verifyForm]);

  const saveDetail = async () => {
    if (!selected) return;
    setSaving(true);
    try {
      const values = await detailForm.validateFields();
      const payload = {
        ...accountToPayload(selected),
        ...values,
        email: selected.email,
        probe_json: selected.probe_json
      } as AccountPayload;
      const updated = await adminApi.updateAccount(selected.id, payload);
      message.success('账号设置已保存');
      setSelectedID(updated.id);
      await load();
    } catch (error) {
      if (error instanceof Error) message.error(error.message);
    } finally {
      setSaving(false);
    }
  };

  const requestLoginCode = async (email: string) => {
    setLoginBusy('start');
    try {
      const result = await adminApi.startAccountLogin(email);
      setSelectedID(result.account.id);
      setLoginEmail(email);
      const seconds = result.status.expires_at
        ? Math.max(30, Math.floor((new Date(result.status.expires_at).getTime() - Date.now()) / 1000))
        : 60;
      setCodeCountdown(seconds);
      message[result.success ? 'success' : 'warning'](result.success ? '验证码请求已提交' : result.status.message || '验证码请求失败');
      await load();
    } catch (error) {
      message.error(error instanceof Error ? error.message : '验证码请求失败');
    } finally {
      setLoginBusy('');
    }
  };

  const startLogin = async () => {
    const values = await startForm.validateFields();
    await requestLoginCode(values.email);
  };

  const verifyLogin = async () => {
    const values = await verifyForm.validateFields();
    const email = loginEmail || startForm.getFieldValue('email');
    if (!email) {
      message.warning('请先输入邮箱并请求验证码');
      return;
    }
    setLoginBusy('verify');
    try {
      const result = await adminApi.verifyAccountLogin(email, values.code);
      setSelectedID(result.account.id);
      setCodeCountdown(0);
      message[result.success ? 'success' : 'warning'](result.success ? '账号已登录并写入账号池' : result.status.message || '验证码校验失败');
      await load();
    } catch (error) {
      message.error(error instanceof Error ? error.message : '验证码校验失败');
    } finally {
      setLoginBusy('');
    }
  };

  const manualImport = async (values: ManualImportAccountPayload) => {
    setImporting(true);
    try {
      const result = await adminApi.manualImportAccount(cleanObject(values) as ManualImportAccountPayload);
      setSelectedID(result.account.id);
      message.success('账号已导入账号池');
      manualForm.resetFields(['cookie_header', 'token_v2', 'probe_json_text']);
      goTab('list');
      await load();
    } catch (error) {
      message.error(error instanceof Error ? error.message : '导入失败');
    } finally {
      setImporting(false);
    }
  };

  const testAccount = async () => {
    const values = await testForm.validateFields();
    const account = rows.find((row) => row.id === values.account_id);
    const startedAt = performance.now();
    setTesting(true);
    setTestResult(null);
    try {
      const result = await adminApi.testAccount(values.account_id, { model: values.model, prompt: values.prompt });
      setTestResult({
        success: result.success,
        status: result.success ? '请求完成' : '请求失败',
        text: result.text || '-',
        error: '',
        elapsedMs: Math.round(performance.now() - startedAt),
        accountEmail: result.account?.email || account?.email || '-',
        model: values.model || 'auto'
      });
      await load();
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : '测试失败';
      setTestResult({
        success: false,
        status: '请求失败',
        text: '-',
        error: errorMessage,
        elapsedMs: Math.round(performance.now() - startedAt),
        accountEmail: account?.email || '-',
        model: values.model || 'auto'
      });
      message.error(errorMessage);
    } finally {
      setTesting(false);
    }
  };

  const deleteSelected = async () => {
    if (!selected) return;
    await adminApi.deleteAccount(selected.id);
    message.success('账号已删除');
    setSelectedID(undefined);
    await load();
  };

  const toggleSelectedEnabled = async () => {
    if (!selected) return;
    const enabled = !selected.enabled;
    await adminApi.updateAccount(selected.id, { ...accountToPayload(selected), enabled });
    message.success(enabled ? '账号已启用' : '账号已停用');
    await load();
  };

  const activateSelected = async () => {
    if (!selected) return;
    await adminApi.activateAccount(selected.id);
    message.success('已设为当前活跃账号');
    await load();
  };

  const refreshSelectedCredentials = async () => {
    if (!selected) return;
    setRefreshingAccountID(selected.id);
    try {
      const refreshed = await adminApi.refreshAccount(selected.id);
      setSelectedID(refreshed.id);
      message.success('凭证已刷新');
      await load();
    } catch (error) {
      message.error(error instanceof Error ? error.message : '刷新凭证失败');
    } finally {
      setRefreshingAccountID(undefined);
    }
  };

  const openSelectedInTester = () => {
    if (!selected) return;
    goTab('test');
  };

  return (
    <Space className="accounts-page" direction="vertical" size={16}>
      <Flex justify="space-between" align="center" wrap="wrap" gap={16}>
        <div>
          <Text type="secondary">账号池</Text>
          <Title level={2} className="accounts-title">账号管理</Title>
        </div>
        <Space wrap>
          <Button icon={<ReloadOutlined />} loading={loading} onClick={load}>刷新账号</Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => goTab('add')}>添加账号</Button>
        </Space>
      </Flex>

      <Card className="accounts-overview-card" styles={{ body: { padding: 16 } }}>
        <Flex justify="space-between" align="center" wrap="wrap" gap={16}>
          <Row gutter={[16, 12]} className="accounts-stat-grid">
            <Col xs={12} md={6}>
              <Statistic title="总账号数" value={rows.length} suffix="个" />
            </Col>
            <Col xs={12} md={6}>
              <Statistic title="可用账号数" value={readyCount} suffix="个" />
            </Col>
            <Col xs={12} md={6}>
              <Statistic title="异常账号数" value={errorCount} suffix="个" />
            </Col>
            <Col xs={12} md={6}>
              <Statistic title="最近刷新时间" value={fmtTime(lastLoadedAt)} />
            </Col>
          </Row>
          <Space wrap className="accounts-overview-actions">
            <Button icon={<ReloadOutlined />} loading={loading} onClick={load}>刷新账号</Button>
            <Button type="primary" icon={<PlusOutlined />} onClick={() => goTab('add')}>添加账号</Button>
          </Space>
        </Flex>
      </Card>

      <Tabs
        activeKey={activeTab}
        renderTabBar={() => <></>}
        items={[
          {
            key: 'list',
            label: '账号列表',
            forceRender: true,
            children: (
              <Row gutter={[16, 16]} align="stretch">
                <Col xs={24} lg={9} xl={8}>
                  <Card title="账号列表" extra={<Text type="secondary">共 {rows.length} 个账号</Text>} className="accounts-fill-card">
                    <Space direction="vertical" className="accounts-card-list" size={12}>
                      {rows.length === 0 ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无账号" /> : pagedRows.map((row) => (
                        <button
                          key={row.id}
                          className={`account-list-item${selected?.id === row.id ? ' account-list-item-active' : ''}`}
                          onClick={() => setSelectedID(row.id)}
                          type="button"
                        >
                          <Flex justify="space-between" align="start" gap={12}>
                            <Space direction="vertical" size={6}>
                              <Text strong>{row.email}</Text>
                              <Space size={[0, 6]} wrap>
                                <StatusTag status={row.status} />
                                {row.active ? <StatusTag status="active" /> : null}
                                <EnabledTag enabled={row.enabled} />
                              </Space>
                              <Text type="secondary">配额：{quotaText(row.hourly_quota)} · 优先级：{row.priority}</Text>
                              <Text type="secondary">最近使用：{fmtTime(row.last_used_at)}</Text>
                            </Space>
                            <Text type="secondary">›</Text>
                          </Flex>
                        </button>
                      ))}
                    </Space>
                    {rows.length > accountPageSize ? (
                      <Pagination
                        className="accounts-list-pagination"
                        current={currentAccountPage}
                        pageSize={accountPageSize}
                        total={rows.length}
                        pageSizeOptions={[5, 8, 12, 20]}
                        showSizeChanger
                        showTotal={(total, range) => `${range[0]}-${range[1]} / ${total}`}
                        size="small"
                        onChange={(page, pageSize) => {
                          setAccountPage(page);
                          setAccountPageSize(pageSize);
                        }}
                      />
                    ) : null}
                  </Card>
                </Col>

                <Col xs={24} lg={15} xl={16}>
                  <Card
                    title="账号详情"
                    className="accounts-fill-card"
                    extra={selected ? (
                      <Space wrap>
                        <Button icon={<ExperimentOutlined />} onClick={openSelectedInTester}>测试账号</Button>
                        <Button icon={<CloudSyncOutlined />} loading={refreshingAccountID === selected.id} onClick={refreshSelectedCredentials}>刷新凭证</Button>
                        <Button icon={<ThunderboltOutlined />} disabled={selected.active} onClick={activateSelected}>设为活跃</Button>
                        <Button onClick={toggleSelectedEnabled}>{selected.enabled ? '停用账号' : '启用账号'}</Button>
                        <Popconfirm title="删除账号" description={selected.email} okText="删除" cancelText="取消" okButtonProps={{ danger: true }} onConfirm={deleteSelected}>
                          <Button danger icon={<DeleteOutlined />}>删除账号</Button>
                        </Popconfirm>
                      </Space>
                    ) : null}
                  >
                    {!selected ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="请选择账号" /> : (
                      <Space direction="vertical" size={16} style={{ width: '100%' }}>
                        <Space size={[0, 8]} wrap>
                          <StatusTag status={selected.status} />
                          {selected.active ? <StatusTag status="active" /> : null}
                          <EnabledTag enabled={selected.enabled} />
                          {selected.has_cookie ? <Tag color="green">Cookie 已保存</Tag> : <Tag>无 Cookie</Tag>}
                          {selected.has_auth_token ? <Tag color="green">token_v2 已保存</Tag> : <Tag>无 token_v2</Tag>}
                        </Space>

                        <Descriptions column={{ xs: 1, md: 2 }} size="small" bordered>
                          <Descriptions.Item label="邮箱">{selected.email}</Descriptions.Item>
                          <Descriptions.Item label="状态"><StatusTag status={selected.status} /></Descriptions.Item>
                          <Descriptions.Item label="用户">{selected.user_name || '-'}</Descriptions.Item>
                          <Descriptions.Item label="空间">{selected.space_name || '-'}</Descriptions.Item>
                          <Descriptions.Item label="User ID">{selected.user_id || '-'}</Descriptions.Item>
                          <Descriptions.Item label="Space ID">{selected.space_id || '-'}</Descriptions.Item>
                          <Descriptions.Item label="Space View ID">{selected.space_view_id || '-'}</Descriptions.Item>
                          <Descriptions.Item label="Client Version">{selected.client_version || '-'}</Descriptions.Item>
                          <Descriptions.Item label="套餐">{selected.plan_type || '-'}</Descriptions.Item>
                          <Descriptions.Item label="冷却截止">{fmtTime(selected.cooldown_until)}</Descriptions.Item>
                          <Descriptions.Item label="成功 / 失败">{`${selected.total_successes} / ${selected.total_failures}`}</Descriptions.Item>
                          <Descriptions.Item label="连续失败">{selected.consecutive_failures}</Descriptions.Item>
                          <Descriptions.Item label="最近登录">{fmtTime(selected.last_login_at)}</Descriptions.Item>
                          <Descriptions.Item label="最近成功">{fmtTime(selected.last_success_at)}</Descriptions.Item>
                          <Descriptions.Item label="最近刷新">{fmtTime(selected.last_refresh_at)}</Descriptions.Item>
                          <Descriptions.Item label="最近使用">{fmtTime(selected.last_used_at)}</Descriptions.Item>
                          <Descriptions.Item label="Probe JSON" span={2}>{selected.probe_json ? '已保存' : '-'}</Descriptions.Item>
                          <Descriptions.Item label="Storage State" span={2}>{selected.storage_state_path || '-'}</Descriptions.Item>
                          <Descriptions.Item label="最后错误" span={2}>{selected.last_error || '-'}</Descriptions.Item>
                        </Descriptions>

                        <Form form={detailForm} layout="vertical" className="accounts-detail-form">
                          <Row gutter={[16, 0]}>
                            <Col xs={24} md={12}>
                              <Form.Item name="status" label="账号状态">
                                <Select options={[
                                  { value: 'ready', label: '可用' },
                                  { value: 'pending_code', label: '等待验证码' },
                                  { value: 'inactive', label: '未激活' },
                                  { value: 'error', label: '异常' }
                                ]} />
                              </Form.Item>
                            </Col>
                            <Col xs={24} md={12}>
                              <Form.Item
                                name="space_id"
                                label="空间"
                                extra={spaceLoadError || '选择该账号请求时使用的 Notion 空间。'}
                                validateStatus={spaceLoadError ? 'warning' : undefined}
                              >
                                <Select
                                  showSearch
                                  loading={loadingSpaces}
                                  placeholder="请选择空间"
                                  optionFilterProp="label"
                                  options={spaceOptions.map((space) => ({
                                    value: space.space_id,
                                    label: spaceLabel(space)
                                  }))}
                                  onChange={(spaceID) => {
                                    const space = spaceOptions.find((item) => item.space_id === spaceID);
                                    detailForm.setFieldsValue({
                                      space_id: space?.space_id,
                                      space_view_id: space?.space_view_id,
                                      space_name: space?.name,
                                      plan_type: space?.plan_type
                                    });
                                  }}
                                />
                              </Form.Item>
                              <Form.Item name="space_view_id" hidden>
                                <Input />
                              </Form.Item>
                              <Form.Item name="space_name" hidden>
                                <Input />
                              </Form.Item>
                              <Form.Item name="plan_type" hidden>
                                <Input />
                              </Form.Item>
                            </Col>
                            <Col xs={24} md={8}>
                              <Form.Item name="priority" label="优先级">
                                <InputNumber style={{ width: '100%' }} />
                              </Form.Item>
                            </Col>
                            <Col xs={24} md={8}>
                              <Form.Item name="hourly_quota" label="每小时配额" extra="填 0 表示不限。">
                                <InputNumber min={0} style={{ width: '100%' }} />
                              </Form.Item>
                            </Col>
                            <Col xs={24} md={8}>
                              <Form.Item name="max_concurrency" label="最大并发">
                                <InputNumber min={1} style={{ width: '100%' }} />
                              </Form.Item>
                            </Col>
                            <Col xs={24} md={8}>
                              <Form.Item name="proxy_mode" label="代理模式">
                                <Select options={[
                                  { value: '', label: '继承全局' },
                                  { value: 'off', label: '关闭' },
                                  { value: 'env', label: '环境变量' },
                                  { value: 'http', label: 'HTTP' },
                                  { value: 'https', label: 'HTTPS' },
                                  { value: 'socks5', label: 'SOCKS5' }
                                ]} />
                              </Form.Item>
                            </Col>
                            <Col xs={24} md={16}>
                              <Form.Item name="proxy_url" label="代理地址" extra="为空时按模式继承全局地址；SOCKS5 可填 socks5://127.0.0.1:1080。">
                                <Input placeholder="http://127.0.0.1:7890" />
                              </Form.Item>
                            </Col>
                          </Row>
                          <Button type="primary" icon={<SaveOutlined />} loading={saving} onClick={saveDetail} disabled={!selected}>
                            保存设置
                          </Button>
                        </Form>
                      </Space>
                    )}
                  </Card>
                </Col>
              </Row>
            )
          },
          {
            key: 'add',
            label: '添加账号',
            forceRender: true,
            children: (
              <Row gutter={[16, 16]}>
                <Col xs={24} xl={10}>
                  <Card title={<Space><LoginOutlined />验证码登录</Space>}>
                    <Space direction="vertical" size={16} style={{ width: '100%' }}>
                      <Form form={startForm} layout="vertical" className="accounts-narrow-form">
                        <div className="accounts-step-label">步骤 1</div>
                        <Title level={5}>输入邮箱并请求验证码</Title>
                        <Form.Item name="email" label="邮箱" rules={[{ required: true, type: 'email', message: '请输入有效邮箱' }]}>
                          <Input placeholder="name@example.com" />
                        </Form.Item>
                        <Button type="primary" icon={<LoginOutlined />} loading={loginBusy === 'start'} onClick={startLogin}>
                          请求验证码
                        </Button>
                      </Form>

                      {loginEmail ? (
                        <Alert
                          type="success"
                          showIcon
                          message="验证码请求已提交"
                          description={`验证码已发送至 ${loginEmail}。${codeCountdown > 0 ? `请在 ${codeCountdown} 秒内完成登录。` : '验证码可能已过期，请重新请求。'}`}
                        />
                      ) : null}

                      <Form form={verifyForm} layout="vertical" className="accounts-narrow-form">
                        <div className="accounts-step-label">步骤 2</div>
                        <Title level={5}>输入验证码并登录</Title>
                        <Text type="secondary">当前邮箱：{loginEmail || '请先完成步骤 1'}</Text>
                        <Form.Item name="code" label="验证码" rules={[{ required: true, message: '请输入验证码' }]} style={{ marginTop: 12 }}>
                          <Input placeholder="请输入邮件中的验证码" />
                        </Form.Item>
                        <Button icon={<CheckCircleOutlined />} loading={loginBusy === 'verify'} onClick={verifyLogin} disabled={!loginEmail}>
                          登录并写入账号池
                        </Button>
                      </Form>

                      {selectedStatus?.error ? (
                        <Alert type="warning" showIcon message={selectedStatus.message || statusText(selectedStatus.status)} description={selectedStatus.error} />
                      ) : null}
                    </Space>
                  </Card>
                </Col>

                <Col xs={24} xl={14}>
                  <Card title={<Space><ImportOutlined />手动导入</Space>}>
                    <Form
                      form={manualForm}
                      layout="vertical"
                      className="accounts-manual-form"
                      initialValues={{ active: true, max_concurrency: 1, priority: 0, hourly_quota: 0 }}
                      onFinish={manualImport}
                    >
                      <Space direction="vertical" size={16} style={{ width: '100%' }}>
                        <Radio.Group
                          optionType="button"
                          buttonStyle="solid"
                          value={manualMode}
                          onChange={(event) => setManualMode(event.target.value as ManualImportMode)}
                          options={[
                            { value: 'probe', label: '粘贴完整 Probe JSON' },
                            { value: 'token', label: '只粘贴 token_v2' },
                            { value: 'cookie', label: '粘贴 Cookie Header' }
                          ]}
                        />

                        {manualMode === 'probe' ? (
                          <Form.Item
                            name="probe_json_text"
                            label="完整 Probe JSON"
                            rules={[{ required: true, message: '请粘贴完整 Probe JSON' }]}
                            preserve={false}
                          >
                            <Input.TextArea
                              rows={10}
                              className="accounts-code-input"
                              placeholder={'{"email":"name@example.com","user_id":"...","space_id":"...","client_version":"...","cookies":[{"name":"token_v2","value":"..."}]}'}
                            />
                          </Form.Item>
                        ) : manualMode === 'token' ? (
                          <Form.Item
                            name="token_v2"
                            label="token_v2"
                            rules={[{ required: true, message: '请粘贴 token_v2' }]}
                            preserve={false}
                          >
                            <Input.Password placeholder="粘贴 Notion token_v2" />
                          </Form.Item>
                        ) : (
                          <Form.Item
                            name="cookie_header"
                            label="Cookie Header"
                            rules={[{ required: true, message: '请粘贴 Cookie Header' }]}
                            extra="支持完整 Cookie header，例如 token_v2=...; notion_user_id=...；也支持只粘贴 token_v2 的裸值。"
                            preserve={false}
                          >
                            <Input.TextArea rows={5} placeholder="token_v2=...; notion_user_id=...; cookie2=value2" />
                          </Form.Item>
                        )}

                        <Form.Item name="active" valuePropName="checked" style={{ marginBottom: 0 }}>
                          <Switch checkedChildren="导入后设为活跃" unCheckedChildren="仅保存" />
                        </Form.Item>

                        <Collapse
                          ghost
                          items={[
                            {
                              key: 'advanced',
                              label: '高级配置',
                              children: (
                                <Row gutter={[16, 0]}>
                                  <Col xs={24} md={12}>
                                    <Form.Item name="email" label="邮箱" extra="留空时后端会尽量从凭证中自动识别。">
                                      <Input placeholder="name@example.com" />
                                    </Form.Item>
                                  </Col>
                                  <Col xs={24} md={12}>
                                    <Form.Item name="user_id" label="User ID">
                                      <Input placeholder="可选，用于覆盖自动识别结果" />
                                    </Form.Item>
                                  </Col>
                                  <Col xs={24} md={12}>
                                    <Form.Item name="space_id" label="Space ID">
                                      <Input placeholder="可选，用于覆盖自动识别结果" />
                                    </Form.Item>
                                  </Col>
                                  <Col xs={24} md={12}>
                                    <Form.Item name="client_version" label="Client Version">
                                      <Input placeholder="例如 23.13.0.0" />
                                    </Form.Item>
                                  </Col>
                                  <Col xs={24} md={12}>
                                    <Form.Item name="user_name" label="UserName">
                                      <Input placeholder="可选" />
                                    </Form.Item>
                                  </Col>
                                  <Col xs={24} md={12}>
                                    <Form.Item name="space_name" label="SpaceName">
                                      <Input placeholder="可选" />
                                    </Form.Item>
                                  </Col>
                                  <Col xs={24} md={8}>
                                    <Form.Item name="priority" label="Priority">
                                      <InputNumber style={{ width: '100%' }} />
                                    </Form.Item>
                                  </Col>
                                  <Col xs={24} md={8}>
                                    <Form.Item name="hourly_quota" label="Hourly" extra="填 0 表示不限。">
                                      <InputNumber min={0} style={{ width: '100%' }} />
                                    </Form.Item>
                                  </Col>
                                  <Col xs={24} md={8}>
                                    <Form.Item name="max_concurrency" label="Max">
                                      <InputNumber min={1} style={{ width: '100%' }} />
                                    </Form.Item>
                                  </Col>
                                  {manualMode !== 'cookie' ? (
                                    <Col span={24}>
                                      <Form.Item name="cookie_header" label="Cookie Header">
                                        <Input.TextArea rows={4} placeholder="cookie1=value1; token_v2=..." />
                                      </Form.Item>
                                    </Col>
                                  ) : null}
                                </Row>
                              )
                            }
                          ]}
                        />

                        <Button type="primary" htmlType="submit" icon={<ImportOutlined />} loading={importing}>
                          导入账号
                        </Button>
                      </Space>
                    </Form>
                  </Card>
                </Col>
              </Row>
            )
          },
          {
            key: 'test',
            label: '测试账号',
            forceRender: true,
            children: (
              <Row gutter={[16, 16]}>
                <Col xs={24} xl={10}>
                  <Card title={<Space><ExperimentOutlined />测试账号</Space>}>
                    <Form form={testForm} layout="vertical" className="accounts-narrow-form">
                      <Form.Item name="account_id" label="账号" rules={[{ required: true, message: '请选择账号' }]}>
                        <Select
                          placeholder="请选择账号"
                          options={rows.map((row) => ({ value: row.id, label: `${row.email}${row.active ? ' · 当前活跃' : ''}` }))}
                        />
                      </Form.Item>
                      <Form.Item name="model" label="模型" rules={[{ required: true, message: '请选择模型' }]}>
                        <Select options={modelOptions} />
                      </Form.Item>
                      <Form.Item name="prompt" label="Prompt" rules={[{ required: true, message: '请输入测试 Prompt' }]}>
                        <Input.TextArea rows={5} placeholder="请输入要发送给模型的测试内容" />
                      </Form.Item>
                      <Button type="primary" icon={<ExperimentOutlined />} loading={testing} onClick={testAccount}>
                        开始测试
                      </Button>
                    </Form>
                  </Card>
                </Col>
                <Col xs={24} xl={14}>
                  <Card title="测试结果">
                    {!testResult ? (
                      <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无测试结果" />
                    ) : (
                      <Space direction="vertical" size={16} style={{ width: '100%' }}>
                        <Alert
                          type={testResult.success ? 'success' : 'error'}
                          showIcon
                          message={testResult.status}
                        />
                        <Descriptions column={{ xs: 1, md: 2 }} size="small" bordered>
                          <Descriptions.Item label="状态">{testResult.status}</Descriptions.Item>
                          <Descriptions.Item label="耗时">{testResult.elapsedMs} ms</Descriptions.Item>
                          <Descriptions.Item label="使用账号">{testResult.accountEmail}</Descriptions.Item>
                          <Descriptions.Item label="使用模型">{testResult.model}</Descriptions.Item>
                          <Descriptions.Item label="错误信息" span={2}>{testResult.error || '-'}</Descriptions.Item>
                        </Descriptions>
                        <Input.TextArea value={testResult.text} rows={8} readOnly />
                      </Space>
                    )}
                  </Card>
                </Col>
              </Row>
            )
          },
          {
            key: 'logs',
            label: '运行日志',
            forceRender: true,
            children: (
              <Card title="账号操作日志">
                <Table<AccountLogRow>
                  rowKey="key"
                  dataSource={logRows}
                  pagination={{ pageSize: 12 }}
                  columns={[
                    { title: '时间', dataIndex: 'time', render: (value: string) => fmtTime(value) },
                    { title: '账号', dataIndex: 'account', ellipsis: true },
                    { title: '动作', dataIndex: 'action' },
                    { title: '状态', dataIndex: 'status', render: (value: string) => <StatusTag status={value} /> },
                    { title: '错误原因', dataIndex: 'error', render: (value: string) => value || '-' }
                  ]}
                />
              </Card>
            )
          }
        ]}
      />
    </Space>
  );
}
