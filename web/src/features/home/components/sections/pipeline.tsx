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
import { Activity, ArrowRight, Boxes, ListOrdered, Route } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { AnimateInView } from '@/components/animate-in-view'

const MEMBERS = [
  { name: 'ch-a', priority: '10', state: 'cooldown' },
  { name: 'ch-b', priority: '20', state: 'served' },
  { name: 'ch-c', priority: '30', state: 'standby' },
] as const

const MEMBER_TONE: Record<(typeof MEMBERS)[number]['state'], string> = {
  cooldown:
    'border-amber-500/30 bg-amber-500/5 text-amber-600 dark:text-amber-400',
  served:
    'border-emerald-500/30 bg-emerald-500/5 text-emerald-600 dark:text-emerald-400',
  standby: 'border-border/40 bg-muted/20 text-muted-foreground',
}

function FlowArrow() {
  return (
    <div
      aria-hidden
      className='text-muted-foreground/40 hidden items-center justify-center lg:flex'
    >
      <ArrowRight className='size-5' />
    </div>
  )
}

export function Pipeline() {
  const { t } = useTranslation()

  const steps = [
    {
      id: 'key',
      num: '01',
      icon: <Boxes className='text-muted-foreground size-4' />,
      title: t('Model = route key'),
      desc: t('The model field in the request selects a same-named lane.'),
    },
    {
      id: 'lane',
      num: '02',
      icon: <Route className='text-muted-foreground size-4' />,
      title: t('Lane is the only entry'),
      desc: t(
        'No lane means 503; the gateway never hits an upstream channel directly.'
      ),
    },
    {
      id: 'order',
      num: '03',
      icon: <ListOrdered className='text-muted-foreground size-4' />,
      title: t('Members by priority'),
      desc: t(
        'Failures retry within the attempt budget, then cool down and escape to the next member.'
      ),
    },
    {
      id: 'served',
      num: '04',
      icon: <Activity className='text-muted-foreground size-4' />,
      title: t('Served & observed'),
      desc: t(
        'The hit upstream comes back in X-Served-By and lands in the attempts chain.'
      ),
    },
  ]

  return (
    <section className='border-border/40 relative z-10 border-t px-6 py-24 md:py-32'>
      <div className='mx-auto max-w-6xl'>
        <AnimateInView className='mb-12 max-w-2xl md:mb-16'>
          <p className='text-muted-foreground mb-3 text-xs font-medium tracking-widest uppercase'>
            {t('Routing Pipeline')}
          </p>
          <h2 className='text-2xl leading-tight font-bold tracking-tight md:text-3xl'>
            {t('How a request is routed')}
          </h2>
          <p className='text-muted-foreground mt-4 text-sm leading-relaxed'>
            {t(
              'Request in, upstream out — the lane decides which channel serves it.'
            )}
          </p>
        </AnimateInView>

        <AnimateInView
          animation='scale-in'
          className='border-border/40 bg-muted/10 rounded-2xl border p-5 md:p-8'
        >
          <div className='grid items-center gap-4 lg:grid-cols-[minmax(0,1fr)_auto_minmax(0,1.6fr)_auto_minmax(0,1fr)] lg:gap-5'>
            <div className='border-border/40 bg-background rounded-xl border p-4'>
              <p className='text-muted-foreground mb-2 text-[10px] font-bold tracking-[0.15em] uppercase'>
                request
              </p>
              <code className='font-mono text-xs'>model: gpt-4o</code>
            </div>

            <FlowArrow />

            <div className='border-border/40 bg-background rounded-xl border p-4'>
              <div className='mb-3 flex items-center justify-between gap-2'>
                <span className='font-mono text-xs font-medium'>gpt-4o</span>
                <span className='border-border/40 bg-muted/30 text-muted-foreground rounded-md border px-1.5 py-0.5 font-mono text-[10px]'>
                  failover
                </span>
              </div>
              <ul className='space-y-2'>
                {MEMBERS.map((member) => (
                  <li
                    key={member.name}
                    className={`flex items-center justify-between gap-2 rounded-lg border px-3 py-1.5 font-mono text-[11px] ${MEMBER_TONE[member.state]}`}
                  >
                    <span>{member.name}</span>
                    <span className='text-[10px] opacity-70'>
                      priority {member.priority}
                    </span>
                    <span className='text-[10px] tracking-wider uppercase'>
                      {member.state}
                    </span>
                  </li>
                ))}
              </ul>
            </div>

            <FlowArrow />

            <div className='border-border/40 bg-background rounded-xl border p-4'>
              <p className='text-muted-foreground mb-2 text-[10px] font-bold tracking-[0.15em] uppercase'>
                upstream
              </p>
              <code className='font-mono text-xs'>vendor-a</code>
            </div>
          </div>

          <p className='text-muted-foreground mt-5 font-mono text-[11px]'>
            X-Served-By: gpt-4o ← ch-b
          </p>
        </AnimateInView>

        <div className='mt-12 grid gap-8 md:grid-cols-2 lg:grid-cols-4'>
          {steps.map((step, index) => (
            <AnimateInView
              key={step.id}
              delay={index * 100}
              animation='fade-up'
            >
              <div className='mb-3 flex items-center gap-3'>
                <span className='border-border/40 bg-muted text-muted-foreground flex size-7 items-center justify-center rounded-md border text-[10px] font-semibold tabular-nums'>
                  {step.num}
                </span>
                {step.icon}
              </div>
              <h3 className='mb-1.5 text-sm font-semibold'>{step.title}</h3>
              <p className='text-muted-foreground text-xs leading-relaxed'>
                {step.desc}
              </p>
            </AnimateInView>
          ))}
        </div>
      </div>
    </section>
  )
}
