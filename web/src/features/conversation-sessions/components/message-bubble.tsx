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
import { cn } from '@/lib/utils'

import type { ConversationMessage } from '../api'

export function ToolDivider(props: { count: number }) {
  const { t } = useTranslation()
  return (
    <div className='text-muted-foreground flex items-center justify-center gap-1.5 py-1 text-xs'>
      <Wrench className='size-3.5' aria-hidden='true' />
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
            <dt className='inline'>{t('Time')}: </dt>
            <dd className='inline'>
              {dayjs.unix(message.created_at).format('YYYY-MM-DD HH:mm:ss')}
            </dd>
          </div>
        </dl>
      </CollapsibleContent>
    </Collapsible>
  )
}

export function MessageBubble(props: {
  message: ConversationMessage
  final: boolean
  toolCalls: number
}) {
  const { t } = useTranslation()
  const message = props.message
  const isUser = message.role === 'user'
  const isContext = isUser && message.kind === 'context'
  const label = isUser ? t('User message') : t('Assistant reply')
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
          isUser ? 'bg-muted/40' : 'bg-background'
        )}
      >
        <div className='flex flex-wrap items-center gap-1.5'>
          <span className='text-muted-foreground text-xs font-medium'>
            {isUser ? t('User') : t('Assistant')}
          </span>
          {message.interjection && (
            <Badge variant='warning'>{t('Interjection')}</Badge>
          )}
          {!isUser && (
            <Badge variant={props.final ? 'default' : 'secondary'}>
              {props.final ? t('Final reply') : t('Process note')}
            </Badge>
          )}
          {isContext && <Badge variant='outline'>{t('Injected context')}</Badge>}
          {message.truncated && (
            <Badge variant='destructive'>{t('Truncated')}</Badge>
          )}
        </div>
        {props.toolCalls > 0 && <ToolDivider count={props.toolCalls} />}
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
        {t('Injected context ({{chars}} characters, click to expand)', {
          chars: props.value.length,
        })}
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