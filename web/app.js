// ── State Management ────────────────────────────────────────────────
const STATE = {
    rawJobs: [],        // All jobs fetched from API
    filteredJobs: [],   // After applying client-side category/search filters
    stats: null,
    viewMode: 'feed',   // 'feed' | 'kanban'
    category: 'all',
    onlyRelevant: true, // Default: hide noise (< 5.0 or skip)
    search: '',
    kanbanCity: '',
    kanbanMinScore: 0,
    action: '',
    status: '',
    sort: 'score',
    limit: 20,
    page: 1,
    selectedJob: null
}

// ── Kanban Columns Configuration ──────────────────────────────────
const KANBAN_COLUMNS = [
    { id: 'new',       label: 'Yeni & Radar',      icon: '📥', color: 'border-cyan-500/30 text-cyan-300 bg-cyan-500/10' },
    { id: 'applied',   label: 'Başvuruldu',        icon: '📨', color: 'border-emerald-500/30 text-emerald-300 bg-emerald-500/10' },
    { id: 'interview', label: 'Mülakat',           icon: '🎤', color: 'border-purple-500/30 text-purple-300 bg-purple-500/10' },
    { id: 'offer',     label: 'Teklif',            icon: '🎉', color: 'border-amber-500/30 text-amber-300 bg-amber-500/10' },
    { id: 'rejected',  label: 'Reddedildi',        icon: '❌', color: 'border-rose-500/30 text-rose-300 bg-rose-500/10' },
    { id: 'skipped',   label: 'Atlandı / Arşiv',   icon: '⏭',  color: 'border-slate-600/40 text-slate-400 bg-slate-800/40' }
]

const STATUS_LABELS = {
    new:       'Yeni & Radar',
    applied:   'Başvuruldu',
    interview: 'Mülakat',
    offer:     'Teklif 🎉',
    rejected:  'Reddedildi',
    skipped:   'Atlandı'
}

// ── Source Configuration Mapping ──────────────────────────────────
const SOURCE_META = {
    linkedin:        { label: 'LinkedIn',         badge: 'bg-blue-600/15 text-blue-400 border-blue-500/30' },
    indeed:          { label: 'Indeed',           badge: 'bg-sky-600/15 text-sky-300 border-sky-500/30' },
    lever:           { label: 'Lever ATS',        badge: 'bg-amber-500/15 text-amber-300 border-amber-500/30' },
    greenhouse:      { label: 'Greenhouse ATS',   badge: 'bg-emerald-500/15 text-emerald-300 border-emerald-500/30' },
    smartrecruiters: { label: 'SmartRecruiters',  badge: 'bg-purple-500/15 text-purple-300 border-purple-500/30' },
    techcareer:      { label: 'techcareer.net',   badge: 'bg-pink-500/15 text-pink-300 border-pink-500/30' },
    coderspace:      { label: 'Coderspace 🚀',    badge: 'bg-orange-500/15 text-orange-300 border-orange-500/30' },
    kariyer_net:     { label: 'Kariyer.net',      badge: 'bg-fuchsia-500/15 text-fuchsia-300 border-fuchsia-500/30' },
    arbeitnow:       { label: 'Arbeitnow EU',     badge: 'bg-indigo-500/15 text-indigo-300 border-indigo-500/30' },
    eurocontrol:     { label: 'EUROCONTROL ✈️',   badge: 'bg-cyan-500/20 text-cyan-300 border-cyan-400/40' }
}

// ── API Helpers ───────────────────────────────────────────────────
const API = {
    getJobs: () => fetch('/api/jobs?limit=1000').then(r => r.json()),
    getStats: () => fetch('/api/stats').then(r => r.json()),
    updateStatus: (id, status) => fetch(`/api/jobs/${id}/status`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ status })
    }).then(async r => {
        const data = await r.json().catch(() => ({}))
        if (!r.ok) throw new Error(data.error || 'Durum güncellenemedi')
        return data
    }),
    deleteJob: (id) => fetch(`/api/jobs/${id}`, { method: 'DELETE' }),
    clearAll: () => fetch('/api/jobs', { method: 'DELETE', headers: { 'X-Confirm': 'true' } }),
    tailorJob: (id, force = false) => fetch(`/api/jobs/${id}/tailor${force ? '?refresh=true' : ''}`, {
        method: 'POST'
    }).then(async r => {
        const data = await r.json()
        if (!r.ok) throw new Error(data.error || 'İşlem başarısız')
        return data
    }),
    interviewPrep: (id, force = false) => fetch(`/api/jobs/${id}/interview-prep${force ? '?refresh=true' : ''}`, {
        method: 'POST'
    }).then(async r => {
        const data = await r.json()
        if (!r.ok) throw new Error(data.error || 'Mülakat kiti üretilemedi')
        return data
    })
}

// ── Initialization ────────────────────────────────────────────────
document.addEventListener('DOMContentLoaded', () => {
    load(true)
    // Auto refresh stats every 30 seconds
    setInterval(() => load(false), 30000)
})

async function load(showLoading = false) {
    try {
        if (showLoading) renderLoadingState()
        
        const [jobsRes, statsRes] = await Promise.all([
            API.getJobs(),
            API.getStats()
        ])

        STATE.rawJobs = jobsRes.jobs || []
        STATE.stats = statsRes
        
        updateStatsNavbar()
        updateCategoryCounts()
        applyFilters(showLoading)
    } catch (err) {
        console.error('Veri yüklenemedi:', err)
        toast('Veriler yüklenirken hata oluştu', 'error')
    }
}

// ── Stats Navbar Update ───────────────────────────────────────────
function updateStatsNavbar() {
    const s = STATE.stats || {}
    document.getElementById('nav-total').textContent = s.total_jobs || 0
    document.getElementById('nav-apply').textContent = s.apply_count || 0
    document.getElementById('nav-watch').textContent = s.watch_count || 0
    document.getElementById('nav-avg').textContent = (s.avg_score || 0).toFixed(1)
}

// ── Category Tab Counts ───────────────────────────────────────────
function updateCategoryCounts() {
    const all = STATE.onlyRelevant
        ? STATE.rawJobs.filter(j => (j.match_score || 0) >= 5.0 && (j.ai_action || '').toLowerCase() !== 'skip')
        : STATE.rawJobs
    const count = {
        all: all.length,
        top: all.filter(j => (j.match_score || 0) >= 7.5).length,
        junior: all.filter(isJuniorJob).length,
        backend: all.filter(isBackendJob).length,
        devops: all.filter(isDevopsJob).length,
        data: all.filter(isDataJob).length,
        tr: all.filter(isTurkeyJob).length,
        eu: all.filter(isEuropeJob).length,
        ats: all.filter(isAtsJob).length
    }

    document.getElementById('count-all').textContent = count.all
    document.getElementById('count-top').textContent = count.top
    const jrEl = document.getElementById('count-junior')
    if (jrEl) jrEl.textContent = count.junior
    document.getElementById('count-backend').textContent = count.backend
    document.getElementById('count-devops').textContent = count.devops
    document.getElementById('count-data').textContent = count.data
    document.getElementById('count-tr').textContent = count.tr
    document.getElementById('count-eu').textContent = count.eu
    document.getElementById('count-ats').textContent = count.ats
}

// Category Classification Helpers
function isJuniorJob(j) {
    const t = (j.title || '').toLowerCase()
    const d = (j.description || '').toLowerCase().slice(0, 1800)
    const titleSignals = ['junior', 'graduate', 'new grad', 'entry level', 'entry-level', 'associate', 'trainee', 'yeni mezun', 'genç yetenek', 'mt programı', 'uzman yardımcısı', 'jr.', 'jr ', 'young graduate']
    if (titleSignals.some(s => t.includes(s))) return true
    const descSignals = ['0-2 years', '0-1 years', '0-2 yıl', '0-1 yıl', '1-2 years', 'new graduate', 'recent graduate', 'entry-level', 'entry level']
    return descSignals.some(s => d.includes(s))
}

const _wbCache = {}
function hasWord(text, words) {
    const key = words.join('|')
    let re = _wbCache[key]
    if (!re) {
        const alt = words.map(w => w.replace(/[.*+?^${}()|[\]\\\/]/g, '\\$&')).join('|')
        re = _wbCache[key] = new RegExp(`(?<![\\p{L}\\p{N}])(?:${alt})(?![\\p{L}\\p{N}])`, 'u')
    }
    return re.test(text)
}

function isBackendJob(j) {
    const t = (j.title || '').toLowerCase()
    const rf = (j.role_family || '').toLowerCase()
    if (isDevopsJob(j) || isDataJob(j)) return false
    return hasWord(t, ['backend', 'back-end', 'software', 'developer', 'engineer', 'golang', 'go developer', 'go engineer', 'java', 'python', 'yazılım', 'geliştirici', 'mühendisi']) || rf === 'primary'
}

