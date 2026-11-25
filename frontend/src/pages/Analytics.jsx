import React, { useState } from 'react'
import { useApp } from '../context/AppContext'
import { AreaChart, Area, XAxis, YAxis, Tooltip, ResponsiveContainer, PieChart, Pie, Cell } from 'recharts'

const Analytics = () => {
  const { analytics, quotes, addNotification, fetchData } = useApp()
  const [daysFilter, setDaysFilter] = useState(null)

  if (!analytics) {
    return (
      <div className="flex items-center justify-center min-h-[400px] text-on-surface-variant font-medium">
        <div className="flex flex-col items-center gap-4">
          <div className="w-12 h-12 border-4 border-primary/30 border-t-primary rounded-full animate-spin"></div>
          <span>Loading Dispatch Analytics...</span>
        </div>
      </div>
    )
  }

  const totalReceivables = analytics.receivables || 0
  const quoteApprovalRate = analytics.quote_to_approval_conversion_pct || 0
  const avgLifecycle = analytics.average_turnaround_time || 'N/A'
  const activeShipments = (analytics.pipeline_stages?.APPROVED || 0) + (analytics.pipeline_stages?.IN_TRANSIT || 0)

  const formatSpend = (value) => {
    if (value >= 1_000_000) return `$${(value / 1_000_000).toFixed(1)}M`
    if (value >= 1_000) return `$${(value / 1_000).toFixed(1)}K`
    return `$${value.toLocaleString(undefined, { maximumFractionDigits: 0 })}`
  }

  const monthlySpendData = (analytics.monthly_spend || []).map(p => ({
    name: p.month,
    Spend: p.spend || 0
  }))

  const displayCarriers = (analytics.carrier_stats || [])
    .slice()
    .sort((a, b) => b.win_rate_pct - a.win_rate_pct)
    .slice(0, 4)

  const carrierColors = ['#adc6ff', '#4edea3', '#ffb95f', '#4d8eff']

  const totalQuotesInDb = Object.values(analytics.pipeline_stages || {}).reduce((a, b) => a + b, 0)
  const completedCount = (analytics.pipeline_stages?.COMPLETED || 0) + (analytics.pipeline_stages?.IN_TRANSIT || 0)
  const awaitingCount = (analytics.pipeline_stages?.AWAITING_APPROVAL || 0) + (analytics.pipeline_stages?.QUOTE_SENT || 0)
  const lostCount = (analytics.pipeline_stages?.LOST || 0) + (analytics.pipeline_stages?.NO_BIDS_ALERT || 0)
  const inProgressCount = Math.max(0, totalQuotesInDb - (completedCount + awaitingCount + lostCount))

  const pieData = [
    { name: 'Completed / In Transit', value: completedCount, color: '#4edea3' },
    { name: 'Awaiting Approval', value: awaitingCount, color: '#ffb95f' },
    { name: 'Needs Review / Lost', value: lostCount, color: '#f87171' },
    { name: 'In Bidding', value: inProgressCount, color: '#4d8eff' }
  ].filter(item => item.value > 0)

  // Real lane aggregation strictly from live workspace quotes (no fabricated fallback lanes)
  const lanesMap = {}
  ;(quotes || []).forEach(q => {
    if (!q.origin || !q.destination || !q.cost_price) return
    const key = `${q.origin.toUpperCase()} → ${q.destination.toUpperCase()}`
    if (!lanesMap[key]) {
      lanesMap[key] = { id: key, origin: q.origin.toUpperCase(), destination: q.destination.toUpperCase(), count: 0, cost_sum: 0, sell_sum: 0 }
    }
    lanesMap[key].count += 1
    lanesMap[key].cost_sum += q.cost_price || 0
    lanesMap[key].sell_sum += q.sell_price || 0
  })

  const laneDataList = Object.values(lanesMap).map(lane => {
    const avgSell = Math.round(lane.sell_sum / lane.count)
    const avgCost = Math.round(lane.cost_sum / lane.count)
    return {
      id: lane.id,
      origin: lane.origin,
      destination: lane.destination,
      avgSell,
      avgCost,
      margin: Math.max(0, avgSell - avgCost),
      count: lane.count
    }
  }).slice(0, 6)

  const handleExportCSV = () => {
    const headers = ['Quote ID', 'Status', 'Origin', 'Destination', 'Weight (lbs)', 'Carrier Cost', 'Sell Price', 'Margin ($)', 'Margin (%)']
    const rows = (quotes || []).map(q => [
      q.id, q.status, `"${q.origin || ''}"`, `"${q.destination || ''}"`,
      q.weight_lbs || 0, q.cost_price || 0, q.sell_price || 0, q.margin_amt || 0, q.margin_pct || 0
    ].join(','))
    const csv = [headers.join(','), ...rows].join('\n')
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `dispatch-freight-analytics-${new Date().toISOString().slice(0, 10)}.csv`
    a.click()
    URL.revokeObjectURL(url)
    addNotification('Exported live freight analytics CSV!', 'success')
  }

  const handleCycleFilter = () => {
    const next = daysFilter === null ? 30 : daysFilter === 30 ? 90 : null
    setDaysFilter(next)
    fetchData(false, next)
    addNotification(`Showing analytics for: ${next ? `Last ${next} Days` : 'All Time'}`, 'info')
  }

  return (
    <div className="pb-12 text-on-surface select-none">
      {/* KPI Cards Top Row */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6 mb-6">
        <div className="glass-card rounded-2xl p-6 flex flex-col justify-between min-h-[140px]">
          <div className="flex justify-between items-start">
            <span className="material-symbols-outlined text-primary bg-primary/10 p-2.5 rounded-xl">payments</span>
            <span className="text-on-surface-variant text-xs font-semibold">{daysFilter ? `${daysFilter}d` : 'All Time'}</span>
          </div>
          <div className="mt-4">
            <p className="text-on-surface-variant text-[11px] font-bold uppercase tracking-wider">Total Booked Receivables</p>
            <h3 className="text-4xl font-extrabold text-on-surface mt-1">{formatSpend(totalReceivables)}</h3>
          </div>
        </div>

        <div className="glass-card rounded-2xl p-6 flex flex-col justify-between min-h-[140px]">
          <div className="flex justify-between items-start">
            <span className="material-symbols-outlined text-secondary bg-secondary/10 p-2.5 rounded-xl">speed</span>
            <span className="text-on-surface-variant text-xs font-semibold">Conversion</span>
          </div>
          <div className="mt-4">
            <p className="text-on-surface-variant text-[11px] font-bold uppercase tracking-wider">Quote Approval Rate</p>
            <h3 className="text-4xl font-extrabold text-on-surface mt-1">{quoteApprovalRate}%</h3>
          </div>
        </div>

        <div className="glass-card rounded-2xl p-6 flex flex-col justify-between min-h-[140px]">
          <div className="flex justify-between items-start">
            <span className="material-symbols-outlined text-tertiary bg-tertiary/10 p-2.5 rounded-xl">timer</span>
            <span className="text-on-surface-variant text-xs font-semibold">Avg Turnaround</span>
          </div>
          <div className="mt-4">
            <p className="text-on-surface-variant text-[11px] font-bold uppercase tracking-wider">Avg. Quote Lifecycle</p>
            <h3 className="text-4xl font-extrabold text-on-surface mt-1">{avgLifecycle}</h3>
          </div>
        </div>

        <div className="glass-card rounded-2xl p-6 flex flex-col justify-between min-h-[140px]">
          <div className="flex justify-between items-start">
            <span className="material-symbols-outlined text-info bg-info/10 p-2.5 rounded-xl">local_shipping</span>
            <span className="text-on-surface-variant text-xs font-semibold">Real-time</span>
          </div>
          <div className="mt-4">
            <p className="text-on-surface-variant text-[11px] font-bold uppercase tracking-wider">Active Shipments</p>
            <h3 className="text-4xl font-extrabold text-on-surface mt-1">{activeShipments}</h3>
          </div>
        </div>
      </div>

      {/* Bento Grid Main Content */}
      <div className="grid grid-cols-12 gap-6">
        <div className="col-span-12 lg:col-span-8 glass-card rounded-2xl p-8 h-[400px] flex flex-col justify-between">
          <div className="flex justify-between items-center mb-4">
            <div>
              <h4 className="text-xl font-bold text-on-surface">Monthly Freight Volume (Last 6 Months)</h4>
              <p className="text-on-surface-variant text-xs">Booked receivables from live workspace quotes</p>
            </div>
            <div className="flex gap-2">
              <button
                onClick={handleExportCSV}
                className="bg-surface-container-high px-4 py-1.5 rounded-full text-xs font-semibold text-on-surface hover:bg-primary/20 transition-all"
              >
                Export CSV
              </button>
              <button
                onClick={handleCycleFilter}
                className="bg-surface-container-high px-4 py-1.5 rounded-full text-xs font-semibold text-on-surface hover:bg-primary/20 transition-all"
              >
                Window: {daysFilter ? `${daysFilter}d` : 'All'}
              </button>
            </div>
          </div>

          <div className="flex-1 w-full mt-4 min-h-[220px]">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={monthlySpendData} margin={{ top: 10, right: 10, left: -15, bottom: 0 }}>
                <defs>
                  <linearGradient id="spendGradient" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="#3b82f6" stopOpacity={0.3} />
                    <stop offset="100%" stopColor="#3b82f6" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <XAxis dataKey="name" stroke="currentColor" className="text-on-surface-variant/40" fontSize={10} tickLine={false} axisLine={false} dy={10} />
                <YAxis stroke="currentColor" className="text-on-surface-variant/40" fontSize={10} tickLine={false} axisLine={false} dx={-5} tickFormatter={(v) => `$${v >= 1000 ? `${(v/1000).toFixed(0)}K` : v}`} />
                <Tooltip
                  contentStyle={{ backgroundColor: '#131b2e', border: '1px solid rgba(255, 255, 255, 0.08)', borderRadius: '12px', color: '#dae2fd', fontSize: '12px' }}
                  formatter={(value) => [`$${Number(value).toLocaleString()}`, 'Booked Volume']}
                />
                <Area type="monotone" dataKey="Spend" stroke="#adc6ff" strokeWidth={2} fillOpacity={1} fill="url(#spendGradient)" />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </div>

        {/* Carrier Performance */}
        <div className="col-span-12 lg:col-span-4 glass-card rounded-2xl p-8 h-[400px] flex flex-col justify-between">
          <div>
            <h4 className="text-xl font-bold text-on-surface">Carrier Performance</h4>
            <p className="text-on-surface-variant text-xs mt-0.5">Live bid win-rate comparison</p>
          </div>
          {displayCarriers.length > 0 ? (
            <div className="space-y-6 flex-1 flex flex-col justify-center">
              {displayCarriers.map((carrier, index) => (
                <div key={carrier.id || index}>
                  <div className="flex justify-between text-on-surface text-sm font-medium mb-1.5">
                    <span>{carrier.name}</span>
                    <span className="font-bold">{Math.round(carrier.win_rate_pct || 0)}%</span>
                  </div>
                  <div className="h-3 w-full rounded-full overflow-hidden" style={{ backgroundColor: '#2d3449' }}>
                    <div
                      className="h-full rounded-full transition-all duration-700"
                      style={{
                        width: `${Math.min(100, carrier.win_rate_pct || 0)}%`,
                        backgroundColor: carrierColors[index % carrierColors.length]
                      }}
                    />
                  </div>
                </div>
              ))}
            </div>
          ) : (
            <div className="flex-1 flex items-center justify-center text-xs text-on-surface-variant italic">
              No carrier bid history recorded yet.
            </div>
          )}
        </div>

        {/* Quote Lifecycle Distribution */}
        <div className="col-span-12 lg:col-span-4 glass-card rounded-2xl p-8 flex flex-col justify-between min-h-[380px]">
          <div>
            <h4 className="text-xl font-bold text-on-surface">Quote Lifecycle</h4>
            <p className="text-on-surface-variant text-xs mt-0.5">Pipeline distribution status</p>
          </div>

          {pieData.length > 0 ? (
            <>
              <div className="relative w-44 h-44 mx-auto my-4">
                <ResponsiveContainer width="100%" height="100%">
                  <PieChart>
                    <Pie data={pieData} cx="50%" cy="50%" innerRadius={58} outerRadius={78} paddingAngle={4} dataKey="value">
                      {pieData.map((entry, index) => (
                        <Cell key={`cell-${index}`} fill={entry.color} />
                      ))}
                    </Pie>
                    <Tooltip contentStyle={{ backgroundColor: '#131b2e', border: '1px solid rgba(255,255,255,0.08)', borderRadius: '12px', color: '#dae2fd', fontSize: '12px' }} />
                  </PieChart>
                </ResponsiveContainer>
                <div className="absolute inset-0 flex flex-col items-center justify-center pointer-events-none">
                  <span className="text-3xl font-extrabold text-on-surface">{totalQuotesInDb}</span>
                  <span className="text-on-surface-variant text-[9px] font-bold tracking-widest uppercase mt-0.5">Total Quotes</span>
                </div>
              </div>

              <div className="space-y-2">
                {pieData.map((item) => (
                  <div key={item.name} className="flex items-center justify-between text-xs">
                    <div className="flex items-center gap-2">
                      <span className="w-2.5 h-2.5 rounded-full" style={{ backgroundColor: item.color }}></span>
                      <span className="text-on-surface-variant font-medium">{item.name}</span>
                    </div>
                    <span className="text-on-surface font-extrabold">
                      {Math.round((item.value / totalQuotesInDb) * 100)}% ({item.value})
                    </span>
                  </div>
                ))}
              </div>
            </>
          ) : (
            <div className="flex-1 flex items-center justify-center text-xs text-on-surface-variant italic">
              No quotes in pipeline yet.
            </div>
          )}
        </div>

        {/* Live Lane Benchmarks Table */}
        <div className="col-span-12 lg:col-span-8 glass-card rounded-2xl overflow-hidden flex flex-col justify-between min-h-[380px]">
          <div className="p-8 pb-4">
            <h4 className="text-xl font-bold text-on-surface">Historical Lane Margin Benchmarks</h4>
            <p className="text-on-surface-variant text-xs">Aggregated directly from priced lanes in your workspace</p>
          </div>
          <div className="flex-1 overflow-x-auto w-full">
            {laneDataList.length > 0 ? (
              <table className="w-full text-left border-collapse">
                <thead>
                  <tr className="border-b border-outline-variant bg-surface-container-high/30 text-[10px] font-bold text-on-surface-variant uppercase tracking-wider">
                    <th className="px-8 py-3.5">Lane</th>
                    <th className="px-8 py-3.5 text-center">Quotes</th>
                    <th className="px-8 py-3.5 text-center">Avg Carrier Cost</th>
                    <th className="px-8 py-3.5 text-center">Avg Sell Rate</th>
                    <th className="px-8 py-3.5 text-right">Avg Margin</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-outline-variant/30">
                  {laneDataList.map((lane) => (
                    <tr key={lane.id} className="hover:bg-surface-container-highest/20 transition-colors text-sm">
                      <td className="px-8 py-5 text-on-surface font-semibold">
                        {lane.origin} <span className="text-primary px-1.5">&rarr;</span> {lane.destination}
                      </td>
                      <td className="px-8 py-5 text-center text-on-surface-variant">{lane.count}</td>
                      <td className="px-8 py-5 text-center text-on-surface-variant">${lane.avgCost.toLocaleString()}</td>
                      <td className="px-8 py-5 text-center text-on-surface font-bold">${lane.avgSell.toLocaleString()}</td>
                      <td className="px-8 py-5 text-right">
                        <span className="bg-secondary/10 text-secondary px-3 py-1 rounded-full text-xs font-bold border border-secondary/20">
                          +${lane.margin.toLocaleString()}
                        </span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : (
              <div className="p-12 text-center text-xs text-on-surface-variant italic">
                Complete carrier bidding on quotes to populate historical lane benchmarks.
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  )
}

export default Analytics
