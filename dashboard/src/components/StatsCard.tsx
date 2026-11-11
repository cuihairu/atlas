import { Card, Statistic } from 'antd';
import type { ReactNode } from 'react';

interface Props {
  title: string;
  value: number | string;
  prefix?: ReactNode;
  suffix?: string;
  loading?: boolean;
}

export default function StatsCard({ title, value, prefix, suffix, loading }: Props) {
  return (
    <Card loading={loading} style={{ height: '100%' }}>
      <Statistic title={title} value={value} prefix={prefix} suffix={suffix} />
    </Card>
  );
}