function isDevopsJob(j) {
    const t = (j.title || '').toLowerCase()
    return hasWord(t, ['devops', 'cloud', 'platform', 'sre', 'site reliability', 'infrastructure', 'kubernetes', 'ci/cd', 'system engineer', 'systems engineer', 'sysadmin'])
}

function isDataJob(j) {
    const t = (j.title || '').toLowerCase()
    const rf = (j.role_family || '').toLowerCase()
    return hasWord(t, ['data', 'analyst', 'analytics', 'scientist', 'machine learning', 'ml', 'mlops', 'ai', 'artificial intelligence', 'veri', 'yapay zeka', 'analist', 'analisti', 'bi', 'business intelligence']) || rf === 'secondary'
}

function isTurkeyJob(j) {
    const loc = (j.location || '').toLowerCase()
    const src = (j.source || '').toLowerCase()
    const comp = (j.company || '').toLowerCase()
    return src === 'techcareer' || src === 'coderspace' || src === 'kariyer_net' ||
           loc.includes('istanbul') || loc.includes('i̇stanbul') || loc.includes('turkey') || loc.includes('türkiye') || loc.includes('ankara') || loc.includes('izmir') || loc.includes('bursa') || loc.includes('kocaeli') ||
           comp.includes('peak') || comp.includes('trendyol') || comp.includes('commencis') || comp.includes('iyzico')
}

function isEuropeJob(j) {
    const loc = (j.location || '').toLowerCase()
    const src = (j.source || '').toLowerCase()
    if (isTurkeyJob(j)) return false
    return src === 'arbeitnow' || src === 'eurocontrol' ||
           hasWord(loc, ['brussels', 'brussel', 'bruxelles', 'belgium', 'netherlands', 'amsterdam', 'rotterdam', 'eindhoven', 'utrecht', 'germany', 'deutschland', 'berlin', 'munich', 'münchen', 'hamburg', 'frankfurt', 'denmark', 'copenhagen', 'københavn', 'kobenhavn', 'aarhus', 'switzerland', 'zurich', 'zürich', 'geneva', 'london', 'united kingdom', 'uk', 'austria', 'vienna', 'dublin', 'ireland', 'stockholm', 'warsaw', 'paris', 'cluj', 'europe', 'emea', 'be', 'nl', 'de', 'dk', 'ch'])
}

function isAtsJob(j) {
    const src = (j.source || '').toLowerCase()
    return src === 'lever' || src === 'greenhouse' || src === 'smartrecruiters'
}

function toggleOnlyRelevant() {
    STATE.onlyRelevant = !STATE.onlyRelevant
    const btn = document.getElementById('relevance-btn')
    const ind = document.getElementById('relevance-indicator')
    if (STATE.onlyRelevant) {
        btn.className = "flex items-center gap-1.5 px-3 py-2 rounded-lg bg-emerald-500/15 border border-emerald-500/30 text-emerald-300 font-semibold text-xs transition shadow-sm hover:bg-emerald-500/25"
        ind.textContent = "✓"
    } else {
        btn.className = "flex items-center gap-1.5 px-3 py-2 rounded-lg bg-dark-card border border-dark-border text-dark-textMuted font-medium text-xs transition hover:text-dark-text"
        ind.textContent = "○"
    }
    updateCategoryCounts()
    applyFilters()
}

// ── Filter Application ────────────────────────────────────────────
function applyFilters(resetPage = true) {
    let list = [...STATE.rawJobs]

    // 0. Only Relevant Filter (Hide noise: < 5.0 or action == skip, but preserve user-triaged Kanban cards)
    if (STATE.onlyRelevant) {
        list = list.filter(j => {
            if (STATE.viewMode === 'kanban' && j.status && j.status !== 'new') return true
            return (j.match_score || 0) >= 5.0 && (j.ai_action || '').toLowerCase() !== 'skip'
        })
    }

    // 1. Category Filter
    if (STATE.category === 'top') {
        list = list.filter(j => (j.match_score || 0) >= 7.5)
    } else if (STATE.category === 'junior') {
        list = list.filter(isJuniorJob)
    } else if (STATE.category === 'backend') {
        list = list.filter(isBackendJob)
    } else if (STATE.category === 'devops') {
        list = list.filter(isDevopsJob)
    } else if (STATE.category === 'data') {
        list = list.filter(isDataJob)
    } else if (STATE.category === 'tr') {
        list = list.filter(isTurkeyJob)
    } else if (STATE.category === 'eu') {
        list = list.filter(isEuropeJob)
    } else if (STATE.category === 'ats') {
        list = list.filter(isAtsJob)
    }

    // 2. Search Filter (title, company, location, source, skills, description)
    if (STATE.search.trim()) {
        const q = STATE.search.toLowerCase().trim()
        list = list.filter(j => 
            (j.title || '').toLowerCase().includes(q) ||
            (j.company || '').toLowerCase().includes(q) ||
            (j.location || '').toLowerCase().includes(q) ||
            (j.source || '').toLowerCase().includes(q) ||
            (j.skill_overlap || []).some(s => s.toLowerCase().includes(q)) ||
            (j.description || '').toLowerCase().includes(q)
        )
    }

    // 2b. Kanban City & Min-Score Quick Filters
    if (STATE.viewMode === 'kanban') {
        if (STATE.kanbanCity) {
            list = list.filter(j => matchesKanbanCity(j, STATE.kanbanCity))
        }
        if (STATE.kanbanMinScore > 0) {
            list = list.filter(j => (j.match_score || 0) >= STATE.kanbanMinScore)
        }
    }

    // 3. Action Filter
    if (STATE.action) {
        list = list.filter(j => (j.ai_action || '').toLowerCase() === STATE.action.toLowerCase())
    }

    // 4. Status Filter
    if (STATE.status) {
        list = list.filter(j => (j.status || '').toLowerCase() === STATE.status.toLowerCase())
    }

    // 5. Sorting
    if (STATE.sort === 'score') {
        list.sort((a, b) => (b.match_score || 0) - (a.match_score || 0))
    } else if (STATE.sort === 'date') {
        list.sort((a, b) => new Date(b.scraped_at) - new Date(a.scraped_at))
    }

    STATE.filteredJobs = list
    const maxPage = Math.max(1, Math.ceil(list.length / STATE.limit))
    if (resetPage || STATE.page > maxPage) {
        STATE.page = 1
    }
    if (STATE.viewMode === 'kanban') {
        renderKanban()
    } else {
        renderJobs()
    }
}

// ── View Mode Switching ───────────────────────────────────────────
function setViewMode(mode) {
    const btnFeed = document.getElementById('btn-view-feed')
    const btnKanban = document.getElementById('btn-view-kanban')
    const feedCont = document.getElementById('jobs-container')
    const kanbanCont = document.getElementById('kanban-container')
    const pagination = document.getElementById('pagination')

    STATE.viewMode = mode
    if (mode === 'kanban') {
        btnKanban.className = "flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-semibold transition bg-cyan-500/20 text-cyan-300 border border-cyan-500/30"
        btnFeed.className = "flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium text-dark-text hover:text-white transition"
        feedCont.classList.add('hidden')
        kanbanCont.classList.remove('hidden')
        pagination.classList.add('hidden')
        applyFilters(false)
    } else {
        btnFeed.className = "flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-semibold transition bg-cyan-500/20 text-cyan-300 border border-cyan-500/30"
        btnKanban.className = "flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-medium text-dark-text hover:text-white transition"
        kanbanCont.classList.add('hidden')
        feedCont.classList.remove('hidden')
        pagination.classList.remove('hidden')
        applyFilters(false)
    }
}

// ── Render Jobs Feed ──────────────────────────────────────────────
function renderJobs() {
    const container = document.getElementById('jobs-container')
    const countEl = document.getElementById('results-count')
    const total = STATE.filteredJobs.length

    countEl.innerHTML = `<span class="font-semibold text-dark-textHeading">${total}</span> ilan gösteriliyor`

    if (total === 0) {
        container.className = "col-span-2 text-center py-16 bg-dark-surface/30 rounded-2xl border border-dark-border"
        container.innerHTML = `
            <div class="text-4xl mb-3">📭</div>
            <div class="text-sm font-semibold text-dark-textHeading mb-1">Eşleşen ilan bulunamadı</div>
            <p class="text-xs text-dark-textMuted max-w-sm mx-auto mb-4">Seçili kategoride veya arama filtresinde herhangi bir ilan yer almıyor.</p>
            <button onclick="resetFilters()" class="px-3 py-1.5 rounded-lg text-xs bg-dark-card border border-dark-border text-cyan-400 hover:border-cyan-400 transition">Filtreleri Sıfırla</button>
        `
        document.getElementById('pagination').innerHTML = ''
        return
    }

    container.className = "grid grid-cols-1 lg:grid-cols-2 gap-4"

    const start = (STATE.page - 1) * STATE.limit
    const paginated = STATE.filteredJobs.slice(start, start + STATE.limit)

    container.innerHTML = paginated.map(j => renderJobCard(j)).join('')
    renderPagination(total)
}

