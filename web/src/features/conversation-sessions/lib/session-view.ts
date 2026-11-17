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
import type {
  ConversationLedger,
  ConversationMessage,
  ConversationSession,
} from '../api'

export type TimelineItem =
  | {
      type: 'message'
      key: string
      createdAt: number
      message: ConversationMessage
      final: boolean
      toolCalls: number
    }
  | {
      type: 'tools'
      key: string
      createdAt: number
      requestId: string
      count: number
    }

// The final reply is the last assistant text before the next user message (or
// the end of the retained window). Earlier assistant texts are process notes.
export function computeFinalReplyIds(
  messages: ConversationMessage[]
): Set<number> {
  const final = new Set<number>()
  let lastAssistantTextId: number | null = null
  for (const message of messages) {
    if (message.role === 'user') {
      if (lastAssistantTextId !== null) final.add(lastAssistantTextId)
      lastAssistantTextId = null
    } else if (message.role === 'assistant' && message.kind === 'text') {
      lastAssistantTextId = message.id
    }
  }
  if (lastAssistantTextId !== null) final.add(lastAssistantTextId)
  return final
}

function rank(item: TimelineItem): number {
  if (item.type === 'tools') return 1
  return item.message.role === 'user' ? 0 : 2
}

// Interleave tool-activity dividers (counts only) with messages by capture time.
// A request that produced assistant text carries its divider on that message;
// tool-only requests become standalone dividers so the timeline stays complete.
export function buildTimeline(
  messages: ConversationMessage[],
  ledger: ConversationLedger[]
): TimelineItem[] {
  const byRequest = new Map(ledger.map((entry) => [entry.request_id, entry]))
  const finalIds = computeFinalReplyIds(messages)
  const assistantRequests = new Set(
    messages
      .filter((message) => message.role === 'assistant')
      .map((message) => message.request_id)
  )
  const items: TimelineItem[] = messages.map((message) => ({
    type: 'message',
    key: `m${message.id}`,
    createdAt: message.created_at,
    message,
    final: finalIds.has(message.id),
    toolCalls: byRequest.get(message.request_id)?.tool_calls ?? 0,
  }))
  for (const entry of ledger) {
    if (entry.tool_calls > 0 && !assistantRequests.has(entry.request_id)) {
      items.push({
        type: 'tools',
        key: `t${entry.request_id}`,
        createdAt: entry.created_at,
        requestId: entry.request_id,
        count: entry.tool_calls,
      })
    }
  }
  items.sort(
    (a, b) => a.createdAt - b.createdAt || rank(a) - rank(b) || (a.key < b.key ? -1 : 1)
  )
  return items
}

export interface IntegritySummary {
  truncatedMessages: number
  interruptedRequests: number
  failedRequests: number
  isResidual: boolean
  branchReason: string
}

export function summarizeIntegrity(
  session: ConversationSession,
  messages: ConversationMessage[],
  ledger: ConversationLedger[]
): IntegritySummary {
  return {
    truncatedMessages: messages.filter((message) => message.truncated).length,
    interruptedRequests: ledger.filter(
      (entry) => entry.outcome === 'interrupted'
    ).length,
    failedRequests: ledger.filter((entry) => entry.outcome === 'failed').length,
    isResidual: session.source === 'residual',
    branchReason: session.link_reason,
  }
}