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
import {
  buildTimeline,
  computeFinalReplyIds,
  summarizeIntegrity,
} from '../lib/session-view'

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

describe('computeFinalReplyIds', () => {
  it('marks only the last assistant text before each user turn and the end', () => {
    const messages = [
      message(1, 'user', 10),
      message(2, 'assistant', 11),
      message(3, 'assistant', 12),
      message(4, 'user', 13),
      message(5, 'assistant', 14),
    ]
    const final = computeFinalReplyIds(messages)
    expect([...final].sort()).toEqual([3, 5])
  })

  it('ignores context items when detecting assistant text', () => {
    const messages = [
      message(1, 'assistant', 10),
      message(2, 'user', 11, { kind: 'context' }),
    ]
    expect([...computeFinalReplyIds(messages)]).toEqual([1])
  })
})

describe('buildTimeline', () => {
  it('attaches tool counts to assistant messages and inserts tool-only dividers in time order', () => {
    const messages = [
      message(1, 'user', 10),
      message(2, 'assistant', 30, { request_id: 'rA' }),
    ]
    const ledgers = [
      ledger('rA', 30, { tool_calls: 2 }),
      ledger('rToolOnly', 20, { tool_calls: 3 }),
    ]
    const timeline = buildTimeline(messages, ledgers)
    expect(timeline.map((item) => item.key)).toEqual(['m1', 'trToolOnly', 'm2'])
    const assistant = timeline[2]
    expect(assistant.type === 'message' && assistant.toolCalls).toBe(2)
    const divider = timeline[1]
    expect(divider.type === 'tools' && divider.count).toBe(3)
  })

  it('does not duplicate a divider when the request already has assistant text', () => {
    const messages = [message(1, 'assistant', 10, { request_id: 'rA' })]
    const timeline = buildTimeline(messages, [ledger('rA', 10, { tool_calls: 1 })])
    expect(timeline).toHaveLength(1)
    expect(timeline[0].type).toBe('message')
  })
})

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