import React, { useState, useEffect } from 'react'
import { useApp } from '../context/AppContext'
import * as api from '../services/api'
import { CreditCard, Check, AlertTriangle, ExternalLink, Zap, Building2 } from 'lucide-react'

const Subscription = () => {
  const { billingSummary, fetchData, addNotification, setSelectedTab } = useApp()
  const [annual, setAnnual] = useState(true)
  const [loadingPlan, setLoadingPlan] = useState(null)
  const [openingPortal, setOpeningPortal] = useState(false)

  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const status = params.get('checkout')
    if (status === 'success') {
      window.history.replaceState({}, '', window.location.pathname)
      addNotification('Subscription activated! Your workspace limits have been updated.', 'success')
      fetchData(true)
    } else if (status === 'canceled') {
      window.history.replaceState({}, '', window.location.pathname)
      addNotification('Stripe checkout canceled. No changes were made to your plan.', 'info')
    }
  }, [addNotification, fetchData])

  const handleCheckout = async (planTier) => {
    try {
      setLoadingPlan(planTier)
      const res = await api.createCheckoutSession({
        plan_tier: planTier,
        interval: annual ? 'annual' : 'monthly'
      })
      if (res.checkout_url) {
        window.location.href = res.checkout_url
      }
    } catch (err) {
      addNotification(err.response?.data?.detail || 'Failed to launch Stripe Checkout', 'error')
    } finally {
      setLoadingPlan(null)
    }
  }

  const handleOpenPortal = async () => {
    try {
      setOpeningPortal(true)
      const res = await api.createPortalSession()
      if (res.portal_url) {
        window.location.href = res.portal_url
      }
    } catch (err) {
      addNotification(err.response?.data?.detail || 'Failed to open Stripe Billing Portal', 'error')
    } finally {
      setOpeningPortal(false)
    }
  }

  if (!billingSummary) {
    return (
      <div className="flex items-center justify-center min-h-[300px] text-on-surface-variant">
        Loading workspace subscription &amp; usage...
      </div>
    )
  }

  const { usage, plans = [], plan_tier, plan_name, billing_status, has_stripe_customer, overage_per_quote_usd } = billingSummary

  const meterBar = (label, used, limit) => {
    const unlimited = limit < 0
    const pct = unlimited ? 0 : Math.min(100, Math.round((used / Math.max(1, limit)) * 100))
    const warn = !unlimited && pct >= 80
    return (
      <div className="p-4 rounded-xl bg-black/20 border border-white/5 space-y-2">
        <div className="flex justify-between text-xs">
          <span className="text-on-surface-variant font-semibold">{label}</span>
          <span className={`font-bold ${warn ? 'text-tertiary' : 'text-on-surface'}`}>
            {used} / {unlimited ? 'Unlimited' : limit}
          </span>
        </div>
        {!unlimited && (
          <div className="h-2 w-full bg-white/5 rounded-full overflow-hidden">
            <div
              className={`h-full rounded-full transition-all ${pct >= 100 ? 'bg-error' : warn ? 'bg-tertiary' : 'bg-primary'}`}
              style={{ width: `${pct}%` }}
            />
          </div>
        )}
      </div>
    )
  }

  return (
    <div className="space-y-8 max-w-7xl mx-auto">
      {billing_status === 'past_due' && (
        <div className="p-4 rounded-xl bg-error/15 border border-error/30 flex items-center justify-between gap-4">
          <div className="flex items-center gap-3 text-error text-sm font-semibold">
            <AlertTriangle size={18} />
            Your latest subscription payment failed. Update your payment method to avoid interrupting automated inbox polling.
          </div>
          {has_stripe_customer && (
            <button onClick={handleOpenPortal} className="px-4 py-2 rounded-lg bg-error text-white text-xs font-bold shrink-0">
              Update Payment Method
            </button>
          )}
        </div>
      )}

      <div className="glass-card rounded-2xl p-6 border border-white/10 bg-surface-container-low/30 flex flex-col md:flex-row md:items-center justify-between gap-6">
        <div>
          <div className="flex items-center gap-3">
            <span className="px-3 py-1 rounded-full text-xs font-bold uppercase tracking-wider bg-primary/15 text-primary border border-primary/30">
              {plan_name}
            </span>
            <span className={`px-2.5 py-0.5 rounded-full text-[11px] font-bold uppercase ${
              billing_status === 'active' ? 'bg-secondary/15 text-secondary' :
              billing_status === 'past_due' ? 'bg-error/15 text-error' :
              'bg-tertiary/15 text-tertiary'
            }`}>
              {billing_status}
            </span>
          </div>
          <h2 className="text-xl font-bold text-on-surface mt-3">{billingSummary.organization_name}</h2>
          <p className="text-xs text-on-surface-variant mt-1">
            {overage_per_quote_usd > 0
              ? `Metered quote overages billed via Stripe at $${overage_per_quote_usd.toFixed(2)} / extra quote.`
              : 'Free Sandbox enforces a hard cap at 25 AI-parsed quotes per month.'}
          </p>
        </div>

        <div className="flex flex-wrap items-center gap-3">
          {billingSummary.supports_organizations && (
            <button
              onClick={() => setSelectedTab('organization')}
              className="px-4 py-2.5 rounded-xl bg-secondary/15 border border-secondary/30 text-secondary font-bold text-xs flex items-center gap-2 hover:bg-secondary/25 transition-all"
            >
              <Building2 size={14} /> Manage Organization Team
            </button>
          )}
          {has_stripe_customer && (
            <button
              onClick={handleOpenPortal}
              disabled={openingPortal}
              className="px-5 py-2.5 rounded-xl bg-white/5 border border-white/10 text-on-surface font-bold text-xs flex items-center gap-2 hover:bg-white/10 transition-all"
            >
              <CreditCard size={14} />
              {openingPortal ? 'Opening Stripe...' : 'Manage Billing & Invoices'}
              <ExternalLink size={12} />
            </button>
          )}
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        {meterBar('Monthly AI Quotes', usage.quotes_used, usage.quotes_limit)}
        {meterBar('Bidding Carriers', usage.carriers_used, usage.carriers_limit)}
        {meterBar('Connected Mailboxes', usage.inboxes_used, usage.inboxes_limit)}
        {meterBar('Workspace Seats', usage.seats_used, usage.seats_limit)}
      </div>

      <div className="space-y-4">
        <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-4">
          <div>
            <h3 className="text-lg font-bold text-on-surface flex items-center gap-2">
              <Zap size={18} className="text-primary" /> Upgrade Workspace Plan
            </h3>
            <p className="text-xs text-on-surface-variant mt-0.5">
              Upgrade instantly via Stripe Checkout. Enterprise unlocks Multi-Seat Organizations &amp; RBAC.
            </p>
          </div>
          <div className="inline-flex items-center gap-2 p-1 rounded-xl bg-surface-container border border-white/10 self-start">
            <button
              onClick={() => setAnnual(false)}
              className={`px-3 py-1.5 rounded-lg text-xs font-bold ${!annual ? 'bg-primary text-on-primary' : 'text-on-surface-variant'}`}
            >
              Monthly
            </button>
            <button
              onClick={() => setAnnual(true)}
              className={`px-3 py-1.5 rounded-lg text-xs font-bold ${annual ? 'bg-primary text-on-primary' : 'text-on-surface-variant'}`}
            >
              Annual (-20%)
            </button>
          </div>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-5">
          {plans.map((p) => {
            const isCurrent = p.id === plan_tier
            const price = annual ? p.annual_monthly_price_usd : p.monthly_price_usd
            return (
              <div
                key={p.id}
                className={`glass-card rounded-2xl p-5 border flex flex-col justify-between ${
                  isCurrent ? 'border-secondary bg-secondary/5' :
                  p.popular ? 'border-primary/50 bg-surface-container-high/30' :
                  'border-white/5 bg-surface-container-low/20'
                }`}
              >
                <div>
                  <div className="flex items-center justify-between mb-2">
                    <h4 className="font-bold text-base text-on-surface">{p.name}</h4>
                    {isCurrent && (
                      <span className="text-[10px] font-bold uppercase px-2 py-0.5 rounded-full bg-secondary/20 text-secondary">Current</span>
                    )}
                  </div>
                  <div className="flex items-baseline gap-1 mb-3">
                    <span className="text-3xl font-extrabold text-on-surface">${price}</span>
                    <span className="text-xs text-on-surface-variant">/mo</span>
                  </div>
                  <ul className="space-y-2 mb-6">
                    {(p.features || []).map((f, idx) => (
                      <li key={idx} className="flex items-start gap-2 text-xs text-on-surface-variant">
                        <Check size={13} className="text-secondary shrink-0 mt-0.5" />
                        <span>{f}</span>
                      </li>
                    ))}
                  </ul>
                </div>

                {isCurrent ? (
                  <button disabled className="w-full py-2.5 rounded-xl bg-white/5 text-on-surface-variant text-xs font-bold cursor-default">
                    Active Plan
                  </button>
                ) : p.id === 'free' ? (
                  <button
                    onClick={handleOpenPortal}
                    disabled={!has_stripe_customer}
                    className="w-full py-2.5 rounded-xl bg-white/5 border border-white/10 text-on-surface text-xs font-bold hover:bg-white/10 disabled:opacity-40"
                  >
                    Manage in Portal
                  </button>
                ) : (
                  <button
                    onClick={() => handleCheckout(p.id)}
                    disabled={loadingPlan === p.id}
                    className="w-full py-2.5 rounded-xl bg-primary text-on-primary text-xs font-bold hover:brightness-110 disabled:opacity-50"
                  >
                    {loadingPlan === p.id ? 'Opening Stripe...' : `Upgrade to ${p.name}`}
                  </button>
                )}
              </div>
            )
          })}
        </div>
      </div>
    </div>
  )
}

export default Subscription
