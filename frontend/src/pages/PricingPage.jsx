import React, { useState } from 'react'
import { useApp } from '../context/AppContext'
import * as api from '../services/api'
import { Check, ArrowRight, Zap, Building2 } from 'lucide-react'

const PricingPage = () => {
  const { isAuthenticated, setSelectedTab, logout, addNotification } = useApp()
  const [annual, setAnnual] = useState(true)
  const [busyPlan, setBusyPlan] = useState(null)

  const plans = [
    {
      id: 'free',
      name: 'Sandbox',
      monthly: 0,
      annual: 0,
      overage: null,
      description: 'Test autonomous freight quote extraction and carrier bidding risk-free.',
      features: [
        '1 broker seat (personal workspace)',
        '25 AI-parsed freight quotes / month',
        'Up to 10 bidding carriers',
        '1 connected email inbox (SMTP/IMAP)',
        'AES-256-GCM / AWS KMS credential encryption'
      ],
      cta: 'Start Free Sandbox',
      popular: false
    },
    {
      id: 'boutique',
      name: 'Boutique Broker',
      monthly: 99,
      annual: 79,
      overage: '$0.75 / extra quote',
      description: 'Built for independent brokers automating daily shipper RFQs and carrier bids.',
      features: [
        '1 broker seat (single-broker workspace)',
        '150 AI-parsed quotes / month (+$0.75 overage)',
        'Up to 30 bidding carriers',
        '1 connected quoting mailbox (tenant SMTP/IMAP)',
        'Automated 2-round carrier re-bid engine',
        'pgvector historical lane rate benchmarking'
      ],
      cta: 'Start Boutique Plan',
      popular: false
    },
    {
      id: 'growth',
      name: 'Growth Brokerage',
      monthly: 299,
      annual: 249,
      overage: '$0.50 / extra quote',
      description: 'High-volume brokerage automation with unlimited carriers and private document vault.',
      features: [
        '750 AI-parsed quotes / month (+$0.50 overage)',
        'Unlimited bidding carriers',
        'Up to 3 connected quoting mailboxes',
        'Custom per-customer markup rules & lane analytics',
        'S3 private BOL & invoice vault (presigned URLs)',
        'CSV freight spend exports & priority support'
      ],
      cta: 'Upgrade to Growth',
      popular: true
    },
    {
      id: 'enterprise',
      name: 'Enterprise / 3PL',
      monthly: 799,
      annual: 649,
      overage: '$0.35 / extra quote',
      description: 'Multi-Seat Organizations with RBAC for scaling brokerage teams and 3PL networks.',
      features: [
        'Multi-Seat Organizations (up to 25 seats + RBAC)',
        'Team member invitations & shared carrier hub',
        '3,000 AI-parsed quotes / month (+$0.35 overage)',
        'Unlimited carriers & unlimited quoting mailboxes',
        'Custom bidding round timers & audit controls',
        'Dedicated onboarding & SLA support'
      ],
      cta: 'Launch Enterprise Org',
      popular: false,
      enterprise: true
    }
  ]

  const handleSelectPlan = async (plan) => {
    if (!isAuthenticated) {
      setSelectedTab('register')
      return
    }
    if (plan.id === 'free') {
      setSelectedTab('dashboard')
      return
    }
    try {
      setBusyPlan(plan.id)
      const res = await api.createCheckoutSession({
        plan_tier: plan.id,
        interval: annual ? 'annual' : 'monthly'
      })
      if (res.checkout_url) {
        window.location.href = res.checkout_url
      }
    } catch (err) {
      addNotification(err.response?.data?.detail || 'Stripe Checkout is not configured yet. Manage plan in Subscription & Usage.', 'error')
      setSelectedTab('subscription')
    } finally {
      setBusyPlan(null)
    }
  }

  return (
    <div className="min-h-screen text-on-surface bg-background mesh-gradient relative">
      <div className="absolute inset-0 bg-grid-pattern opacity-10 pointer-events-none"></div>

      <nav className="sticky top-0 w-full z-50 border-b border-white/5 bg-surface/80 backdrop-blur-xl transition-all duration-300">
        <div className="flex justify-between items-center h-20 px-8 max-w-7xl mx-auto">
          <div onClick={() => setSelectedTab('landing')} className="flex items-center gap-3 cursor-pointer">
            <div className="w-10 h-10 bg-primary/20 rounded-lg flex items-center justify-center border border-primary/30">
              <Zap className="text-primary" size={20} />
            </div>
            <div>
              <span className="font-bold text-xl text-primary tracking-tight">Dispatch</span>
              <p className="text-[9px] uppercase tracking-widest text-secondary font-bold -mt-0.5">Enterprise Logistics</p>
            </div>
          </div>

          <div className="flex items-center gap-6">
            <button onClick={() => setSelectedTab('landing')} className="text-sm font-medium text-on-surface hover:text-primary transition-colors">Home</button>
            <button onClick={() => setSelectedTab('pricing')} className="text-sm font-medium text-primary transition-colors">Pricing</button>
            <div className="h-4 w-[1px] bg-white/10 mx-2"></div>
            {isAuthenticated ? (
              <div className="flex items-center gap-4">
                <button
                  onClick={() => setSelectedTab('dashboard')}
                  className="px-5 py-2 rounded-lg bg-primary text-on-primary font-semibold text-sm hover:brightness-110 transition-all neon-glow-primary flex items-center gap-2"
                >
                  Dashboard <ArrowRight size={14} />
                </button>
                <button onClick={logout} className="text-sm font-medium text-on-surface-variant hover:text-error transition-colors">Logout</button>
              </div>
            ) : (
              <div className="flex items-center gap-4">
                <button onClick={() => setSelectedTab('login')} className="text-sm font-medium text-on-surface hover:text-primary transition-colors">Sign In</button>
                <button onClick={() => setSelectedTab('register')} className="px-5 py-2 rounded-lg bg-primary-container/20 border border-primary/30 text-primary font-semibold text-sm hover:bg-primary-container/40 transition-all">Get Started</button>
              </div>
            )}
          </div>
        </div>
      </nav>

      <section className="pt-16 pb-10 px-8 max-w-7xl mx-auto text-center relative z-10">
        <span className="text-primary font-semibold text-xs uppercase tracking-widest block mb-4">Transparent Volume Pricing</span>
        <h1 className="text-4xl md:text-5xl font-bold tracking-tight text-on-surface">
          Predictable Pricing for <br />
          <span className="text-transparent bg-clip-text bg-gradient-to-r from-primary to-secondary">Modern Freight Brokerages</span>
        </h1>
        <p className="mt-4 text-base text-on-surface-variant max-w-2xl mx-auto leading-relaxed">
          Pay for AI-automated freight quotes — never raw tokens. Upgrade to Enterprise to unlock Multi-Seat Organizations &amp; RBAC.
        </p>

        <div className="mt-8 inline-flex items-center gap-3 p-1.5 rounded-xl bg-surface-container border border-white/10">
          <button
            onClick={() => setAnnual(false)}
            className={`px-4 py-2 rounded-lg text-xs font-bold transition-all ${!annual ? 'bg-primary text-on-primary' : 'text-on-surface-variant hover:text-on-surface'}`}
          >
            Monthly
          </button>
          <button
            onClick={() => setAnnual(true)}
            className={`px-4 py-2 rounded-lg text-xs font-bold transition-all flex items-center gap-2 ${annual ? 'bg-primary text-on-primary' : 'text-on-surface-variant hover:text-on-surface'}`}
          >
            Annual <span className="text-[10px] px-1.5 py-0.5 rounded bg-secondary/20 text-secondary">Save ~20%</span>
          </button>
        </div>
      </section>

      <section className="pb-24 px-8 max-w-7xl mx-auto grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6 relative z-10">
        {plans.map((plan) => {
          const displayPrice = annual ? plan.annual : plan.monthly
          return (
            <div
              key={plan.id}
              className={`glass-card rounded-2xl p-6 border flex flex-col justify-between transition-all duration-300 relative ${
                plan.popular
                  ? 'border-primary bg-surface-container-high/40 neon-glow-primary scale-[1.02] z-20'
                  : 'border-white/5 bg-surface-container-low/30 hover:border-white/20'
              }`}
            >
              {plan.popular && (
                <span className="absolute top-4 right-4 bg-primary text-on-primary text-[10px] uppercase font-bold tracking-wider px-2 py-0.5 rounded-full">
                  Most Popular
                </span>
              )}
              {plan.enterprise && (
                <span className="absolute top-4 right-4 bg-secondary/20 text-secondary border border-secondary/30 text-[10px] uppercase font-bold tracking-wider px-2 py-0.5 rounded-full flex items-center gap-1">
                  <Building2 size={10} /> Multi-Seat Orgs
                </span>
              )}
              <div>
                <h3 className="text-lg font-bold text-on-surface mb-2">{plan.name}</h3>
                <div className="flex items-baseline gap-1 mb-1">
                  <span className="text-4xl font-bold text-on-surface">${displayPrice}</span>
                  <span className="text-xs text-on-surface-variant">/month</span>
                </div>
                <p className="text-[11px] text-secondary font-semibold mb-4 min-h-[16px]">
                  {plan.overage ? `Metered overage: ${plan.overage}` : '14-day trial sandbox'}
                </p>
                <p className="text-xs text-on-surface-variant mb-6 leading-relaxed">{plan.description}</p>
                <div className="h-[1px] bg-white/5 w-full mb-5"></div>
                <ul className="space-y-3 mb-8">
                  {plan.features.map((feature, idx) => (
                    <li key={idx} className="flex items-start gap-2.5">
                      <Check className="text-secondary shrink-0 mt-0.5" size={14} />
                      <span className="text-xs text-on-surface-variant leading-snug">{feature}</span>
                    </li>
                  ))}
                </ul>
              </div>

              <button
                onClick={() => handleSelectPlan(plan)}
                disabled={busyPlan === plan.id}
                className={`w-full py-3 rounded-xl font-bold text-xs transition-all duration-150 disabled:opacity-50 ${
                  plan.popular
                    ? 'bg-primary text-on-primary hover:brightness-110'
                    : 'bg-white/5 border border-white/10 hover:bg-white/10 hover:border-white/20'
                }`}
              >
                {busyPlan === plan.id ? 'Redirecting to Stripe...' : plan.cta}
              </button>
            </div>
          )
        })}
      </section>

      <footer className="py-8 text-center text-xs text-on-surface-variant/50 border-t border-white/5 max-w-7xl mx-auto">
        &copy; {new Date().getFullYear()} Dispatch Inc. All rights reserved. Secured with AES-256-GCM / AWS KMS &amp; Stripe Billing.
      </footer>
    </div>
  )
}

export default PricingPage