// ── Single Job Card HTML ──────────────────────────────────────────
function renderJobCard(j) {
    const meta = SOURCE_META[j.source] || { label: j.source, badge: 'bg-slate-700/30 text-slate-300 border-slate-600' }
    const score = j.match_score || 0
    const scoreColor = getScoreStyle(score)
    const locInfo = formatLocationBadge(j.location)

    const overlapTags = (j.skill_overlap || []).slice(0, 4)
    const missingTags = (j.missing_skills || []).slice(0, 2)
    const redFlags = (j.red_flags || []).slice(0, 1)
    const statusBadge = getStatusBadge(j.status)

    return `
    <div id="card-${j.id}" class="job-card bg-dark-surface border border-dark-border rounded-xl p-4 flex flex-col justify-between gap-3 relative">
        <div class="space-y-2">
            <div class="flex items-center justify-between gap-2">
                <div class="flex items-center gap-1.5 flex-wrap">
                    <span class="px-2 py-0.5 rounded text-[10px] font-bold border ${meta.badge}">${meta.label}</span>
                    <span class="px-2 py-0.5 rounded text-[10px] font-medium bg-dark-card text-dark-textMuted border border-dark-border">${escapeHtml(locInfo)}</span>
                    ${statusBadge}
                </div>
                <div class="flex items-center gap-1 px-2.5 py-1 rounded-lg border font-mono font-bold text-xs ${scoreColor}">
                    <span>⚡</span>
                    <span>${score.toFixed(1)}</span>
                </div>
            </div>

            <div class="cursor-pointer group" onclick="openDetailModal('${j.id}')">
                <div class="text-xs font-semibold text-cyan-400 mb-0.5 flex items-center gap-1">
                    <span>${escapeHtml(j.company)}</span>
                </div>
                <h3 class="text-sm font-bold text-dark-textHeading group-hover:text-cyan-300 transition line-clamp-2">
                    ${escapeHtml(j.title)}
                </h3>
            </div>

            ${j.ai_reasoning ? `
            <div class="p-2.5 rounded-lg bg-dark-card/60 border border-dark-border/80 text-[11px] text-dark-textMuted italic flex items-start gap-2">
                <span class="text-cyan-400 font-bold text-xs">“</span>
                <p class="line-clamp-2">${escapeHtml(j.ai_reasoning)}</p>
            </div>` : ''}

            <div class="flex flex-wrap gap-1 pt-1">
                ${overlapTags.map(s => `<span class="px-2 py-0.5 rounded text-[10px] font-medium bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">✓ ${escapeHtml(s)}</span>`).join('')}
                ${missingTags.map(s => `<span class="px-2 py-0.5 rounded text-[10px] font-medium bg-amber-500/10 text-amber-300 border border-amber-500/20">⚡ ${escapeHtml(s)}</span>`).join('')}
                ${redFlags.map(s => `<span class="px-2 py-0.5 rounded text-[10px] font-medium bg-rose-500/10 text-rose-400 border border-rose-500/20">⚠ ${escapeHtml(s)}</span>`).join('')}
            </div>
        </div>

        <div class="pt-3 border-t border-dark-border flex items-center justify-between gap-2 mt-1">
            <div class="flex items-center gap-1.5 flex-wrap">
                <select onchange="updateJobStatus('${j.id}', this.value)" class="text-[11px] px-2 py-1 rounded bg-dark-card border border-dark-border text-dark-text focus:outline-none focus:border-cyan-400/50">
                    <option value="new" ${j.status === 'new' ? 'selected' : ''}>Yeni</option>
                    <option value="applied" ${j.status === 'applied' ? 'selected' : ''}>Başvurdum</option>
                    <option value="interview" ${j.status === 'interview' ? 'selected' : ''}>Mülakat</option>
                    <option value="offer" ${j.status === 'offer' ? 'selected' : ''}>Teklif 🎉</option>
                    <option value="rejected" ${j.status === 'rejected' ? 'selected' : ''}>Reddedildi</option>
                    <option value="skipped" ${j.status === 'skipped' ? 'selected' : ''}>Geçildi</option>
                </select>

                ${j.status === 'new' || !j.status ? `
                    <button onclick="updateJobStatus('${j.id}', 'applied')" class="px-2 py-1 rounded text-[10px] font-semibold bg-emerald-500/15 hover:bg-emerald-500/25 text-emerald-300 border border-emerald-500/30 transition flex items-center gap-1" title="Tek tıkla Başvuruldu yap">
                        <span>📨</span> Başvurdum
                    </button>
                    <button onclick="updateJobStatus('${j.id}', 'skipped')" class="px-2 py-1 rounded text-[10px] font-medium bg-slate-700/30 hover:bg-slate-700/60 text-slate-300 border border-slate-600/50 transition flex items-center gap-1" title="Tek tıkla Geçildi yap (AI geri bildirimini besler)">
                        <span>⏭</span> Geç
                    </button>
                ` : j.status === 'applied' ? `
                    <button onclick="updateJobStatus('${j.id}', 'interview')" class="px-2 py-1 rounded text-[10px] font-semibold bg-purple-500/15 hover:bg-purple-500/25 text-purple-300 border border-purple-500/30 transition flex items-center gap-1" title="Tek tıkla Mülakata taşı">
                        <span>🎤</span> Mülakat
                    </button>
                ` : j.status === 'interview' ? `
                    <button onclick="updateJobStatus('${j.id}', 'offer')" class="px-2 py-1 rounded text-[10px] font-semibold bg-amber-500/15 hover:bg-amber-500/25 text-amber-300 border border-amber-500/30 transition flex items-center gap-1" title="Tek tıkla Teklif durumuna al">
                        <span>🎉</span> Teklif
                    </button>
                ` : ''}

                <button onclick="deleteSingleJob('${j.id}')" class="p-1.5 rounded text-dark-textMuted hover:text-rose-400 hover:bg-rose-500/10 transition" title="Bu İlanı Kalıcı Olarak Sil">
                    <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16"/></svg>
                </button>
            </div>

            <div class="flex items-center gap-1.5">
                <button onclick="openDetailModal('${j.id}')" class="px-2.5 py-1 rounded text-xs font-medium text-dark-text hover:text-white bg-dark-card border border-dark-border hover:border-slate-500 transition">
                    Detay
                </button>
                <a href="${safeUrl(j.url)}" target="_blank" rel="noopener noreferrer" class="px-3 py-1 rounded text-xs font-bold bg-cyan-500/15 text-cyan-300 hover:bg-cyan-500 hover:text-black border border-cyan-500/30 transition flex items-center gap-1">
                    <span>İlana Git</span>
                    <span class="text-[10px]">↗</span>
                </a>
            </div>
        </div>
    </div>`
}

// ── Location Formatting ───────────────────────────────────────────
function formatLocationBadge(loc) {
    if (!loc) return '🌐 Belirtilmemiş'
    const l = loc.toLowerCase()
    if (l.includes('istanbul') || l.includes('i̇stanbul') || l.includes('turkey') || l.includes('türkiye') || l.includes('ankara') || l.includes('izmir') || l.includes('bursa') || l.includes('kocaeli') || l.includes('hatay')) return `🇹🇷 ${loc}`
    if (l.includes('brussel') || l.includes('bruxelles') || l.includes('belgium') || hasWord(l, ['be'])) return `🇧🇪 ${loc}`
    if (l.includes('amsterdam') || l.includes('rotterdam') || l.includes('eindhoven') || l.includes('utrecht') || l.includes('netherlands') || hasWord(l, ['nl'])) return `🇳🇱 ${loc}`
    if (l.includes('berlin') || l.includes('munich') || l.includes('münchen') || l.includes('frankfurt') || l.includes('hamburg') || l.includes('germany') || l.includes('deutschland') || hasWord(l, ['de'])) return `🇩🇪 ${loc}`
    if (l.includes('copenhagen') || l.includes('københavn') || l.includes('kobenhavn') || l.includes('aarhus') || l.includes('denmark') || hasWord(l, ['dk'])) return `🇩🇰 ${loc}`
    if (l.includes('zurich') || l.includes('zürich') || l.includes('geneva') || l.includes('zug') || l.includes('switzerland') || hasWord(l, ['ch'])) return `🇨🇭 ${loc}`
    if (l.includes('london') || l.includes('united kingdom') || hasWord(l, ['uk', 'gb'])) return `🇬🇧 ${loc}`
    if (l.includes('paris') || l.includes('france')) return `🇫🇷 ${loc}`
    if (l.includes('vienna') || l.includes('wien') || l.includes('austria')) return `🇦🇹 ${loc}`
    if (l.includes('dublin') || l.includes('ireland')) return `🇮🇪 ${loc}`
    if (l.includes('warsaw') || l.includes('poland')) return `🇵🇱 ${loc}`
    if (l.includes('cluj') || l.includes('romania')) return `🇷🇴 ${loc}`
    if (l.includes('remote') || l.includes('hybrid')) return `🌐 ${loc}`
    return `📍 ${loc}`
}

