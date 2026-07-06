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
