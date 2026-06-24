import { DeleteOutlined, InfoCircleOutlined, RollbackOutlined, SaveOutlined } from '@ant-design/icons';
import { App as AntApp, Button, Empty, InputNumber, Modal, Popconfirm, Select, Space, Switch, Table, Tag, Tooltip, Typography } from 'antd';
import { useEffect, useMemo, useState } from 'react';
import { adminApi } from '../../services/adminApi';
import type { Account, Group, GroupMember } from '../../types/api';

interface AccountGroupMembersModalProps {
  open: boolean;
  group: Group | null;
  onCancel: () => void;
  onSaved: () => void;
}

function normalizeMembers(list: GroupMember[]) {
  return list
    .map((item) => ({
      account_id: item.account_id,
      enabled: Boolean(item.enabled),
      priority: Number(item.priority || 0)
    }))
    .sort((a, b) => String(a.account_id).localeCompare(String(b.account_id)));
}

function isSameMembers(a: GroupMember[], b: GroupMember[]) {
  return JSON.stringify(normalizeMembers(a)) === JSON.stringify(normalizeMembers(b));
}

function cloneMembers(rows: GroupMember[]) {
  return rows.map((row) => ({ ...row }));
}

function sameAccountID(left: number, right: number) {
  return String(left) === String(right);
}

function accountOptionLabel(account: Account) {
  return [account.email, account.user_name].filter(Boolean).join(' · ');
}

function accountSearchText(account: Account) {
  return [account.email, account.user_name, account.user_id].filter(Boolean).join(' ').toLowerCase();
}

function fmtTime(value?: string | null) {
  if (!value) return '-';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '-' : date.toLocaleString();
}

function accountStatusColor(status?: string) {
  const value = (status || '').toLowerCase();
  if (value === 'ready' || value === 'active') return 'green';
  if (value === 'disabled') return 'default';
  if (value === 'error' || value === 'failed') return 'red';
  return 'default';
}