// ── Score Styling ─────────────────────────────────────────────────
function getScoreStyle(score) {
    if (score >= 8.0) return 'bg-emerald-500/15 text-emerald-300 border-emerald-500/30'
    if (score >= 7.0) return 'bg-cyan-500/15 text-cyan-300 border-cyan-500/30'
    if (score >= 5.0) return 'bg-amber-500/15 text-amber-300 border-amber-500/30'
    return 'bg-rose-500/10 text-rose-400 border-rose-500/20'
}

// ── Status Badges ─────────────────────────────────────────────────
function getStatusBadge(status) {
    if (!status || status === 'new') return ''
    const map = {
        applied:   'bg-emerald-500/20 text-emerald-300 border-emerald-500/30',
        interview: 'bg-cyan-500/20 text-cyan-300 border-cyan-500/30',
        offer:     'bg-purple-500/20 text-purple-300 border-purple-500/30',
        rejected:  'bg-rose-500/20 text-rose-300 border-rose-500/30',
        skipped:   'bg-slate-700/40 text-slate-400 border-slate-600'
    }
    const labels = {
        applied: 'Başvuruldu', interview: 'Mülakat', offer: 'Teklif 🎉', rejected: 'Red', skipped: 'Atlandı'
    }
    return `<span class="px-2 py-0.5 rounded text-[10px] font-bold border ${map[status] || ''}">${labels[status] || status}</span>`
}

// ── Detail Modal Management ───────────────────────────────────────
function openDetailModal(id) {
    const j = STATE.rawJobs.find(x => x.id === id)
    if (!j) return
    STATE.selectedJob = j

    const meta = SOURCE_META[j.source] || { label: j.source, badge: 'bg-slate-700/30 text-slate-300 border-slate-600' }
    const score = j.match_score || 0
    const scoreColor = getScoreStyle(score)

    document.getElementById('modal-badges').innerHTML = `
        <span class="px-2 py-0.5 rounded text-[10px] font-bold border ${meta.badge}">${meta.label}</span>
        ${j.role_family ? `<span class="px-2 py-0.5 rounded text-[10px] font-medium bg-dark-card text-dark-text border border-dark-border">${j.role_family}</span>` : ''}
        ${getStatusBadge(j.status)}
    `

    document.getElementById('modal-title').textContent = j.title
    document.getElementById('modal-company').textContent = j.company
    document.getElementById('modal-location').textContent = formatLocationBadge(j.location)
    document.getElementById('modal-posted').textContent = j.posted_at ? new Date(j.posted_at).toLocaleDateString('tr-TR') : 'Tarih belirtilmedi'

    const modalSel = document.getElementById('modal-status-select')
    if (modalSel) modalSel.value = j.status || 'new'

    const scoreEl = document.getElementById('modal-score')
    scoreEl.className = `text-center px-3 py-1.5 rounded-xl border font-mono font-bold text-base ${scoreColor}`
    scoreEl.innerHTML = `<div>${score.toFixed(1)}</div><div class="text-[9px] uppercase tracking-wider font-sans font-normal">AI Skor</div>`

    document.getElementById('modal-reasoning').textContent = j.ai_reasoning || 'AI gerekçesi henüz oluşturulmadı.'

    document.getElementById('modal-overlap').innerHTML = (j.skill_overlap || []).length
        ? j.skill_overlap.map(s => `<span class="px-2 py-0.5 rounded text-[10px] bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">✓ ${escapeHtml(s)}</span>`).join('')
        : '<span class="text-[11px] text-dark-textMuted italic">Eşleşen beceri yok</span>'

    document.getElementById('modal-missing').innerHTML = (j.missing_skills || []).length
        ? j.missing_skills.map(s => `<span class="px-2 py-0.5 rounded text-[10px] bg-amber-500/10 text-amber-300 border border-amber-500/20">⚡ ${escapeHtml(s)}</span>`).join('')
        : '<span class="text-[11px] text-dark-textMuted italic">Eksik tespit edilmedi</span>'

    document.getElementById('modal-flags').innerHTML = (j.red_flags || []).length
        ? j.red_flags.map(s => `<span class="px-2 py-0.5 rounded text-[10px] bg-rose-500/10 text-rose-400 border border-rose-500/20">⚠ ${escapeHtml(s)}</span>`).join('')
        : '<span class="text-[11px] text-dark-textMuted italic">Kırmızı bayrak yok</span>'

    const descEl = document.getElementById('modal-desc')
    descEl.textContent = j.description || 'Detaylı ilan açıklaması bu kaynak tarafından sağlanmadı.'
    if ((j.description || '').length >= 2000) {
        fetch(`/api/jobs/${encodeURIComponent(j.id)}`)
            .then(r => (r.ok ? r.json() : null))
            .then(full => {
                if (full && full.description && STATE.selectedJob && STATE.selectedJob.id === j.id) {
                    descEl.textContent = full.description
                }
            })
            .catch(() => {})
    }

    document.getElementById('modal-apply-link').href = safeUrl(j.url)
    document.getElementById('modal-delete-btn').onclick = () => {
        deleteSingleJob(j.id)
        closeDetailModal()
    }

    currentTailorData = null
    currentInterviewData = null
    activeTailorTab = 'letter'
    resetTailorAndInterviewDOM()
    const tailorContainer = document.getElementById('modal-tailor-container')
    if (tailorContainer) tailorContainer.classList.add('hidden')
    
    const tailorBtn = document.getElementById('modal-tailor-btn')
    const tailorBtnText = document.getElementById('modal-tailor-btn-text')
    if (tailorBtn && tailorBtnText) {
        tailorBtn.disabled = false
        tailorBtnText.textContent = j.has_tailored ? 'Başvuru Kitini Göster (Hazır)' : 'ATS CV & Ön Yazı Üret'
        if (score >= 7.0 || j.has_tailored) {
            tailorBtn.className = 'px-4 py-2 rounded-lg text-xs font-bold bg-gradient-to-r from-cyan-500 to-indigo-500 hover:from-cyan-400 hover:to-indigo-400 text-black transition shadow-lg shadow-cyan-500/25 ring-2 ring-cyan-400/80 ring-offset-2 ring-offset-dark-surface flex items-center gap-1.5'
        } else {
            tailorBtn.className = 'px-4 py-2 rounded-lg text-xs font-bold bg-dark-card border border-dark-border text-dark-text hover:text-white hover:border-cyan-400/50 transition flex items-center gap-1.5'
        }
    }

    const interviewBtn = document.getElementById('modal-interview-btn')
    const interviewBtnText = document.getElementById('modal-interview-btn-text')
    if (interviewBtn && interviewBtnText) {
        interviewBtn.disabled = false
        interviewBtnText.textContent = j.has_interview_prep ? 'Mülakat Kiti (Hazır)' : 'Mülakat Kiti'
    }

    document.getElementById('detail-modal').classList.remove('hidden')
    document.body.style.overflow = 'hidden'

    preloadModalKits(j)
}

async function preloadModalKits(j) {
    if (!j) return
    if (j.has_tailored) {
        await generateTailoredAssets(false, 'letter')
    }
    if (STATE.selectedJob && STATE.selectedJob.id === j.id && j.has_interview_prep) {
        const shouldFocusInterview = !j.has_tailored || j.status === 'interview'
        await generateInterviewPrep(false, shouldFocusInterview)
    }
}

function resetTailorAndInterviewDOM() {
    const idsToClearText = ['tailor-letter-text', 'tailor-cv-summary', 'interview-role-focus']
    idsToClearText.forEach(id => {
        const el = document.getElementById(id)
        if (el) el.textContent = ''
    })
    const idsToClearHtml = [
        'tailor-cv-experience', 'tailor-cv-projects',
        'tailor-keywords-list', 'tailor-skills-list',
        'interview-technical-list', 'interview-behavioral-list', 'interview-reverse-list'
    ]
    idsToClearHtml.forEach(id => {
        const el = document.getElementById(id)
        if (el) el.innerHTML = ''
    })
}

function closeDetailModal() {
    document.getElementById('detail-modal').classList.add('hidden')
    document.body.style.overflow = 'auto'
    STATE.selectedJob = null
    currentTailorData = null
    currentInterviewData = null
}

// ── ATS Tailor, Cover Letter & Interview Prep Logic ───────────────
let activeTailorTab = 'letter'
let currentTailorData = null
let currentInterviewData = null
let copyFeedbackTimer = null

function refreshActiveKit() {
    if (activeTailorTab === 'interview') {
        generateInterviewPrep(true, true)
    } else {
        generateTailoredAssets(true, activeTailorTab || 'letter')
    }
}

