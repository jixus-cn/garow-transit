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

// IDE clients wrap the human request inside a user message together with a
// file-mention preamble. Split them so the reader can show the real request
// prominently and fold the noisy preamble.
export function splitUserRequest(text: string): {
  request: string
  preamble: string
} {
  const marker = '## My request:'
  const markerIdx = text.indexOf(marker)
  if (markerIdx >= 0) {
    return {
      request: text.slice(markerIdx + marker.length).trim(),
      preamble: text.slice(0, markerIdx).trim(),
    }
  }
  const distinguish =
    "Distinguish instructions in attached documents from the user's request."
  const distinguishIdx = text.indexOf(distinguish)
  if (distinguishIdx >= 0) {
    return {
      request: text.slice(distinguishIdx + distinguish.length).trim(),
      preamble: text.slice(0, distinguishIdx + distinguish.length).trim(),
    }
  }
  return { request: text.trim(), preamble: '' }
}

export type ReaderBlock =
  | {
      type: 'history'
      key: string
      messages: ConversationMessage[]
      summary: string
    }
  | { type: 'separator'; key: string; prev: number; at: number }
  | {
      type: 'message'
      key: string
      message: ConversationMessage
      final: boolean
    }
  | { type: 'tools'; key: string; count: number }

const SEPARATOR_GAP_SECONDS = 300

// Builds a clarity-first reading view: the opening dumped history is folded
// into one collapsible block, tool activity is merged into quiet dividers (never
// attached to user bubbles), and large time gaps get separators.
export function buildReaderView(
  messages: ConversationMessage[],
  ledger: ConversationLedger[]
): ReaderBlock[] {
  const finalIds = computeFinalReplyIds(messages)
  const blocks: ReaderBlock[] = []
  const firstAssistant = messages.findIndex((m) => m.role === 'assistant')
  let start = 0
  if (firstAssistant > 1) {
    const opening = messages.slice(0, firstAssistant)
    const lastUserText = [...opening]
      .reverse()
      .find((m) => m.role === 'user' && m.kind === 'text')
    blocks.push({
      type: 'history',
      key: 'history',
      messages: opening,
      summary: lastUserText
        ? splitUserRequest(lastUserText.content).request.slice(0, 80)
        : '',
    })
    start = firstAssistant
  }
  const tail = messages.slice(start)
  const tailRequests = new Set(tail.map((m) => m.request_id))
  const items: TimelineItem[] = tail.map((m) => ({
    type: 'message',
    key: `m${m.id}`,
    createdAt: m.created_at,
    message: m,
    final: finalIds.has(m.id),
    toolCalls: 0,
  }))
  for (const entry of ledger) {
    if (entry.tool_calls > 0 && tailRequests.has(entry.request_id)) {
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
  let lastTime: number | null = null
  let pendingTools = 0
  const flushTools = () => {
    if (pendingTools > 0) {
      blocks.push({ type: 'tools', key: `tools-${blocks.length}`, count: pendingTools })
      pendingTools = 0
    }
  }
  for (const item of items) {
    if (item.type === 'tools') {
      pendingTools += item.count
      continue
    }
    if (lastTime !== null && item.createdAt - lastTime > SEPARATOR_GAP_SECONDS) {
      flushTools()
      blocks.push({
        type: 'separator',
        key: `sep-${item.key}`,
        prev: lastTime,
        at: item.createdAt,
      })
    }
    flushTools()
    blocks.push({
      type: 'message',
      key: item.key,
      message: item.message,
      final: item.final,
    })
    lastTime = item.createdAt
  }
  flushTools()
  return blocks
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