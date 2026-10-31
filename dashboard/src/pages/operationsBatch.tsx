// 舰队级批量维护窗口：一份时段模板铺到多台服务器（POST
// /v1/admin/maintenance-windows/batch）。三个运维语义在这里收口：
//   - 错峰：第 i 台的窗口整体后移 i × stagger_seconds，滚动维护；
//   - 预览：dry_run 只校验并返回计划中的窗口，不落库；
//   - 部分成功：未知/非法服务器逐台进 failed，其余照常创建。
// 表单值 → 请求体的拼装与结果渲染抽到这里，便于不依赖 antd 表单单测
// （RangePicker 的取值交互在 jsdom 里不可靠，逻辑单测比 UI 单测稳）。
import { Table, Tag, Alert, Typography } from 'antd';
import dayjs, { type Dayjs } from 'dayjs';
import { t } from '../i18n';
import { createMaintenanceWindowsBatch } from '../api/client';
import type { MaintenanceWindowBatchRequest, MaintenanceWindowBatchResponse } from '../types';

export interface BatchFormValues {
  server_ids?: string[];
  stagger_seconds?: number | null;
  announce?: boolean;
  dry_run?: boolean;
}

/**
 * 表单值 → 批量请求体。服务器 ID 去空白、去空项、去重保序；缺省项与后端
 * 口径一致（announce=true / stagger=0 / dry_run=false），前端显式下发以免
 * 依赖服务端缺省。错峰取整秒，避免浮点下发。
 */
export function buildBatchRequest(
  values: BatchFormValues,
  range: [Dayjs, Dayjs],
): MaintenanceWindowBatchRequest {
  const seen = new Set<string>();
  const serverIds: string[] = [];
  for (const raw of values.server_ids ?? []) {
    const id = (raw ?? '').trim();
    if (!id || seen.has(id)) continue;
    seen.add(id);
    serverIds.push(id);
  }
  const stagger = values.stagger_seconds ?? 0;
  return {
    server_ids: serverIds,
    start_at: range[0].toISOString(),
    end_at: range[1].toISOString(),
    stagger_seconds: stagger > 0 ? Math.floor(stagger) : 0,
    announce: values.announce ?? true,
    dry_run: values.dry_run ?? false,
  };
}

/** 调批量端点并补齐缺省字段（后端两数组恒返回，防御旧版本回退）。 */
export async function submitBatch(
  values: BatchFormValues,
  range: [Dayjs, Dayjs],
): Promise<MaintenanceWindowBatchResponse> {
  const res = await createMaintenanceWindowsBatch(buildBatchRequest(values, range));
  return { windows: res?.windows ?? [], failed: res?.failed ?? [] };
}

type OutcomeRow = {
  key: string;
  server_id: string;
  window: string;
  ok: boolean;
  error?: string;
};

function fmtTime(v?: string | null): string {
  if (!v) return '—';
  return dayjs(v).format('MM-DD HH:mm');
}

/** 批量结果：成功与失败合到一张表，运营一眼看清哪台没排上。 */
export function BatchOutcome({
  result,
  dryRun,
}: {
  result: MaintenanceWindowBatchResponse;
  dryRun: boolean;
}) {
  const rows: OutcomeRow[] = [
    ...result.windows.map((w, i) => ({
      key: `ok-${w.id || w.server_id}-${i}`,
      server_id: w.server_id,
      window: `${fmtTime(w.start_at)} → ${fmtTime(w.end_at)}`,
      ok: true,
    })),
    ...result.failed.map((f, i) => ({
      key: `fail-${f.server_id}-${i}`,
      server_id: f.server_id,
      window: '—',
      ok: false,
      error: f.error,
    })),
  ];

  return (
    <div>
      {dryRun && (
        <Alert type="info" showIcon message={t('winBatchPreviewHint')} style={{ marginBottom: 12 }} />
      )}
      {!dryRun && result.failed.length > 0 && (
        <Alert
          type="warning"
          showIcon
          message={`${t('winBatchPartialMsg')}（${result.failed.length}）`}
          style={{ marginBottom: 12 }}
        />
      )}
      <Table
        scroll={{ x: 'max-content' }}
        rowKey="key"
        size="small"
        dataSource={rows}
        pagination={false}
        locale={{ emptyText: t('noData') }}
        columns={[
          { title: t('serverId'), dataIndex: 'server_id', width: 150, ellipsis: true },
          { title: t('winWindow'), dataIndex: 'window', width: 200 },
          {
            title: t('winBatchOutcome'),
            key: 'outcome',
            render: (_: unknown, row: OutcomeRow) =>
              row.ok ? (
                <Tag color="green">{t('winBatchOk')}</Tag>
              ) : (
                <Tag color="red">
                  {t('winBatchFailed')}
                  <Typography.Text type="secondary" style={{ marginLeft: 6 }}>
                    {row.error}
                  </Typography.Text>
                </Tag>
              ),
          },
        ]}
      />
    </div>
  );
}