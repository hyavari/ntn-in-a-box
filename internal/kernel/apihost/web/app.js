// NTN-in-a-Box GUI — app.js
// Core module: SSE connection, state management, metrics, view dispatch.

'use strict';

// --- State ---

const state = {
    inCoverage: false,
    elapsedSec: 0,
    untilNext: 0,
    // cyclePosSec is schedule-period position. Progress bars use this so
    // a blockage does not reset or hijack the bar (elapsed_sec is
    // blockage-relative while blocked).
    cyclePosSec: 0,
    inBlockage: false,
    windowSec: 90,
    periodSec: 600,
    lookaheadSec: 30,
    profileName: '',
    mode: 'periodic',
    // True once session_info reports mode=tle. Dynamic window/period
    // derivation from coverage events is TLE-only; profile runs (including
    // continuous+blockage) must keep the YAML schedule intact.
    isTle: false,
    metrics: { delay: 0, jitter: 0, loss: 0, bandwidth: 0 },
    history: { delay: [], jitter: [], loss: [], bandwidth: [] },
    connected: false,
    hasData: false,
    replayDone: false,
    messages: {}, // id -> { id, from, to, status, body, at }
    messageLog: [], // activity lines (mirrors CLI)
    // Primary device for metrics panel (multi-device: ignore other device_ids).
    focusDeviceId: 'sandbox-0',
    // Second device for peer UE messaging / peer strip (profile or TLE).
    peerDeviceId: '',
    hasPeerUe: false,
    peer: {
        inCoverage: false,
        untilNext: 0,
        kind: '',
    },
    // Per-device in-coverage for globe multi-pin beams.
    coverageByDevice: {},
    // From session_info.observers (TLE multi-pin).
    observers: [],
};

// Match globe-view.js pin palette.
const OBSERVER_PIN_COLORS = ['#ff4444', '#44aaff', '#ffaa44', '#aa44ff'];


const MAX_HISTORY = 20;

// --- Active view module ---
let activeView = null;
let viewLoading = false; // True while activateView is in-flight
let sessionInfoReceived = false; // True once session_info SSE event arrives
let pendingSessionInfo = null; // Queued TLE session_info if it arrives while loading

// --- DOM refs ---

const $ = (id) => document.getElementById(id);

const els = {
    animationPanel: $('animationPanel'),
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
    messagesList: $('messagesList'),
    messagesEmpty: $('messagesEmpty'),
    messagesHint: $('messagesHint'),
    messagesExplain: $('messagesExplain'),
    messagesLog: $('messagesLog'),
    messagesUeHint: $('messagesUeHint'),
    peerStrip: $('peerStrip'),
    peerStripLabel: $('peerStripLabel'),
    peerStatus: $('peerStatus'),
    peerCountdown: $('peerCountdown'),
    globeLegend: $('globeLegend'),
    globeCaption: $('globeCaption'),
    messageForm: $('messageForm'),
    messageTo: $('messageTo'),
    messageToUe: $('messageToUe'),
    messageBody: $('messageBody'),
    messageSend: $('messageSend'),
};

// --- View Management ---

async function activateView(sessionInfo) {
    // If TLE session_info arrives after sky fallback loaded, tear down
    // sky and switch to globe.
    if (activeView && sessionInfo && sessionInfo.mode === 'tle') {
        activeView.destroy();
        activeView = null;
    }

    // If currently loading and TLE arrives, queue it for after load completes.
    if (viewLoading) {
        if (sessionInfo && sessionInfo.mode === 'tle') {
            pendingSessionInfo = sessionInfo;
        }
        return;
    }

    if (activeView) return;
    viewLoading = true;

    try {
        if (sessionInfo && sessionInfo.mode === 'tle') {
            // TLE mode: try to load globe view.
            try {
                const globe = await import('./globe-view.js');
                const sky = els.animationPanel.querySelector('.sky');
                const globeContainer = $('globeContainer');
                if (sky) sky.style.display = 'none';
                if (globeContainer) globeContainer.style.display = 'block';
                globe.init(globeContainer, sessionInfo);
                activeView = globe;
                return;
            } catch (e) {
                console.warn('Globe view failed to load, falling back to sky view:', e);
            }
        }

        // Default: sky view (profile mode or fallback).
        const sky = await import('./sky-view.js');
        sky.init(els.animationPanel);
        activeView = sky;
    } finally {
        viewLoading = false;
        // Check if TLE session_info arrived while we were loading.
        if (pendingSessionInfo) {
            const pending = pendingSessionInfo;
            pendingSessionInfo = null;
            if (activeView) {
                activeView.destroy();
                activeView = null;
            }
            await activateView(pending);
        }
    }
}

