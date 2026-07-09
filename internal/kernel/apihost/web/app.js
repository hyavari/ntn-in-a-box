// NTN-in-a-Box GUI — app.js
// SSE connection, state management, animation, metrics updates.

'use strict';

// --- State ---

const state = {
    inCoverage: false,
    elapsedSec: 0,
    untilNext: 0,
    windowSec: 90,
    periodSec: 600,
    lookaheadSec: 30,
    profileName: '',
    mode: 'periodic',
    metrics: { delay: 0, jitter: 0, loss: 0, bandwidth: 0 },
    history: { delay: [], jitter: [], loss: [], bandwidth: [] },
    connected: false,
    hasData: false,
};

const MAX_HISTORY = 20;

// --- DOM refs ---

const $ = (id) => document.getElementById(id);

const els = {
    sky: $('sky'),
    satellite: $('satellite'),
    beam: $('beam'),
    device: document.querySelector('.device-body'),
    linkStatus: $('linkStatus'),
    animProgress: $('animProgress'),
    coverageIndicator: $('coverageIndicator'),
    profileName: $('profileName'),
    countdown: $('countdown'),
    metricDelay: $('metricDelay'),
    metricJitter: $('metricJitter'),
    metricLoss: $('metricLoss'),
    metricBw: $('metricBw'),
    sparkDelay: $('sparkDelay'),
    sparkJitter: $('sparkJitter'),
    sparkLoss: $('sparkLoss'),
    sparkBw: $('sparkBw'),
    timelineBar: $('timelineBar'),
    timelinePeriod: $('timelinePeriod'),
    timelineLeft: $('timelineLeft'),
    timelineRight: $('timelineRight'),
    progressFill: $('progressFill'),
    progressInfo: $('progressInfo'),
    connectionBadge: $('connectionBadge'),
    idleOverlay: $('idleOverlay'),
    profileDetail: $('profileDetail'),
    profileMode: $('profileMode'),
    profileSchedule: $('profileSchedule'),
};

// --- SSE Connection ---

function connect() {
    showBadge('connecting...', '');

    const es = new EventSource('/events');

    es.addEventListener('coverage', (e) => {
        const data = JSON.parse(e.data);
        state.inCoverage = data.in_coverage;
        if (data.elapsed_sec > 0 || data.until_next_transition > 0) {
            state.elapsedSec = data.elapsed_sec;
            state.untilNext = data.until_next_transition;
        }
        // Reset elapsed on new window open (replay or live).
        if (data.kind === 'window_opened') {
            state.elapsedSec = data.elapsed_sec || 0;
        }
        state.hasData = true;
        hideIdle();
        updateAll();
    });

    es.addEventListener('linkstate', (e) => {
        const data = JSON.parse(e.data);
        state.metrics.delay = data.delay_ms;
        state.metrics.jitter = data.jitter_ms;
        state.metrics.loss = data.loss_pct;
        state.metrics.bandwidth = data.bandwidth_kbps;
        state.hasData = true;
        // Link-state events only arrive while in coverage, so if we
        // haven't received a coverage event yet, infer coverage.
        if (!state.inCoverage) {
            state.inCoverage = true;
            updateCoverageStatus();
            updateAnimation();
        }
        hideIdle();
        pushHistory();
        updateMetrics();
        updateSparklines();
    });

    es.onopen = () => {
        state.connected = true;
        showBadge('connected', 'connected');
        setTimeout(() => hideBadge(), 2000);
    };

    es.onerror = () => {
        state.connected = false;
        showBadge('reconnecting...', 'error');
    };
}

// --- Profile fetch ---

async function fetchProfile() {
    try {
        const resp = await fetch('/profiles');
        const profiles = await resp.json();
        if (profiles && profiles.length > 0) {
            const name = profiles[0].name || profiles[0].Name;
            const detailResp = await fetch(`/profiles/${name}`);
            const p = await detailResp.json();
            // Handle both capitalized (Go default) and lowercase JSON keys.
            const sched = p.schedule || p.Schedule || {};
            const mode = sched.mode || sched.Mode || 'periodic';
            const periodSec = sched.period_sec || sched.PeriodSec || 600;
            const windowSec = sched.window_sec || sched.WindowSec || 90;
            const lookaheadSec = sched.lookahead_sec || sched.LookaheadSec || 30;
            const profileName = p.name || p.Name || 'unknown';

            state.profileName = profileName;
            state.periodSec = periodSec;
            state.windowSec = windowSec;
            state.lookaheadSec = lookaheadSec;
            state.mode = mode;

            els.profileName.textContent = profileName;
            els.timelinePeriod.textContent = `period: ${state.periodSec}s`;

            // Profile details
            els.profileDetail.textContent = profileName;
            els.profileMode.textContent = mode;
            if (mode === 'periodic') {
                els.profileSchedule.textContent = `${periodSec}s period / ${windowSec}s window / ${lookaheadSec}s lookahead`;
            } else {
                els.profileSchedule.textContent = `${periodSec}s period (continuous)`;
            }
            buildTimeline();
        } else {
            // Replay mode: no profiles available. Use defaults.
            els.profileName.textContent = 'replay';
            els.profileDetail.textContent = 'replay';
            els.profileMode.textContent = state.mode;
            els.profileSchedule.textContent = `${state.periodSec}s period / ${state.windowSec}s window`;
            buildTimeline();
        }
    } catch (e) {
        console.warn('Failed to fetch profile:', e);
    }
}

