import { PlusOutlined } from '@ant-design/icons';
import { Button, Form, Input, InputNumber, Modal, Popconfirm, Space, Switch, Table, Tag, Typography, App as AntApp } from 'antd';
import { useEffect, useState } from 'react';
import { adminApi } from '../../services/adminApi';
import type { ApiKey } from '../../types/api';

export function ApiKeysPage() {
  const { message, modal } = AntApp.useApp();
  const [rows, setRows] = useState<ApiKey[]>([]);
  const [open, setOpen] = useState(false);
  const [form] = Form.useForm();

  const load = () => {
    adminApi.apiKeys()
      .then((res) => setRows(res.data))
      .catch((error) => message.error(error instanceof Error ? error.message : '加载失败'));
  };

  useEffect(load, []);

  return (
    <>
      <div className="page-header">
        <h1 className="page-title">API Key</h1>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
          新建
        </Button>
      </div>
      <Table
        rowKey="id"
        dataSource={rows}
        columns={[
          { title: '名称', dataIndex: 'name' },
          { title: '前缀', dataIndex: 'key_prefix' },
          { title: '后缀', dataIndex: 'key_suffix' },
          {
            title: '状态',
            dataIndex: 'enabled',
            render: (enabled: boolean) => <Tag color={enabled ? 'green' : 'default'}>{enabled ? '启用' : '禁用'}</Tag>
          },
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
                <Popconfirm title="删除 API Key" onConfirm={async () => { await adminApi.deleteApiKey(row.id); load(); }}>
                  <Button size="small" danger>
                    删除
                  </Button>
                </Popconfirm>
              </Space>
            )
          }
        ]}
      />
      <Modal title="新建 API Key" open={open} onCancel={() => setOpen(false)} onOk={() => form.submit()}>
        <Form
          form={form}
          layout="vertical"
          initialValues={{ enabled: true, token_quota_total: 0, token_quota_daily: 0 }}
          onFinish={async (values) => {
            const created = await adminApi.createApiKey(values);
            setOpen(false);
            form.resetFields();
            load();
            modal.info({
              title: '完整 Key 仅显示一次',
              content: <Typography.Text copyable>{created.secret}</Typography.Text>,
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

