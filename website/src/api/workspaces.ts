import client from 'src/api/client'

export interface Workspace {
  id: string
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
