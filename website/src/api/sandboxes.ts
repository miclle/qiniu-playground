import client from 'src/api/client'

export interface SandboxSession {
  id: string
  sandbox_id: string
  template_id: string
  state: string
  endpoint?: string
  github_repo_id?: number
  repo_full_name?: string
  workspace_path?: string
  region?: string
  cpu_count?: number
  memory_gb?: number
  disk_size_mb?: number
  ide_url?: string
  local_session: boolean
  metadata?: Record<string, string>
  last_connected_at?: string
}

export interface SandboxMetric {
  timestamp: string
  timestamp_unix: number
  cpu_count: number
  cpu_used_pct: number
  mem_total: number
  mem_used: number
  disk_total: number
  disk_used: number
}

export function sandboxSessions(region?: string) {
  return client.get<{ sandboxes: SandboxSession[] }>('/sandboxes', {
    params: region ? { region } : undefined,
  })
}

export function createSandbox(options?: { templateID?: string; region?: string }) {
  return client.post<SandboxSession>('/sandboxes', {
    ...(options?.templateID ? { template_id: options.templateID } : {}),
    ...(options?.region ? { region: options.region } : {}),
  })
}

export function connectSandbox(sandboxID: string, options?: { region?: string }) {
  return client.post<SandboxSession>(`/sandboxes/${sandboxID}/connect`, {
    ...(options?.region ? { region: options.region } : {}),
  })
}

export function sandboxMetrics(sandboxID: string, options?: { start?: number, end?: number }) {
  return client.get<{ sandbox_id: string; metrics: SandboxMetric[] }>(`/sandboxes/${sandboxID}/metrics`, {
    params: options,
  })
}
