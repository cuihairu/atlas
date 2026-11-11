import { Tag } from 'antd';

const STATUS_MAP: Record<string, { color: string; label: string }> = {
  online: { color: 'green', label: '在线' },
  maintenance: { color: 'orange', label: '维护中' },
  suspect: { color: 'gold', label: '可疑' },
  offline: { color: 'red', label: '离线' },
  draining: { color: 'purple', label: '排水中' },
};

export default function StatusTag({ status }: { status: string }) {
  const cfg = STATUS_MAP[status] ?? { color: 'default', label: status };
  return <Tag color={cfg.color}>{cfg.label}</Tag>;
}