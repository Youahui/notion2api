import { LockOutlined, UserOutlined } from '@ant-design/icons';
import { Button, Card, Form, Input, Typography, App as AntApp } from 'antd';
import { useNavigate } from 'react-router-dom';
import { adminApi } from '../../services/adminApi';

export function LoginPage() {
  const navigate = useNavigate();
  const { message } = AntApp.useApp();

  return (
    <div style={{ minHeight: '100vh', display: 'grid', placeItems: 'center', background: '#f6f8fb', padding: 24 }}>
      <Card style={{ width: 380 }}>
        <Typography.Title level={3} style={{ marginTop: 0 }}>
          notaion2api
        </Typography.Title>
        <Form
          layout="vertical"
          initialValues={{ username: 'admin' }}
          onFinish={async (values: { username: string; password: string }) => {
            try {
              await adminApi.login(values.username, values.password);
              message.success('登录成功');
              navigate('/admin', { replace: true });
            } catch (error) {
              message.error(error instanceof Error ? error.message : '登录失败');
            }
          }}
        >
          <Form.Item name="username" label="用户名" rules={[{ required: true }]}>
            <Input prefix={<UserOutlined />} autoComplete="username" />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true }]}>
            <Input.Password prefix={<LockOutlined />} autoComplete="current-password" />
          </Form.Item>
          <Button type="primary" htmlType="submit" block>
            登录
          </Button>
        </Form>
      </Card>
    </div>
  );
}

