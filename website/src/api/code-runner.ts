import client from 'src/api/client'

export type CodeRunnerLanguage = 'python' | 'javascript' | 'typescript' | 'r' | 'java' | 'bash'

export interface CodeRunnerSession {
  id: string
  created_at?: string
  updated_at?: string
  name: string
  region: string
  sandbox_id?: string
  template_id: string
  state?: string
  endpoint?: string
  workspace_path?: string
}

export interface CodeRun {
  id: string
  created_at?: string
  session_id: string
  sandbox_id?: string
  language: string
  code: string
  stdin?: string
  stdout: string
  stderr: string
  error: string
  exit_code: number
  duration_ms: number
}

export interface CreateCodeRunnerSessionPayload {
  name?: string
  region: string
}

export interface RunCodePayload {
  language: CodeRunnerLanguage
  code: string
  stdin?: string
  timeout_seconds?: number
}

export function codeRunnerSessions() {
  return client.get<{ sessions: CodeRunnerSession[] }>('/code-runner/sessions')
}

export function createCodeRunnerSession(payload: CreateCodeRunnerSessionPayload) {
  return client.post<CodeRunnerSession>('/code-runner/sessions', payload)
}

export function connectCodeRunnerSession(sessionID: string) {
  return client.post<CodeRunnerSession>(`/code-runner/sessions/${sessionID}/connect`)
}

export function codeRuns(sessionID: string) {
  return client.get<{ runs: CodeRun[] }>(`/code-runner/sessions/${sessionID}/runs`)
}

export function runCode(sessionID: string, payload: RunCodePayload) {
  return client.post<CodeRun>(`/code-runner/sessions/${sessionID}/runs`, payload)
}
