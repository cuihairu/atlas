import { useState } from 'react';
import { Form, Input, Select, InputNumber, Button, Table, Card } from 'antd';
import { useLang, t } from '../i18n';
import { searchCharacters } from '../api/client';
import type { Character } from '../types';

const CLASS_OPTIONS = [
  { value: 'warrior', key: 'classWarrior' },
  { value: 'mage', key: 'classMage' },
  { value: 'priest', key: 'classPriest' },
  { value: 'rogue', key: 'classRogue' },
  { value: 'hunter', key: 'classHunter' },
  { value: 'warlock', key: 'classWarlock' },
  { value: 'druid', key: 'classDruid' },
  { value: 'paladin', key: 'classPaladin' },
] as const;

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
        class_id: v.class_id as string | undefined,
        min_level: v.min_level as number | undefined,
        max_level: v.max_level as number | undefined,
        limit: 50,
        cursor,
      });
      setCharacters(res.characters);
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

  return (
    <>
      <Card title={t('characterSearch')} style={{ marginBottom: 24 }}>
        <Form form={form} layout="inline" onFinish={() => onSearch()} style={{ flexWrap: 'wrap', gap: 8 }}>
          <Form.Item name="q" label={t('characterName')}>
            <Input placeholder={t('characterName')} allowClear style={{ width: 160 }} />
          </Form.Item>
          <Form.Item name="server_id" label={t('server')}>
            <Input placeholder={t('serverId')} allowClear style={{ width: 200 }} />
          </Form.Item>
          <Form.Item name="class_id" label={t('class')}>
            <Select
              allowClear
              placeholder={t('selectClass')}
              style={{ width: 120 }}
              options={CLASS_OPTIONS.map((o) => ({ label: t(o.key), value: o.value }))}
            />
          </Form.Item>
          <Form.Item name="min_level" label={t('minLevel')}>
            <InputNumber min={1} max={100} style={{ width: 100 }} />
          </Form.Item>
          <Form.Item name="max_level" label={t('maxLevel')}>
            <InputNumber min={1} max={100} style={{ width: 100 }} />
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
            { title: t('level'), dataIndex: 'level', width: 80 },
            { title: t('class'), dataIndex: 'class_id', width: 100 },
            {
              title: t('lastLogin'),
              dataIndex: 'last_login',
              render: (v?: string) => (v ? new Date(v).toLocaleString() : '-'),
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
