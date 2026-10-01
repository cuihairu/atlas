import { useState } from 'react';
import { Form, Input, Select, InputNumber, Button, Table, Card, Row, Col } from 'antd';
import { searchCharacters } from '../api/client';
import type { Character } from '../types';

export default function Characters() {
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
      <Card title="角色搜索" style={{ marginBottom: 24 }}>
        <Form form={form} layout="inline" onFinish={() => onSearch()} style={{ flexWrap: 'wrap', gap: 8 }}>
          <Form.Item name="q" label="角色名">
            <Input placeholder="角色名" allowClear style={{ width: 160 }} />
          </Form.Item>
          <Form.Item name="server_id" label="服务器">
            <Input placeholder="服务器ID" allowClear style={{ width: 200 }} />
          </Form.Item>
          <Form.Item name="class_id" label="职业">
            <Select
              allowClear
              placeholder="选择职业"
              style={{ width: 120 }}
              options={[
                { label: '战士', value: 'warrior' },
                { label: '法师', value: 'mage' },
                { label: '牧师', value: 'priest' },
                { label: '盗贼', value: 'rogue' },
                { label: '猎人', value: 'hunter' },
                { label: '术士', value: 'warlock' },
                { label: '德鲁伊', value: 'druid' },
                { label: '圣骑士', value: 'paladin' },
              ]}
            />
          </Form.Item>
          <Form.Item name="min_level" label="最低等级">
            <InputNumber min={1} max={100} style={{ width: 100 }} />
          </Form.Item>
          <Form.Item name="max_level" label="最高等级">
            <InputNumber min={1} max={100} style={{ width: 100 }} />
          </Form.Item>
          <Form.Item>
            <Button type="primary" htmlType="submit" loading={loading}>
              搜索
            </Button>
          </Form.Item>
          <Form.Item>
            <Button onClick={onReset}>重置</Button>
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
            { title: '账户ID', dataIndex: 'account_id', ellipsis: true },
            { title: '服务器ID', dataIndex: 'server_id', ellipsis: true },
            { title: '角色ID', dataIndex: 'character_id', ellipsis: true },
            { title: '名称', dataIndex: 'name' },
            { title: '等级', dataIndex: 'level', width: 80 },
            { title: '职业', dataIndex: 'class_id', width: 100 },
            {
              title: '最后登录',
              dataIndex: 'last_login',
              render: (v?: string) => (v ? new Date(v).toLocaleString('zh-CN') : '-'),
            },
          ]}
          footer={() =>
            nextCursor ? (
              <Button onClick={() => { setCursor(nextCursor); onSearch(); }}>
                加载更多
              </Button>
            ) : null
          }
        />
      )}
    </>
  );
}