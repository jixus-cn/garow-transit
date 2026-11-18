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
import type { ConversationThread } from '../api'

export interface ResponsesMessage {
  id: string
  role: string
  kind:
    | 'message'
    | 'tool'
    | 'reasoning'
    | 'item'
    | 'context'
    | 'unknown'
    | 'snapshot'
    | 'partial'
  name: string
  body: string
}

function object(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : {}
}

function parse(value: string): unknown {
  try {
    return JSON.parse(value)
  } catch {
    return null
  }
}

function text(value: unknown): string {
  if (typeof value === 'string') return value
  if (Array.isArray(value)) return value.map(text).filter(Boolean).join('\n')
  const item = object(value)
  if (typeof item.text === 'string') return item.text
  if (typeof item.refusal === 'string') return item.refusal
  return ''
}

function messages(value: unknown, prefix: string): ResponsesMessage[] {
  if (typeof value === 'string') {
    return value
      ? [{ id: prefix, role: 'user', kind: 'message', name: '', body: value }]
      : []
  }
  if (!Array.isArray(value)) return []
  return value
    .map((raw, index): ResponsesMessage => {
      const item = object(raw)
      const type = typeof item.type === 'string' ? item.type : 'message'
      const id = `${prefix}:${index}`
      const role = typeof item.role === 'string' ? item.role : 'assistant'
      if (type === 'message') {
        return { id, role, kind: 'message', name: '', body: text(item.content) }
      }
      if (type === 'reasoning') {
        return {
          id,
          role: 'assistant',
          kind: 'reasoning',
          name: '',
          body: text(item.summary),
        }
      }
      if (
        [
          'function_call',
          'custom_tool_call',
          'function_call_output',
          'custom_tool_call_output',
        ].includes(type)
      ) {
        const output = type.endsWith('_output')
        const body = output ? item.output : (item.arguments ?? item.input)
        return {
          id,
          role: output ? 'tool' : 'assistant',
          kind: 'tool',
          name: [item.name, item.call_id]
            .filter((v) => typeof v === 'string')
            .join(' · '),
          body:
            typeof body === 'string'
              ? body
              : JSON.stringify(body ?? null, null, 2),
        }
      }
      return {
        id,
        role,
        kind: 'item',
        name: type,
        body: JSON.stringify(item, null, 2),
      }
    })
    .filter((item) => item.body || item.kind !== 'reasoning')
}

export function responsesTurnMessages(
  turn: ConversationThread['turns'][number],
  includeContext = true
): ResponsesMessage[] {
  if (!turn.content) return []
  const requestObject = object(parse(turn.content.request))
  const request = requestObject.response
    ? object(requestObject.response)
    : requestObject
  const result: ResponsesMessage[] = []
  if (
    includeContext &&
    typeof request.instructions === 'string' &&
    request.instructions
  ) {
    result.push({
      id: `${turn.record.request_id}:instructions`,
      role: 'system',
      kind: 'message',
      name: '',
      body: request.instructions,
    })
  }
  const input = messages(request.input, `${turn.record.request_id}:input`)
  const sourceKind = (
    kind: string,
    origin: string
  ): ResponsesMessage['kind'] => {
    if (kind === 'context' || kind === 'unknown' || kind === 'partial') {
      return kind
    }
    return origin === 'snapshot' ? 'snapshot' : 'message'
  }
  const reading = turn.content.reading
  if (reading) {
    const parts = reading.input ?? []
    const projected = parts.map((p, i): ResponsesMessage => ({
      id: `${turn.record.request_id}:projected:${i}`,
      role: p.role,
      kind: sourceKind(p.kind, p.origin),
      name: '',
      body: p.text,
    }))
    if (includeContext) {
      for (const [i, raw] of input.entries()) {
        if (
          raw.role === 'user' ||
          (raw.role === 'assistant' && raw.kind === 'message')
        ) {
          const sourceIndex = Number(raw.id.split(':').at(-1))
          result.push(
            ...projected.filter(
              (_, j) =>
                parts[j].item ===
                (Number.isFinite(sourceIndex) ? sourceIndex : i)
            )
          )
        } else result.push(raw)
      }
      if (!input.length) result.push(...projected)
    } else {
      result.push(
        ...projected.filter(
          (p, i) =>
            p.role === 'user' &&
            p.kind === 'message' &&
            parts[i].origin === 'request'
        )
      )
      const lastSource = Math.max(
        -1,
        ...parts.filter((p) => p.role === 'user').map((p) => p.item)
      )
      result.push(
        ...input.filter((p, i) => p.kind === 'tool' && i > lastSource)
      )
    }
  } else if (includeContext) {
    result.push(...input)
  } else {
    // An older server has no source evidence. Only show tool evidence here.
    result.push(...input.filter((p) => p.kind === 'tool'))
  }
  const response = parse(turn.content.response)
  let output = object(response).output
  if (Array.isArray(response)) {
    const items = new Map<string, unknown>()
    for (const raw of response) {
      const event = object(raw)
      const final = object(event.response).output
      if (Array.isArray(final) && final.length) output = final
      const item = object(event.item)
      if (
        event.type === 'response.output_item.done' &&
        typeof item.id === 'string'
      ) {
        items.set(item.id, item)
      }
    }
    if (!output) output = [...items.values()]
  }
  const replies = messages(output, `${turn.record.request_id}:output`)
  // Delta text is a fallback for partial streams or empty terminal output arrays.
  if (
    !replies.some(
      (item) =>
        item.role === 'assistant' && item.kind === 'message' && item.body
    ) &&
    turn.content.output
  ) {
    replies.push({
      id: `${turn.record.request_id}:delta`,
      role: 'assistant',
      kind: 'message',
      name: '',
      body: turn.content.output,
    })
  }
  if (reading) {
    result.push(
      ...replies.filter(
        (p) => p.kind === 'tool' || (includeContext && p.kind !== 'message')
      )
    )
    for (const [i, p] of (reading.replies ?? []).entries()) {
      result.push({
        id: `${turn.record.request_id}:reply:${i}`,
        role: 'assistant',
        kind: sourceKind(p.kind, p.origin),
        name: '',
        body: p.text,
      })
    }
  } else result.push(...replies)
  return result
}

export function responsesThreadPath(
  thread: ConversationThread,
  selectedId: string
) {
  const byId = new Map(
    thread.turns.map((turn) => [turn.record.request_id, turn])
  )
  const path: ConversationThread['turns'] = []
  const seen = new Set<string>()
  let current = byId.get(selectedId)
  while (current && !seen.has(current.record.request_id)) {
    path.push(current)
    seen.add(current.record.request_id)
    current = byId.get(current.parent_request_id)
  }
  return path.reverse()
}
