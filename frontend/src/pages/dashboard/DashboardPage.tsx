import { Card, Col, Row, Statistic, Tag, App as AntApp } from 'antd';
import { useEffect, useState } from 'react';
import { adminApi } from '../../services/adminApi';
import type { ApiKey, HealthResponse, UsageRow } from '../../types/api';

export function DashboardPage() {
  const { message } = AntApp.useApp();
  const [health, setHealth] = useState<HealthResponse | null>(null);
  const [keys, setKeys] = useState<ApiKey[]>([]);
  const [usage, setUsage] = useState<UsageRow[]>([]);

  useEffect(() => {
    Promise.all([adminApi.health(), adminApi.apiKeys(), adminApi.usage('?limit=20')])
      .then(([healthRes, keyRes, usageRes]) => {
        setHealth(healthRes);
        setKeys(keyRes.data);
        setUsage(usageRes.data);
      })
      .catch((error) => message.error(error instanceof Error ? error.message : '加载失败'));
  }, [message]);

  const totalTokens = usage.reduce((sum, row) => sum + row.total_tokens, 0);

  return (
    <>
      <div className="page-header">
        <h1 className="page-title">仪表盘</h1>
        <Tag color={health?.ok ? 'green' : 'red'}>{health?.database_driver ?? 'unknown'}</Tag>
      </div>
      <Row gutter={16}>
        <Col span={8}>
          <Card>
            <Statistic title="API Key" value={keys.length} />
          </Card>
        </Col>
        <Col span={8}>
          <Card>
            <Statistic title="启用 Key" value={keys.filter((key) => key.enabled).length} />
          </Card>
        </Col>
        <Col span={8}>
          <Card>
            <Statistic title="近期 token" value={totalTokens} />
          </Card>
        </Col>
      </Row>
    </>
  );
}