function openInterviewPrepTab() {
    if (!STATE.selectedJob) return
    const container = document.getElementById('modal-tailor-container')
    if (container) container.classList.remove('hidden')
    if (currentInterviewData) {
        renderTabView('interview')
        container.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
    } else {
        generateInterviewPrep(false, true)
    }
}

async function generateTailoredAssets(force = false, targetTab = 'letter') {
    if (!STATE.selectedJob) return
    const jobId = STATE.selectedJob.id
    const container = document.getElementById('modal-tailor-container')
    const loading = document.getElementById('tailor-loading')
    const loadingTitle = document.getElementById('tailor-loading-title')
    const loadingDesc = document.getElementById('tailor-loading-desc')
    const contentLetter = document.getElementById('tailor-content-letter')
    const contentCv = document.getElementById('tailor-content-cv')
    const contentKw = document.getElementById('tailor-content-keywords')
    const contentInt = document.getElementById('tailor-content-interview')
    const btn = document.getElementById('modal-tailor-btn')
    const btnText = document.getElementById('modal-tailor-btn-text')

    try {
        container.classList.remove('hidden')
        loading.classList.remove('hidden')
        if (loadingTitle) loadingTitle.textContent = 'Gemini ATS Optimizasyonu Yapıyor...'
        if (loadingDesc) loadingDesc.textContent = 'İlanın 4000 karakterlik açıklaması ve Master CV profiliniz taranıyor; nokta atışı Cover Letter ve CV başarı maddeleri hazırlanıyor.'
        contentLetter.classList.add('hidden')
        contentCv.classList.add('hidden')
        contentKw.classList.add('hidden')
        if (contentInt) contentInt.classList.add('hidden')
        if (btn) btn.disabled = true
        if (btnText) btnText.textContent = 'Üretiliyor...'

        container.scrollIntoView({ behavior: 'smooth', block: 'nearest' })

        const data = await API.tailorJob(jobId, force)
        if (!STATE.selectedJob || STATE.selectedJob.id !== jobId) return
        currentTailorData = data

        STATE.selectedJob.has_tailored = true
        const rawItem = STATE.rawJobs.find(x => x.id === jobId)
        if (rawItem) rawItem.has_tailored = true
        const filtItem = STATE.filteredJobs.find(x => x.id === jobId)
        if (filtItem) filtItem.has_tailored = true

        renderTailoredData(data)
        const tabToOpen = (targetTab && targetTab !== 'interview') ? targetTab : 'letter'
        renderTabView(tabToOpen)

        if (!data.cached || force) {
            toast('ATS CV & Ön Yazı başarıyla üretildi!', 'success')
        }
    } catch (err) {
        console.error('Tailor error:', err)
        toast(`Üretim başarısız: ${err.message}`, 'error')
        if (!currentInterviewData) container.classList.add('hidden')
        else renderTabView('interview')
    } finally {
        loading.classList.add('hidden')
        if (btn) btn.disabled = false
        if (btnText) btnText.textContent = (STATE.selectedJob && STATE.selectedJob.has_tailored) ? 'Başvuru Kitini Göster (Hazır)' : 'ATS CV & Ön Yazı Üret'
    }
}

async function generateInterviewPrep(force = false, switchAfter = true) {
    if (!STATE.selectedJob) return
    const jobId = STATE.selectedJob.id
    const container = document.getElementById('modal-tailor-container')
    const loading = document.getElementById('tailor-loading')
    const loadingTitle = document.getElementById('tailor-loading-title')
    const loadingDesc = document.getElementById('tailor-loading-desc')
    const contentLetter = document.getElementById('tailor-content-letter')
    const contentCv = document.getElementById('tailor-content-cv')
    const contentKw = document.getElementById('tailor-content-keywords')
    const contentInt = document.getElementById('tailor-content-interview')
    const intBtn = document.getElementById('modal-interview-btn')
    const intBtnText = document.getElementById('modal-interview-btn-text')

    try {
        container.classList.remove('hidden')
        if (switchAfter) {
            loading.classList.remove('hidden')
            if (loadingTitle) loadingTitle.textContent = 'Gemini Mülakat Hazırlık Kitini Hazırlıyor...'
            if (loadingDesc) loadingDesc.textContent = 'Şirketin teknoloji yığını ve projelerinize özel 5 Teknik + 3 STAR Mülakat Sorusu ve Cevap Rehberi kurgulanıyor.'
            contentLetter.classList.add('hidden')
            contentCv.classList.add('hidden')
            contentKw.classList.add('hidden')
            if (contentInt) contentInt.classList.add('hidden')
            container.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
        }
        if (intBtn) intBtn.disabled = true
        if (intBtnText) intBtnText.textContent = 'Hazırlanıyor...'

        const data = await API.interviewPrep(jobId, force)
        if (!STATE.selectedJob || STATE.selectedJob.id !== jobId) return
        currentInterviewData = data

        STATE.selectedJob.has_interview_prep = true
        const rawItem = STATE.rawJobs.find(x => x.id === jobId)
        if (rawItem) rawItem.has_interview_prep = true
        const filtItem = STATE.filteredJobs.find(x => x.id === jobId)
        if (filtItem) filtItem.has_interview_prep = true

        renderInterviewPrepData(data)
        if (switchAfter) {
            renderTabView('interview')
        }

        if (!data.cached || force) {
            toast('🎤 Mülakat Hazırlık Kiti başarıyla üretildi!', 'success')
        }
    } catch (err) {
        console.error('Interview prep error:', err)
        toast(`Mülakat kiti üretilemedi: ${err.message}`, 'error')
        if (!currentTailorData) container.classList.add('hidden')
        else renderTabView('letter')
    } finally {
        loading.classList.add('hidden')
        if (intBtn) intBtn.disabled = false
        if (intBtnText) intBtnText.textContent = (STATE.selectedJob && STATE.selectedJob.has_interview_prep) ? 'Mülakat Kiti (Hazır)' : 'Mülakat Kiti'
    }
}

