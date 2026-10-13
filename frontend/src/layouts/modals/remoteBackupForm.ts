export type BackupForm = {
  enabled: boolean
  intervalMinutes: number
  retention: number
  destination: string
  username: string
  hasPassword: boolean
}

export function backupSettingsPayload(form: BackupForm, password: string, clearPassword: boolean) {
  return {
    enabled: form.enabled, intervalMinutes: Number(form.intervalMinutes), retention: Number(form.retention),
    destination: form.destination, username: form.username, clearPassword,
    ...(password && !clearPassword ? { password } : {}),
  }
}

export function backupSettingsDirty(form: BackupForm, saved: BackupForm, password: string, clearPassword: boolean): boolean {
  return !!password || clearPassword || JSON.stringify(backupSettingsPayload(form, '', false)) !== JSON.stringify(backupSettingsPayload(saved, '', false))
}
