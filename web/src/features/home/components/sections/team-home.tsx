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
import { Link } from '@tanstack/react-router'
import {
  ArrowRight,
  Layers,
  MessagesSquare,
  Compass,
  Check,
  Circle,
  FolderOpen,
  History,
  UserRound,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { AnimateInView } from '@/components/animate-in-view'
import { Button } from '@/components/ui/button'

export function TeamHome(props: { isAuthenticated: boolean }) {
  const { t } = useTranslation()
  const principles = [
    {
      icon: Compass,
      title: t('Align on the goal'),
      text: t('Start with shared context and a clear definition of done.'),
    },
    {
      icon: MessagesSquare,
      title: t('Keep progress visible'),
      text: t('Share the decisions and context that help others move forward.'),
    },
    {
      icon: Layers,
      title: t('Build on what we learn'),
      text: t(
        'Turn each outcome into a useful starting point for the next task.'
      ),
    },
  ]
  const entries = [
    {
      icon: FolderOpen,
      title: t('Work overview'),
      text: t('Find shared resources and continue your work.'),
      to: '/dashboard',
    },
    {
      icon: History,
      title: t('Review activity'),
      text: t('Return to the details that matter and keep context close.'),
      to: '/usage-logs',
    },
    {
      icon: UserRound,
      title: t('Personal space'),
      text: t('Manage your preferences and account in one place.'),
      to: '/profile',
    },
  ]
  return (
    <main>
      <section className='relative z-10 overflow-hidden px-6 pt-24 pb-16 md:pt-32 md:pb-24 lg:pt-36 lg:pb-28'>
        <div
          aria-hidden
          className='pointer-events-none absolute inset-0 -z-10 opacity-25 dark:opacity-[0.12]'
          style={{
            background:
              'radial-gradient(ellipse 60% 50% at 20% 20%, oklch(0.72 0.18 250 / 80%) 0%, transparent 70%), radial-gradient(ellipse 50% 40% at 80% 15%, oklch(0.65 0.15 200 / 60%) 0%, transparent 70%)',
          }}
        />
        <div
          aria-hidden
          className='absolute inset-0 -z-10 bg-[linear-gradient(to_right,var(--border)_1px,transparent_1px),linear-gradient(to_bottom,var(--border)_1px,transparent_1px)] [mask-image:radial-gradient(ellipse_60%_50%_at_50%_30%,black_20%,transparent_100%)] bg-[size:4rem_4rem] opacity-[0.08]'
        />
        <div className='mx-auto grid max-w-6xl grid-cols-1 items-start gap-12 lg:grid-cols-12 lg:gap-8'>
          <div className='flex flex-col items-start text-left lg:col-span-6'>
            <div className='mb-5 inline-flex items-center gap-1.5 rounded-full border border-blue-500/20 bg-blue-500/5 px-3 py-1.5 text-[11px] font-medium text-blue-600 dark:border-blue-400/20 dark:text-blue-400'>
              <span aria-hidden className='size-1.5 rounded-full bg-blue-500' />
              {t('Our shared workspace')}
            </div>
            <h1 className='text-[clamp(2.25rem,4.5vw,3.25rem)] leading-[1.15] font-bold tracking-tight'>
              {t('One shared space.')}
              <br />
              <span className='bg-gradient-to-r from-blue-400 via-violet-400 to-purple-500 bg-clip-text text-transparent'>
                {t('Better work, together.')}
              </span>
            </h1>
            <p className='text-muted-foreground/80 mt-5 max-w-xl text-base leading-relaxed md:text-[15px]'>
              {t(
                'Bring resources, context, and everyday work together. Less switching, more focus, and a clear starting point for every collaboration.'
              )}
            </p>
            <div className='mt-8 flex flex-wrap items-center gap-3'>
              <Button
                className='group h-11 rounded-lg px-5 text-sm font-medium'
                render={
                  <Link
                    to={props.isAuthenticated ? '/dashboard' : '/sign-in'}
                  />
                }
              >
                {props.isAuthenticated
                  ? t('Enter workspace')
                  : t('Member sign in')}
                <ArrowRight className='ml-1.5 size-4 transition-transform group-hover:translate-x-0.5' />
              </Button>
              <Button
                variant='outline'
                className='h-11 rounded-lg px-5 text-sm font-medium'
                render={<a href='#team-workflows' />}
              >
                {t('Explore the workspace')}
              </Button>
            </div>
            <div className='mt-10 flex flex-wrap gap-3'>
              {[
                t('Shared resources'),
                t('Clear ownership'),
                t('Steady progress'),
              ].map((label) => (
                <span
                  key={label}
                  className='border-border/40 bg-muted/15 text-foreground/70 flex items-center gap-2 rounded-full border px-4 py-2 text-xs'
                >
                  <Check className='size-3.5 text-blue-400' />
                  {label}
                </span>
              ))}
            </div>
            <p className='text-muted-foreground/50 mt-5 text-xs'>
              {t('A workspace for our team. Access is managed internally.')}
            </p>
          </div>
          <div className='lg:col-span-6'>
            <div className='border-border/40 bg-muted/15 overflow-hidden rounded-3xl border shadow-2xl shadow-black/10'>
              <div className='border-border/30 flex items-center justify-between border-b px-6 py-5'>
                <span className='text-xs font-semibold tracking-widest'>
                  {t('TEAM SPACE')}
                </span>
                <span className='text-muted-foreground flex items-center gap-2 text-xs'>
                  <Circle className='size-2 fill-blue-400 text-blue-400' />
                  {t('Working together')}
                </span>
              </div>
              <div className='space-y-3 p-5 md:p-6'>
                <p className='text-muted-foreground mb-5 text-xs'>
                  {t('A simple rhythm for everyday collaboration')}
                </p>
                {principles.map((item, i) => (
                  <div
                    key={item.title}
                    className='border-border/40 bg-background/60 flex gap-4 rounded-xl border p-5'
                  >
                    <div className='flex size-10 shrink-0 items-center justify-center rounded-xl border border-blue-400/15 bg-blue-400/5 text-blue-400'>
                      <item.icon className='size-5' strokeWidth={1.5} />
                    </div>
                    <div className='min-w-0'>
                      <div className='mb-1 flex items-center gap-2'>
                        <span className='text-muted-foreground/40 font-mono text-xs'>
                          0{i + 1}
                        </span>
                        <h2 className='text-sm font-semibold'>{item.title}</h2>
                      </div>
                      <p className='text-muted-foreground text-xs leading-relaxed'>
                        {item.text}
                      </p>
                    </div>
                  </div>
                ))}
              </div>
              <div className='border-border/30 text-muted-foreground/60 border-t px-6 py-4 text-xs'>
                {t('Stay focused. Keep learning. Move forward together.')}
              </div>
            </div>
          </div>
        </div>
      </section>
      <section
        id='team-workflows'
        className='border-border/40 relative z-10 scroll-mt-24 border-t px-6 py-16 md:py-24'
      >
        <div className='mx-auto max-w-6xl'>
          <AnimateInView>
            <p className='text-muted-foreground mb-3 text-xs font-medium tracking-widest uppercase'>
              {t('EVERYDAY WORK')}
            </p>
            <h2 className='mb-10 text-2xl font-bold tracking-tight md:text-3xl'>
              {t('Keep collaboration simple.')}
            </h2>
          </AnimateInView>
          <div className='border-border/40 bg-border/40 grid gap-px overflow-hidden rounded-xl border md:grid-cols-3'>
            {entries.map((item) => (
              <Link
                key={item.title}
                to={props.isAuthenticated ? item.to : '/sign-in'}
                className='bg-background hover:bg-muted/20 group p-7 transition-colors md:p-8'
              >
                <item.icon
                  className='mb-5 size-5 text-blue-400'
                  strokeWidth={1.5}
                />
                <h3 className='mb-2 flex items-center justify-between text-sm font-semibold'>
                  {item.title}
                  <ArrowRight className='text-muted-foreground size-4 transition-transform group-hover:translate-x-1' />
                </h3>
                <p className='text-muted-foreground text-sm leading-relaxed'>
                  {item.text}
                </p>
              </Link>
            ))}
          </div>
        </div>
      </section>
    </main>
  )
}
