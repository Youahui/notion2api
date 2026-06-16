import { Table, Tag, App as AntApp } from 'antd';
import { useEffect, useState } from 'react';
import { adminApi } from '../../services/adminApi';
import type { ModelRow } from '../../types/api';

export function ModelsPage() {
  const { message } = AntApp.useApp();
  const [rows, setRows] = useState<ModelRow[]>([]);

  useEffect(() => {
    adminApi.models()
      .then((res) => setRows(res.data))
      .catch((error) => message.error(error instanceof Error ? error.message : '加载失败'));
  }, [message]);

  return (
    <>
      <div className="page-header">
        <h1 className="page-title">模型</h1>
      </div>
      <Table
        rowKey="id"
        dataSource={rows}
        columns={[
          { title: '公开 ID', dataIndex: 'public_id' },
          { title: '名称', dataIndex: 'name' },
          { title: 'Notion 模型', dataIndex: 'notion_model' },
          { title: '家族', dataIndex: 'family' },
          { title: '分组', dataIndex: 'group' },
          { title: '状态', dataIndex: 'enabled', render: (enabled: boolean) => <Tag color={enabled ? 'green' : 'default'}>{enabled ? '启用' : '禁用'}</Tag> },
          { title: 'Beta', dataIndex: 'beta', render: (beta: boolean) => beta ? <Tag color="blue">Beta</Tag> : '-' }
        ]}
      />
    </>
  );
}

