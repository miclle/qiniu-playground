import { afterEach, expect, test, vi } from 'vitest'

import { streamWorkspaceChatMessage } from './workspace-chat'

afterEach(() => {
  vi.mocked(window.fetch).mockRestore()
})

function streamResponse(...chunks: string[]) {
  const encoder = new TextEncoder()
  return new Response(new ReadableStream({
    start(controller) {
      for (const chunk of chunks) {
        controller.enqueue(encoder.encode(chunk))
      }
      controller.close()
    },
  }), {
    headers: { 'Content-Type': 'text/event-stream' },
    status: 200,
  })
}

test('streams workspace chat events with CRLF line endings', async () => {
  vi.spyOn(window, 'fetch').mockResolvedValue(streamResponse(
    'event:assistant_delta\r\ndata: {"delta":"Hel"}\r\n\r',
    '\n',
    'event: assistant_delta\r\ndata:{"delta":"lo"}\r\n\r\n',
    'event: done\r\ndata: {}\r\n\r\n',
  ))
  const deltas: string[] = []

  await streamWorkspaceChatMessage('wks_123', 'Hi', {
    onAssistantDelta: (delta) => deltas.push(delta),
  })

  expect(deltas).toEqual(['Hel', 'lo'])
})

test('rejects when the workspace chat stream ends before a terminal event', async () => {
  vi.spyOn(window, 'fetch').mockResolvedValue(streamResponse(
    'event: assistant_delta\ndata: {"delta":"partial"}\n\n',
  ))

  await expect(streamWorkspaceChatMessage('wks_123', 'Hi')).rejects.toThrow(
    'AI Chat stream ended before receiving a complete response.',
  )
})

test('rejects when the workspace chat stream emits an error event', async () => {
  const cancel = vi.fn()
  const encoder = new TextEncoder()
  vi.spyOn(window, 'fetch').mockResolvedValue(new Response(new ReadableStream({
    start(controller) {
      controller.enqueue(encoder.encode('event: error\ndata: {"error":"Failed to save assistant response."}\n\n'))
    },
    cancel,
  }), {
    headers: { 'Content-Type': 'text/event-stream' },
    status: 200,
  }))

  await expect(streamWorkspaceChatMessage('wks_123', 'Hi')).rejects.toThrow('Failed to save assistant response.')
  expect(cancel).toHaveBeenCalled()
})