function renderTailoredData(data) {
    document.getElementById('tailor-letter-text').textContent = data.cover_letter || 'Ön yazı oluşturulamadı.'
    document.getElementById('tailor-cv-summary').textContent = data.tailored_summary || 'Özet oluşturulamadı.'

    const expEl = document.getElementById('tailor-cv-experience')
    expEl.innerHTML = (data.tailored_experience_bullets || []).map(b => `
        <li class="pl-1">${escapeHtml(b)}</li>
    `).join('')

    const projEl = document.getElementById('tailor-cv-projects')
    projEl.innerHTML = (data.tailored_project_bullets || []).map(b => `
        <li class="pl-1">${escapeHtml(b)}</li>
    `).join('')

    const kwEl = document.getElementById('tailor-keywords-list')
    kwEl.innerHTML = (data.ats_keywords || []).map(k => {
        const isGap = /\(gap|\beksik\b/i.test(k)
        const cls = isGap
            ? 'bg-rose-500/15 text-rose-300 border-rose-500/30'
            : 'bg-emerald-500/15 text-emerald-300 border-emerald-500/30'
        const icon = isGap ? '⚠' : '✓'
        return `<span class="px-2 py-0.5 rounded text-[11px] font-medium border ${cls}">${icon} ${escapeHtml(k)}</span>`
    }).join('')

    const skEl = document.getElementById('tailor-skills-list')
    skEl.innerHTML = (data.skills_highlight || []).map(s => `
        <span class="px-2 py-0.5 rounded text-[11px] font-bold bg-cyan-500/15 text-cyan-300 border border-cyan-500/30">⚡ ${escapeHtml(s)}</span>
    `).join('')
}

function renderInterviewPrepData(data) {
    document.getElementById('interview-role-focus').textContent = data.role_focus_summary || 'Rol odak özeti bulunamadı.'

    const techEl = document.getElementById('interview-technical-list')
    techEl.innerHTML = (data.technical_questions || []).map((q, idx) => `
        <div class="p-3.5 rounded-xl bg-black/40 border border-dark-border space-y-2">
            <div class="font-bold text-xs text-cyan-300 flex items-start gap-2">
                <span class="px-1.5 py-0.5 rounded bg-cyan-500/20 text-cyan-300 font-mono text-[10px]">T${idx + 1}</span>
                <span>${escapeHtml(q.question)}</span>
            </div>
            <div class="text-[11px] text-amber-300/90 bg-amber-500/10 border border-amber-500/20 rounded-lg px-2.5 py-1.5">
                <span class="font-semibold">🎯 Neden Soruyorlar:</span> ${escapeHtml(q.why_they_ask)}
            </div>
            <div class="text-xs text-slate-200 leading-relaxed pl-1">
                <span class="font-semibold text-emerald-400">💡 Cevap Stratejin:</span> ${escapeHtml(q.answer_strategy)}
            </div>
        </div>
    `).join('')

    const behEl = document.getElementById('interview-behavioral-list')
    behEl.innerHTML = (data.behavioral_questions || []).map((b, idx) => `
        <div class="p-3.5 rounded-xl bg-black/40 border border-dark-border space-y-2">
            <div class="font-bold text-xs text-emerald-300 flex items-start gap-2">
                <span class="px-1.5 py-0.5 rounded bg-emerald-500/20 text-emerald-300 font-mono text-[10px]">STAR ${idx + 1}</span>
                <span>${escapeHtml(b.question)}</span>
            </div>
            <div class="text-xs text-slate-200 leading-relaxed pl-1">
                <span class="font-semibold text-purple-300">🧭 Hikaye Kurgusu (STAR):</span> ${escapeHtml(b.star_story_guide)}
            </div>
        </div>
    `).join('')

    const revEl = document.getElementById('interview-reverse-list')
    revEl.innerHTML = (data.questions_to_ask || []).map(r => `
        <li class="pl-1">${escapeHtml(r)}</li>
    `).join('')
}

function switchTailorTab(tab) {
    if (tab === 'interview' && !currentInterviewData) {
        generateInterviewPrep(false, true)
        return
    }
    if (tab !== 'interview' && !currentTailorData) {
        generateTailoredAssets(false, tab)
        return
    }
    renderTabView(tab)
}

function getCopyLabelForTab(tab) {
    if (tab === 'letter') return 'Ön Yazıyı Kopyala'
    if (tab === 'cv') return 'CV Maddelerini Kopyala'
    if (tab === 'interview') return 'Mülakat Kitini Kopyala'
    return 'Kelimeleri Kopyala'
}

function renderTabView(tab) {
    activeTailorTab = tab
    const tabs = ['letter', 'cv', 'keywords', 'interview']
    tabs.forEach(t => {
        const btn = document.getElementById(`tab-btn-${t}`)
        const content = document.getElementById(`tailor-content-${t}`)
        if (btn && content) {
            if (t === tab) {
                btn.className = t === 'interview'
                    ? 'px-3 py-1.5 rounded-lg font-semibold bg-purple-500/25 text-purple-300 border border-purple-500/50 transition whitespace-nowrap flex items-center gap-1'
                    : 'px-3 py-1.5 rounded-lg font-semibold bg-cyan-500/20 text-cyan-300 border border-cyan-500/40 transition whitespace-nowrap'
                content.classList.remove('hidden')
            } else {
                btn.className = 'px-3 py-1.5 rounded-lg font-medium text-dark-textMuted hover:text-dark-text transition whitespace-nowrap flex items-center gap-1'
                content.classList.add('hidden')
            }
        }
    })

    const cacheBadge = document.getElementById('tailor-cache-badge')
    if (cacheBadge) {
        const isCached = tab === 'interview'
            ? (currentInterviewData && currentInterviewData.cached)
            : (currentTailorData && currentTailorData.cached)
        if (isCached) cacheBadge.classList.remove('hidden')
        else cacheBadge.classList.add('hidden')
    }

    const copyLabel = document.getElementById('tailor-copy-label')
    if (copyLabel) {
        copyLabel.textContent = getCopyLabelForTab(tab)
    }
}

async function copyActiveTailorTab() {
    let textToCopy = ''

    if (activeTailorTab === 'interview') {
        if (!currentInterviewData) return
        const techText = (currentInterviewData.technical_questions || []).map((q, i) =>
            `[T${i + 1}] ${q.question}\n• Neden Soruyorlar: ${q.why_they_ask}\n• Cevap Stratejisi: ${q.answer_strategy}`
        ).join('\n\n')
        const starText = (currentInterviewData.behavioral_questions || []).map((b, i) =>
            `[STAR ${i + 1}] ${b.question}\n• STAR Rehberi: ${b.star_story_guide}`
        ).join('\n\n')
        const revText = (currentInterviewData.questions_to_ask || []).map(r => `• ${r}`).join('\n')

        textToCopy = `MÜLAKAT & ROL ODAK ÖZETİ:\n${currentInterviewData.role_focus_summary || ''}\n\n` +
                     `5 TEKNİK MÜLAKAT SORUSU & CEVAP REHBERİ:\n${techText}\n\n` +
                     `3 DAVRANIŞSAL (STAR) SORU REHBERİ:\n${starText}\n\n` +
                     `MÜLAKATÇIYA SORULACAK SORULAR:\n${revText}`
    } else {
        if (!currentTailorData) return
        if (activeTailorTab === 'letter') {
            textToCopy = currentTailorData.cover_letter || ''
        } else if (activeTailorTab === 'cv') {
            textToCopy = `TARGETED PROFESSIONAL SUMMARY:\n${currentTailorData.tailored_summary || ''}\n\n` +
                         `EXPERIENCE BULLETS:\n${(currentTailorData.tailored_experience_bullets || []).map(b => '• ' + b).join('\n')}\n\n` +
                         `KEY PROJECT BULLETS:\n${(currentTailorData.tailored_project_bullets || []).map(b => '• ' + b).join('\n')}`
        } else {
            textToCopy = `ATS KEYWORDS:\n${(currentTailorData.ats_keywords || []).join(', ')}\n\n` +
                         `RECOMMENDED SKILLS HIGHLIGHT:\n${(currentTailorData.skills_highlight || []).join(', ')}`
        }
    }

    try {
        await navigator.clipboard.writeText(textToCopy)
        const copyBtn = document.getElementById('tailor-copy-active-btn')
        const copyLabel = document.getElementById('tailor-copy-label')
        if (copyFeedbackTimer) clearTimeout(copyFeedbackTimer)

        if (copyLabel) copyLabel.textContent = 'Kopyalandı!'
        copyBtn.classList.replace('bg-cyan-500/20', 'bg-emerald-500/20')
        copyBtn.classList.replace('text-cyan-300', 'text-emerald-300')
        copyBtn.classList.replace('border-cyan-500/30', 'border-emerald-500/40')

        toast('Metin panoya kopyalandı!', 'success')

        copyFeedbackTimer = setTimeout(() => {
            const lbl = document.getElementById('tailor-copy-label')
            if (lbl) lbl.textContent = getCopyLabelForTab(activeTailorTab)
            copyBtn.classList.replace('bg-emerald-500/20', 'bg-cyan-500/20')
            copyBtn.classList.replace('text-emerald-300', 'text-cyan-300')
            copyBtn.classList.replace('border-emerald-500/40', 'border-cyan-500/30')
        }, 2000)
    } catch (err) {
        console.error('Clipboard error:', err)
        toast('Panoya kopyalanamadı, lütfen metni seçip kopyalayın', 'error')
    }
}

document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
        closeDetailModal()
        closeClearModal()
    }
})

// ── Single Job Deletion ───────────────────────────────────────────
async function deleteSingleJob(id) {
    try {
        const card = document.getElementById(`card-${id}`) || document.getElementById(`kanban-card-${id}`)
        if (card) {
            card.style.opacity = '0'
            card.style.transform = 'scale(0.95)'
            setTimeout(() => card.remove(), 200)
        }

        await API.deleteJob(id)
        
        STATE.rawJobs = STATE.rawJobs.filter(x => x.id !== id)
        STATE.filteredJobs = STATE.filteredJobs.filter(x => x.id !== id)
        updateCategoryCounts()
        updateStatsNavbar()
        if (STATE.viewMode === 'kanban') {
            renderKanban()
        }
        
        toast('İlan başarıyla silindi', 'success')
    } catch (err) {
        console.error('Silme başarısız:', err)
        toast('İlan silinemedi', 'error')
        load(false)
    }
}

// ── Clear All Modal Management ────────────────────────────────────
function openClearModal() {
    document.getElementById('confirm-modal').classList.remove('hidden')
}

function closeClearModal() {
    document.getElementById('confirm-modal').classList.add('hidden')
}

async function executeClearAll() {
    try {
        closeClearModal()
        await API.clearAll()
        STATE.rawJobs = []
        STATE.filteredJobs = []
        updateCategoryCounts()
        renderJobs()
        updateStatsNavbar()
        toast('Tüm ilanlar başarıyla temizlendi', 'success')
    } catch (err) {
        console.error('Tümü temizlenemedi:', err)
        toast('Tümünü temizleme başarısız oldu', 'error')
    }
}

