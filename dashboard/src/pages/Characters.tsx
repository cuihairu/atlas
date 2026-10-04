// 角色搜索（管理台）：角色名 / 服务器 / 玩家 ID（不透明 account 引用）/
// 元数据键值对 / 等级区间。职业等游戏业务概念不内建筛选（平台去硬编码
// 裁决，item 10）：用元数据过滤表达，如 class=warrior、vip_level=6。
import { useState } from 'react';
import { Form, Input, InputNumber, Button, Table, Card, Space, Typography } from 'antd';
import { useLang, t } from '../i18n';
import { searchCharacters } from '../api/client';
import type { Character } from '../types';

export default function Characters() {
  useLang(); // re-render on language switch
  const [form] = Form.useForm();
  const [characters, setCharacters] = useState<Character[]>([]);
  const [loading, setLoading] = useState(false);
  const [cursor, setCursor] = useState<string | undefined>();
  const [nextCursor, setNextCursor] = useState<string | undefined>();

  const onSearch = async (values?: Record<string, unknown>) => {
    setLoading(true);
    try {
      const v = values ?? form.getFieldsValue();
      const res = await searchCharacters({
        q: v.q as string | undefined,
        server_id: v.server_id as string | undefined,
        account_id: v.account_id as string | undefined,
        metadata_key: v.metadata_key as string | undefined,
        metadata_value: v.metadata_value as string | undefined,
        min_level: v.min_level as number | undefined,
        max_level: v.max_level as number | undefined,
        limit: 50,
        cursor,
      });
      setCharacters(res.characters ?? []);
      setNextCursor(res.next_cursor);
    } finally {
      setLoading(false);
    }
  };

  const onReset = () => {
    form.resetFields();
    setCharacters([]);
    setNextCursor(undefined);
    setCursor(undefined);
  };

  const metaKey = Form.useWatch('metadata_key', form);

  return (
    <>
      <Card title={t('characterSearch')} style={{ marginBottom: 24 }}>
        <Form form={form} layout="inline" onFinish={() => onSearch()} style={{ flexWrap: 'wrap', gap: 8 }}>
          <Form.Item name="account_id" label={t('accountIdSearch')}>
            <Input placeholder={t('accountId')} allowClear style={{ width: 140 }} />
          </Form.Item>
          <Form.Item name="q" label={t('characterName')}>
            <Input placeholder={t('characterName')} allowClear style={{ width: 140 }} />
          </Form.Item>
          <Form.Item name="server_id" label={t('server')}>
            <Input placeholder={t('serverId')} allowClear style={{ width: 170 }} />
          </Form.Item>
          <Form.Item label={t('metadataFilter')}>
            <Space.Compact>
              <Form.Item name="metadata_key" noStyle>
                <Input placeholder={t('metadataKey')} allowClear style={{ width: 120 }} />
              </Form.Item>
              <Form.Item name="metadata_value" noStyle>
                <Input
                  placeholder={t('metadataValue')}
                  allowClear
                  style={{ width: 110 }}
                  disabled={!metaKey}
                />
              </Form.Item>
            </Space.Compact>
          </Form.Item>
          <Form.Item name="min_level" label={t('minLevel')}>
            <InputNumber min={1} max={100} style={{ width: 90 }} />
          </Form.Item>
          <Form.Item name="max_level" label={t('maxLevel')}>
            <InputNumber min={1} max={100} style={{ width: 90 }} />
          </Form.Item>
          <Form.Item>
            <Button type="primary" htmlType="submit" loading={loading}>
              {t('search')}
            </Button>
          </Form.Item>
          <Form.Item>
            <Button onClick={onReset}>{t('reset')}</Button>
          </Form.Item>
        </Form>
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          {t('metadataExampleHint')}
        </Typography.Text>
      </Card>

      {characters.length > 0 && (
        <Table
          dataSource={characters}
          rowKey="character_id"
          loading={loading}
          pagination={false}
          columns={[
            { title: t('accountId'), dataIndex: 'account_id', ellipsis: true },
            { title: t('serverId'), dataIndex: 'server_id', ellipsis: true },
            { title: t('characterId'), dataIndex: 'character_id', ellipsis: true },
            { title: t('name'), dataIndex: 'name' },
            { title: t('level'), dataIndex: 'level', width: 70 },
            {
              title: t('metadata'),
              dataIndex: 'metadata',
              ellipsis: true,
              render: (m?: Record<string, string>) =>
                m && Object.keys(m).length > 0
                  ? Object.entries(m).map(([k, v]) => (
                      <Typography.Text key={k} code style={{ fontSize: 12, marginRight: 6 }}>
                        {k}={v}
                      </Typography.Text>
                    ))
                  : '—',
            },
            {
              title: t('lastLogin'),
              dataIndex: 'last_login_at',
              render: (v?: string | null) => (v ? new Date(v).toLocaleString() : '-'),
            },
          ]}
          footer={() =>
            nextCursor ? (
              <Button onClick={() => { setCursor(nextCursor); onSearch(); }}>
                {t('loadMore')}
              </Button>
            ) : null
          }
        />
      )}
    </>
  );
}
