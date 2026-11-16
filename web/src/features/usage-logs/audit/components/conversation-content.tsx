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
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { CopyButton } from '@/components/copy-button'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'

import { DetailSection } from '../../components/dialogs/log-detail-layout'

function ConversationText(props: {
  value: string
  label: string
  json?: boolean
}) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)
  const text = useMemo(() => {
    if (props.json && props.value) {
      try {
        return JSON.stringify(JSON.parse(props.value), null, 2)
      } catch {
        // Preserve partial captures.
      }
    }
    return props.value
  }, [props.value, props.json])
  if (!text) {
    return (
      <p className='text-muted-foreground text-sm'>{t('No recorded text')}</p>
    )
  }
  const long = text.length > 4000
  return (
    <div className='min-w-0 space-y-2'>
      <CopyButton
        value={text}
        size='sm'
        aria-label={t('Copy {{label}}', { label: props.label })}
      />
      <pre className='max-h-80 min-w-0 overflow-y-auto font-sans text-sm leading-relaxed break-words whitespace-pre-wrap'>
        {long && !expanded ? `${text.slice(0, 4000)}…` : text}
      </pre>
      {long && (
        <Button
          variant='outline'
          size='sm'
          aria-expanded={expanded}
          onClick={() => setExpanded(!expanded)}
        >
          {expanded ? t('Show less text') : t('Show full text')}
        </Button>
      )}
    </div>
  )
}

export function ConversationContent(props: {
  label: string
  value: string
  json?: boolean
  collapsed?: boolean
}) {
  const [open, setOpen] = useState(false)
  if (!props.collapsed) {
    return (
      <DetailSection label={props.label}>
        <ConversationText
          value={props.value}
          label={props.label}
          json={props.json}
        />
      </DetailSection>
    )
  }
  return (
    <Collapsible
      open={open}
      onOpenChange={setOpen}
      className='min-w-0 rounded-md border p-2.5'
    >
      <CollapsibleTrigger
        render={
          <Button
            variant='ghost'
            size='sm'
            className='h-auto max-w-full justify-start text-left whitespace-normal'
          />
        }
      >
        {props.label}
      </CollapsibleTrigger>
      <CollapsibleContent>
        {open && (
          <div className='pt-2'>
            <ConversationText
              value={props.value}
              label={props.label}
              json={props.json}
            />
          </div>
        )}
      </CollapsibleContent>
    </Collapsible>
  )
}