// --- SSE Connection ---

function connect() {
    showBadge('connecting...', '');

    const es = new EventSource('/events');

    es.addEventListener('session_info', (e) => {
        const info = JSON.parse(e.data);
        sessionInfoReceived = true;
        state.isTle = info.mode === 'tle';
        // Update UI header with session info.
        if (info.mode === 'tle' && info.satellite_name) {
            els.profileName.textContent = `TLE: ${info.satellite_name}`;
            els.profileDetail.textContent = info.satellite_name;
            els.profileMode.textContent = 'tle (orbital)';
            const lat = info.observer_lat_deg?.toFixed(2) || '?';
            const lon = info.observer_lon_deg?.toFixed(2) || '?';
            if (info.observers && info.observers.length > 1) {
                state.observers = info.observers;
                applyObserverRoles();
                els.profileSchedule.textContent = info.observers
                    .map((o) => `${o.id} @ ${Number(o.lat_deg).toFixed(1)}°, ${Number(o.lon_deg).toFixed(1)}°`)
                    .join(' · ');
            } else {
                state.observers = info.observers || [];
                applyObserverRoles();
                els.profileSchedule.textContent = `observer: ${lat}\u00B0, ${lon}\u00B0`;
            }
            renderGlobeLegend();
            updatePeerStripLabel();
            updateMessageExplain();
        }
        activateView(info);
    });

    es.addEventListener('coverage', (e) => {
        const data = JSON.parse(e.data);
        const deviceId = data.device_id || state.focusDeviceId;
        if (typeof data.in_coverage === 'boolean') {
            state.coverageByDevice[deviceId] = data.in_coverage;
        }

        // Peer UE window progress (multi-device).
        const peerId = peerDeviceId();
        if (peerId && data.device_id === peerId) {
            applyPeerCoverage(data);
            if (activeView) activeView.update(state);
            renderGlobeLegend();
            return;
        }

        // Ignore other devices when device_id is present (multi-device serve).
        if (data.device_id && data.device_id !== state.focusDeviceId && data.kind !== 'initial') {
            if (activeView) activeView.update(state);
            renderGlobeLegend();
            return;
        }

        if (state.replayDone) {
            state.replayDone = false;
        }

        state.inCoverage = data.in_coverage;
        // Always apply numeric fields when present (including 0). The old
        // " > 0 " guard skipped cycle wraps and left untilNext stuck at 0.
        if (typeof data.elapsed_sec === 'number') {
            state.elapsedSec = data.elapsed_sec;
        }
        if (typeof data.until_next_transition === 'number') {
            state.untilNext = data.until_next_transition;
        }
        if (typeof data.cycle_pos_sec === 'number') {
            state.cyclePosSec = data.cycle_pos_sec;
        }
        state.inBlockage = !state.inCoverage && !!data.in_blockage;
        if (data.kind === 'window_opened') {
            state.elapsedSec = data.elapsed_sec || 0;
            if (typeof data.cycle_pos_sec === 'number') {
                state.cyclePosSec = data.cycle_pos_sec;
            }
            if (typeof data.until_next_transition === 'number') {
                state.untilNext = data.until_next_transition;
            }
            // TLE only: derive windowSec from the coverage event
            // (until_next_transition at window open = window duration).
            // Must not run for profile mode — blockages also emit
            // window_opened/closed and would corrupt the YAML schedule.
            if (state.isTle && data.until_next_transition > 0) {
                state.windowSec = data.until_next_transition;
                updateScheduleLabels();
            }
        }
        // Keep countdown aligned with cycle position while in coverage
        // (SSE only fires on transitions; local ticks otherwise drift).
        if (state.inCoverage) {
            syncUntilNextFromSchedule();
        }
        if (data.kind === 'initial' && data.in_coverage && state.isTle) {
            // TLE: derive windowSec from initial state.
            if (data.until_next_transition > 0) {
                state.windowSec = (data.elapsed_sec || 0) + data.until_next_transition;
                updateScheduleLabels();
            }
        }
        if (data.kind === 'window_closed') {
            // TLE only: derive gap duration from coverage event.
            if (state.isTle && data.until_next_transition > 0) {
                state.periodSec = state.windowSec + data.until_next_transition;
                updateScheduleLabels();
            }
        }
        state.hasData = true;
        hideIdle();
        updateAll();
        renderGlobeLegend();
    });

    es.addEventListener('lifecycle', (e) => {
        const data = JSON.parse(e.data);
        if (data.kind === 'replay_done') {
            state.replayDone = true;
            els.coverageIndicator.textContent = '✓ REPLAY COMPLETE';
            els.coverageIndicator.classList.remove('out');
            els.countdown.textContent = 'done';
            els.progressFill.style.width = '100%';
            els.progressFill.classList.remove('out');
            els.progressInfo.textContent = '100% · complete';
        }
    });

    es.addEventListener('linkstate', (e) => {
        const data = JSON.parse(e.data);
        if (data.device_id && data.device_id !== state.focusDeviceId) {
            return;
        }
        state.metrics.delay = data.delay_ms;
        state.metrics.jitter = data.jitter_ms;
        state.metrics.loss = data.loss_pct;
        state.metrics.bandwidth = data.bandwidth_kbps;
        state.hasData = true;
        if (!state.inCoverage) {
            state.inCoverage = true;
            state.inBlockage = false;
            updateCoverageStatus();
            if (activeView) activeView.update(state);
        }
        hideIdle();
        pushHistory();
        updateMetrics();
        updateSparklines();
    });

    es.addEventListener('satellite_position', (e) => {
        const data = JSON.parse(e.data);
        if (activeView && activeView.updatePosition) {
            activeView.updatePosition(data);
        }
    });

    es.addEventListener('message', (e) => {
        const data = JSON.parse(e.data);
        upsertMessage(data);
        appendMessageLog(data);
        // SSE omits body (CORS); fill from same-origin status GET when missing.
        const cur = state.messages[data.id];
        if (data.id && cur && !cur.body) {
            fetch(`/messages/${encodeURIComponent(data.id)}`)
                .then((r) => (r.ok ? r.json() : null))
                .then((full) => {
                    if (!full) return;
                    upsertMessage({
                        id: full.id,
                        from: full.from,
                        to: full.to,
                        status: full.status,
                        body: full.body,
                        at: full.delivered_at || full.accepted_at || data.at,
                    });
                })
                .catch(() => {});
        }
        hideIdle();
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

function upsertMessage(data) {
    if (!data || !data.id) return;
    const prev = state.messages[data.id] || {};
    state.messages[data.id] = {
        id: data.id,
        from: data.from || prev.from || '?',
        to: data.to || prev.to || '?',
        status: data.status || prev.status || 'queued',
        body: data.body != null && data.body !== '' ? data.body : (prev.body || ''),
        at: data.at || prev.at || '',
    };
    renderMessages();
}

function appendMessageLog(data) {
    if (!data || !data.id || !data.status) return;
    const line = `${data.id}  ${data.from || '?'} → ${data.to || '?'}  ${data.status}`;
    const prev = state.messageLog[state.messageLog.length - 1];
    if (prev === line) return;
    state.messageLog.push(line);
    if (state.messageLog.length > 40) {
        state.messageLog = state.messageLog.slice(-40);
    }
    renderMessageLog();
}

function renderMessageLog() {
    if (!els.messagesLog) return;
    els.messagesLog.textContent = state.messageLog.join('\n');
    els.messagesLog.scrollTop = els.messagesLog.scrollHeight;
}

function renderMessages() {
    const rows = Object.values(state.messages).sort((a, b) => {
        if (a.at === b.at) return b.id.localeCompare(a.id);
        return (b.at || '').localeCompare(a.at || '');
    });
    if (!els.messagesList || !els.messagesEmpty) return;
    if (rows.length === 0) {
        els.messagesEmpty.style.display = '';
        els.messagesList.innerHTML = '';
        return;
    }
    els.messagesEmpty.style.display = 'none';
    els.messagesList.innerHTML = rows.map((m) => {
        const st = escapeHtml(m.status || 'queued');
        const route = `${escapeHtml(m.from)} → ${escapeHtml(m.to)}`;
        const body = escapeHtml(m.body || '(no body)');
        return `<div class="message-row">
            <span class="message-route">${route}</span>
            <span class="message-body" title="${body}">${body}</span>
            <span class="message-status ${st}">${st.replace('_', ' ')}</span>
        </div>`;
    }).join('');
}

function escapeHtml(s) {
    return String(s)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;');
}

function peerDeviceId() {
    if (state.peerDeviceId) return state.peerDeviceId;
    if (state.observers && state.observers.length > 1) return state.observers[1].id;
    return '';
}

function applyObserverRoles() {
    if (state.observers && state.observers.length > 0) {
        state.focusDeviceId = state.observers[0].id;
    }
    if (state.observers && state.observers.length > 1) {
        state.peerDeviceId = state.observers[1].id;
    }
}

function selectedMessageTo() {
    const v = (els.messageTo && els.messageTo.value) || 'cloud';
    const peer = peerDeviceId();
    return peer && v === peer ? peer : 'cloud';
}

function updateMessageExplain() {
    const focus = state.focusDeviceId || 'sandbox-0';
    const peer = peerDeviceId() || 'peer';
    if (els.messagesExplain) {
        els.messagesExplain.innerHTML =
            `You are <strong>${escapeHtml(focus)}</strong> (metrics above). Send to ` +
            `<strong>Network</strong> (cloud) for immediate delivery, or ` +
            `<strong>UE ${escapeHtml(peer)}</strong> to queue until that peer has coverage.`;
    }
    if (els.messageToUe) {
        els.messageToUe.value = peerDeviceId() || 'sandbox-1';
        els.messageToUe.textContent = peerDeviceId()
            ? `UE (${peerDeviceId()})`
            : 'UE (peer)';
    }
}

function updateMessageDestinationUI() {
    if (els.messageToUe) {
        els.messageToUe.disabled = !state.hasPeerUe;
        const peer = peerDeviceId();
        if (peer) {
            els.messageToUe.value = peer;
            els.messageToUe.textContent = `UE (${peer})`;
        }
    }
    if (els.messagesUeHint) {
        els.messagesUeHint.hidden = state.hasPeerUe;
    }
    const peer = peerDeviceId();
    if (els.messageTo && !state.hasPeerUe && peer && els.messageTo.value === peer) {
        els.messageTo.value = 'cloud';
    }
    const to = selectedMessageTo();
    if (els.messagesHint) {
        const destLabel = to === 'cloud' ? 'network (cloud)' : `UE (${to})`;
        els.messagesHint.textContent = `${state.focusDeviceId} → ${destLabel}`;
    }
    updatePeerStrip();
}

function applyPeerCoverage(data) {
    state.peer.inCoverage = !!data.in_coverage;
    if (typeof data.until_next_transition === 'number') {
        state.peer.untilNext = data.until_next_transition;
    }
    if (data.kind) {
        state.peer.kind = data.kind;
    }
    updatePeerStrip();
    renderGlobeLegend();
}

function updatePeerStripLabel() {
    if (!els.peerStripLabel) return;
    const peerId = peerDeviceId();
    const peer = (state.observers || []).find((o) => o.id === peerId);
    if (peer) {
        els.peerStripLabel.textContent =
            `Peer UE ${peer.id} @ ${Number(peer.lat_deg).toFixed(1)}°, ${Number(peer.lon_deg).toFixed(1)}°`;
    } else if (peerId) {
        els.peerStripLabel.textContent = `Peer UE (${peerId})`;
    } else {
        els.peerStripLabel.textContent = 'Peer UE';
    }
}

function renderGlobeLegend() {
    if (!els.globeLegend) return;
    const list = state.observers && state.observers.length
        ? state.observers
        : [];
    if (list.length === 0) {
        els.globeLegend.hidden = true;
        if (els.globeCaption) els.globeCaption.hidden = true;
        return;
    }
    els.globeLegend.hidden = false;
    if (els.globeCaption) els.globeCaption.hidden = false;
    els.globeLegend.innerHTML = list.map((o, i) => {
        const color = OBSERVER_PIN_COLORS[i % OBSERVER_PIN_COLORS.length];
        const inCov = state.coverageByDevice[o.id];
        const covClass = inCov ? '' : 'out';
        const covText = inCov === undefined ? '—' : (inCov ? 'IN' : 'OUT');
        const role = o.id === state.focusDeviceId ? 'you' : 'peer';
        return `<div class="globe-legend-row">` +
            `<span class="globe-legend-dot" style="background:${color}"></span>` +
            `<span><strong>${escapeHtml(o.id)}</strong> <span class="globe-legend-meta">(${role})</span><br>` +
            `<span class="globe-legend-meta">${Number(o.lat_deg).toFixed(2)}°, ${Number(o.lon_deg).toFixed(2)}°</span></span>` +
            `<span class="globe-legend-cov ${covClass}">${covText}</span>` +
            `</div>`;
    }).join('');
}

function updatePeerStrip() {
    if (!els.peerStrip) return;
    els.peerStrip.hidden = !state.hasPeerUe;
    if (!state.hasPeerUe) return;

    if (els.peerStatus) {
        if (state.peer.inCoverage) {
            els.peerStatus.textContent = '▲ IN COVERAGE';
            els.peerStatus.classList.remove('out');
        } else {
            els.peerStatus.textContent = '▼ OUT OF COVERAGE';
            els.peerStatus.classList.add('out');
        }
    }
    if (els.peerCountdown) {
        const sec = Math.max(0, Math.round(state.peer.untilNext));
        if (state.peer.inCoverage) {
            els.peerCountdown.textContent = `window closes in ${formatPeerDuration(sec)}`;
        } else {
            els.peerCountdown.textContent = `next open in ${formatPeerDuration(sec)}`;
        }
    }
}

function formatPeerDuration(sec) {
    if (sec >= 3600) {
        const h = Math.floor(sec / 3600);
        const m = Math.floor((sec % 3600) / 60);
        return `${h}h ${m}m`;
    }
    if (sec >= 60) {
        const m = Math.floor(sec / 60);
        const s = sec % 60;
        return `${m}m ${s}s`;
    }
    return `${sec}s`;
}

async function pollPeerCondition() {
    const peer = peerDeviceId();
    if (!state.hasPeerUe || !peer) return;
    try {
        const resp = await fetch(`/devices/${encodeURIComponent(peer)}/condition`);
        if (!resp.ok) return;
        const c = await resp.json();
        state.peer.inCoverage = !!c.in_coverage;
        if (typeof c.until_next_transition_sec === 'number') {
            state.peer.untilNext = c.until_next_transition_sec;
        }
        updatePeerStrip();
    } catch (_) { /* ignore */ }
}

async function refreshPeerDevices() {
    try {
        const resp = await fetch('/devices');
        if (!resp.ok) return;
        const list = await resp.json();
        if (!Array.isArray(list)) {
            state.hasPeerUe = false;
        } else {
            const focus = state.focusDeviceId;
            const peer = list.find((d) => d && d.id && d.id !== focus && d.id !== 'cloud');
            state.hasPeerUe = !!peer;
            if (peer) {
                state.peerDeviceId = peer.id;
            } else if (!(state.observers && state.observers.length > 1)) {
                state.peerDeviceId = '';
            }
        }
    } catch (_) {
        state.hasPeerUe = false;
    }
    updateMessageDestinationUI();
    updatePeerStripLabel();
    updateMessageExplain();
    if (state.hasPeerUe) {
        pollPeerCondition();
    }
}

async function sendMessageFromForm(ev) {
    ev.preventDefault();
    const to = selectedMessageTo();
    const peer = peerDeviceId();
    if (peer && to === peer && !state.hasPeerUe) {
        showBadge('UE peer not registered (need --devices 2 or a second --observer)', 'error');
        return;
    }
    const body = (els.messageBody.value || '').trim();
    if (!body) return;
    els.messageSend.disabled = true;
    try {
        const resp = await fetch(`/devices/${encodeURIComponent(state.focusDeviceId)}/messages`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ to, body }),
        });
        const data = await resp.json().catch(() => ({}));
        if (!resp.ok) {
            showBadge(data.error || `send failed (${resp.status})`, 'error');
            return;
        }
        // List row only — Activity log comes from SSE (queued → in_flight → delivered).
        // Do not append HTTP "accepted" here; it races after delivery and looks wrong.
        upsertMessage({
            id: data.id,
            from: state.focusDeviceId,
            to,
            status: 'queued',
            body,
            at: new Date().toISOString(),
        });
        if (data.id) {
            try {
                const st = await fetch(`/messages/${encodeURIComponent(data.id)}`);
                if (st.ok) {
                    const full = await st.json();
                    upsertMessage({
                        id: full.id,
                        from: full.from,
                        to: full.to,
                        status: full.status,
                        body: full.body,
                        at: full.delivered_at || full.accepted_at || new Date().toISOString(),
                    });
                }
            } catch (_) { /* ignore */ }
        }
        els.messageBody.value = '';
    } catch (err) {
        showBadge('send failed', 'error');
    } finally {
        els.messageSend.disabled = false;
    }
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
            const sched = p.schedule || p.Schedule || {};
            const mode = sched.mode || sched.Mode || 'periodic';
            const periodSec = sched.period_sec || sched.PeriodSec || 600;
            // Prefer nullish coalescing so continuous profiles (window_sec: 0)
            // are not rewritten to the periodic default of 90.
            const rawWindow = sched.window_sec ?? sched.WindowSec;
            const windowSec = mode === 'continuous'
                ? periodSec
                : (rawWindow || 90);
            const lookaheadSec = sched.lookahead_sec ?? sched.LookaheadSec ?? 30;
            const profileName = p.name || p.Name || 'unknown';

            state.profileName = profileName;
            state.periodSec = periodSec;
            state.windowSec = windowSec;
            state.lookaheadSec = lookaheadSec;
            state.mode = mode;

            els.profileName.textContent = profileName;
            els.timelinePeriod.textContent = `period: ${state.periodSec}s`;

            els.profileDetail.textContent = profileName;
            els.profileMode.textContent = mode;
            if (mode === 'periodic') {
                els.profileSchedule.textContent = `${periodSec}s period / ${windowSec}s window / ${lookaheadSec}s lookahead`;
            } else {
                els.profileSchedule.textContent = `${periodSec}s period (continuous)`;
            }
            buildTimeline();
        } else {
            // No profiles available (replay or TLE mode).
            // Only set fallback labels if session_info hasn't already set TLE labels.
            if (!sessionInfoReceived) {
                els.profileName.textContent = 'replay';
                els.profileDetail.textContent = 'replay';
                els.profileMode.textContent = state.mode;
                els.profileSchedule.textContent = `${state.periodSec}s period / ${state.windowSec}s window`;
            }
            buildTimeline();
        }
    } catch (e) {
        console.warn('Failed to fetch profile:', e);
    }
}

