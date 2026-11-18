import { expect, it } from 'vitest'

import type { ConversationMessage, ConversationLedger } from '../api'
import { buildReaderView } from '../lib/session-view'

it('keeps the current question out of supplied history and shows intervening tools', () => {
  const row = (
    id: number,
    role: string,
    kind: string,
    origin: string,
    request_id: string
  ) =>
    ({
      id,
      role,
      kind,
      origin,
      request_id,
      created_at: id,
      content: 'fixture',
    }) as ConversationMessage
  const blocks = buildReaderView(
    [
      row(1, 'user', 'text', 'snapshot', 'r1'),
      row(2, 'user', 'context', 'request', 'r1'),
      row(3, 'user', 'text', 'request', 'r1'),
      row(5, 'assistant', 'text', 'response', 'r3'),
    ],
    [{ request_id: 'r2', created_at: 4, tool_calls: 2 } as ConversationLedger]
  )
  expect(blocks.some((b) => b.type === 'message' && b.message.id === 3)).toBe(
    true
  )
  expect(blocks.some((b) => b.type === 'tools' && b.count === 2)).toBe(true)
  expect(
    blocks.some((b) => b.type === 'message' && b.message.id === 5 && b.final)
  ).toBe(false)
})
