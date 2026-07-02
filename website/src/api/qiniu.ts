import client from 'src/api/client'

export interface QiniuCredentialStatus {
  configured: boolean
  key_hint?: string
  maas_configured: boolean
  maas_key_hint?: string
  access_key_configured: boolean
  access_key_hint?: string
  secret_key_configured: boolean
  secret_key_hint?: string
  updated_at?: string
}

export function qiniuCredentialStatus() {
  return client.get<QiniuCredentialStatus>('/qiniu/credentials')
}

export function saveQiniuCredential(payload: {
  sandbox_api_key: string
  maas_api_key?: string
  access_key?: string
  secret_key?: string
}) {
  return client.put<QiniuCredentialStatus>('/qiniu/credentials', payload)
}

export function deleteQiniuCredential() {
  return client.delete<{ ok: boolean }>('/qiniu/credentials')
}