export function AccountGroupMembersModal({ open, group, onCancel, onSaved }: AccountGroupMembersModalProps) {
  const { message, modal } = AntApp.useApp();
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [originalMembers, setOriginalMembers] = useState<GroupMember[]>([]);
  const [memberDraftList, setMemberDraftList] = useState<GroupMember[]>([]);
  const [allAccounts, setAllAccounts] = useState<Account[]>([]);
  const [selectedAccountId, setSelectedAccountId] = useState<number>();
  const [memberPage, setMemberPage] = useState(1);
  const [memberPageSize, setMemberPageSize] = useState(10);

  const membersDirty = useMemo(() => !isSameMembers(originalMembers, memberDraftList), [memberDraftList, originalMembers]);
  const availableAccounts = useMemo(() => {
    const joinedIds = new Set(memberDraftList.map((item) => String(item.account_id)));
    return allAccounts.filter((account) => !joinedIds.has(String(account.id)));
  }, [allAccounts, memberDraftList]);

  const loadMemberData = async (targetGroup: Group) => {
    setLoading(true);
    try {
      const [memberRes, accountRes] = await Promise.all([adminApi.groupMembers(targetGroup.id), adminApi.accounts()]);
      const nextMembers = cloneMembers(memberRes.data);
      setOriginalMembers(nextMembers);
      setMemberDraftList(cloneMembers(nextMembers));
      setAllAccounts(accountRes.data);
      setSelectedAccountId(undefined);
      setMemberPage(1);
    } catch (error) {
      message.error(error instanceof Error ? error.message : '加载成员失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (!open || !group) return;
    void loadMemberData(group);
  }, [group?.id, open]);

  const closeModal = () => {
    setSelectedAccountId(undefined);
    setMemberPage(1);
    onCancel();
  };

  const requestClose = () => {
    if (!membersDirty) {
      closeModal();
      return;
    }
    modal.confirm({
      title: '关闭成员管理',
      content: '当前成员变更尚未保存，关闭后将丢失，是否继续？',
      okText: '继续关闭',
      cancelText: '继续编辑',
      onOk: closeModal
    });
  };

  const handleAddMember = () => {
    if (!selectedAccountId || memberDraftList.some((member) => sameAccountID(member.account_id, selectedAccountId))) return;
    const account = allAccounts.find((row) => sameAccountID(row.id, selectedAccountId));
    if (!account) return;
    const nextList = [
      ...memberDraftList,
      {
        account_id: account.id,
        email: account.email,
        enabled: true,
        priority: 0,
        status: account.status ?? '',
        last_used_at: account.last_used_at ?? null
      }
    ];
    setMemberDraftList(nextList);
    setSelectedAccountId(undefined);
    setMemberPage(Math.max(1, Math.ceil(nextList.length / memberPageSize)));
  };

  const handleRemoveMember = (accountID: number) => {
    const nextList = memberDraftList.filter((member) => !sameAccountID(member.account_id, accountID));
    setMemberDraftList(nextList);
    setMemberPage((current) => Math.min(current, Math.max(1, Math.ceil(nextList.length / memberPageSize))));
  };

  const handleMemberEnabledChange = (accountID: number, enabled: boolean) => {
    setMemberDraftList((current) => current.map((member) => (sameAccountID(member.account_id, accountID) ? { ...member, enabled } : member)));
  };

  const handleMemberPriorityChange = (accountID: number, priority: number | null) => {
    setMemberDraftList((current) =>
      current.map((member) => (sameAccountID(member.account_id, accountID) ? { ...member, priority: Number(priority ?? 0) } : member))
    );
  };

  const handleResetMembers = () => {
    setMemberDraftList(cloneMembers(originalMembers));
    setSelectedAccountId(undefined);
    setMemberPage(1);
  };

  const handleSaveMembers = async () => {
    if (!group) return;
    setSaving(true);
    try {
      const payload = memberDraftList.map((member) => ({
        account_id: member.account_id,
        enabled: Boolean(member.enabled),
        priority: member.priority ?? 0
      }));
      const res = await adminApi.updateGroupMembers(group.id, payload);
      const nextMembers = cloneMembers(res.data);
      setOriginalMembers(nextMembers);
      setMemberDraftList(cloneMembers(nextMembers));
      setSelectedAccountId(undefined);
      message.success('成员变更已保存');
      onSaved();
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存成员变更失败');
    } finally {
      setSaving(false);
    }
  };

  return (
    <Modal
      title={`管理成员 - ${group?.name ?? ''}`}
      open={open}
      width={1100}
      destroyOnClose
      maskClosable
      footer={
        <div className="groups-member-footer">
          <Space>
            <Button icon={<RollbackOutlined />} disabled={!membersDirty || saving} onClick={handleResetMembers}>
              还原成员
            </Button>
            <Button type="primary" icon={<SaveOutlined />} loading={saving} disabled={!membersDirty || saving} onClick={() => void handleSaveMembers()}>
              保存成员变更
            </Button>
          </Space>
        </div>
      }
      onCancel={requestClose}
    >
      <Space direction="vertical" size={16} className="groups-member-stack">
        <Space direction="vertical" size={8} className="groups-member-stack">
          <Typography.Title level={5} className="groups-section-title">
            添加成员账号
          </Typography.Title>
          <Space.Compact className="groups-add-member-row">
            <Select
              showSearch
              value={selectedAccountId}
              disabled={!availableAccounts.length}
              loading={loading}
              placeholder={availableAccounts.length ? '请选择要加入的账号' : '暂无可添加账号'}
              optionFilterProp="searchText"
              filterOption={(input, option) => String(option?.searchText ?? '').includes(input.trim().toLowerCase())}
              onChange={setSelectedAccountId}
              options={availableAccounts.map((account) => ({
                value: account.id,
                label: `${accountOptionLabel(account)}${account.enabled ? '' : '（账号已禁用）'}`,
                searchText: accountSearchText(account)
              }))}
            />
            <Button type="primary" disabled={!selectedAccountId} onClick={handleAddMember}>
              添加到当前组
            </Button>
          </Space.Compact>
        </Space>

        <Table<GroupMember>
          rowKey="account_id"
          size="small"
          loading={loading}
          dataSource={memberDraftList}
          locale={{ emptyText: <Empty description="当前账号组暂无成员账号" /> }}
          pagination={{
            current: memberPage,
            pageSize: memberPageSize,
            total: memberDraftList.length,
            showSizeChanger: true,
            pageSizeOptions: ['10', '20', '50'],
            showTotal: (total) => `共 ${total} 个成员`,
            onChange: (page, pageSize) => {
              setMemberPage(page);
              setMemberPageSize(pageSize);
            }
          }}
          columns={[
            { title: '账号', dataIndex: 'email' },
            {
              title: '账号状态',
              dataIndex: 'status',
              width: 120,
              render: (value: string) => <Tag color={accountStatusColor(value)}>{value || '-'}</Tag>
            },
            {
              title: '组内启用',
              dataIndex: 'enabled',
              width: 110,
              render: (_: boolean, row) => (
                <Switch size="small" checked={Boolean(row.enabled)} onChange={(checked) => handleMemberEnabledChange(row.account_id, checked)} />
              )
            },
            {
              title: (
                <Space size={4}>
                  调度优先级
                  <Tooltip title="数值越大越优先。同一账号组内先按组内调度优先级降序选择；相同再按 active、账号优先级、最近使用时间排序。">
                    <InfoCircleOutlined className="groups-priority-help" />
                  </Tooltip>
                </Space>
              ),
              dataIndex: 'priority',
              width: 130,
              render: (value: number, row) => (
                <InputNumber
                  min={0}
                  precision={0}
                  value={value}
                  style={{ width: 100 }}
                  onChange={(next) => handleMemberPriorityChange(row.account_id, next)}
                />
              )
            },
            { title: '最近使用时间', dataIndex: 'last_used_at', width: 180, render: fmtTime },
            {
              title: '操作',
              width: 90,
              render: (_, row) => (
                <Popconfirm
                  title="移除成员账号"
                  description={memberDraftList.length <= 1 ? '删除后该账号组将没有可用账号，确定继续吗？' : '确定从当前账号组移除该账号吗？'}
                  okText="删除"
                  cancelText="取消"
                  onConfirm={() => handleRemoveMember(row.account_id)}
                >
                  <Button type="link" danger icon={<DeleteOutlined />}>
                    删除
                  </Button>
                </Popconfirm>
              )
            }
          ]}
        />
      </Space>
    </Modal>
  );
}
