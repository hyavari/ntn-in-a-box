// sky-view.js — Flat sky animation (profile mode).
// Extracted from app.js. Implements the view module interface:
// init(container, sessionInfo), update(state), destroy()

'use strict';

let els = {};

export function init(container) {
    // DOM refs within the animation panel's .sky element.
    els = {
        sky: container.querySelector('.sky'),
        satellite: container.querySelector('#satellite'),
        beam: container.querySelector('#beam'),
        device: container.querySelector('.device-body'),
        linkStatus: container.querySelector('#linkStatus'),
        animProgress: container.querySelector('#animProgress'),
    };
    // Ensure sky view is visible.
    if (els.sky) els.sky.style.display = '';
}

export function update(state) {
    if (!els.sky) return;

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
        els.beam.style.width = (85 - pos.y) * 0.8 + 'px';
        els.beam.classList.remove('hidden');

        // Device linked
        els.device.classList.add('linked');
        els.device.classList.remove('unlinked');

        // Sky bright
        els.sky.classList.remove('dark');

        // Link status
        els.linkStatus.textContent = '● LINKED';
        els.linkStatus.classList.remove('disconnected');

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

        // Link status
        els.linkStatus.textContent = '● NO LINK';
        els.linkStatus.classList.add('disconnected');

        // Gap progress
        const gap = state.periodSec - state.windowSec;
        const gapElapsed = gap - state.untilNext;
        const gapProgress = gap > 0 ? Math.min(gapElapsed / gap, 1) : 0;
        els.animProgress.style.width = (gapProgress * 100) + '%';
        els.animProgress.classList.add('out');
    }
}

export function destroy() {
    els = {};
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
