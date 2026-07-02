import client from 'src/api/client'

export interface SandboxTemplate {
  template_id: string
  aliases?: string[]
  build_status?: string
  cpu_count?: number
  memory_mb?: number
  disk_size_mb?: number
  public: boolean
  default: boolean
}

export function sandboxTemplates(region?: string) {
  return client.get<{ default_template_id: string; templates: SandboxTemplate[] }>('/templates', {
    params: region ? { region } : undefined,
  })
}