// --- Update functions ---

function updateAll() {
    updateCoverageStatus();
    updateAnimation();
    updateProgressBar();
    updateTimeline();
}

function updateCoverageStatus() {
    if (state.inCoverage) {
        els.coverageIndicator.textContent = '▲ IN COVERAGE';
        els.coverageIndicator.classList.remove('out');
        els.linkStatus.textContent = '● LINKED';
        els.linkStatus.classList.remove('disconnected');
    } else {
        els.coverageIndicator.textContent = '▼ OUT OF COVERAGE';
        els.coverageIndicator.classList.add('out');
        els.linkStatus.textContent = '● NO LINK';
        els.linkStatus.classList.add('disconnected');
    }
    els.countdown.textContent = `${Math.round(state.untilNext)}s remaining`;
}

function updateAnimation() {
    if (state.inCoverage) {
        // Satellite position: map elapsedSec/windowSec to arc position
        const progress = Math.min(state.elapsedSec / state.windowSec, 1);
        const pos = positionOnArc(progress);
        els.satellite.style.left = pos.x + '%';
        els.satellite.style.top = pos.y + '%';
        els.satellite.classList.remove('hidden');

        // Beam: position at satellite X, stretch from satellite Y to device
        els.beam.style.left = pos.x + '%';
        els.beam.style.top = (pos.y + 2) + '%';
        els.beam.style.height = (85 - pos.y - 2) + '%';
        els.beam.style.width = (85 - pos.y) * 0.8 + 'px'; // wider when higher
        els.beam.classList.remove('hidden');

        // Device linked
        els.device.classList.add('linked');
        els.device.classList.remove('unlinked');

        // Sky bright
        els.sky.classList.remove('dark');

        // Progress
        els.animProgress.style.width = (progress * 100) + '%';
        els.animProgress.classList.remove('out');
    } else {
        // Out of coverage
        els.satellite.classList.add('hidden');
        els.beam.classList.add('hidden');
        els.device.classList.remove('linked');
        els.device.classList.add('unlinked');
        els.sky.classList.add('dark');

        // Gap progress
        const gap = state.periodSec - state.windowSec;
        const gapElapsed = gap - state.untilNext;
        const gapProgress = gap > 0 ? Math.min(gapElapsed / gap, 1) : 0;
        els.animProgress.style.width = (gapProgress * 100) + '%';
        els.animProgress.classList.add('out');
    }
}

function positionOnArc(progress) {
    // Parametric half-ellipse matching CSS orbit-arc:
    // left 8% to right 92%, arc top at ~10%, bottom at ~70%
    const angle = Math.PI * (1 - progress); // PI to 0 (left to right)
    const x = 8 + 84 * progress; // 8% to 92%
    // Ellipse: center at 40% vertically, radius 30%
    const y = 40 - 30 * Math.sin(angle); // peaks at 10% when overhead
    return { x, y };
}

function updateMetrics() {
    if (state.inCoverage && state.hasData) {
        els.metricDelay.innerHTML = formatMetric(state.metrics.delay, 'ms');
        els.metricJitter.innerHTML = formatMetric(state.metrics.jitter, 'ms');
        els.metricLoss.innerHTML = formatMetric(state.metrics.loss, '%');
        els.metricBw.innerHTML = formatMetric(state.metrics.bandwidth / 1000, 'Mbps');
    } else if (!state.inCoverage) {
        els.metricDelay.textContent = '—';
        els.metricJitter.textContent = '—';
        els.metricLoss.textContent = '—';
        els.metricBw.textContent = '—';
    }
}

function formatMetric(value, unit) {
    const v = value >= 100 ? Math.round(value) : value.toFixed(1);
    return `${v}<span class="unit">${unit}</span>`;
}