// --- Update functions ---

function updateAll() {
    updateCoverageStatus();
    if (activeView) activeView.update(state);
    updateProgressBar();
    updateTimeline();
}

// updateScheduleLabels refreshes the timeline period label when
// windowSec/periodSec change dynamically (TLE mode derives these
// from coverage events).
function updateScheduleLabels() {
    if (state.periodSec > 0) {
        els.timelinePeriod.textContent = `period: ${Math.round(state.periodSec)}s`;
    }
}

function updateCoverageStatus() {
    const who = state.focusDeviceId || 'device';
    if (state.inCoverage) {
        els.coverageIndicator.textContent = `▲ ${who} IN COVERAGE`;
        els.coverageIndicator.classList.remove('out');
    } else if (state.inBlockage) {
        els.coverageIndicator.textContent = `▼ ${who} BLOCKED`;
        els.coverageIndicator.classList.add('out');
    } else {
        els.coverageIndicator.textContent = `▼ ${who} OUT OF COVERAGE`;
        els.coverageIndicator.classList.add('out');
    }
    els.countdown.textContent = `${Math.round(state.untilNext)}s remaining`;
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
    // Progress through the scheduled window/cycle using cyclePosSec —
    // same approach as the TUI. Blockages change inCoverage / untilNext
    // but must not reset or hijack this bar.
    const period = state.periodSec;
    const pos = state.cyclePosSec;
    let pct = 0;
    const continuous = isContinuousSchedule();

    if (period > 0) {
        if (continuous) {
            pct = Math.min(Math.max(pos / period * 100, 0), 100);
        } else if (state.windowSec > 0) {
            if (pos < state.windowSec) {
                pct = Math.min(Math.max(pos / state.windowSec * 100, 0), 100);
            } else {
                const gap = period - state.windowSec;
                const gapElapsed = pos - state.windowSec;
                pct = gap > 0 ? Math.min(Math.max(gapElapsed / gap * 100, 0), 100) : 0;
            }
        }
    }

    els.progressFill.style.width = pct + '%';
    if (state.inCoverage) {
        els.progressFill.classList.remove('out');
        els.progressInfo.textContent =
            `${Math.round(pct)}% · ${Math.round(state.untilNext)}s left`;
    } else if (state.inBlockage) {
        els.progressFill.classList.add('out');
        els.progressInfo.textContent =
            `${Math.round(pct)}% · blocked · ${Math.round(state.untilNext)}s left`;
    } else {
        els.progressFill.classList.add('out');
        els.progressInfo.textContent =
            `${Math.round(pct)}% · next in ${Math.round(state.untilNext)}s`;
    }
}

