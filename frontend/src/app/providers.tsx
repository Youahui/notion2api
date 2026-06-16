import type { PropsWithChildren } from 'react';
import { App, ConfigProvider, theme } from 'antd';
import zhCN from 'antd/locale/zh_CN';

export function AppProviders({ children }: PropsWithChildren) {
  return (
    <ConfigProvider
      locale={zhCN}
      theme={{
        algorithm: theme.defaultAlgorithm,
        token: {
          colorPrimary: '#2563eb',
          borderRadius: 6
        },
        components: {
          Layout: {
            bodyBg: '#f6f8fb',
            siderBg: '#101828',
            headerBg: '#ffffff'
          }
        }
      }}
    >
      <App>{children}</App>
    </ConfigProvider>
  );
}

