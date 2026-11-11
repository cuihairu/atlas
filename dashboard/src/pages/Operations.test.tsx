import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/react'

import Operations from './Operations'
import * as client from '../api/client'
import { t } from '../i18n'

vi.mock('../api/client', () => ({
  listAnnouncements: vi.fn(),
  createAnnouncement: vi.fn(),
  deleteAnnouncement: vi.fn(),
  listMaintenanceWindows: vi.fn(),
  createMaintenanceWindow: vi.fn(),
  createMaintenanceWindowsBatch: vi.fn(),
  deleteMaintenanceWindow: vi.fn(),
}))

const listAnnouncements = vi.mocked(client.listAnnouncements)
const listMaintenanceWindows = vi.mocked(client.listMaintenanceWindows)
const createMaintenanceWindowsBatch = vi.mocked(client.createMaintenanceWindowsBatch)

// antd 在中文按钮文案之间插空格，可访问名要用容忍空白的正则。
const loose = (key: Parameters<typeof t>[0]) => new RegExp([...t(key)].join('\\s*'))

beforeEach(() => {
  vi.clearAllMocks()
  listAnnouncements.mockResolvedValue({ announcements: [] })
  listMaintenanceWindows.mockResolvedValue({ maintenance_windows: [] })
  createMaintenanceWindowsBatch.mockResolvedValue({ windows: [], failed: [] })
})

afterEach(() => cleanup())

describe('Operations fleet batch windows', () => {
  it('offers the fleet batch entry next to the single-server one', async () => {
    render(<Operations />)
    await screen.findByText(t('announcements'))
    expect(screen.getByRole('button', { name: loose('winCreate') })).toBeTruthy()
    expect(screen.getByRole('button', { name: loose('winBatch') })).toBeTruthy()
  })

  it('opens the batch form with server list, stagger and preview switches', async () => {
    render(<Operations />)
    fireEvent.click(await screen.findByRole('button', { name: loose('winBatch') }))
    expect(await screen.findByText(t('winBatchStagger'))).toBeTruthy()
    expect(screen.getByText(t('winBatchServers'))).toBeTruthy()
    expect(screen.getByText(t('winBatchDryRun'))).toBeTruthy()
    expect(screen.getByText(t('winBatchTitle'))).toBeTruthy()
  })

  it('refuses to submit a batch without servers or a time range', async () => {
    render(<Operations />)
    fireEvent.click(await screen.findByRole('button', { name: loose('winBatch') }))
    await screen.findByText(t('winBatchStagger'))
    fireEvent.click(screen.getByRole('button', { name: 'OK' }))
    expect(createMaintenanceWindowsBatch).not.toHaveBeenCalled()
  })
})