import { PlusOutlined } from '@ant-design/icons';
import { Button, Form, Input, InputNumber, Modal, Popconfirm, Select, Space, Switch, Table, Tag, Typography, App as AntApp } from 'antd';
import { useEffect, useState } from 'react';
import { adminApi } from '../../services/adminApi';
import type { ApiKey, Group } from '../../types/api';

const apiKeyDisplayValue = (row: ApiKey) => {
  if (row.api_key) {
    return row.api_key;
  }
  if (row.api_key_masked) {
    return row.api_key_masked;
  }
  if (row.key_prefix || row.key_suffix) {
    return `${row.key_prefix || ''}...${row.key_suffix || ''}`;
  }
  return '';
};

export function ApiKeysPage() {
  const { message, modal } = AntApp.useApp();
  const [rows, setRows] = useState<ApiKey[]>([]);
  const [groups, setGroups] = useState<Group[]>([]);
  const [open, setOpen] = useState(false);
  const [form] = Form.useForm();

  const load = () => {
    Promise.all([adminApi.apiKeys(), adminApi.groups()])
      .then(([keyRes, groupRes]) => {
        setRows(keyRes.data);
        setGroups(groupRes.data);
      })
      .catch((error) => message.error(error instanceof Error ? error.message : '加载失败'));
  };

  useEffect(load, []);

  return (
    <>
      <div className="page-header">
        <h1 className="page-title">API 密钥</h1>
        <Button
          type="primary"
          icon={<PlusOutlined />}
          onClick={() => {
            form.setFieldsValue({ group_id: groups.find((group) => group.is_default)?.id ?? groups[0]?.id });
            setOpen(true);
          }}
        >
          新建
        </Button>
      </div>
      <Table
        rowKey="id"
        dataSource={rows}
        columns={[
          { title: '名称', dataIndex: 'name' },
          {
            title: 'API 密钥',
            dataIndex: 'api_key',
            render: (_value: string, row) => {
              const displayValue = apiKeyDisplayValue(row);
              if (!displayValue) {
                return '-';
              }
              return (
                <Space direction="vertical" size={0}>
                  <Typography.Text code copyable={{ text: displayValue }} ellipsis style={{ maxWidth: 360 }}>
                    {displayValue}
                  </Typography.Text>
                  {!row.api_key && <Typography.Text type="secondary">历史密钥缺少明文，无法完整展示</Typography.Text>}
                </Space>
              );
            }
          },
          {
            title: '状态',
            dataIndex: 'enabled',
            render: (enabled: boolean) => <Tag color={enabled ? 'green' : 'default'}>{enabled ? '启用' : '禁用'}</Tag>
          },
          { title: '账号组', dataIndex: 'group_name', render: (value: string) => value || '-' },
          { title: '请求数', dataIndex: 'total_requests' },
          { title: 'token', dataIndex: 'total_tokens' },
          { title: '最近使用', dataIndex: 'last_used_at', render: (value?: string) => value ? new Date(value).toLocaleString() : '-' },
          {
            title: '操作',
            render: (_, row) => (
              <Space>
                <Button
                  size="small"
                  onClick={async () => {
                    row.enabled ? await adminApi.disableApiKey(row.id) : await adminApi.enableApiKey(row.id);
                    load();
                  }}
                >
                  {row.enabled ? '禁用' : '启用'}
                </Button>
                <Popconfirm title="删除 API 密钥" onConfirm={async () => { await adminApi.deleteApiKey(row.id); load(); }}>
                  <Button size="small" danger>
                    删除
                  </Button>
                </Popconfirm>
              </Space>
            )
          }
        ]}
      />
      <Modal title="新建 API 密钥" open={open} onCancel={() => setOpen(false)} onOk={() => form.submit()}>
        <Form
          form={form}
          layout="vertical"
          initialValues={{ enabled: true, token_quota_total: 0, token_quota_daily: 0 }}
          onFinish={async (values) => {
            const created = await adminApi.createApiKey(values);
            setOpen(false);
            form.resetFields();
            const secret = created.secret || created.api_key;
            setRows((current) => [{ ...created, api_key: secret }, ...current.filter((row) => row.id !== created.id)]);
            modal.info({
              title: '完整密钥',
              content: <Typography.Text code copyable={{ text: secret }}>{secret}</Typography.Text>,
              width: 560
            });
          }}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="enabled" label="状态" valuePropName="checked">
            <Switch checkedChildren="启用" unCheckedChildren="禁用" />
          </Form.Item>
          <Form.Item name="group_id" label="账号组" rules={[{ required: true, message: '请选择账号组' }]}>
            <Select
              options={groups.filter((group) => group.enabled).map((group) => ({
                value: group.id,
                label: group.is_default ? `${group.name}（默认）` : group.name
              }))}
            />
          </Form.Item>
          <Form.Item name="token_quota_total" label="总 token 额度">
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="token_quota_daily" label="每日 token 额度">
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}
