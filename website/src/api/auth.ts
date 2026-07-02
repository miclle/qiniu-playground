import client from 'src/api/client'

export interface AuthUser {
  account_id: string
  provider: string
  subject: string
  login: string
  name: string
  avatar_url: string
  email?: string
}

export function currentUser() {
  return client.get<AuthUser>('/auth/me')
}

export function logout() {
  return client.post<{ ok: boolean }>('/auth/logout')
}

export function githubLoginURL() {
  return '/api/v1/auth/github/login'
}
