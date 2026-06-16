import { ApiOutlined, BarChartOutlined, DashboardOutlined, DatabaseOutlined, KeyOutlined, LogoutOutlined, RobotOutlined, SettingOutlined, TeamOutlined, UnorderedListOutlined } from '@ant-design/icons';
import { Button, Layout, Menu, Space, Typography, App as AntApp } from 'antd';
import { useMemo } from 'react';
import { Link, Navigate, Outlet, useLocation, useNavigate } from 'react-router-dom';
import { getAdminToken, setAdminToken } from '../services/apiClient';
import { adminApi } from '../services/adminApi';

const { Header, Content, Sider } = Layout;

export function AdminLayout() {
  const token = getAdminToken();
  const location = useLocation();
  const navigate = useNavigate();
  const { message } = AntApp.useApp();

  const selectedKeys = useMemo(() => {
    const parts = location.pathname.split('/').filter(Boolean);
    return [parts[1] ?? 'dashboard'];
  }, [location.pathname]);

  if (!token) {
    return <Navigate to="/admin/login" replace />;
  }

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider width={232}>
        <div style={{ height: 56, display: 'flex', alignItems: 'center', padding: '0 20px', color: '#fff', fontWeight: 700 }}>
          notaion2api
        </div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={selectedKeys}
          items={[
            { key: 'dashboard', icon: <DashboardOutlined />, label: <Link to="/admin">仪表盘</Link> },
            { key: 'admins', icon: <TeamOutlined />, label: <Link to="/admin/admins">管理员</Link> },
            { key: 'api-keys', icon: <KeyOutlined />, label: <Link to="/admin/api-keys">API Key</Link> },
            { key: 'usage', icon: <BarChartOutlined />, label: <Link to="/admin/usage">用量</Link> },
            { key: 'models', icon: <RobotOutlined />, label: <Link to="/admin/models">模型</Link> },
            { key: 'accounts', icon: <DatabaseOutlined />, label: <Link to="/admin/accounts">账号池</Link> },
            { key: 'conversations', icon: <ApiOutlined />, label: <Link to="/admin/conversations">会话</Link> },
            { key: 'tester', icon: <ApiOutlined />, label: <Link to="/admin/tester">测试台</Link> },
            { key: 'logs', icon: <UnorderedListOutlined />, label: <Link to="/admin/logs">日志</Link> },
            { key: 'settings', icon: <SettingOutlined />, label: <Link to="/admin/settings">设置</Link> }
          ]}
        />
      </Sider>
      <Layout>
        <Header style={{ height: 56, padding: '0 24px', display: 'flex', alignItems: 'center', justifyContent: 'space-between', borderBottom: '1px solid #edf0f5' }}>
          <Typography.Text strong>管理控制台</Typography.Text>
          <Space>
            <Button
              icon={<LogoutOutlined />}
              onClick={async () => {
                await adminApi.logout().catch(() => setAdminToken(''));
                message.success('已退出');
                navigate('/admin/login', { replace: true });
              }}
            >
              退出
            </Button>
          </Space>
        </Header>
        <Content style={{ padding: 24 }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
}
