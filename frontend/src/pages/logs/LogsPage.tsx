import { Table, Tag, App as AntApp } from 'antd';
import { useEffect, useState } from 'react';
import { adminApi } from '../../services/adminApi';
import type { RequestLog } from '../../types/api';

export function LogsPage() {
  const { message } = AntApp.useApp();
  const [rows, setRows] = useState<RequestLog[]>([]);

  useEffect(() => {
    adminApi.logs()
      .then((res) => setRows(res.data))
      .catch((error) => message.error(error instanceof Error ? error.message : '加载失败'));
  }, [message]);

  return (
    <>
      <div className="page-header">
        <h1 className="page-title">请求日志</h1>
      </div>
      <Table
        rowKey="id"
        dataSource={rows}
        columns={[
          { title: '时间', dataIndex: 'created_at', render: (value: string) => new Date(value).toLocaleString() },
          { title: '方法', dataIndex: 'method' },
          { title: '路由', dataIndex: 'route' },
          { title: '状态', dataIndex: 'status_code', render: (value: number) => <Tag color={value < 400 ? 'green' : 'red'}>{value}</Tag> },
          { title: '耗时 ms', dataIndex: 'latency_ms' },
          { title: '模型', dataIndex: 'model' },
          { title: 'token', dataIndex: 'total_tokens' },
          { title: '估算', dataIndex: 'estimated', render: (value: boolean) => value ? <Tag color="gold">是</Tag> : '否' }
        ]}
      />
    </>
  );
}

