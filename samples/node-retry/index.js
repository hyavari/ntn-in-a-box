/**
 * NTN-in-a-Box Sample: Resilient HTTP Client (Node.js)
 *
 * Demonstrates patterns for satellite-aware applications:
 * - Exponential backoff on failures
 * - Offline message queue (buffer during coverage gaps)
 * - Automatic flush when connectivity returns
 * - Adaptive timeout based on observed latency
 *
 * Run under ntnbox to see real NTN behavior:
 *   ntnbox run --profile testdata/profiles/leo_pass_90s.yaml -- node samples/node-retry/index.js
 */

'use strict';

const https = require('https');
const http = require('http');

// --- Configuration ---
const TARGET_URL = process.env.TARGET_URL || 'https://example.com';
const SEND_INTERVAL_MS = 3000;
const INITIAL_TIMEOUT_MS = 5000;
const MAX_TIMEOUT_MS = 15000;
const MAX_RETRIES = 5;
const MAX_QUEUE_SIZE = 100;

// --- State ---
const messageQueue = [];
let messageId = 0;
let currentTimeout = INITIAL_TIMEOUT_MS;
let consecutiveFailures = 0;
let totalSent = 0;
let totalFailed = 0;
let totalQueued = 0;

// --- Helpers ---

function log(color, symbol, msg) {
    const ts = new Date().toISOString().substring(11, 19);
    const colors = { green: '\x1b[32m', red: '\x1b[31m', yellow: '\x1b[33m', cyan: '\x1b[36m', dim: '\x1b[2m', reset: '\x1b[0m' };
    console.log(`  ${colors.dim}${ts}${colors.reset}  ${colors[color]}${symbol}${colors.reset}  ${msg}`);
}

function httpGet(url, timeoutMs) {
    return new Promise((resolve, reject) => {
        const start = Date.now();
        const mod = url.startsWith('https') ? https : http;
        const req = mod.get(url, { timeout: timeoutMs }, (res) => {
            let body = '';
            res.on('data', chunk => body += chunk);
            res.on('end', () => {
                resolve({ status: res.statusCode, latency: Date.now() - start, body });
            });
        });
        req.on('timeout', () => { req.destroy(); reject(new Error('timeout')); });
        req.on('error', reject);
    });
}

// --- Core Logic ---

async function sendMessage(msg) {
    for (let attempt = 0; attempt <= MAX_RETRIES; attempt++) {
        try {
            const result = await httpGet(TARGET_URL, currentTimeout);

            // Success — adapt timeout down toward observed latency.
            currentTimeout = Math.max(INITIAL_TIMEOUT_MS, result.latency * 3);
            consecutiveFailures = 0;
            totalSent++;
            log('green', '✓', `sent msg#${msg.id} (${result.latency}ms, timeout=${currentTimeout}ms)`);
            return true;

        } catch (err) {
            consecutiveFailures++;
            // Adapt timeout up on failure.
            currentTimeout = Math.min(currentTimeout * 1.5, MAX_TIMEOUT_MS);

            if (attempt < MAX_RETRIES) {
                const backoffMs = Math.min(1000 * Math.pow(2, attempt), 30000);
                log('yellow', '↻', `retry msg#${msg.id} attempt ${attempt + 1}/${MAX_RETRIES} (backoff ${backoffMs}ms)`);
                await sleep(backoffMs);
            }
        }
    }

    // All retries exhausted — queue for later.
    totalFailed++;
    return false;
}

async function flushQueue() {
    if (messageQueue.length === 0) return;

    log('cyan', '⟳', `flushing ${messageQueue.length} queued messages...`);
    const toSend = [...messageQueue];
    messageQueue.length = 0;

    for (const msg of toSend) {
        const ok = await sendMessage(msg);
        if (!ok) {
            // Re-queue if still failing.
            messageQueue.push(msg);
            log('red', '✗', `flush failed, ${messageQueue.length} still queued`);
            return;
        }
    }
    log('green', '✓', `flush complete — all queued messages delivered`);
}

function enqueue(msg) {
    if (messageQueue.length >= MAX_QUEUE_SIZE) {
        messageQueue.shift(); // drop oldest
        log('yellow', '⚠', `queue full, dropped oldest message`);
    }
    messageQueue.push(msg);
    totalQueued++;
    log('red', '◌', `queued msg#${msg.id} (queue: ${messageQueue.length}, failures: ${consecutiveFailures})`);
}

function sleep(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

// --- Main Loop ---

async function main() {
    console.log(`\n  ntn-retry-demo: sending to ${TARGET_URL} every ${SEND_INTERVAL_MS}ms`);
    console.log(`  Demonstrates: exponential backoff, offline queue, adaptive timeout\n`);

    while (true) {
        const msg = { id: ++messageId, ts: Date.now(), payload: `message-${messageId}` };

        const ok = await sendMessage(msg);
        if (!ok) {
            enqueue(msg);
        } else if (messageQueue.length > 0) {
            // Connectivity restored — flush the queue.
            await flushQueue();
        }

        // Print stats periodically.
        if (messageId % 10 === 0) {
            log('dim', '│', `stats: sent=${totalSent} failed=${totalFailed} queued=${totalQueued} pending=${messageQueue.length}`);
        }

        await sleep(SEND_INTERVAL_MS);
    }
}

main().catch(err => {
    console.error('fatal:', err);
    process.exit(1);
});
