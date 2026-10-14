import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/react'

import AuditLog from './AuditLog'
import * as client from '../api/client'
import { t } from '../i18n'
import type { AuditEntry } from '../types'

vi.mock('../api/client', () => ({
  listAuditEntries: vi.fn(),
}))

const listAuditEntries = vi.mocked(client.listAuditEntries)

const entries: AuditEntry[] = [
  {
    time: '2026-10-14T02:10:00Z',
    actor: 'operator:9f2c',
    method: 'POST',
    path: '/v1/admin/servers/game-1001/weight',
    status: 200,
    body: { weight: 200 },
  },
  {
    time: '2026-10-14T02:12:30Z',
    actor: 'operator:9f2c',
    method: 'GET',
    path: '/v1/admin/servers',
    status: 200,
    query: 'limit=50',
  },
]

const loose = (key: Parameters<typeof t>[0]) => new RegExp([...t(key)].join('\\s*'))

beforeEach(() => {
  vi.clearAllMocks()
  listAuditEntries.mockResolvedValue({ entries })
})

afterEach(() => cleanup())

describe('AuditLog 操作审计', () => {
  it('渲染时间 / 操作者 / 方法 / 路径 / 状态码', async () => {
    render(<AuditLog />)
    expect(await screen.findByText('/v1/admin/servers/game-1001/weight')).toBeTruthy()
    expect(screen.getAllByText('operator:9f2c').length).toBe(2)
    expect(screen.getByText(t('auditWrite'))).toBeTruthy()
    expect(screen.getByText(t('auditRead'))).toBeTruthy()
  })

  it('只读请求没有变更体，写请求按 JSON 展开', async () => {
    render(<AuditLog />)
    await screen.findByText('/v1/admin/servers/game-1001/weight')
    // GET 行没有 body 列（列在有 body 的行渲染为空），POST 行给出可读 JSON
    expect(screen.getByText(/"weight": 200/)).toBeTruthy()
  })

  it('4xx 与 5xx 分色标出，2xx 走成功色', async () => {
    listAuditEntries.mockResolvedValue({
      entries: [
        { time: '2026-10-14T03:00:00Z', actor: 'op', method: 'DELETE', path: '/v1/admin/servers/x', status: 404 },
        { time: '2026-10-14T03:01:00Z', actor: 'op', method: 'POST', path: '/v1/admin/servers/y', status: 500 },
        { time: '2026-10-14T03:02:00Z', actor: 'op', method: 'POST', path: '/v1/admin/servers/z', status: 204 },
      ],
    })
    render(<AuditLog />)
    await screen.findByText('/v1/admin/servers/x')
    const tagFor = (code: string) => screen.getByText(code).closest('.ant-tag')?.className ?? ''
    expect(tagFor('404')).toContain('ant-tag-volcano')
    expect(tagFor('500')).toContain('ant-tag-red')
    expect(tagFor('204')).toContain('ant-tag-green')
  })

  it('空审计流渲染空态而不是报错', async () => {
    listAuditEntries.mockResolvedValue({ entries: [] })
    render(<AuditLog />)
    expect(await screen.findByText(t('noData'))).toBeTruthy()
  })

  it('拉取失败时给出重试入口', async () => {
    listAuditEntries.mockRejectedValue(new Error('boom'))
    render(<AuditLog />)
    expect(await screen.findByText(t('auditLoadFailed'))).toBeTruthy()
    expect(screen.getByRole('button', { name: loose('retry') })).toBeTruthy()
  })

  it('刷新重新拉取审计流', async () => {
    render(<AuditLog />)
    await screen.findByText('/v1/admin/servers/game-1001/weight')
    fireEvent.click(screen.getByRole('button', { name: loose('refresh') }))
    expect(listAuditEntries).toHaveBeenCalledTimes(2)
  })

  it('改条数重新按新 limit 拉取', async () => {
    render(<AuditLog />)
    await screen.findByText('/v1/admin/servers/game-1001/weight')
    fireEvent.mouseDown(screen.getByRole('combobox'))
    fireEvent.click(await screen.findByTitle('200'))
    expect(listAuditEntries).toHaveBeenLastCalledWith({ limit: 200 })
  })
})