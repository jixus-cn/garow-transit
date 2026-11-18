/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program.  If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { Wrench } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { ConversationText } from '@/features/usage-logs/audit/components/conversation-content'
import dayjs from '@/lib/dayjs'
import { formatNumber } from '@/lib/format'
import { cn } from '@/lib/utils'

import type { ConversationMessage } from '../api'

export function ToolDivider(props: { count: number }) {
  const { t } = useTranslation()
  return (
    <div className='text-muted-foreground/70 flex items-center justify-center gap-1.5 py-0.5 text-xs'>
      <Wrench className='size-3' aria-hidden='true' />
      <span>
        {t('Executed {{count}} operations (details not recorded)', {
          count: props.count,
        })}
      </span>
    </div>
  )
}

function Provenance(props: { message: ConversationMessage }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const message = props.message
  return (
    <Collapsible open={open} onOpenChange={setOpen} className='min-w-0'>
      <CollapsibleTrigger
        render={
          <Button
            variant='ghost'
            size='sm'
            className='text-muted-foreground h-auto px-1 py-0.5 text-xs'
          />
        }
      >
        {t('Provenance')}
      </CollapsibleTrigger>
      <CollapsibleContent>
        <dl className='text-muted-foreground mt-1 space-y-0.5 font-mono text-xs break-all'>
          <div>
            <dt className='inline'>{t('Request ID')}: </dt>
            <dd className='inline'>{message.request_id}</dd>
          </div>
          <div>
            <dt className='inline'>{t('Capture time')}: </dt>
            <dd className='inline'>
              {dayjs.unix(message.created_at).format('YYYY-MM-DD HH:mm:ss')}
            </dd>
          </div>
          {message.origin !== 'legacy' && message.item_index !== undefined && (
            <div>
              <dt className='inline'>{t('Protocol item / part')}: </dt>
              <dd className='inline'>
                {formatNumber(message.item_index + 1)} /{' '}
                {formatNumber((message.part_index ?? 0) + 1)}
              </dd>
            </div>
          )}
        </dl>
      </CollapsibleContent>
    </Collapsible>
  )
}

export function MessageBubble(props: {
  message: ConversationMessage
  final: boolean
}) {
  const { t } = useTranslation()
  const message = props.message
  const isUser = message.role === 'user'
  const isContext = message.kind === 'context' || message.kind === 'unknown'
  let label = isUser ? t('Client-submitted text') : t('Assistant')
  if (message.kind === 'context') label = t('Client context')
  if (message.kind === 'unknown') label = t('Unverified source')
  let status = t('Observed reply')
  if (props.final) status = t('Final reply')
  else if (message.kind === 'commentary') status = t('Process note')
  else if (message.kind === 'partial') status = t('Incomplete')
  let bubbleTone = 'bg-background'
  if (isUser) {
    bubbleTone = 'bg-muted/40'
  } else if (props.final) {
    bubbleTone = 'border-primary/40 bg-primary/5'
  }
  return (
    <div
      className={cn(
        'flex w-full flex-col gap-1',
        isUser ? 'items-end' : 'items-start'
      )}
    >
      <div
        className={cn(
          'flex max-w-[85%] min-w-0 flex-col gap-1.5 rounded-lg border p-3',
          bubbleTone
        )}
      >
        <div className='flex flex-wrap items-center gap-1.5'>
          <span className='text-muted-foreground text-xs font-medium'>
            {label}
          </span>
          {message.interjection && (
            <Badge variant='warning'>{t('Interjection')}</Badge>
          )}
          {!isUser && !isContext && message.origin !== 'snapshot' && (
            <Badge variant={props.final ? 'default' : 'secondary'}>
              {status}
            </Badge>
          )}
          {message.truncated && (
            <Badge variant='destructive'>{t('Truncated')}</Badge>
          )}
        </div>
        {isContext ? (
          <ContextBody value={message.content} label={label} />
        ) : (
          <ConversationText value={message.content} label={label} />
        )}
        <Provenance message={message} />
      </div>
    </div>
  )
}

function ContextBody(props: { value: string; label: string }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  return (
    <Collapsible open={open} onOpenChange={setOpen} className='min-w-0'>
      <CollapsibleTrigger
        render={
          <Button
            variant='outline'
            size='sm'
            className='h-auto max-w-full justify-start whitespace-normal'
          />
        }
      >
        {props.label} 路 {t('Show full text')}
      </CollapsibleTrigger>
      <CollapsibleContent>
        {open && (
          <div className='pt-2'>
            <ConversationText value={props.value} label={props.label} />
          </div>
        )}
      </CollapsibleContent>
    </Collapsible>
  )
}
