import { Table, Tag, Typography, App as AntApp } from 'antd';
import { useEffect, useState } from 'react';
import { adminApi } from '../../services/adminApi';
import type { UsageRow } from '../../types/api';

export function UsagePage() {
  const { message } = AntApp.useApp();
  const [rows, setRows] = useState<UsageRow[]>([]);

  useEffect(() => {
    adminApi.usage()
      .then((res) => setRows(res.data))
      .catch((error) => message.error(error instanceof Error ? error.message : '加载失败'));
  }, [message]);

  return (
    <>
      <div className="page-header">
        <h1 className="page-title">token 用量</h1>
      </div>
      <Table
        rowKey="id"
        dataSource={rows}
        scroll={{ x: 1200 }}
        columns={[
          { title: '日期', dataIndex: 'date' },
          { title: 'API 密钥名称', dataIndex: 'api_key_name', render: (value: string) => value || '-' },
          {
            title: 'apiKey',
            dataIndex: 'api_key',
            render: (value: string, row) => value ? (
              <Typography.Text code copyable={{ text: value }} ellipsis style={{ maxWidth: 360 }}>
                {value}
              </Typography.Text>
            ) : (
              <Typography.Text type="secondary">{row.api_key_masked || '-'}</Typography.Text>
            )
          },
          { title: '模型', dataIndex: 'model' },
          { title: '请求数', dataIndex: 'request_count' },
          { title: 'Prompt', dataIndex: 'prompt_tokens' },
          { title: 'Completion', dataIndex: 'completion_tokens' },
          { title: 'Total', dataIndex: 'total_tokens' },
          { title: '估算', dataIndex: 'estimated_tokens', render: (value: number) => value > 0 ? <Tag color="gold">估算</Tag> : '-' }
        ]}
      />
    </>
  );
}
