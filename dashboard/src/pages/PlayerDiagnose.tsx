// 玩家视角排查（item 7，排查/诊断菜单）：输入玩家账号 ID（可选带上
// 客户端过滤条件复现推荐请求），页面展示这条管线会怎么决策——推荐
// 冠军、匹配阶段、逐台判定（排序/命中/可入选/归因）与该账号的目录
// 条目。后端是同一条 Recommend 管线摊开中间结果（/v1/admin/diagnose/
// routing），本页不另写判据。
import { useState } from 'react';
import {
  Card, Form, Input, Button, Table, Tag, Descriptions, Result, Space, Typography, Alert,
} from 'antd';
import { SearchOutlined } from '@ant-design/icons';
import { useLang, t } from '../i18n';
import { diagnoseRouting } from '../api/client';
import type { RoutingDiagnosis, Character, DiagnosisServerVerdict } from '../types';

export default function PlayerDiagnose() {
  useLang();
  const [form] = Form.useForm();
  const [diagnosis, setDiagnosis] = useState<RoutingDiagnosis | null>(null);
  const [characters, setCharacters] = useState<Character[]>([]);
  const [loading, setLoading] = useState(false);
  const [searched, setSearched] = useState(false);

  const run = async (v: Record<string, unknown>) => {
    setLoading(true);
    try {
      const res = await diagnoseRouting({
        account_id: (v.account_id as string) || undefined,
        region: (v.region as string) || undefined,
        version: (v.version as string) || undefined,
        platform: (v.platform as string) || undefined,
      });
      setDiagnosis(res.diagnosis);
      setCharacters(res.characters ?? []);
      setSearched(true);
    } catch {
      // request() 已提示
    } finally {
      setLoading(false);
    }
  };

  const winner = diagnosis?.servers.find((s) => s.server?.id === diagnosis?.winner_id);

  const stageTag = (stage: string) => {
    if (stage === 'strict') return <Tag color="green">{t('strictStage')}</Tag>;
    if (stage === 'fallback') return <Tag color="orange">{t('fallbackStage')}</Tag>;
    return <Tag color="red">{t('noData')}</Tag>;
  };

  const verdictColumns = [
    {
      title: t('rank'),
      dataIndex: 'rank',
      width: 60,
      render: (v: number, row: DiagnosisServerVerdict) =>
        row.server?.id === diagnosis?.winner_id ? (
          <Tag color="gold">{t('diagnoseWinner')}</Tag>
        ) : v > 0 ? v : '—',
    },
    {
      title: t('serverId'),
      render: (_: unknown, row: DiagnosisServerVerdict) => row.server?.id ?? '—',
      ellipsis: true,
    },
    {
      title: t('region'),
      width: 70,
      render: (_: unknown, row: DiagnosisServerVerdict) => row.server?.region ?? '—',
    },
    {
      title: t('status'),
      width: 90,
      render: (_: unknown, row: DiagnosisServerVerdict) => row.server?.status ?? '—',
    },
    {
      title: t('playersCapacity'),
      width: 110,
      render: (_: unknown, row: DiagnosisServerVerdict) =>
        row.server ? `${row.server.players}/${row.server.capacity}` : '—',
    },
    {
      title: t('verdict'),
      width: 220,
      render: (_: unknown, row: DiagnosisServerVerdict) => (
        <Space size={4} wrap>
          {row.matched_strict && <Tag color="green">{t('matchedStrict')}</Tag>}
          {row.matched_fallback && !row.matched_strict && <Tag color="orange">{t('matchedFallback')}</Tag>}
          {row.owned && <Tag color="geekblue">{t('ownedServer')}</Tag>}
          {row.eligible ? <Tag color="blue">{t('eligibleServer')}</Tag> : <Tag>{t('no')}</Tag>}
        </Space>
      ),
    },
    {
      title: t('verdictReason'),
      dataIndex: 'reason',
      ellipsis: true,
      render: (v?: string) => v || '—',
    },
  ];

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Card title={t('playerDiagnose')}>
        <Alert type="info" showIcon style={{ marginBottom: 16 }} message={t('diagnoseTip')} />
        <Form
          form={form}
          layout="inline"
          onFinish={run}
          style={{ flexWrap: 'wrap', gap: 8 }}
        >
          <Form.Item name="account_id" label={t('diagnoseInput')}>
            <Input placeholder={t('diagnoseInputPlaceholder')} allowClear style={{ width: 180 }} />
          </Form.Item>
          <Form.Item name="region" label={t('region')}>
            <Input placeholder="cn" allowClear style={{ width: 100 }} />
          </Form.Item>
          <Form.Item name="version" label={t('version')}>
            <Input placeholder="1.0.0" allowClear style={{ width: 100 }} />
          </Form.Item>
          <Form.Item name="platform" label={t('platform')}>
            <Input placeholder="ios" allowClear style={{ width: 90 }} />
          </Form.Item>
          <Form.Item>
            <Button type="primary" htmlType="submit" icon={<SearchOutlined />} loading={loading}>
              {t('diagnoseRun')}
            </Button>
          </Form.Item>
        </Form>
      </Card>

      {searched && diagnosis && (
        <>
          <Card title={t('loginableServers')} size="small">
            {diagnosis.winner_id ? (
              <Descriptions size="small" column={3}>
                <Descriptions.Item label={t('serverId')}>
                  <Typography.Text code>{diagnosis.winner_id}</Typography.Text>
                </Descriptions.Item>
                <Descriptions.Item label={t('verdictReason')}>
                  {diagnosis.winner_reason ?? winner?.reason ?? '—'}
                </Descriptions.Item>
                <Descriptions.Item label={t('verdict')}>{stageTag(diagnosis.stage)}</Descriptions.Item>
              </Descriptions>
            ) : (
              <Result status="warning" title={t('noData')} />
            )}
          </Card>

          <Card title={t('candidateVerdicts')} size="small">
            <Table
              rowKey={(row) => row.server?.id ?? String(row.rank)}
              size="small"
              columns={verdictColumns}
              dataSource={diagnosis.servers ?? []}
              pagination={{ pageSize: 20, hideOnSinglePage: true }}
              locale={{ emptyText: t('noData') }}
            />
          </Card>

          {diagnosis.request && (
            <Card title={t('diagnoseRequest')} size="small">
              <Descriptions size="small" column={4}>
                <Descriptions.Item label={t('accountId')}>
                  {diagnosis.request.account_id ?? '—'}
                </Descriptions.Item>
                <Descriptions.Item label={t('region')}>
                  {diagnosis.request.region || '—'}
                </Descriptions.Item>
                <Descriptions.Item label={t('version')}>
                  {diagnosis.request.version || '—'}
                </Descriptions.Item>
                <Descriptions.Item label={t('platform')}>
                  {diagnosis.request.platform || '—'}
                </Descriptions.Item>
              </Descriptions>
            </Card>
          )}

          <Card title={`${t('accountCharacters')} (${characters.length})`} size="small">
            <Table
              rowKey="character_id"
              size="small"
              dataSource={characters}
              pagination={false}
              locale={{ emptyText: t('noData') }}
              columns={[
                { title: t('serverId'), dataIndex: 'server_id', ellipsis: true },
                { title: t('characterId'), dataIndex: 'character_id', ellipsis: true },
                { title: t('name'), dataIndex: 'name' },
                { title: t('level'), dataIndex: 'level', width: 70 },
                {
                  title: t('lastLogin'),
                  dataIndex: 'last_login_at',
                  render: (v?: string | null) => (v ? new Date(v).toLocaleString() : '-'),
                },
              ]}
            />
          </Card>
        </>
      )}
    </Space>
  );
}