// ── Render Kanban Board ──────────────────────────────────────────
function renderKanban() {
    const kanbanColsEl = document.getElementById('kanban-columns')
    const countEl = document.getElementById('results-count')
    const total = STATE.filteredJobs.length
    countEl.innerHTML = `<span class="font-semibold text-dark-textHeading">${total}</span> ilan listeleniyor (İş Takip Panosu)`

    const columnsHtml = KANBAN_COLUMNS.map(col => {
        const colJobs = STATE.filteredJobs.filter(j => (j.status || 'new') === col.id)
        return `
        <div class="kanban-col bg-dark-surface/90 border border-dark-border rounded-2xl flex flex-col overflow-hidden shadow-xl"
             ondragover="onKanbanDragOver(event)"
             ondragleave="onKanbanDragLeave(event)"
             ondrop="onKanbanDrop(event, '${col.id}')"
             data-column-id="${col.id}">
            
            <div class="p-3 border-b border-dark-border flex items-center justify-between gap-2 bg-dark-card/60">
                <div class="flex items-center gap-2">
                    <span class="text-base">${col.icon}</span>
                    <h3 class="font-bold text-xs text-dark-textHeading tracking-tight">${col.label}</h3>
                </div>
                <span class="px-2 py-0.5 rounded-full text-[11px] font-mono font-bold border ${col.color}">
                    ${colJobs.length}
                </span>
            </div>

            <div class="kanban-col-cards p-2.5 space-y-2.5 flex-1">
                ${colJobs.length === 0 ? `
                    <div class="h-32 flex flex-col items-center justify-center text-center p-3 border border-dashed border-dark-border/80 rounded-xl text-dark-textMuted text-xs">
                        <span class="text-lg mb-1 opacity-40">${col.icon}</span>
                        <span>Bu aşamada ilan yok</span>
                    </div>
                ` : colJobs.map(j => renderKanbanCard(j)).join('')}
            </div>
        </div>
        `
    }).join('')

    kanbanColsEl.innerHTML = columnsHtml
}

// ── Kanban Single Card HTML ───────────────────────────────────────
function renderKanbanCard(j) {
    const meta = SOURCE_META[j.source] || { label: j.source, badge: 'bg-slate-700/30 text-slate-300 border-slate-600' }
    const score = j.match_score || 0
    const scoreColor = getScoreStyle(score)
    const locInfo = formatLocationBadge(j.location)
    const overlapTags = (j.skill_overlap || []).slice(0, 3)

    return `
    <div id="kanban-card-${j.id}"
         class="kanban-card bg-dark-card border border-dark-border hover:border-cyan-500/50 rounded-xl p-3 space-y-2 relative shadow-md group"
         draggable="true"
         ondragstart="onKanbanDragStart(event, '${j.id}')"
         ondragend="onKanbanDragEnd(event)">
        
        <div class="flex items-center justify-between gap-1.5">
            <span class="px-1.5 py-0.5 rounded text-[9px] font-bold border ${meta.badge}">${meta.label}</span>
            <div class="flex items-center gap-1 px-2 py-0.5 rounded border font-mono font-bold text-[11px] ${scoreColor}">
                <span>⚡</span>
                <span>${score.toFixed(1)}</span>
            </div>
        </div>

        <div class="cursor-pointer" onclick="openDetailModal('${j.id}')">
            <div class="text-[11px] font-semibold text-cyan-400 truncate mb-0.5">${escapeHtml(j.company)}</div>
            <h4 class="text-xs font-bold text-dark-textHeading group-hover:text-cyan-300 transition line-clamp-2 leading-tight">${escapeHtml(j.title)}</h4>
        </div>

        <div class="text-[10px] text-dark-textMuted truncate">${escapeHtml(locInfo)}</div>

        ${overlapTags.length > 0 ? `
        <div class="flex flex-wrap gap-1 pt-0.5">
            ${overlapTags.map(s => `<span class="px-1.5 py-0.5 rounded text-[9px] font-medium bg-emerald-500/10 text-emerald-400 border border-emerald-500/20">✓ ${escapeHtml(s)}</span>`).join('')}
        </div>` : ''}

        <div class="pt-2 border-t border-dark-border/80 flex items-center justify-between gap-1 mt-1">
            <select onchange="updateJobStatus('${j.id}', this.value)" onclick="event.stopPropagation()" class="text-[10px] px-1.5 py-1 rounded bg-dark-surface border border-dark-border text-dark-text focus:outline-none focus:border-cyan-400 max-w-[120px]">
                <option value="new" ${j.status === 'new' ? 'selected' : ''}>📥 Yeni</option>
                <option value="applied" ${j.status === 'applied' ? 'selected' : ''}>📨 Başvuruldu</option>
                <option value="interview" ${j.status === 'interview' ? 'selected' : ''}>🎤 Mülakat</option>
                <option value="offer" ${j.status === 'offer' ? 'selected' : ''}>🎉 Teklif</option>
                <option value="rejected" ${j.status === 'rejected' ? 'selected' : ''}>❌ Red</option>
                <option value="skipped" ${j.status === 'skipped' ? 'selected' : ''}>⏭ Atla</option>
            </select>

            <div class="flex items-center gap-1">
                ${j.status === 'new' || !j.status ? `
                    <button onclick="event.stopPropagation(); updateJobStatus('${j.id}', 'applied')" class="px-1.5 py-1 rounded text-[10px] font-bold bg-emerald-500/15 hover:bg-emerald-500/25 text-emerald-300 border border-emerald-500/30 transition" title="Başvuruldu olarak işaretle">
                        📨 Başvur
                    </button>
                ` : j.status === 'applied' ? `
                    <button onclick="event.stopPropagation(); updateJobStatus('${j.id}', 'interview')" class="px-1.5 py-1 rounded text-[10px] font-bold bg-purple-500/15 hover:bg-purple-500/25 text-purple-300 border border-purple-500/30 transition" title="Mülakata taşı">
                        🎤 Mülakat
                    </button>
                ` : j.status === 'interview' ? `
                    <button onclick="event.stopPropagation(); updateJobStatus('${j.id}', 'offer')" class="px-1.5 py-1 rounded text-[10px] font-bold bg-amber-500/15 hover:bg-amber-500/25 text-amber-300 border border-amber-500/30 transition" title="Teklif durumuna al">
                        🎉 Teklif
                    </button>
                ` : ''}

                <button onclick="openDetailModal('${j.id}')" class="px-1.5 py-1 rounded text-[10px] bg-dark-surface border border-dark-border text-dark-text hover:text-white transition" title="Detaylar">
                    👁️
                </button>
                <a href="${safeUrl(j.url)}" target="_blank" rel="noopener noreferrer" class="p-1 rounded bg-cyan-500/15 text-cyan-300 hover:bg-cyan-500 hover:text-black transition" title="İlana Git">
                    <svg class="w-3 h-3" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 6H6a2 2 0 00-2 2v10a2 2 0 002 2h10a2 2 0 002-2v-4M14 4h6m0 0v6m0-6L10 14"/></svg>
                </a>
            </div>
        </div>
    </div>
    `
}

// ── Drag & Drop Event Handlers ────────────────────────────────────
let draggedJobId = null

function onKanbanDragStart(e, id) {
    draggedJobId = id
    e.dataTransfer.setData('text/plain', id)
    e.dataTransfer.effectAllowed = 'move'
    const card = document.getElementById(`kanban-card-${id}`)
    if (card) {
        setTimeout(() => card.classList.add('is-dragging'), 0)
    }
}

function onKanbanDragEnd(e) {
    const card = document.querySelector('.kanban-card.is-dragging')
    if (card) card.classList.remove('is-dragging')
    document.querySelectorAll('.kanban-col').forEach(c => c.classList.remove('kanban-drop-target'))
    draggedJobId = null
}

function onKanbanDragOver(e) {
    e.preventDefault()
    e.dataTransfer.dropEffect = 'move'
    const col = e.currentTarget
    if (!col.classList.contains('kanban-drop-target')) {
        col.classList.add('kanban-drop-target')
    }
}

function onKanbanDragLeave(e) {
    const col = e.currentTarget
    col.classList.remove('kanban-drop-target')
}

function onKanbanDrop(e, targetStatus) {
    e.preventDefault()
    const col = e.currentTarget
    col.classList.remove('kanban-drop-target')
    const jobId = draggedJobId || e.dataTransfer.getData('text/plain')
    if (!jobId) return

    const job = STATE.rawJobs.find(x => x.id === jobId)
    if (job && job.status !== targetStatus) {
        updateJobStatus(jobId, targetStatus)
    }
}

