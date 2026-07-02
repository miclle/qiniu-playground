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
  ide_url?: string
  last_connected_at?: string
}

export function sandboxSessions() {
  return client.get<{ sandboxes: SandboxSession[] }>('/sandboxes')
}

export function createSandbox(templateID?: string) {
  return client.post<SandboxSession>('/sandboxes', templateID ? { template_id: templateID } : {})
}

export function connectSandbox(sandboxID: string) {
  return client.post<SandboxSession>(`/sandboxes/${sandboxID}/connect`)
}
