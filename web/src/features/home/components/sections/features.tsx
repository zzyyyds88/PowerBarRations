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
import {
  Activity,
  Coins,
  ListOrdered,
  Network,
  Route,
  Server,
  ShieldCheck,
  UserRound,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { AnimateInView } from '@/components/animate-in-view'

interface FeaturesProps {
  className?: string
}

export function Features(_props: FeaturesProps) {
  const { t } = useTranslation()

  const features = [
    {
      id: 'route-key',
      num: '01',
      title: t('Model Is the Route Key'),
      desc: t(
        'The model name in the request selects a same-named lane. No lane, no call — the gateway returns 503 instead of hitting an upstream directly.'
      ),
      span: 'md:col-span-2',
      icon: <Route className='size-4 text-blue-400' />,
      visual: (
        <div className='mt-4 flex flex-wrap items-center gap-2 font-mono text-[11px]'>
          <span className='border-border/40 bg-muted/20 text-muted-foreground rounded-lg border px-3 py-1.5'>
            model: gpt-4o
          </span>
          <span className='text-muted-foreground/40'>→</span>
          <span className='rounded-lg border border-blue-500/30 bg-blue-500/5 px-3 py-1.5 text-blue-600 dark:text-blue-400'>
            lane: gpt-4o
          </span>
        </div>
      ),
    },
    {
      id: 'failover',
      num: '02',
      title: t('Priority Failover'),
      desc: t(
        'Members are tried in priority order; failures retry within the attempt budget, then cool down and escape to the next member.'
      ),
      span: 'md:col-span-1',
      icon: <ListOrdered className='size-4 text-emerald-400' />,
      visual: (
        <div className='mt-4 space-y-2 font-mono text-[11px]'>
          {[
            {
              name: 'ch-a',
              tone: 'text-amber-600 dark:text-amber-400',
              state: 'cooldown',
            },
            {
              name: 'ch-b',
              tone: 'text-emerald-600 dark:text-emerald-400',
              state: 'served',
            },
            {
              name: 'ch-c',
              tone: 'text-muted-foreground',
              state: 'standby',
            },
          ].map((member) => (
            <div
              key={member.name}
              className='border-border/30 bg-muted/15 flex items-center justify-between rounded-lg border px-3 py-1.5'
            >
              <span className={member.tone}>{member.name}</span>
              <span className='text-muted-foreground text-[10px] tracking-wider uppercase'>
                {member.state}
              </span>
            </div>
          ))}
        </div>
      ),
    },
    {
      id: 'circuit',
      num: '03',
      title: t('Cooldown · Affinity · Circuit Breaker'),
      desc: t(
        'Six lane-level control keys, a three-state circuit breaker (closed / open / half-open), and affinity that can be turned off per lane.'
      ),
      span: 'md:col-span-1',
      icon: <Activity className='size-4 text-violet-400' />,
      visual: (
        <div className='mt-4 flex flex-wrap items-center gap-1.5'>
          {['closed', 'open', 'half-open'].map((state) => (
            <span
              key={state}
              className='border-border/40 bg-muted/20 text-muted-foreground rounded-md border px-2 py-1 font-mono text-[10px]'
            >
              {state}
            </span>
          ))}
        </div>
      ),
    },
    {
      id: 'security',
      num: '04',
      title: t('Secure by Design'),
      desc: t(
        'The admin key is derived from your passphrase; client keys are stored as hashes and shown only once.'
      ),
      span: 'md:col-span-2',
      icon: <ShieldCheck className='size-4 text-amber-400' />,
      visual: (
        <div className='mt-4 flex flex-wrap items-center gap-2 font-mono text-[11px]'>
          <span className='border-border/40 bg-muted/20 text-muted-foreground rounded-lg border px-3 py-1.5'>
            passphrase → admin key
          </span>
          <span className='border-border/40 bg-muted/20 text-muted-foreground rounded-lg border px-3 py-1.5'>
            client key → hash only
          </span>
        </div>
      ),
    },
  ]

  const additionalFeatures = [
    {
      icon: <Server className='size-5' strokeWidth={1.5} />,
      title: t('Single-node Simplicity'),
      desc: t('One binary and SQLite; no external database, cache, or queue'),
    },
    {
      icon: <Coins className='size-5' strokeWidth={1.5} />,
      title: t('Upstream Cost Visibility'),
      desc: t('Upstream spend in CNY, derived from hourly aggregates'),
    },
    {
      icon: <UserRound className='size-5' strokeWidth={1.5} />,
      title: t('Single-user by Design'),
      desc: t('One operator, one node — no user groups or wallet'),
    },
    {
      icon: <Network className='size-5' strokeWidth={1.5} />,
      title: t('Four Protocols In & Out'),
      desc: t(
        'OpenAI Chat, Responses, Anthropic, and Gemini are translated on both the inbound and upstream sides.'
      ),
    },
  ]

  return (
    <section className='relative z-10 px-6 py-24 md:py-32'>
      <div className='mx-auto max-w-6xl'>
        <AnimateInView className='mb-16 max-w-lg'>
          <p className='text-muted-foreground mb-3 text-xs font-medium tracking-widest uppercase'>
            {t('Core Features')}
          </p>
          <h2 className='text-2xl leading-tight font-bold tracking-tight md:text-3xl'>
            {t('Built for one operator,')}
            <br />
            {t('tuned for routing')}
          </h2>
        </AnimateInView>

        {/* Bento grid */}
        <div className='border-border/40 bg-border/40 grid gap-px overflow-hidden rounded-xl border md:grid-cols-3'>
          {features.map((feature, index) => (
            <AnimateInView
              key={feature.id}
              delay={index * 100}
              animation='scale-in'
              className={`bg-background group hover:bg-muted/20 p-7 transition-colors duration-300 md:p-8 ${feature.span}`}
            >
              <div className='mb-3 flex items-center gap-3'>
                <span className='border-border/40 bg-muted text-muted-foreground flex size-7 items-center justify-center rounded-md border text-[10px] font-semibold tabular-nums'>
                  {feature.num}
                </span>
                {feature.icon}
                <h3 className='text-sm font-semibold'>{feature.title}</h3>
              </div>
              <p className='text-muted-foreground text-sm leading-relaxed'>
                {feature.desc}
              </p>
              {feature.visual}
            </AnimateInView>
          ))}
        </div>

        {/* Additional features row */}
        <div className='mt-12 grid grid-cols-2 gap-8 md:grid-cols-4 md:gap-12'>
          {additionalFeatures.map((feature, index) => (
            <AnimateInView
              key={feature.title}
              delay={index * 100}
              animation='fade-up'
              className='flex flex-col items-center text-center'
            >
              <div className='text-muted-foreground border-border/50 bg-muted/30 group-hover:text-foreground mb-3 flex size-12 items-center justify-center rounded-xl border transition-colors'>
                {feature.icon}
              </div>
              <h3 className='mb-1.5 text-sm font-semibold'>{feature.title}</h3>
              <p className='text-muted-foreground max-w-[200px] text-xs leading-relaxed'>
                {feature.desc}
              </p>
            </AnimateInView>
          ))}
        </div>
      </div>
    </section>
  )
}
