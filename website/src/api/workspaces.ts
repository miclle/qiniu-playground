import client from 'src/api/client'

export interface Workspace {
  id: string
  created_at?: string
  updated_at?: string
  name?: string
  github_repo_id?: number
  repo_full_name?: string
  region: string
  sandbox_id?: string
  template_id: string
  state?: string
  endpoint?: string
  workspace_path?: string
  ide_url?: string
}

export function workspaces() {
  return client.get<{ workspaces: Workspace[] }>('/workspaces')
}

export interface CreateWorkspacePayload {
  name?: string
  region: string
  template_id: string
}

export function createWorkspace(payload: CreateWorkspacePayload) {
  return client.post<Workspace>('/workspaces', payload)
}

export interface ConnectWorkspaceOptions {
  recreate?: boolean
}

export function connectWorkspace(workspaceID: string, options?: ConnectWorkspaceOptions) {
  return client.post<Workspace>(`/workspaces/${workspaceID}/connect`, options)
}

export function heartbeatWorkspace(workspaceID: string) {
  return client.post<{ ok: boolean, timeout_seconds: number }>(`/workspaces/${workspaceID}/heartbeat`)
}

export function pauseWorkspaceSandbox(workspaceID: string, options?: { keepalive?: boolean }) {
  if (options?.keepalive && typeof fetch === 'function') {
    return fetch(`/api/v1/workspaces/${workspaceID}/pause`, {
      method: 'POST',
      credentials: 'include',
      keepalive: true,
    }).then((response) => {
      if (!response.ok) {
        throw new Error(`pause workspace sandbox failed with status ${response.status}`)
      }
      return response
    })
  }
  return client.post<Workspace>(`/workspaces/${workspaceID}/pause`)
}
