import client from 'src/api/client'

export interface WorkspaceChatMessage {
  id: string
  created_at: string
  role: 'user' | 'assistant'
  content: string
  provider?: string
  exit_code?: number
}

export function workspaceChatMessages(workspaceID: string) {
  return client.get<{ messages: WorkspaceChatMessage[] }>(`/workspaces/${workspaceID}/chat/messages`)
}

export function sendWorkspaceChatMessage(workspaceID: string, message: string) {
  return client.post<{
    user_message: WorkspaceChatMessage
    assistant_message: WorkspaceChatMessage
  }>(`/workspaces/${workspaceID}/chat/messages`, { message })
}
