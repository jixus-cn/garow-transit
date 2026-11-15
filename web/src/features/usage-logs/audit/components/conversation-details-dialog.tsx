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

import { CopyButton } from '@/components/copy-button'
import { Dialog } from '@/components/dialog'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useAuthStore } from '@/stores/auth-store'

import {
  DetailRow,
  DetailSection,
} from '../../components/dialogs/log-detail-layout'
import { getConversation, type AuditLog } from '../api'
import { ResponsesConversation } from './responses-conversation'

function ContentSection(props: {
  label: string
  value: string
  json?: boolean
}) {
  const { t } = useTranslation()
  let text = props.value
  if (props.json && text) {
    try {
      text = JSON.stringify(JSON.parse(text), null, 2)
    } catch {
      /* Preserve partial text. */
    }
  }
  return (
    <DetailSection label={props.label}>
      {text ? (
        <>
          <CopyButton
            value={text}
            size='sm'
            aria-label={t('Copy {{label}}', { label: props.label })}
          />
          <pre className='bg-muted/40 max-h-80 overflow-y-auto rounded-md border p-3 text-xs break-words whitespace-pre-wrap'>
            {text}
          </pre>
        </>
      ) : (
        <p className='text-muted-foreground text-sm'>{t('No recorded text')}</p>
      )}
    </DetailSection>
  )
}

function ConversationBody(props: { entry: AuditLog }) {
  const { t } = useTranslation()
  const viewerId = useAuthStore((state) => state.auth.user?.id)
  const query = useQuery({
    queryKey: ['conversation', viewerId, props.entry.request_id],
    queryFn: () => getConversation(props.entry.request_id),
    retry: false,
    gcTime: 0,
    staleTime: 0,
    meta: { errorToast: false },
  })
  const detail = query.data
  const outcomes: Record<string, string> = {
    completed: t('Completed'),
    failed: t('Failed'),
    incomplete: t('Incomplete'),
    interrupted: t('Interrupted'),
  }
  return (
    <>
      {query.isPending && <p role='status'>{t('Loading...')}</p>}
      {query.isError && (
        <Alert variant='destructive'>
          <AlertDescription>
            {t('Conversation unavailable, expired, or access denied.')}
          </AlertDescription>
        </Alert>
      )}
      {!query.isError && detail && (
        <Tabs
          defaultValue={detail.record.kind === 'responses' ? 'chat' : 'details'}
        >
          <TabsList>
            {detail.record.kind === 'responses' && (
              <TabsTrigger value='chat'>{t('Conversation replay')}</TabsTrigger>
            )}
            <TabsTrigger value='details'>{t('Request details')}</TabsTrigger>
          </TabsList>
          {detail.record.kind === 'responses' && (
            <TabsContent value='chat'>
              <ResponsesConversation requestId={props.entry.request_id} />
            </TabsContent>
          )}
          <TabsContent value='details' className='space-y-4'>
            <DetailSection label={t('Request')}>
              <DetailRow label={t('Username')} value={props.entry.username} />
              <DetailRow
                label={t('Request ID')}
                value={detail.record.request_id}
                mono
              />
              <DetailRow
                label={t('Model')}
                value={detail.record.model || '—'}
              />
              <DetailRow
                label={t('Response model')}
                value={detail.record.response_model || '—'}
              />
              <DetailRow
                label={t('Token name')}
                value={detail.record.token_name || `#${detail.record.token_id}`}
              />
              <DetailRow label={t('Protocol')} value={detail.record.protocol} />
              <DetailRow
                label={t('Result')}
                value={outcomes[detail.record.outcome] ?? detail.record.outcome}
              />
            </DetailSection>
            {detail.record.truncated && (
              <Alert>
                <AlertDescription>
                  {t('Recording limit reached. This record is partial.')}
                </AlertDescription>
              </Alert>
            )}
            <ContentSection
              label={t('Latest user input')}
              value={detail.content.latest_input}
            />
            <ContentSection
              label={t('Assistant response')}
              value={detail.content.output}
            />
            <ContentSection
              label={t('Request context and tools')}
              value={detail.content.request}
              json
            />
            <ContentSection
              label={t('Response events and tool calls')}
              value={detail.content.response}
              json
            />
          </TabsContent>
        </Tabs>
      )}
    </>
  )
}

export function ConversationDetailsDialog(props: { entry: AuditLog }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  return (
    <Dialog
      open={open}
      onOpenChange={setOpen}
      title={t('Conversation details')}
      description={t(
        'Recorded text only. Attachments and encrypted reasoning are omitted. Records expire after the configured retention period.'
      )}
      trigger={
        <Button variant='ghost' size='sm' className='h-7 px-2'>
          {t('Details')}
        </Button>
      }
      contentClassName='sm:max-w-4xl'
      bodyClassName='space-y-4'
    >
      {open && <ConversationBody entry={props.entry} />}
    </Dialog>
  )
}
