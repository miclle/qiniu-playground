import client from 'src/api/client'

export type SandboxFileType = 'file' | 'dir' | 'unknown'

export interface SandboxFileEntry {
  name: string
  type: SandboxFileType
  path: string
  size: number
  mode?: number
  permissions?: string
  owner?: string
  group?: string
  modified_time?: string
  symlink_target?: string
}

export function sandboxFiles(sandboxID: string, path: string, depth = 1) {
  return client.get<{ entries: SandboxFileEntry[] }>(`/sandboxes/${sandboxID}/filesystem`, {
    params: { path, depth },
  })
}

export function sandboxFileContent(sandboxID: string, path: string) {
  return client.get<Blob>(`/sandboxes/${sandboxID}/filesystem/content`, {
    params: { path },
    responseType: 'blob',
  })
}

export function sandboxFilePreviewURL(sandboxID: string, path: string) {
  return `/api/v1/sandboxes/${encodeURIComponent(sandboxID)}/preview${encodedPreviewPath(path)}`
}

export function workspaceFilePreviewURL(workspaceID: string, path: string) {
  return `/api/v1/workspaces/${encodeURIComponent(workspaceID)}/preview${encodedPreviewPath(path)}`
}

function encodedPreviewPath(path: string) {
  const normalizedPath = path.startsWith('/') ? path : `/${path}`
  return normalizedPath
    .split('/')
    .map((segment) => encodeURIComponent(segment))
    .join('/')
}
