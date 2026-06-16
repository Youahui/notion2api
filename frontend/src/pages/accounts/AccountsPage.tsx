import { PlusOutlined } from '@ant-design/icons';
import { Button, Form, Input, InputNumber, Modal, Popconfirm, Select, Space, Switch, Table, Tag, App as AntApp } from 'antd';
import { useEffect, useState } from 'react';
import { adminApi } from '../../services/adminApi';
import type { Account, AccountPayload } from '../../types/api';

export function AccountsPage() {
  const { message, modal } = AntApp.useApp();
  const [rows, setRows] = useState<Account[]>([]);
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<Account | null>(null);
  const [form] = Form.useForm<AccountPayload>();

  const load = () => {
    adminApi.accounts()
      .then((res) => setRows(res.data))
      .catch((error) => message.error(error instanceof Error ? error.message : '加载失败'));
  };

  useEffect(load, []);

  const openEditor = (row?: Account) => {
    setEditing(row ?? null);
    form.resetFields();
    form.setFieldsValue(row ? {
      email: row.email,
      enabled: row.enabled,
      active: row.active,
      user_id: row.user_id,
      user_name: row.user_name,
      space_id: row.space_id,
      space_view_id: row.space_view_id,
      space_name: row.space_name,
      client_version: row.client_version,
      status: row.status,
      priority: row.priority,
      max_concurrency: row.max_concurrency,
      proxy_mode: row.proxy_mode,
      proxy_url: row.proxy_url,
      proxy_http_url: row.proxy_http_url,
      proxy_https_url: row.proxy_https_url,
      probe_json: row.probe_json
    } : { enabled: true, active: false, status: 'ready', max_concurrency: 1, priority: 0, proxy_mode: 'off' });
    setOpen(true);
  };

  const submit = async (values: AccountPayload) => {
    const payload = Object.fromEntries(Object.entries(values).filter(([, value]) => value !== undefined && value !== '')) as AccountPayload;
    if (editing) {
      await adminApi.updateAccount(editing.id, payload);
      message.success('账号已更新');
    } else {
      await adminApi.createAccount(payload);
      message.success('账号已创建');
    }
    setOpen(false);
    load();
  };

  return (
    <>
      <div className="page-header">
        <h1 className="page-title">账号池</h1>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => openEditor()}>
          新建
        </Button>
      </div>
      <Table
        rowKey="id"
        dataSource={rows}
        columns={[
          {
            title: '账号',
            dataIndex: 'email',
            render: (email: string, row) => (
              <Space direction="vertical" size={0}>
                <span>{email}</span>
                <span className="muted">{row.space_name || row.space_id || '-'}</span>
              </Space>
            )
          },
          {
            title: '状态',
            render: (_, row) => (
              <Space>
                {row.active ? <Tag color="blue">当前</Tag> : null}
                <Tag color={row.enabled ? 'green' : 'default'}>{row.enabled ? '启用' : '停用'}</Tag>
                <Tag>{row.status || 'unknown'}</Tag>
              </Space>
            )
          },
          {
            title: '凭证',
            render: (_, row) => (
              <Space>
                <Tag color={row.has_auth_token ? 'green' : 'default'}>Token</Tag>
                <Tag color={row.has_cookie ? 'green' : 'default'}>Cookie</Tag>
              </Space>
            )
          },
          { title: '优先级', dataIndex: 'priority' },
          { title: '并发', dataIndex: 'max_concurrency' },
          { title: '成功/失败', render: (_, row) => `${row.total_successes}/${row.total_failures}` },
          { title: '最近成功', dataIndex: 'last_success_at', render: (value?: string) => value ? new Date(value).toLocaleString() : '-' },
          {
            title: '操作',
            render: (_, row) => (
              <Space>
                <Button size="small" onClick={() => openEditor(row)}>编辑</Button>
                <Button size="small" onClick={async () => { await adminApi.activateAccount(row.id); load(); }}>激活</Button>
                <Button
                  size="small"
                  onClick={async () => {
                    const result = await adminApi.testAccount(row.id, {});
                    modal.info({ title: '账号测试结果', content: result.text, width: 640 });
                    load();
                  }}
                >
                  测试
                </Button>
                <Popconfirm title="删除账号" onConfirm={async () => { await adminApi.deleteAccount(row.id); load(); }}>
                  <Button size="small" danger>删除</Button>
                </Popconfirm>
              </Space>
            )
          }
        ]}
      />
      <Modal title={editing ? '编辑账号' : '新建账号'} open={open} onCancel={() => setOpen(false)} onOk={() => form.submit()} width={760}>
        <Form form={form} layout="vertical" onFinish={submit}>
          <Form.Item name="email" label="邮箱" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Space align="start" style={{ width: '100%' }} size={16}>
            <Form.Item name="enabled" label="启用" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item name="active" label="设为当前" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item name="status" label="状态">
              <Select style={{ width: 140 }} options={[
                { value: 'ready', label: 'ready' },
                { value: 'inactive', label: 'inactive' },
                { value: 'error', label: 'error' }
              ]} />
            </Form.Item>
            <Form.Item name="priority" label="优先级">
              <InputNumber style={{ width: 120 }} />
            </Form.Item>
            <Form.Item name="max_concurrency" label="最大并发">
              <InputNumber min={1} style={{ width: 120 }} />
            </Form.Item>
          </Space>
          <Space align="start" style={{ width: '100%' }} size={16}>
            <Form.Item name="user_name" label="用户名" style={{ flex: 1 }}>
              <Input />
            </Form.Item>
            <Form.Item name="user_id" label="User ID" style={{ flex: 1 }}>
              <Input />
            </Form.Item>
          </Space>
          <Space align="start" style={{ width: '100%' }} size={16}>
            <Form.Item name="space_name" label="空间名" style={{ flex: 1 }}>
              <Input />
            </Form.Item>
            <Form.Item name="space_id" label="Space ID" style={{ flex: 1 }}>
              <Input />
            </Form.Item>
            <Form.Item name="space_view_id" label="Space View ID" style={{ flex: 1 }}>
              <Input />
            </Form.Item>
          </Space>
          <Form.Item name="client_version" label="Notion Client Version">
            <Input placeholder="23.13.0.0" />
          </Form.Item>
          <Form.Item name="auth_token" label={editing?.has_auth_token ? 'Auth Token（留空保持不变）' : 'Auth Token'}>
            <Input.Password />
          </Form.Item>
          <Form.Item name="cookie" label={editing?.has_cookie ? 'Cookie（留空保持不变）' : 'Cookie'}>
            <Input.TextArea rows={3} />
          </Form.Item>
          {editing ? (
            <Space>
              <Form.Item name="clear_auth_token" valuePropName="checked">
                <Switch checkedChildren="清 token" unCheckedChildren="保留 token" />
              </Form.Item>
              <Form.Item name="clear_cookie" valuePropName="checked">
                <Switch checkedChildren="清 cookie" unCheckedChildren="保留 cookie" />
              </Form.Item>
            </Space>
          ) : null}
          <Space align="start" style={{ width: '100%' }} size={16}>
            <Form.Item name="proxy_mode" label="代理模式" style={{ flex: 1 }}>
              <Select options={[
                { value: 'off', label: 'off' },
                { value: 'http', label: 'http' },
                { value: 'https', label: 'https' },
                { value: 'socks5', label: 'socks5' }
              ]} />
            </Form.Item>
            <Form.Item name="proxy_url" label="代理 URL" style={{ flex: 2 }}>
              <Input />
            </Form.Item>
          </Space>
          <Form.Item name="probe_json" label="Probe JSON">
            <Input.TextArea rows={3} />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}