function updateProgressBar() {
    if (state.inCoverage) {
        const pct = Math.min(state.elapsedSec / state.windowSec * 100, 100);
        els.progressFill.style.width = pct + '%';
        els.progressFill.classList.remove('out');
        els.progressInfo.textContent = `${Math.round(pct)}% · ${Math.round(state.elapsedSec)}s / ${state.windowSec}s`;
    } else {
        const gap = state.periodSec - state.windowSec;
        const gapElapsed = gap - state.untilNext;
        const pct = gap > 0 ? Math.min(gapElapsed / gap * 100, 100) : 0;
        els.progressFill.style.width = pct + '%';
        els.progressFill.classList.add('out');
        els.progressInfo.textContent = `gap ${Math.round(pct)}% · next in ${Math.round(state.untilNext)}s`;
    }
}

// --- Sparklines ---

function pushHistory() {
    const h = state.history;
    h.delay.push(state.metrics.delay);
    h.jitter.push(state.metrics.jitter);
    h.loss.push(state.metrics.loss);
    h.bandwidth.push(state.metrics.bandwidth);
    if (h.delay.length > MAX_HISTORY) h.delay.shift();
    if (h.jitter.length > MAX_HISTORY) h.jitter.shift();
    if (h.loss.length > MAX_HISTORY) h.loss.shift();
    if (h.bandwidth.length > MAX_HISTORY) h.bandwidth.shift();
}

function updateSparklines() {
    renderSparkline(els.sparkDelay, state.history.delay);
    renderSparkline(els.sparkJitter, state.history.jitter);
    renderSparkline(els.sparkLoss, state.history.loss);
    renderSparkline(els.sparkBw, state.history.bandwidth);
}

function renderSparkline(svg, values) {
    if (values.length < 2) {
        svg.innerHTML = '';
        return;
    }
    const min = Math.min(...values);
    const max = Math.max(...values);
    const range = max - min || 1;
    const step = 100 / (values.length - 1);

    const points = values.map((v, i) => {
        const x = i * step;
        const y = 28 - ((v - min) / range) * 26; // 2px top margin, 2px bottom
        return `${x.toFixed(1)},${y.toFixed(1)}`;
    }).join(' ');

    svg.innerHTML = `<polyline points="${points}"/>`;
}

// --- Timeline ---

function buildTimeline() {
    updateTimeline();
}

function updateTimeline() {
    const bar = els.timelineBar;
    const total = state.periodSec * 2; // show 2 periods
    const windowPct = (state.windowSec / total) * 100;
    const gapPct = ((state.periodSec - state.windowSec) / total) * 100;

    // Build segments: [past-window, past-gap, current-window, current-gap]
    bar.innerHTML = '';

    // Period 1 (past)
    addSegment(bar, windowPct, 'coverage past');
    addSegment(bar, gapPct, 'gap past');

    // Period 2 (current)
    addSegment(bar, windowPct, 'coverage');
    addSegment(bar, gapPct, 'gap future');

    // Now cursor position
    let cursorPct = 50; // default: middle of current window
    if (state.inCoverage) {
        const windowStart = windowPct + gapPct; // start of current window in %
        cursorPct = windowStart + (state.elapsedSec / state.windowSec) * windowPct;
    } else {
        const gapStart = windowPct + gapPct + windowPct; // after current window
        const gap = state.periodSec - state.windowSec;
        const gapElapsed = gap - state.untilNext;
        cursorPct = gapStart + (gapElapsed / gap) * gapPct;
    }

    const cursor = document.createElement('div');
    cursor.className = 'cursor';
    cursor.style.left = cursorPct + '%';
    bar.appendChild(cursor);

    // Labels
    const halfPeriod = Math.round(state.periodSec);
    els.timelineLeft.textContent = `-${halfPeriod}s`;
    els.timelineRight.textContent = `+${halfPeriod}s`;
}

function addSegment(bar, widthPct, classes) {
    const seg = document.createElement('div');
    seg.className = 'segment ' + classes;
    seg.style.flex = widthPct;
    bar.appendChild(seg);
}

// --- Countdown tick ---

setInterval(() => {
    if (!state.hasData) return;
    if (state.untilNext > 0) {
        state.untilNext -= 1;
    }
    if (state.inCoverage) {
        state.elapsedSec += 1;
    }
    updateCoverageStatus();
    updateAnimation();
    updateProgressBar();
    updateTimeline();
}, 1000);

// --- Connection badge ---

function showBadge(text, cls) {
    els.connectionBadge.textContent = text;
    els.connectionBadge.className = 'connection-badge visible ' + cls;
}

function hideBadge() {
    els.connectionBadge.classList.remove('visible');
}

// --- Idle overlay ---

function hideIdle() {
    if (els.idleOverlay) {
        els.idleOverlay.classList.add('hidden');
    }
}

// --- Init ---

fetchProfile();
connect();
