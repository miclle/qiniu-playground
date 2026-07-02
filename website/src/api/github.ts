import client from 'src/api/client'
import type { Workspace } from 'src/api/workspaces'

export interface GitHubRepository {
  id: string
  installation_id: number
  github_repo_id: number
  owner: string
  name: string
  full_name: string
  private: boolean
  default_branch: string
  html_url: string
}

export interface GitHubInstallation {
  id: string
  installation_id: number
  account_id: string
}

export function githubAppInstall() {
  return client.get<{ url: string }>('/github/app/install')
}

export function githubInstallations() {
  return client.get<{ installations: GitHubInstallation[] }>('/github/installations')
}

export function githubRepositories() {
  return client.get<{ repositories: GitHubRepository[] }>('/github/repositories')
}

export interface OpenRepositoryPayload {
  repositoryID: string
  name?: string
  region: string
  template_id: string
}

export function openRepository({ repositoryID, ...payload }: OpenRepositoryPayload) {
  return client.post<Workspace>(`/repositories/${repositoryID}/open`, payload)
}
