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
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import dayjs from '@/lib/dayjs'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import { getConversationThread } from '../api'
import {
  responsesThreadPath,
  responsesTurnMessages,
} from '../lib/responses-thread'
import { ConversationContent } from './conversation-content'

export function ResponsesConversation(props: { requestId: string }) {
  const { t } = useTranslation()
  const viewerId = useAuthStore((state) => state.auth.user?.id)
  const [selectedId, setSelectedId] = useState(props.requestId)
  const [showHistory, setShowHistory] = useState(false)
  const [showContext, setShowContext] = useState(false)
  const query = useQuery({
    queryKey: ['conversation-thread', viewerId, props.requestId],
    queryFn: () => getConversationThread(props.requestId),
    retry: false,
    gcTime: 0,
    staleTime: 0,
    meta: { errorToast: false },
  })
  if (query.isPending) return <p role='status'>{t('Loading...')}</p>
  if (query.isError) {
    return (
      <Alert variant='destructive'>
        <AlertDescription>
          {t('Conversation unavailable, expired, or access denied.')}
        </AlertDescription>
      </Alert>
    )
  }
  const thread = query.data
  const branch = responsesThreadPath(thread, selectedId)
  const path = showHistory ? branch : branch.slice(-1)
  const roles: Record<string, string> = {
    user: t('User'),
    assistant: t('Assistant'),
    system: t('System'),
    developer: t('Developer'),
    tool: t('Tool result'),
  }
  const outcomes: Record<string, string> = {
    completed: t('Completed'),
    failed: t('Failed'),
    interrupted: t('Interrupted'),
    incomplete: t('Incomplete'),
  }
  return (
    <div className='space-y-3'>
      <p className='text-muted-foreground text-xs'>
        {t(
          'Linked by Responses IDs within the same user. Select a turn to view its branch. Unlinked requests remain separate context snapshots.'
        )}
      </p>
      {thread.missing_previous && (
        <Alert>
          <AlertDescription>
            {t(
              'Earlier context is missing, expired, or ambiguous. Only recorded content is shown.'
            )}
          </AlertDescription>
        </Alert>
      )}
      {thread.truncated && (
        <Alert>
          <AlertDescription>
            {t(
              'Conversation view limit reached. Some turns or bodies are omitted.'
            )}
          </AlertDescription>
        </Alert>
      )}
      <div className='flex flex-wrap gap-2'>
        <Button
          size='sm'
          variant='outline'
          aria-pressed={showHistory}
          onClick={() => setShowHistory(!showHistory)}
        >
          {showHistory ? t('Focus selected turn') : t('Show earlier turns')}
        </Button>
        <Button
          size='sm'
          variant='outline'
          aria-pressed={showContext}
          onClick={() => setShowContext(!showContext)}
        >
          {showContext
            ? t('Hide supplied context')
            : t('Show supplied context')}
        </Button>
      </div>
      {!showContext && (
        <p className='text-muted-foreground text-xs'>
          {t(
            'Earlier input and instructions are folded. Tool details can be expanded below.'
          )}
        </p>
      )}
      <div className='grid min-w-0 gap-4 md:grid-cols-[12rem_minmax(0,1fr)]'>
        <nav
          aria-label={t('Conversation turns')}
          className='flex max-h-96 gap-2 overflow-auto md:flex-col'
        >
          {thread.turns.map((turn, index) => (
            <Button
              key={turn.record.request_id}
              variant={
                selectedId === turn.record.request_id ? 'secondary' : 'ghost'
              }
              onClick={() => {
                setSelectedId(turn.record.request_id)
                setShowHistory(false)
                setShowContext(false)
              }}
              aria-pressed={selectedId === turn.record.request_id}
              className='h-auto shrink-0 flex-col items-start gap-1 px-3 py-2 text-left whitespace-normal'
            >
              <span>
                {t('Turn {{number}}', { number: index + 1 })}
                {turn.parent_request_id ? '' : ` · ${t('Context snapshot')}`}
              </span>
              <span className='text-muted-foreground text-xs'>
                {dayjs.unix(turn.record.created_at).format('MM-DD HH:mm:ss')}
              </span>
              <span className='text-muted-foreground max-w-full truncate text-xs'>
                {turn.record.model}
              </span>
            </Button>
          ))}
        </nav>
        <div
          aria-label={t('Conversation messages')}
          className='min-w-0 space-y-6'
        >
          {path.map((turn) => (
            <section
              key={turn.record.request_id}
              aria-label={turn.record.request_id}
              className='space-y-3'
            >
              <div className='text-muted-foreground flex flex-wrap gap-2 border-b pb-2 text-xs'>
                <span>
                  {dayjs
                    .unix(turn.record.created_at)
                    .format('YYYY-MM-DD HH:mm:ss')}
                </span>
                <span>{turn.record.response_model || turn.record.model}</span>
                <span>
                  {outcomes[turn.record.outcome] ?? turn.record.outcome}
                </span>
              </div>
              {(turn.record.truncated || !turn.content) && (
                <p className='text-muted-foreground text-xs'>
                  {t('This turn is partial or its body is unavailable.')}
                </p>
              )}
              {responsesTurnMessages(turn, showContext).map((message) => (
                <article
                  key={message.id}
                  className={cn(
                    'max-w-[95%] min-w-0',
                    message.role === 'user' ? 'ml-auto' : 'mr-auto'
                  )}
                >
                  <ConversationContent
                    label={[
                      roles[message.role] ?? message.role,
                      message.kind === 'reasoning'
                        ? t('Reasoning summary')
                        : '',
                      message.kind === 'tool' ? t('Tool call or result') : '',
                      message.name,
                    ]
                      .filter(Boolean)
                      .join(' · ')}
                    value={message.body}
                    collapsed={
                      message.kind !== 'message' ||
                      message.role === 'system' ||
                      message.role === 'developer'
                    }
                  />
                </article>
              ))}
            </section>
          ))}
        </div>
      </div>
    </div>
  )
}