// isContinuousSchedule matches TUI continuous handling even if profile
// fetch left mode as the default "periodic" (window covers the period).
function isContinuousSchedule() {
    return state.mode === 'continuous' ||
        (state.periodSec > 0 && state.windowSec >= state.periodSec);
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
        const y = 28 - ((v - min) / range) * 26;
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
    const total = state.periodSec * 2;
    const windowPct = (state.windowSec / total) * 100;
    const gapPct = ((state.periodSec - state.windowSec) / total) * 100;

    bar.innerHTML = '';

    // Period 1 (past)
    addSegment(bar, windowPct, 'coverage past');
    addSegment(bar, gapPct, 'gap past');

    // Period 2 (current)
    addSegment(bar, windowPct, 'coverage');
    addSegment(bar, gapPct, 'gap future');

    // Now cursor position — always from cyclePosSec so blockages don't
    // jump the cursor (elapsed_sec is blockage-relative while blocked).
    let cursorPct = 50;
    if (state.windowSec > 0 && state.periodSec > 0) {
        const pos = state.cyclePosSec;
        if (pos < state.windowSec) {
            const windowStart = windowPct + gapPct;
            cursorPct = windowStart + (pos / state.windowSec) * windowPct;
        } else {
            const gap = state.periodSec - state.windowSec;
            if (gap > 0) {
                const gapStart = windowPct + gapPct + windowPct;
                const gapElapsed = pos - state.windowSec;
                cursorPct = gapStart + (gapElapsed / gap) * gapPct;
            }
        }
    }

    const cursor = document.createElement('div');
    cursor.className = 'cursor';
    cursor.style.left = cursorPct + '%';
    bar.appendChild(cursor);

    els.timelineLeft.textContent = `-${Math.round(state.periodSec)}s`;
    els.timelineRight.textContent = `+${Math.round(state.periodSec)}s`;
}

