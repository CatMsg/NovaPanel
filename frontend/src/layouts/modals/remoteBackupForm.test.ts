import { describe, expect, it } from 'vitest'
import { backupSettingsDirty, backupSettingsPayload, type BackupForm } from './remoteBackupForm'

const form: BackupForm = { enabled: false, intervalMinutes: 1440, retention: 7, destination: 'https://dav.example/backups/', username: 'test', hasPassword: true }
describe('remote backup write-only settings', () => {
  it('never sends a masked or empty saved password', () => {
    expect(backupSettingsPayload(form, '', false)).not.toHaveProperty('password')
    expect(backupSettingsPayload(form, '', false)).not.toHaveProperty('hasPassword')
    expect(backupSettingsPayload(form, 'new-secret', false).password).toBe('new-secret')
    expect(backupSettingsPayload(form, 'new-secret', true)).not.toHaveProperty('password')
  })
  it('requires saving changed credentials or settings before remote operations', () => {
    expect(backupSettingsDirty(form, { ...form }, '', false)).toBe(false)
    expect(backupSettingsDirty(form, form, 'new-secret', false)).toBe(true)
    expect(backupSettingsDirty(form, form, '', true)).toBe(true)
    expect(backupSettingsDirty({ ...form, destination: 'https://other.example/' }, form, '', false)).toBe(true)
    expect(backupSettingsDirty({ ...form, enabled: true }, form, '', false)).toBe(true)
  })
})
