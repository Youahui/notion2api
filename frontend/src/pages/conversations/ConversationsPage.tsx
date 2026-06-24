import { DeleteOutlined, HistoryOutlined, ReloadOutlined } from '@ant-design/icons';
import {
  Alert,
  App as AntApp,
  Button,
  Card,
  Col,
  Descriptions,
  Empty,
  Flex,
  List,
  Pagination,
  Popconfirm,
  Row,
  Select,
  Space,
  Spin,
  Statistic,
  Tag,
  Typography
} from 'antd';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { adminApi } from '../../services/adminApi';
import type {
  Account,
  AccountSpace,
  NotionConversationDetail,
  NotionConversationMessage,
  NotionConversationSummary,
  PaginatedListResponse
} from '../../types/api';

const { Text, Title } = Typography;
const defaultConversationPageSize = 10;

function fmtTime(value?: string | null) {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '-';
  return date.toLocaleString();
}

function statusColor(status?: string) {
  if (status === 'completed') return 'green';
  if (status === 'streaming' || status === 'running') return 'blue';
  if (status === 'failed' || status === 'error') return 'red';
  return 'default';
}

function roleColor(role?: string) {
  if (role === 'user') return 'blue';
  if (role === 'assistant') return 'green';
  return 'default';
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

function mergeSpaces(primary: AccountSpace | null, rows: AccountSpace[]) {
  const byID = new Map<string, AccountSpace>();
  [primary, ...rows].forEach((space) => {
    if (space?.space_id) {
      byID.set(space.space_id, { ...byID.get(space.space_id), ...space });
    }
  });
  return [...byID.values()];
}

function errorText(error: unknown, fallback: string) {
  return error instanceof Error ? error.message : fallback;
}

export function ConversationsPage() {
  const { message } = AntApp.useApp();
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [spaces, setSpaces] = useState<AccountSpace[]>([]);
  const [rows, setRows] = useState<NotionConversationSummary[]>([]);
  const [detail, setDetail] = useState<NotionConversationDetail | null>(null);
  const [conversationPage, setConversationPage] = useState(1);
  const [conversationPageSize, setConversationPageSize] = useState(defaultConversationPageSize);
  const [conversationTotal, setConversationTotal] = useState(0);
  const [conversationHasMore, setConversationHasMore] = useState(false);
  const [selectedAccountID, setSelectedAccountID] = useState<number>();
  const [selectedSpaceID, setSelectedSpaceID] = useState('');
  const [selectedThreadID, setSelectedThreadID] = useState('');
  const [loadingAccounts, setLoadingAccounts] = useState(false);
  const [loadingSpaces, setLoadingSpaces] = useState(false);
  const [loadingConversations, setLoadingConversations] = useState(false);
  const [loadingDetail, setLoadingDetail] = useState(false);
  const [deletingThreadID, setDeletingThreadID] = useState('');
  const [spacesError, setSpacesError] = useState('');
  const [remoteError, setRemoteError] = useState('');
  const [lastLoadedAt, setLastLoadedAt] = useState<string | null>(null);
  const conversationCacheRef = useRef<Record<string, PaginatedListResponse<NotionConversationSummary>>>({});
  const detailCacheRef = useRef<Record<string, NotionConversationDetail>>({});
  const spacesCacheRef = useRef<Record<number, AccountSpace[]>>({});
  const listInFlightRef = useRef<Record<string, Promise<PaginatedListResponse<NotionConversationSummary>>>>({});
  const detailInFlightRef = useRef<Record<string, Promise<NotionConversationDetail>>>({});
  const spacesInFlightRef = useRef<Record<number, Promise<{ data: AccountSpace[] }>>>({});
  const listRequestRef = useRef(0);
  const detailRequestRef = useRef(0);
  const spacesRequestRef = useRef(0);

  const selectedAccount = useMemo(() => accounts.find((row) => row.id === selectedAccountID) ?? null, [accounts, selectedAccountID]);
  const selectedSpace = useMemo(() => spaces.find((space) => space.space_id === selectedSpaceID) ?? null, [selectedSpaceID, spaces]);
  const selectedSummary = useMemo(() => rows.find((row) => row.thread_id === selectedThreadID) ?? null, [rows, selectedThreadID]);
  const conversationTargetKey = selectedAccount && selectedSpaceID ? `${selectedAccount.id}:${selectedSpaceID}` : '';
  const detailCacheKey = conversationTargetKey && selectedThreadID ? `${conversationTargetKey}:${selectedThreadID}` : '';

  const conversationParams = useCallback(() => {
    if (!selectedAccount || !selectedSpaceID) return null;
    return {
      account_id: selectedAccount.id,
      space_id: selectedSpaceID,
      space_view_id: selectedSpace?.space_view_id || undefined,
      space_name: selectedSpace?.name || undefined
    };
  }, [selectedAccount, selectedSpace, selectedSpaceID]);

  const applyConversationPage = useCallback((pageData: PaginatedListResponse<NotionConversationSummary>) => {
    setRows(pageData.data);
    setConversationPage(pageData.page);
    setConversationPageSize(pageData.page_size);
    setConversationTotal(pageData.total);
    setConversationHasMore(pageData.has_more);
    setSelectedThreadID((current) => (current && pageData.data.some((row) => row.thread_id === current) ? current : ''));
    if (!pageData.data.length) setDetail(null);
  }, []);

  const loadAccounts = useCallback(async () => {
    setLoadingAccounts(true);
    try {
      const res = await adminApi.accounts();
      setAccounts(res.data);
      setSelectedAccountID((current) => {
        if (current && res.data.some((row) => row.id === current)) return current;
        return res.data.find((row) => row.active)?.id ?? res.data[0]?.id;
      });
    } catch (error) {
      message.error(errorText(error, '加载账号失败'));
    } finally {
      setLoadingAccounts(false);
    }
  }, [message]);

  const loadSpaces = useCallback(async (account: Account) => {
    const fallback = currentAccountSpace(account);
    setSpaces(fallback ? [fallback] : []);
    setSelectedSpaceID(fallback?.space_id || '');
    setSpacesError('');
    if (spacesCacheRef.current[account.id]) {
      const nextSpaces = mergeSpaces(fallback, spacesCacheRef.current[account.id]);
      setSpaces(nextSpaces);
      setSelectedSpaceID(nextSpaces.find((space) => space.current)?.space_id || fallback?.space_id || nextSpaces[0]?.space_id || '');
      return;
    }
    const requestID = spacesRequestRef.current + 1;
    spacesRequestRef.current = requestID;
    setLoadingSpaces(true);
    try {
      if (!spacesInFlightRef.current[account.id]) {
        spacesInFlightRef.current[account.id] = adminApi.accountSpaces(account.id);
      }
      const res = await spacesInFlightRef.current[account.id];
      if (spacesRequestRef.current !== requestID) return;
      spacesCacheRef.current[account.id] = res.data;
      const nextSpaces = mergeSpaces(fallback, res.data);
      setSpaces(nextSpaces);
      setSelectedSpaceID(nextSpaces.find((space) => space.current)?.space_id || fallback?.space_id || nextSpaces[0]?.space_id || '');
    } catch (error) {
      if (spacesRequestRef.current !== requestID) return;
      setSpacesError(errorText(error, '加载工作区失败'));
    } finally {
      delete spacesInFlightRef.current[account.id];
      if (spacesRequestRef.current === requestID) {
        setLoadingSpaces(false);
      }
    }
  }, []);

  const loadConversations = useCallback(async (force = false, page = 1, pageSize = defaultConversationPageSize) => {
    const params = conversationParams();
    const targetKey = params ? `${params.account_id}:${params.space_id}` : '';
    const cacheKey = targetKey ? `${targetKey}:${page}:${pageSize}` : '';
    if (!params) {
      setRows([]);
      setSelectedThreadID('');
      setDetail(null);
      setConversationTotal(0);
      setConversationHasMore(false);
      return;
    }
    setConversationPage(page);
    setConversationPageSize(pageSize);
    if (!force && conversationCacheRef.current[cacheKey]) {
      applyConversationPage(conversationCacheRef.current[cacheKey]);
    } else {
      setRows([]);
      setSelectedThreadID('');
      setDetail(null);
    }
    const requestID = listRequestRef.current + 1;
    listRequestRef.current = requestID;
    setLoadingConversations(true);
    setRemoteError('');
    try {
      if (!listInFlightRef.current[cacheKey]) {
        listInFlightRef.current[cacheKey] = adminApi.notionConversations({ ...params, page, page_size: pageSize });
      }
      const res = await listInFlightRef.current[cacheKey];
      if (listRequestRef.current !== requestID) return;
      conversationCacheRef.current[cacheKey] = res;
      applyConversationPage(res);
      setLastLoadedAt(new Date().toISOString());
    } catch (error) {
      if (listRequestRef.current !== requestID) return;
      setRows([]);
      setSelectedThreadID('');
      setDetail(null);
      setConversationTotal(0);
      setConversationHasMore(false);
      setRemoteError(errorText(error, '加载 Notion 会话失败'));
    } finally {
      delete listInFlightRef.current[cacheKey];
      if (listRequestRef.current === requestID) {
        setLoadingConversations(false);
      }
    }
  }, [applyConversationPage, conversationParams]);

  useEffect(() => {
    void loadAccounts();
  }, [loadAccounts]);

  useEffect(() => {
    if (!selectedAccount) {
      setSpaces([]);
      setSelectedSpaceID('');
      return;
    }
    void loadSpaces(selectedAccount);
  }, [loadSpaces, selectedAccount]);

  useEffect(() => {
    void loadConversations();
  }, [conversationTargetKey]);

  useEffect(() => {
    const params = conversationParams();
    if (!params || !selectedThreadID) {
      setDetail(null);
      return;
    }
    const cacheKey = `${params.account_id}:${params.space_id}:${selectedThreadID}`;
    const cached = detailCacheRef.current[cacheKey];
    if (cached) {
      setDetail(cached);
      return;
    }
    const requestID = detailRequestRef.current + 1;
    detailRequestRef.current = requestID;
    setLoadingDetail(true);
    if (!detailInFlightRef.current[cacheKey]) {
      detailInFlightRef.current[cacheKey] = adminApi.notionConversation(selectedThreadID, params);
    }
    detailInFlightRef.current[cacheKey]
      .then((row) => {
        if (detailRequestRef.current === requestID) {
          detailCacheRef.current[cacheKey] = row;
          setDetail(row);
        }
      })
      .catch((error) => {
        if (detailRequestRef.current === requestID) {
          setDetail(null);
          message.error(errorText(error, '加载会话详情失败'));
        }
      })
      .finally(() => {
        delete detailInFlightRef.current[cacheKey];
        if (detailRequestRef.current === requestID) setLoadingDetail(false);
      });
  }, [conversationTargetKey, detailCacheKey, message]);

  const deleteConversation = async (threadID: string) => {
    const params = conversationParams();
    if (!params) return;
    setDeletingThreadID(threadID);
    try {
      await adminApi.deleteNotionConversation(threadID, params);
      const nextRows = rows.filter((row) => row.thread_id !== threadID);
      const targetKey = `${params.account_id}:${params.space_id}`;
      Object.keys(conversationCacheRef.current).forEach((key) => {
        if (key.startsWith(`${targetKey}:`)) delete conversationCacheRef.current[key];
      });
      delete detailCacheRef.current[`${targetKey}:${threadID}`];
      setRows(nextRows);
      setConversationTotal((current) => Math.max(0, current - 1));
      if (selectedThreadID === threadID) {
        setSelectedThreadID('');
        setDetail(null);
      }
      const nextPage = nextRows.length === 0 && conversationPage > 1 ? conversationPage - 1 : conversationPage;
      await loadConversations(true, nextPage, conversationPageSize);
      message.success('Notion 会话已删除');
    } catch (error) {
      message.error(errorText(error, '删除 Notion 会话失败'));
    } finally {
      setDeletingThreadID('');
    }
  };

  const messages = detail?.messages || [];
  const knownConversationTotal = Math.max(conversationTotal, rows.length);
  const overview = [
    { label: conversationHasMore ? '已知会话' : '会话总数', value: knownConversationTotal, suffix: conversationHasMore ? '条+' : '条' },
    { label: '当前页', value: rows.length, suffix: '条' },
    { label: '消息数', value: messages.length, suffix: '条' },
    { label: '工作区', value: spaces.length, suffix: '个' }
  ];

  return (
    <div className="conversations-page">
      <div className="page-header">
        <div>
          <Title level={3} className="accounts-title">
            Notion 会话
          </Title>
          <Text type="secondary">选择账号和工作区后，读取该 workspace 下的当前 Notion 会话列表。</Text>
        </div>
        <Button
          icon={<ReloadOutlined />}
          loading={loadingConversations}
          disabled={!selectedAccount || !selectedSpaceID}
          onClick={() => void loadConversations(true, conversationPage, conversationPageSize)}
        >
          刷新会话
        </Button>
      </div>

      <Card className="conversations-toolbar-card">
        <Flex wrap gap={16} align="center">
          <Select
            loading={loadingAccounts}
            placeholder="选择账号"
            value={selectedAccountID}
            onChange={setSelectedAccountID}
            className="conversations-select"
            options={accounts.map((account) => ({
              value: account.id,
              label: `${account.email}${account.active ? ' · 当前' : ''}${account.enabled ? '' : ' · 停用'}`
            }))}
          />
          <Select
            loading={loadingSpaces}
            placeholder="选择工作区"
            value={selectedSpaceID || undefined}
            disabled={!selectedAccount}
            onChange={setSelectedSpaceID}
            className="conversations-select"
            options={spaces.map((space) => ({
              value: space.space_id,
              label: `${spaceLabel(space)}${space.current ? ' · 当前' : ''}`
            }))}
          />
          <Text type="secondary">最后刷新：{fmtTime(lastLoadedAt)}</Text>
        </Flex>
      </Card>

      {spacesError ? <Alert className="conversations-alert" type="warning" showIcon message="工作区列表加载失败" description={spacesError} /> : null}
      {remoteError ? <Alert className="conversations-alert" type="error" showIcon message="Notion 会话加载失败" description={remoteError} /> : null}

      <Row gutter={[16, 16]} className="conversations-overview">
        {overview.map((item) => (
          <Col key={item.label} xs={12} lg={6}>
            <Card>
              <Statistic title={item.label} value={item.value} suffix={item.suffix} />
            </Card>
          </Col>
        ))}
      </Row>

      <Row gutter={[16, 16]} align="stretch">
        <Col xs={24} xl={9}>
          <Card
            title={
              <Space>
                <HistoryOutlined />
                当前会话列表
              </Space>
            }
            extra={<Tag color="blue">{`${rows.length}/${knownConversationTotal}`}</Tag>}
            className="conversations-fill-card"
          >
            <Spin spinning={loadingConversations}>
              {rows.length ? (
                <>
                  <List<NotionConversationSummary>
                    dataSource={rows}
                    renderItem={(item) => {
                      const active = item.thread_id === selectedThreadID;
                      return (
                        <List.Item className="conversations-list-row">
                          <button
                            type="button"
                            className={active ? 'conversation-list-item conversation-list-item-active' : 'conversation-list-item'}
                            onClick={() => setSelectedThreadID(item.thread_id)}
                          >
                            <Flex justify="space-between" gap={12} align="start">
                              <div className="conversation-list-title">{item.title || 'Untitled conversation'}</div>
                              <Tag color={statusColor(item.status)}>{item.status || 'unknown'}</Tag>
                            </Flex>
                            <Flex justify="space-between" gap={12} className="conversation-list-meta">
                              <Text type="secondary">{item.created_by_display_name || item.transport || 'transcript_sync'}</Text>
                              <Text type="secondary">{fmtTime(item.updated_at || item.created_at)}</Text>
                            </Flex>
                          </button>
                        </List.Item>
                      );
                    }}
                  />
                  {knownConversationTotal > conversationPageSize ? (
                    <Pagination
                      className="conversations-list-pagination"
                      current={conversationPage}
                      pageSize={conversationPageSize}
                      total={knownConversationTotal}
                      pageSizeOptions={[10, 20, 50, 100]}
                      showSizeChanger
                      showTotal={(total, range) => `${range[0]}-${range[1]} / ${conversationHasMore ? `至少 ${total}` : total}`}
                      size="small"
                      onChange={(page, pageSize) => void loadConversations(false, page, pageSize)}
                    />
                  ) : null}
                </>
              ) : (
                <Empty description={selectedAccount && selectedSpaceID ? '当前工作区没有会话' : '先选择账号和工作区'} />
              )}
            </Spin>
          </Card>
        </Col>

        <Col xs={24} xl={15}>
          {selectedThreadID ? (
            <Card
              title={detail?.title || selectedSummary?.title || '会话详情'}
              extra={
                <Popconfirm title="删除 Notion 会话？" description="该操作会同步软删除远端 thread。" onConfirm={() => void deleteConversation(selectedThreadID)}>
                  <Button danger icon={<DeleteOutlined />} loading={deletingThreadID === selectedThreadID}>
                    删除
                  </Button>
                </Popconfirm>
              }
              className="conversations-fill-card"
            >
              <Spin spinning={loadingDetail}>
                {detail ? (
                  <Space direction="vertical" size={16} className="conversations-detail-stack">
                    <Descriptions bordered size="small" column={{ xs: 1, md: 2 }}>
                      <Descriptions.Item label="账号">{detail.account_email || selectedAccount?.email || '-'}</Descriptions.Item>
                      <Descriptions.Item label="工作区">{selectedSpace?.name || detail.space_id || '-'}</Descriptions.Item>
                      <Descriptions.Item label="Thread">{detail.thread_id}</Descriptions.Item>
                      <Descriptions.Item label="来源">{detail.origin}</Descriptions.Item>
                      <Descriptions.Item label="创建时间">{fmtTime(detail.created_at)}</Descriptions.Item>
                      <Descriptions.Item label="更新时间">{fmtTime(detail.updated_at)}</Descriptions.Item>
                    </Descriptions>

                    {messages.length ? (
                      <div className="conversation-message-list">
                        {messages.map((item, index) => (
                          <ConversationMessageItem key={item.id || `${item.role}-${index}`} item={item} />
                        ))}
                      </div>
                    ) : (
                      <Empty description="该会话没有可显示消息" />
                    )}
                  </Space>
                ) : (
                  <Empty description="请选择左侧会话查看详情" />
                )}
              </Spin>
            </Card>
          ) : (
            <Card className="conversations-fill-card">
              <Empty description="点击左侧会话查看详情" />
            </Card>
          )}
        </Col>
      </Row>
    </div>
  );
}

function ConversationMessageItem({ item }: { item: NotionConversationMessage }) {
  return (
    <div className={item.role === 'user' ? 'conversation-message conversation-message-user' : 'conversation-message'}>
      <Flex justify="space-between" gap={12} align="center">
        <Tag color={roleColor(item.role)}>{item.role || 'message'}</Tag>
        <Text type="secondary">{fmtTime(item.updated_at || item.created_at)}</Text>
      </Flex>
      <div className="conversation-message-content">{item.content || (item.attachments?.length ? '[附件]' : '[无文本内容]')}</div>
      {item.attachments?.length ? (
        <Space wrap className="conversation-attachments">
          {item.attachments.map((attachment, index) => (
            <Tag key={`${attachment.name || 'attachment'}-${index}`}>{attachment.name || attachment.content_type || 'attachment'}</Tag>
          ))}
        </Space>
      ) : null}
    </div>
  );
}
