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

export interface IntegritySummary {
  truncatedMessages: number
  interruptedRequests: number
  failedRequests: number
  isResidual: boolean
  branchReason: string
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

// The ledger supplies capture order and tool-only turns. A supplied snapshot
// is explicitly marked by the recorder, never inferred from where a page starts.
export function buildReaderView(
  messages: ConversationMessage[],
  ledger: ConversationLedger[],
  hasEarlier = false
): ReaderBlock[] {
  const groups = new Map<
    string,
    { at: number; messages: ConversationMessage[]; tools: number }
  >()
  const earliest = messages.length
    ? Math.min(...messages.map((m) => m.created_at))
    : 0
  for (const r of ledger) {
    if (hasEarlier && r.created_at < earliest) continue
    groups.set(r.request_id, {
      at: r.created_at,
      messages: [],
      tools: r.tool_calls,
    })
  }
  for (const m of messages) {
    let g = groups.get(m.request_id)
    if (!g) {
      g = { at: m.created_at, messages: [], tools: 0 }
      groups.set(m.request_id, g)
    }
    g.messages.push(m)
  }
  const blocks: ReaderBlock[] = []
  let lastTime: number | undefined
  for (const [id, g] of [...groups].sort((a, b) => a[1].at - b[1].at)) {
    if (!g.messages.length && !g.tools) continue
    if (
      lastTime !== undefined &&
      (g.at - lastTime > SEPARATOR_GAP_SECONDS ||
        new Date(g.at * 1000).toDateString() !==
          new Date(lastTime * 1000).toDateString())
    ) {
      blocks.push({
        type: 'separator',
        key: `s${id}`,
        prev: lastTime,
        at: g.at,
      })
    }
    lastTime = g.at
    const reported = g.messages.filter((m) => m.origin === 'snapshot')
    if (reported.length) {
      blocks.push({
        type: 'history',
        key: `h${id}`,
        messages: reported,
        summary: '',
      })
    }
    const visible = g.messages.filter((m) => m.origin !== 'snapshot')
    const render = (m: ConversationMessage) =>
      blocks.push({
        type: 'message',
        key: `m${m.id}`,
        message: m,
        final:
          m.role === 'assistant' &&
          m.origin === 'response' &&
          m.kind === 'final' &&
          !m.truncated,
      })
    for (const m of visible) if (m.role === 'user') render(m)
    if (g.tools) {
      const prev = blocks.at(-1)
      if (prev?.type === 'tools') prev.count += g.tools
      else blocks.push({ type: 'tools', key: `t${id}`, count: g.tools })
    }
    for (const m of visible) if (m.role === 'assistant') render(m)
  }
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