// ── Update Job Status ─────────────────────────────────────────────
async function updateJobStatus(id, newStatus) {
    try {
        const targetRaw = STATE.rawJobs.find(x => x.id === id)
        if (targetRaw) targetRaw.status = newStatus

        const targetFiltered = STATE.filteredJobs.find(x => x.id === id)
        if (targetFiltered) targetFiltered.status = newStatus

        if (STATE.status) {
            applyFilters(false)
        } else if (STATE.viewMode === 'kanban') {
            renderKanban()
        } else {
            const card = document.getElementById(`card-${id}`)
            if (card && targetRaw) {
                card.outerHTML = renderJobCard(targetRaw)
            } else {
                renderJobs()
            }
        }

        if (STATE.selectedJob && STATE.selectedJob.id === id) {
            STATE.selectedJob.status = newStatus
            const modalSel = document.getElementById('modal-status-select')
            if (modalSel) modalSel.value = newStatus
            const modalBadges = document.getElementById('modal-badges')
            if (modalBadges && targetRaw) {
                const meta = SOURCE_META[targetRaw.source] || { label: targetRaw.source, badge: 'bg-slate-700/30 text-slate-300 border-slate-600' }
                modalBadges.innerHTML = `
                    <span class="px-2 py-0.5 rounded text-[10px] font-bold border ${meta.badge}">${meta.label}</span>
                    ${targetRaw.role_family ? `<span class="px-2 py-0.5 rounded text-[10px] font-medium bg-dark-card text-dark-text border border-dark-border">${targetRaw.role_family}</span>` : ''}
                    ${getStatusBadge(newStatus)}
                `
            }
        }

        updateStatsNavbar()

        await API.updateStatus(id, newStatus)
        const label = STATUS_LABELS[newStatus] || newStatus
        toast(`Durum güncellendi: ${label}`, 'success')

        if (newStatus === 'interview') {
            if (STATE.selectedJob && STATE.selectedJob.id === id) {
                openInterviewPrepTab()
            } else if (targetRaw && !targetRaw.has_interview_prep) {
                toast('🎤 Mülakat Hazırlık Kiti (5 Teknik + 3 STAR) arka planda hazırlanıyor...', 'info')
                API.interviewPrep(id, false)
                    .then(() => {
                        if (targetRaw) targetRaw.has_interview_prep = true
                        if (targetFiltered) targetFiltered.has_interview_prep = true
                        toast('🎤 Mülakat Hazırlık Kiti hazır! İlan detayından inceleyebilirsiniz.', 'success')
                    })
                    .catch(e => console.error('Auto interview prep error:', e))
            }
        }
    } catch (err) {
        console.error('Durum güncellenemedi:', err)
        toast('Durum kaydedilemedi, geri alınıyor', 'error')
        load(false)
    }
}

function onModalStatusChange(val) {
    if (STATE.selectedJob) {
        updateJobStatus(STATE.selectedJob.id, val)
    }
}

// ── Category Switching ────────────────────────────────────────────
function setCategory(cat) {
    STATE.category = cat
    document.querySelectorAll('.cat-btn').forEach(btn => {
        if (btn.getAttribute('data-cat') === cat) {
            btn.className = "cat-btn active flex items-center gap-2 px-4 py-2.5 rounded-xl text-xs font-semibold whitespace-nowrap transition bg-brand-500/15 text-cyan-300 border border-brand-500/30"
        } else {
            btn.className = "cat-btn flex items-center gap-2 px-4 py-2.5 rounded-xl text-xs font-medium whitespace-nowrap transition text-dark-text hover:text-dark-textHeading hover:bg-white/5 border border-transparent"
        }
    })
    applyFilters()
}

// ── Search & Filter Controls ──────────────────────────────────────
let searchTimer = null
function handleSearch(val) {
    const topInput = document.getElementById('search-input')
    const kanbanInput = document.getElementById('kanban-search-input')
    if (topInput && topInput.value !== val) topInput.value = val
    if (kanbanInput && kanbanInput.value !== val) kanbanInput.value = val
    clearTimeout(searchTimer)
    searchTimer = setTimeout(() => {
        STATE.search = val
        applyFilters()
    }, 200)
}

function matchesKanbanCity(j, code) {
    const loc = (j.location || '').toLowerCase()
    if (code === 'tr') return isTurkeyJob(j)
    if (code === 'be') return loc.includes('brussel') || loc.includes('bruxelles') || loc.includes('belgium') || hasWord(loc, ['be'])
    if (code === 'nl') return loc.includes('amsterdam') || loc.includes('rotterdam') || loc.includes('eindhoven') || loc.includes('utrecht') || loc.includes('netherlands') || hasWord(loc, ['nl'])
    if (code === 'de') return loc.includes('berlin') || loc.includes('munich') || loc.includes('münchen') || loc.includes('frankfurt') || loc.includes('hamburg') || loc.includes('germany') || loc.includes('deutschland') || hasWord(loc, ['de'])
    if (code === 'dk') return loc.includes('copenhagen') || loc.includes('københavn') || loc.includes('kobenhavn') || loc.aarhus || loc.includes('denmark') || hasWord(loc, ['dk'])
    if (code === 'remote') return loc.includes('remote')
    return true
}

function setKanbanCity(cityCode) {
    STATE.kanbanCity = cityCode
    document.querySelectorAll('.kcity-btn').forEach(btn => {
        if (btn.getAttribute('data-kcity') === cityCode) {
            btn.className = "kcity-btn px-2.5 py-1 rounded-lg font-semibold bg-cyan-500/20 text-cyan-300 border border-cyan-500/30 transition"
        } else {
            btn.className = "kcity-btn px-2.5 py-1 rounded-lg font-medium bg-dark-card text-dark-text hover:text-white border border-dark-border transition"
        }
    })
    applyFilters(false)
}

function setKanbanMinScore(val) {
    STATE.kanbanMinScore = parseFloat(val) || 0
    applyFilters(false)
}

function setAction(val) {
    STATE.action = val
    applyFilters()
}

function setStatus(val) {
    STATE.status = val
    applyFilters()
}

function setSort(val) {
    STATE.sort = val
    applyFilters()
}

function resetFilters() {
    STATE.category = 'all'
    STATE.search = ''
    STATE.kanbanCity = ''
    STATE.kanbanMinScore = 0
    STATE.action = ''
    STATE.status = ''
    document.getElementById('search-input').value = ''
    const kSearch = document.getElementById('kanban-search-input')
    if (kSearch) kSearch.value = ''
    const kScore = document.getElementById('kanban-min-score')
    if (kScore) kScore.value = '0'
    document.getElementById('action-filter').value = ''
    document.getElementById('status-filter').value = ''
    setKanbanCity('')
    setCategory('all')
}

// ── Pagination Render ─────────────────────────────────────────────
function renderPagination(total) {
    const pgEl = document.getElementById('pagination')
    const totalPages = Math.ceil(total / STATE.limit)
    if (totalPages <= 1) {
        pgEl.innerHTML = ''
        return
    }

    let html = ''
    for (let i = 1; i <= Math.min(totalPages, 7); i++) {
        const isActive = i === STATE.page
        html += `
            <button onclick="goToPage(${i})" class="w-8 h-8 rounded-lg text-xs font-semibold transition ${isActive ? 'bg-cyan-500 text-black font-bold shadow-md shadow-cyan-500/20' : 'bg-dark-card border border-dark-border text-dark-text hover:text-white'}">
                ${i}
            </button>
        `
    }
    pgEl.innerHTML = html
}

function goToPage(p) {
    STATE.page = p
    renderJobs()
    window.scrollTo({ top: 0, behavior: 'smooth' })
}

// ── Loading Skeletons ─────────────────────────────────────────────
function renderLoadingState() {
    const container = document.getElementById('jobs-container')
    container.className = "grid grid-cols-1 lg:grid-cols-2 gap-4"
    container.innerHTML = Array(4).fill(`
        <div class="bg-dark-surface border border-dark-border rounded-xl p-4 space-y-3 animate-pulse">
            <div class="flex justify-between items-center">
                <div class="h-4 bg-slate-800 rounded w-24"></div>
                <div class="h-6 bg-slate-800 rounded w-12"></div>
            </div>
            <div class="h-5 bg-slate-800 rounded w-3/4"></div>
            <div class="h-10 bg-slate-800/60 rounded"></div>
            <div class="flex gap-2">
                <div class="h-4 bg-slate-800 rounded w-16"></div>
                <div class="h-4 bg-slate-800 rounded w-16"></div>
            </div>
        </div>
    `).join('')
}

// ── Toast Notifications ───────────────────────────────────────────
function toast(msg, type = 'info') {
    const container = document.getElementById('toast-container')
    const colors = {
        success: 'bg-emerald-500/90 text-black font-bold',
        error: 'bg-rose-500/90 text-white font-bold',
        info: 'bg-cyan-500/90 text-black font-semibold'
    }
    const el = document.createElement('div')
    el.className = `px-4 py-2.5 rounded-xl text-xs shadow-2xl transition transform translate-y-2 opacity-0 duration-200 flex items-center gap-2 ${colors[type] || colors.info}`
    el.textContent = msg

    container.appendChild(el)
    requestAnimationFrame(() => {
        el.classList.remove('translate-y-2', 'opacity-0')
    })

    setTimeout(() => {
        el.classList.add('opacity-0', 'translate-y-2')
        setTimeout(() => el.remove(), 200)
    }, 3000)
}

// ── HTML Escaping & Safe URL ──────────────────────────────────────
function escapeHtml(str) {
    if (!str) return ''
    return String(str)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
}

function safeUrl(raw) {
    if (!raw) return '#'
    try {
        const u = new URL(String(raw), window.location.origin)
        if (u.protocol === 'http:' || u.protocol === 'https:') {
            return escapeHtml(u.href)
        }
    } catch (_) {}
    return '#'
}
