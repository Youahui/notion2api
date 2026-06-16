import { PlusOutlined } from '@ant-design/icons';
import { Button, Form, Input, Modal, Popconfirm, Select, Space, Switch, Table, Tag, App as AntApp } from 'antd';
import { useEffect, useState } from 'react';
import { adminApi } from '../../services/adminApi';
import type { AdminUser } from '../../types/api';

export function AdminsPage() {
  const { message } = AntApp.useApp();
  const [rows, setRows] = useState<AdminUser[]>([]);
  const [open, setOpen] = useState(false);
  const [form] = Form.useForm();

  const load = () => {
    adminApi.admins()
      .then((res) => setRows(res.data))
      .catch((error) => message.error(error instanceof Error ? error.message : '加载失败'));
  };

  useEffect(load, []);

  return (
    <>
      <div className="page-header">
        <h1 className="page-title">管理员</h1>
        <Button type="primary" icon={<PlusOutlined />} onClick={() => setOpen(true)}>
          新建
        </Button>
      </div>
      <Table
        rowKey="id"
        dataSource={rows}
        columns={[
          { title: '用户名', dataIndex: 'username' },
          { title: '角色', dataIndex: 'role' },
          {
            title: '状态',
            dataIndex: 'enabled',
            render: (enabled: boolean) => <Tag color={enabled ? 'green' : 'default'}>{enabled ? '启用' : '停用'}</Tag>
          },
          { title: '最后登录', dataIndex: 'last_login_at', render: (value?: string) => value ? new Date(value).toLocaleString() : '-' },
          {
            title: '操作',
            render: (_, row) => (
              <Space>
                <Button
                  size="small"
                  onClick={async () => {
                    await adminApi.updateAdmin(row.id, { enabled: !row.enabled });
                    load();
                  }}
                >
                  {row.enabled ? '停用' : '启用'}
                </Button>
                <Popconfirm title="删除管理员" onConfirm={async () => { await adminApi.deleteAdmin(row.id); load(); }}>
                  <Button size="small" danger>
                    删除
                  </Button>
                </Popconfirm>
              </Space>
            )
          }
        ]}
      />
      <Modal
        title="新建管理员"
        open={open}
        onCancel={() => setOpen(false)}
        onOk={() => form.submit()}
      >
        <Form
          form={form}
          layout="vertical"
          initialValues={{ role: 'admin', enabled: true }}
          onFinish={async (values) => {
            await adminApi.createAdmin(values);
            message.success('已创建');
            setOpen(false);
            form.resetFields();
            load();
          }}
        >
          <Form.Item name="username" label="用户名" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true }]}>
            <Input.Password />
          </Form.Item>
          <Form.Item name="role" label="角色">
            <Select options={[{ value: 'admin', label: 'admin' }, { value: 'super_admin', label: 'super_admin' }]} />
          </Form.Item>
          <Form.Item name="enabled" label="状态" valuePropName="checked">
            <Switch checkedChildren="启用" unCheckedChildren="停用" />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}

