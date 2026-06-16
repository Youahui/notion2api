import { Table, Tag, App as AntApp } from 'antd';
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
        columns={[
          { title: '日期', dataIndex: 'date' },
          { title: 'API 密钥 ID', dataIndex: 'api_key_id' },
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