function addSegment(bar, widthPct, classes) {
    const seg = document.createElement('div');
    seg.className = 'segment ' + classes;
    seg.style.flex = widthPct;
    bar.appendChild(seg);
}

// --- Countdown tick ---

setInterval(() => {
    if (!state.hasData || state.replayDone) return;
    if (state.periodSec > 0) {
        state.cyclePosSec += 1;
        if (state.cyclePosSec >= state.periodSec) {
            state.cyclePosSec -= state.periodSec;
        }
    }
    syncUntilNextFromSchedule();
    if (state.inCoverage) {
        state.elapsedSec += 1;
    }
    updateCoverageStatus();
    if (activeView) activeView.update(state);
    updateProgressBar();
    updateTimeline();
}, 1000);

// syncUntilNextFromSchedule mirrors the TUI: keep the countdown aligned
// with cycle position for scheduled phases. Mid-window / continuous
// blockages count down toward clearance instead.
function syncUntilNextFromSchedule() {
    const period = state.periodSec;
    if (!(period > 0)) return;
    const pos = state.cyclePosSec;
    const continuous = isContinuousSchedule();

    if (continuous) {
        if (state.inCoverage) {
            state.untilNext = Math.max(period - pos, 0);
            return;
        }
        if (state.untilNext > 0) state.untilNext -= 1;
        return;
    }

    const window = state.windowSec;
    if (state.inCoverage) {
        state.untilNext = Math.max(window - pos, 0);
        return;
    }
    if (pos >= window) {
        state.untilNext = Math.max(period - pos, 0);
        return;
    }
    if (state.untilNext > 0) state.untilNext -= 1;
}

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

// If no session_info arrives within 2s (old backend), default to sky view.
setTimeout(async () => {
    if (!activeView && !viewLoading && !sessionInfoReceived) {
        await activateView(null);
    }
}, 2000);

fetchProfile();
connect();
refreshPeerDevices();
setInterval(refreshPeerDevices, 2000);
setInterval(pollPeerCondition, 1000);
if (els.messageForm) {
    els.messageForm.addEventListener('submit', sendMessageFromForm);
}
if (els.messageTo) {
    els.messageTo.addEventListener('change', updateMessageDestinationUI);
}
updateMessageDestinationUI();
renderMessageLog();
renderMessages();
