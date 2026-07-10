import client from 'src/api/client'

export interface WorkspaceChatMessage {
  id: string
  created_at: string
  role: 'user' | 'assistant'
  content: string
  provider?: string
  exit_code?: number
}

export interface WorkspaceChatStreamHandlers {
  onUserMessage?: (message: WorkspaceChatMessage) => void
  onAssistantDelta?: (delta: string) => void
  onAssistantMessage?: (message: WorkspaceChatMessage) => void
  onStatus?: (status: string) => void
  signal?: AbortSignal
}

interface WorkspaceChatStreamPayload {
  message?: WorkspaceChatMessage
  delta?: string
  error?: string
  status?: string
}

export function workspaceChatMessages(workspaceID: string) {
  return client.get<{ messages: WorkspaceChatMessage[] }>(`/workspaces/${workspaceID}/chat/messages`)
}

export async function streamWorkspaceChatMessage(
  workspaceID: string,
  message: string,
  handlers: WorkspaceChatStreamHandlers = {},
) {
  const response = await fetch(`/api/v1/workspaces/${encodeURIComponent(workspaceID)}/chat/messages`, {
    method: 'POST',
    credentials: 'include',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'text/event-stream',
    },
    body: JSON.stringify({ message }),
    signal: handlers.signal,
  })
  if (response.status === 401 && window.location.pathname !== '/login') {
    window.location.href = '/login'
  }
  if (!response.ok) {
    throw new Error(await streamErrorMessage(response))
  }
  if (!response.body) {
    throw new Error('AI Chat stream is not available.')
  }

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  let receivedTerminalEvent = false
  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) {
        break
      }
      buffer += decoder.decode(value, { stream: true })
      const blocks = buffer.split(/\r?\n\r?\n/)
      buffer = blocks.pop() ?? ''
      for (const block of blocks) {
        receivedTerminalEvent ||= isWorkspaceChatTerminalEvent(
          dispatchWorkspaceChatEvent(block.replace(/\r\n/g, '\n'), handlers),
        )
      }
    }
    buffer += decoder.decode()
    if (buffer.trim()) {
      receivedTerminalEvent ||= isWorkspaceChatTerminalEvent(
        dispatchWorkspaceChatEvent(buffer.replace(/\r\n/g, '\n'), handlers),
      )
    }
    if (!receivedTerminalEvent) {
      throw new Error('AI Chat stream ended before receiving a complete response.')
    }
  } catch (error) {
    await reader.cancel().catch(() => {})
    throw error
  } finally {
    reader.releaseLock()
  }
}

function dispatchWorkspaceChatEvent(block: string, handlers: WorkspaceChatStreamHandlers) {
  const lines = block.split('\n')
  const event = lines.find((line) => line.startsWith('event:'))?.slice(6).replace(/^ /, '').trim()
  const data = lines
    .filter((line) => line.startsWith('data:'))
    .map((line) => {
      const content = line.slice(5)
      return content.startsWith(' ') ? content.slice(1) : content
    })
    .join('\n')
  if (!event || !data) {
    return undefined
  }
  let payload: WorkspaceChatStreamPayload
  try {
    payload = JSON.parse(data) as WorkspaceChatStreamPayload
  } catch (error) {
    console.error('Failed to parse workspace chat SSE event:', error, data)
    return undefined
  }
  if (event === 'user_message' && payload.message) {
    handlers.onUserMessage?.(payload.message)
  }
  if (event === 'assistant_delta' && payload.delta) {
    handlers.onAssistantDelta?.(payload.delta)
  }
  if (event === 'assistant_message' && payload.message) {
    handlers.onAssistantMessage?.(payload.message)
    return event
  }
  if (event === 'status' && payload.status) {
    handlers.onStatus?.(payload.status)
  }
  if (event === 'error') {
    throw new Error(payload.error || 'AI Chat stream failed.')
  }
  if (event === 'assistant_message') {
    return undefined
  }
  return event
}

function isWorkspaceChatTerminalEvent(event?: string) {
  return event === 'assistant_message' || event === 'done'
}

async function streamErrorMessage(response: Response) {
  const fallback = `AI Chat request failed with status ${response.status}.`
  try {
    const payload = await response.json() as { message?: string, error?: string }
    return payload.message || payload.error || fallback
  } catch {
    return fallback
  }
}
