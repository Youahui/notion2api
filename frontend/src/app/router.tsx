import { createBrowserRouter, Navigate } from 'react-router-dom';
import { AdminLayout } from '../layouts/AdminLayout';
import { LoginPage } from '../pages/login/LoginPage';
import { DashboardPage } from '../pages/dashboard/DashboardPage';
import { AdminsPage } from '../pages/admins/AdminsPage';
import { ApiKeysPage } from '../pages/api-keys/ApiKeysPage';
import { AccountsPage } from '../pages/accounts/AccountsPage';
import { GroupsPage } from '../pages/groups/GroupsPage';
import { ModelsPage } from '../pages/models/ModelsPage';
import { ConversationsPage } from '../pages/conversations/ConversationsPage';
import { SettingsPage } from '../pages/settings/SettingsPage';
import { TesterPage } from '../pages/tester/TesterPage';
import { LogsPage } from '../pages/logs/LogsPage';
import { UsagePage } from '../pages/usage/UsagePage';

export const router = createBrowserRouter([
  { path: '/', element: <Navigate to="/admin" replace /> },
  { path: '/admin/login', element: <LoginPage /> },
  {
    path: '/admin',
    element: <AdminLayout />,
    children: [
      { index: true, element: <DashboardPage /> },
      { path: 'admins', element: <AdminsPage /> },
      { path: 'api-keys', element: <ApiKeysPage /> },
      { path: 'accounts', element: <Navigate to="/admin/accounts/list" replace /> },
      { path: 'accounts/:tab', element: <AccountsPage /> },
      { path: 'groups', element: <GroupsPage /> },
      { path: 'models', element: <ModelsPage /> },
      { path: 'conversations', element: <ConversationsPage /> },
      { path: 'settings', element: <SettingsPage /> },
      { path: 'tester', element: <TesterPage /> },
      { path: 'logs', element: <LogsPage /> },
      { path: 'usage', element: <UsagePage /> }
    ]
  }
]);
