import { Tag } from 'antd';
import { FireFilled, StopOutlined } from '@ant-design/icons';
import type { ServerTag } from '../types';

// Tier → badge style (docs/concepts.md §Server Tags): hot = hot red /
// new = new-server green / warning = full·maintenance yellow /
// info = recommended blue / neutral = neutral gray.
const TIER_COLOR: Record<string, string> = {
  hot: 'red',
  new: 'green',
  warning: 'gold',
  info: 'blue',
  neutral: 'default',
};

/**
 * Renders one server tag. The label text is operator data (display copy),
 * not UI chrome, so it is never translated.
 */
export default function TagBadge({ tag }: { tag: ServerTag }) {
  const color = TIER_COLOR[tag.tier] ?? 'default';
  const icon =
    tag.code === 'hot' ? <FireFilled /> : tag.code === 'no_register' ? <StopOutlined /> : undefined;
  return (
    <Tag color={color} icon={icon} style={{ marginInlineEnd: 4 }}>
      {tag.label}
    </Tag>
  );
}
