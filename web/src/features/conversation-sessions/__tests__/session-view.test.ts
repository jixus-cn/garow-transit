/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { describe, expect, it } from 'vitest'

import type {
  ConversationLedger,
  ConversationMessage,
  ConversationSession,
} from '../api'
import { buildReaderView, summarizeIntegrity } from '../lib/session-view'

function message(
  id: number,
  role: 'user' | 'assistant',
  createdAt: number,
  overrides: Partial<ConversationMessage> = {}
): ConversationMessage {
  return {
    id,
    session_id: 1,
    role,
    kind: 'text',
    content: `text-${id}`,
    truncated: false,
    interjection: false,
    content_hash: `h${id}`,
    request_id: `r${id}`,
    created_at: createdAt,
    ...overrides,
  }
}

function ledger(
  requestId: string,
  createdAt: number,
  overrides: Partial<ConversationLedger> = {}
): ConversationLedger {
  return {
    request_id: requestId,
    user_id: 1,
    created_at: createdAt,
    model: 'm',
    response_model: 'm',
    token_id: 1,
    token_name: 'k',
    protocol: 'http',
    kind: 'responses',
    response_id: '',
    previous_response_id: '',
    outcome: 'completed',
    truncated: false,
    session_key: 's',
    session_state: 'linked',
    new_user_messages: 0,
    assistant_text_chars: 0,
    tool_calls: 0,
    ...overrides,
  }
}

describe('summarizeIntegrity', () => {
  it('counts truncation, interruption and flags residual sessions', () => {
    const session = {
      source: 'residual',
      link_reason: 'branch',
    } as ConversationSession
    const messages = [message(1, 'user', 10, { truncated: true })]
    const ledgers = [
      ledger('r1', 10, { outcome: 'interrupted' }),
      ledger('r2', 11, { outcome: 'failed' }),
    ]
    const summary = summarizeIntegrity(session, messages, ledgers)
    expect(summary.truncatedMessages).toBe(1)
    expect(summary.interruptedRequests).toBe(1)
    expect(summary.failedRequests).toBe(1)
    expect(summary.isResidual).toBe(true)
    expect(summary.branchReason).toBe('branch')
  })
})

describe('buildReaderView', () => {
  it('folds the opening dumped history into one block', () => {
    const messages = [
      message(1, 'user', 10, {
        kind: 'context',
        origin: 'snapshot',
        request_id: 'first',
      }),
      message(2, 'user', 10, { origin: 'snapshot', request_id: 'first' }),
      message(3, 'user', 10, { origin: 'request', request_id: 'first' }),
      message(4, 'assistant', 12),
    ]
    const blocks = buildReaderView(messages, [])
    expect(blocks[0].type).toBe('history')
    if (blocks[0].type === 'history') {
      expect(blocks[0].messages).toHaveLength(2)
    }
    expect(blocks.some((b) => b.type === 'message' && b.message.id === 4)).toBe(
      true
    )
  })

  it('does not fold a single opening user message', () => {
    const messages = [message(1, 'user', 10), message(2, 'assistant', 12)]
    const blocks = buildReaderView(messages, [])
    expect(blocks[0].type).toBe('message')
  })

  it('merges tool activity into quiet dividers, not on user bubbles', () => {
    const messages = [
      message(1, 'user', 10, { request_id: 'r1' }),
      message(2, 'assistant', 11, { request_id: 'r2' }),
    ]
    const ledgers = [
      ledger('r1', 10, { tool_calls: 1 }),
      ledger('r2', 11, { tool_calls: 2 }),
    ]
    const blocks = buildReaderView(messages, ledgers)
    const toolBlocks = blocks.filter((b) => b.type === 'tools')
    expect(toolBlocks).toHaveLength(1)
    if (toolBlocks[0].type === 'tools') expect(toolBlocks[0].count).toBe(3)
  })

  it('inserts a separator across large time gaps', () => {
    const messages = [message(1, 'user', 10), message(2, 'assistant', 1000)]
    const blocks = buildReaderView(messages, [])
    expect(blocks.some((b) => b.type === 'separator')).toBe(true)
  })
})
