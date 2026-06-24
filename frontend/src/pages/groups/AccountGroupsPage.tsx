import { DeleteOutlined, EditOutlined, PlusOutlined, ReloadOutlined, TeamOutlined } from '@ant-design/icons';
import { App as AntApp, Button, Card, Empty, Popconfirm, Space, Table, Tag, Tooltip, Typography } from 'antd';
import { useEffect, useState } from 'react';
import { adminApi } from '../../services/adminApi';
import type { Group } from '../../types/api';
import { AccountGroupModal } from './AccountGroupModal';
import { AccountGroupMembersModal } from './AccountGroupMembersModal';

type GroupModalMode = 'create' | 'edit';

type GroupWithMemberCount = Group & {
  member_count?: number;
};

function groupAccountCount(group: Group) {
  const row = group as GroupWithMemberCount;
  return row.member_count ?? row.account_count ?? 0;
}

function groupDescription(group: Group) {
  return group.description?.trim() || '-';
}

export function AccountGroupsPage() {
  const { message } = AntApp.useApp();
  const [groups, setGroups] = useState<Group[]>([]);
  const [loading, setLoading] = useState(false);
  const [groupModalOpen, setGroupModalOpen] = useState(false);
  const [groupModalMode, setGroupModalMode] = useState<GroupModalMode>('create');
  const [editingGroup, setEditingGroup] = useState<Group | null>(null);
  const [memberModalOpen, setMemberModalOpen] = useState(false);
  const [memberGroup, setMemberGroup] = useState<Group | null>(null);

  const loadGroups = async () => {
    setLoading(true);
    try {
      const res = await adminApi.groups();
      setGroups(res.data);
    } catch (error) {
      message.error(error instanceof Error ? error.message : '加载账号组失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void loadGroups();
  }, []);

  const openGroupModal = (mode: GroupModalMode, group?: Group) => {
    setGroupModalMode(mode);
    setEditingGroup(group ?? null);
    setGroupModalOpen(true);
  };

  const openMemberModal = (group: Group) => {
    setMemberGroup(group);
    setMemberModalOpen(true);
  };

  const handleDeleteGroup = async (group: Group) => {
    if (group.is_default) return;
    try {
      await adminApi.deleteGroup(group.id);
      message.success('账号组已删除');
      await loadGroups();
    } catch (error) {
      message.error(error instanceof Error ? error.message : '删除账号组失败');
    }
  };

  return (
    <>
      <div className="page-header">
        <h1 className="page-title">账号组</h1>
        <Space>
          <Button icon={<ReloadOutlined />} loading={loading} onClick={() => void loadGroups()}>
            刷新
          </Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={() => openGroupModal('create')}>
            新建
          </Button>
        </Space>
      </div>

      <Card>
        <Table<Group>
          className="groups-list-table"
          rowKey="id"
          loading={loading}
          dataSource={groups}
          locale={{ emptyText: <Empty description="暂无账号组" /> }}
          pagination={false}
          columns={[
            {
              title: '名称',
              dataIndex: 'name',
              render: (value: string, row) => (
                <Space>
                  <Typography.Text strong>{value}</Typography.Text>
                  {row.is_default ? <Tag color="blue">默认组</Tag> : null}
                </Space>
              )
            },
            {
              title: '状态',
              dataIndex: 'enabled',
              width: 120,
              render: (enabled: boolean) => <Tag color={enabled ? 'green' : 'default'}>{enabled ? '启用' : '禁用'}</Tag>
            },
            {
              title: '描述',
              dataIndex: 'description',
              render: (_: string, row) => (
                <Typography.Text className="groups-description-text" type="secondary" ellipsis={{ tooltip: groupDescription(row) }}>
                  {groupDescription(row)}
                </Typography.Text>
              )
            },
            {
              title: '账号数',
              width: 120,
              render: (_, row) => groupAccountCount(row)
            },
            {
              title: '操作',
              width: 260,
              render: (_, row) => (
                <Space>
                  <Button type="link" icon={<EditOutlined />} onClick={() => openGroupModal('edit', row)}>
                    编辑
                  </Button>
                  <Button type="link" icon={<TeamOutlined />} onClick={() => openMemberModal(row)}>
                    成员
                  </Button>
                  {row.is_default ? (
                    <Tooltip title="默认组不可删除">
                      <Button type="link" danger disabled icon={<DeleteOutlined />}>
                        删除
                      </Button>
                    </Tooltip>
                  ) : (
                    <Popconfirm
                      title="删除账号组"
                      description="绑定到该组的 API Key 会自动回到默认组。"
                      okText="删除"
                      cancelText="取消"
                      onConfirm={() => void handleDeleteGroup(row)}
                    >
                      <Button type="link" danger icon={<DeleteOutlined />}>
                        删除
                      </Button>
                    </Popconfirm>
                  )}
                </Space>
              )
            }
          ]}
        />
      </Card>

      <AccountGroupModal
        open={groupModalOpen}
        mode={groupModalMode}
        group={editingGroup}
        onCancel={() => {
          setGroupModalOpen(false);
          setEditingGroup(null);
        }}
        onSaved={() => void loadGroups()}
      />

      <AccountGroupMembersModal
        open={memberModalOpen}
        group={memberGroup}
        onCancel={() => {
          setMemberModalOpen(false);
          setMemberGroup(null);
        }}
        onSaved={() => void loadGroups()}
      />
    </>
  );
}
