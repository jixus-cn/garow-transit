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
import { useInfiniteQuery } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { ArrowLeft, History } from 'lucide-react'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { EmptyState } from '@/components/empty-state'
import { ErrorState } from '@/components/error-state'
import { LoadingState } from '@/components/loading-state'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Separator } from '@/components/ui/separator'
import dayjs from '@/lib/dayjs'
import { formatNumber } from '@/lib/format'

import {
  getConversationSessionDetail,
  type ConversationMessage,
  type SessionDetail,
} from '../api'
import { buildReaderView, summarizeIntegrity } from '../lib/session-view'
import { MessageBubble, ToolDivider } from './message-bubble'

const SOURCE_LABEL: Record<string, string> = {
  snapshot: 'Context snapshot',
  fragment: 'Unattributed fragments',
  client: 'Client identifier',
  responses: 'Responses link',
  prefix: 'Inferred link',
  residual: 'Unattributed fragments',
}

const EMPTY_PAGES: SessionDetail[] = []
const EMPTY_LEDGER: SessionDetail['ledger'] = []

export function SessionReader(props: { sessionId: number }) {
  const { t } = useTranslation()
  const query = useInfiniteQuery({
    queryKey: ['conversation-session', props.sessionId],
    queryFn: ({ pageParam }) =>
      getConversationSessionDetail(props.sessionId, pageParam),
    initialPageParam: undefined as number | undefined,
    getNextPageParam: (lastPage) =>
      lastPage.has_more ? lastPage.messages[0]?.id : undefined,
    retry: false,
    gcTime: 0,
  })

  const pages = query.data?.pages ?? EMPTY_PAGES
  const session = pages[0]?.session
  const ledger = useMemo(() => pages[0]?.ledger ?? EMPTY_LEDGER, [pages])
  const messages = useMemo(
    () => [...pages].reverse().flatMap((page) => page.messages),
    [pages]
  )
  const readerBlocks = useMemo(
    () => buildReaderView(messages, ledger, query.hasNextPage),
    [messages, ledger, query.hasNextPage]
  )
  const integrity = useMemo(
    () => (session ? summarizeIntegrity(session, messages, ledger) : null),
    [session, messages, ledger]
  )

  if (query.isPending) return <LoadingState />
  if (query.isError || !session) {
    return (
      <ErrorState
        title={t('Failed to load conversation')}
        onRetry={() => void query.refetch()}
      />
    )
  }

  const banner: string[] = []
  if (integrity?.isResidual) {
    banner.push(t('No verified conversation link for these fragments.'))
  }
  if (integrity?.branchReason && session.parent_session_id) {
    banner.push(
      t(
        'This session is an inferred branch or compaction of an earlier session.'
      )
    )
  }
  if (integrity && integrity.truncatedMessages > 0) {
    banner.push(
      t('{{count}} messages are truncated.', {
        count: integrity.truncatedMessages,
      })
    )
  }
  if (integrity && integrity.interruptedRequests > 0) {
    banner.push(
      t('{{count}} requests were interrupted.', {
        count: integrity.interruptedRequests,
      })
    )
  }

  return (
    <div className='flex h-full min-h-0 flex-col gap-3'>
      <div className='flex shrink-0 flex-wrap items-center gap-2'>
        <Button
          variant='outline'
          size='sm'
          render={<Link to='/conversation-sessions' />}
        >
          <ArrowLeft className='size-4' aria-hidden='true' />
          {t('Back to conversations')}
        </Button>
        <span className='text-muted-foreground font-mono text-xs'>
          #{session.id}
        </span>
        <Badge variant='secondary'>
          {t(SOURCE_LABEL[session.source] ?? session.source)}
        </Badge>
        {session.last_outcome && (
          <Badge variant='outline'>{session.last_outcome}</Badge>
        )}
      </div>

      <div className='text-muted-foreground shrink-0 space-y-1 text-xs'>
        <p>
          {t('User')} #{session.user_id} · {t('API Keys')}{' '}
          {session.token_name || '—'} · {t('Models')}{' '}
          {session.last_model || session.model || '—'}
        </p>
        <p>
          {t('Updated')}:{' '}
          {dayjs.unix(session.updated_at).format('YYYY-MM-DD HH:mm:ss')} ·{' '}
          {t('{{messages}} messages / {{requests}} requests', {
            messages: formatNumber(session.message_count),
            requests: formatNumber(session.request_count),
          })}
        </p>
      </div>

      {banner.length > 0 && (
        <Alert className='shrink-0'>
          <AlertDescription>
            <ul className='list-disc space-y-0.5 pl-4'>
              {banner.map((line) => (
                <li key={line}>{line}</li>
              ))}
            </ul>
          </AlertDescription>
        </Alert>
      )}

      <Separator className='shrink-0' />

      <div className='min-h-0 flex-1 overflow-y-auto pr-1'>
        {query.hasNextPage && (
          <div className='flex justify-center pb-3'>
            <Button
              variant='outline'
              size='sm'
              disabled={query.isFetchingNextPage}
              onClick={() => void query.fetchNextPage()}
            >
              {query.isFetchingNextPage
                ? t('Loading...')
                : t('Load earlier messages')}
            </Button>
          </div>
        )}
        {readerBlocks.length === 0 ? (
          <EmptyState title={t('No recorded text')} />
        ) : (
          <div className='flex flex-col gap-3'>
            {readerBlocks.map((block) => {
              if (block.type === 'history') {
                return (
                  <HistoryFold
                    key={block.key}
                    messages={block.messages}
                    summary={block.summary}
                  />
                )
              }
              if (block.type === 'separator') {
                return (
                  <TimeSeparator
                    key={block.key}
                    prev={block.prev}
                    at={block.at}
                  />
                )
              }
              if (block.type === 'tools') {
                return <ToolDivider key={block.key} count={block.count} />
              }
              return (
                <MessageBubble
                  key={block.key}
                  message={block.message}
                  final={block.final}
                />
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}

function HistoryFold(props: {
  messages: ConversationMessage[]
  summary: string
}) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  return (
    <Collapsible
      open={open}
      onOpenChange={setOpen}
      className='rounded-lg border border-dashed p-3'
    >
      <CollapsibleTrigger
        render={
          <Button
            variant='ghost'
            size='sm'
            className='text-muted-foreground h-auto max-w-full justify-start gap-1.5 whitespace-normal'
          />
        }
      >
        <History className='size-4 shrink-0' aria-hidden='true' />
        <span>
          {t('Client-supplied history ({{count}} messages)', {
            count: props.messages.length,
          })}
          {props.summary ? ` — ${props.summary}` : ''}
        </span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        {open && (
          <div className='flex flex-col gap-3 pt-3'>
            {props.messages.map((message) => (
              <MessageBubble key={message.id} message={message} final={false} />
            ))}
          </div>
        )}
      </CollapsibleContent>
    </Collapsible>
  )
}

function TimeSeparator(props: { prev: number; at: number }) {
  const { t } = useTranslation()
  const prev = dayjs.unix(props.prev)
  const at = dayjs.unix(props.at)
  const crossDay = !prev.isSame(at, 'day')
  const label = crossDay ? at.format('YYYY-MM-DD HH:mm') : at.format('HH:mm')
  return (
    <div className='text-muted-foreground/70 flex items-center gap-2 py-1 text-xs'>
      <Separator className='flex-1' />
      <span className='shrink-0'>
        {crossDay ? t('Date') : t('Time')} · {label}
      </span>
      <Separator className='flex-1' />
    </div>
  )
}
