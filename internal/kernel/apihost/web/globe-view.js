// globe-view.js — 3D Earth globe visualization (TLE mode).
// Uses Three.js from CDN. Implements the view module interface:
// init(container, sessionInfo), update(state), updatePosition(posData), destroy()

import * as THREE from 'three';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';

'use strict';

// --- Constants ---

const EARTH_RADIUS = 1.0;
// SAT_RADIUS is exaggerated for visibility. Real ISS altitude (420km)
// would be 1.066 × Earth radius — too close to the surface to see.
const SAT_RADIUS = 1.15;
const OBSERVER_PIN_HEIGHT = 0.04;
const LERP_FACTOR = 0.08;
const AUTO_TRACK_RESUME_MS = 5000;

// --- Module state ---

let scene, camera, renderer, controls;
let earthMesh, atmosphereMesh;
let observerMarker, satelliteMesh, beamLine, orbitLine;
let container;
let animFrameId;
let targetSatPos = new THREE.Vector3();
let currentSatPos = new THREE.Vector3();
let observerPos = new THREE.Vector3();
let inCoverage = false;
let userInteracting = false;
let lastInteractionTime = 0;
let camDistance = 3.0;

// Multi-observer pins/beams: [{ id, pos, marker, beam, inCoverage }]
let observers = [];


// --- Public interface ---

export function init(containerEl, sessionInfo) {
    container = containerEl;

    setupScene();
    setupLights();
    setupEarth();
    setupAtmosphere();

    const list = sessionInfo.observers && sessionInfo.observers.length
        ? sessionInfo.observers
        : [{ id: 'sandbox-0', lat_deg: sessionInfo.observer_lat_deg, lon_deg: sessionInfo.observer_lon_deg }];
    setupObservers(list);

    setupOrbitTrack(sessionInfo.orbit_points);
    setupSatellite();
    setupCamera();
    setupControls();

    window.addEventListener('resize', onResize);
    animate();
}

export function update(state) {
    if (state.coverageByDevice) {
        for (const o of observers) {
            if (Object.prototype.hasOwnProperty.call(state.coverageByDevice, o.id)) {
                o.inCoverage = !!state.coverageByDevice[o.id];
            }
        }
    } else {
        inCoverage = state.inCoverage;
        if (observers.length === 1) {
            observers[0].inCoverage = state.inCoverage;
        }
    }
    updateBeamVisibility();
}

export function updatePosition(posData) {
    targetSatPos = latLonToVec3(posData.lat_deg, posData.lon_deg, SAT_RADIUS);
}

export function destroy() {
    window.removeEventListener('resize', onResize);
    if (animFrameId) cancelAnimationFrame(animFrameId);
    animFrameId = null;

    // Remove pointer/wheel listeners.
    if (renderer && renderer.domElement) {
        renderer.domElement.removeEventListener('pointerdown', onPointerDown);
        renderer.domElement.removeEventListener('pointerup', onPointerUp);
        renderer.domElement.removeEventListener('wheel', onWheel);
    }

    // Dispose all scene objects (geometries, materials, textures).
    if (scene) {
        scene.traverse((obj) => {
            if (obj.geometry) obj.geometry.dispose();
            if (obj.material) {
                if (obj.material.map) obj.material.map.dispose();
                obj.material.dispose();
            }
        });
    }

    if (controls) controls.dispose();
    if (renderer) {
        renderer.dispose();
        renderer.forceContextLoss();
        if (renderer.domElement && renderer.domElement.parentNode) {
            renderer.domElement.parentNode.removeChild(renderer.domElement);
        }
    }
    scene = camera = renderer = controls = null;
    earthMesh = atmosphereMesh = observerMarker = satelliteMesh = beamLine = orbitLine = null;
    observers = [];
}

// --- Setup functions ---

function setupScene() {
    scene = new THREE.Scene();
    scene.background = new THREE.Color(0x0a0e1a);

    renderer = new THREE.WebGLRenderer({ antialias: true, alpha: false });
    renderer.setPixelRatio(window.devicePixelRatio);
    renderer.setSize(container.clientWidth, container.clientHeight);
    container.appendChild(renderer.domElement);
}

function setupLights() {
    const ambient = new THREE.AmbientLight(0xffffff, 0.35);
    scene.add(ambient);

    const directional = new THREE.DirectionalLight(0xffffff, 0.8);
    directional.position.set(5, 3, 5);
    scene.add(directional);
}

function setupEarth() {
    const geometry = new THREE.SphereGeometry(EARTH_RADIUS, 64, 64);

    // Load Earth texture from CDN (Blue Marble, NASA public domain).
    const loader = new THREE.TextureLoader();
    const material = new THREE.MeshPhongMaterial({
        color: 0xffffff,
        shininess: 10,
    });
    earthMesh = new THREE.Mesh(geometry, material);
    scene.add(earthMesh);

    // Load texture asynchronously — globe shows dark until loaded.
    loader.load(
        'https://cdn.jsdelivr.net/gh/mrdoob/three.js@r165/examples/textures/planets/earth_atmos_2048.jpg',
        (texture) => {
            material.map = texture;
            material.needsUpdate = true;
        },
        undefined,
        () => {
            // Texture failed to load — fall back to dark sphere with grid.
            material.color.set(0x0d1b2a);
            material.emissive.set(0x0a1628);
            addGridLines();
        }
    );
}

function addGridLines() {
    const lineMaterial = new THREE.LineBasicMaterial({
        color: 0x1b3a5c,
        transparent: true,
        opacity: 0.4,
    });

    // Latitude lines
    for (let lat = -60; lat <= 60; lat += 30) {
        const points = [];
        for (let lon = 0; lon <= 360; lon += 5) {
            points.push(latLonToVec3(lat, lon, EARTH_RADIUS * 1.001));
        }
        const geometry = new THREE.BufferGeometry().setFromPoints(points);
        const line = new THREE.Line(geometry, lineMaterial);
        scene.add(line);
    }

    // Longitude lines
    for (let lon = 0; lon < 360; lon += 30) {
        const points = [];
        for (let lat = -90; lat <= 90; lat += 5) {
            points.push(latLonToVec3(lat, lon, EARTH_RADIUS * 1.001));
        }
        const geometry = new THREE.BufferGeometry().setFromPoints(points);
        const line = new THREE.Line(geometry, lineMaterial);
        scene.add(line);
    }
}

function setupAtmosphere() {
    const geometry = new THREE.SphereGeometry(EARTH_RADIUS * 1.02, 64, 64);
    const material = new THREE.MeshBasicMaterial({
        color: 0x4488ff,
        transparent: true,
        opacity: 0.08,
        side: THREE.BackSide,
    });
    atmosphereMesh = new THREE.Mesh(geometry, material);
    scene.add(atmosphereMesh);
}

function setupObservers(list) {
    observers = [];
    const colors = [0xff4444, 0x44aaff, 0xffaa44, 0xaa44ff];
    list.forEach((o, i) => {
        const pos = latLonToVec3(o.lat_deg, o.lon_deg, EARTH_RADIUS);
        const color = colors[i % colors.length];

        const coneGeo = new THREE.ConeGeometry(0.015, OBSERVER_PIN_HEIGHT, 8);
        const coneMat = new THREE.MeshBasicMaterial({ color });
        const marker = new THREE.Mesh(coneGeo, coneMat);
        const surfaceNormal = pos.clone().normalize();
        marker.position.copy(pos.clone().add(surfaceNormal.clone().multiplyScalar(OBSERVER_PIN_HEIGHT / 2)));
        marker.quaternion.setFromUnitVectors(new THREE.Vector3(0, 1, 0), surfaceNormal);
        scene.add(marker);

        const baseGeo = new THREE.SphereGeometry(0.012, 8, 8);
        const baseMat = new THREE.MeshBasicMaterial({ color });
        const base = new THREE.Mesh(baseGeo, baseMat);
        base.position.copy(pos);
        scene.add(base);

        const beamMat = new THREE.LineBasicMaterial({
            color: 0x44ffaa,
            transparent: true,
            opacity: 0.6,
        });
        const beamGeo = new THREE.BufferGeometry().setFromPoints([
            new THREE.Vector3(0, 0, 0),
            new THREE.Vector3(0, 0, 0),
        ]);
        const beam = new THREE.Line(beamGeo, beamMat);
        beam.visible = false;
        scene.add(beam);

        observers.push({
            id: o.id || `sandbox-${i}`,
            pos,
            marker,
            beam,
            inCoverage: false,
        });
    });

    // Legacy single-pin refs for camera framing.
    observerPos = observers[0] ? observers[0].pos.clone() : new THREE.Vector3(0, EARTH_RADIUS, 0);
    observerMarker = observers[0] ? observers[0].marker : null;
    beamLine = observers[0] ? observers[0].beam : null;
}

function setupOrbitTrack(orbitPoints) {
    if (!orbitPoints || orbitPoints.length === 0) return;

    // orbit_points[i] = [lat, lon, alt]. Altitude is ignored here —
    // all points are rendered at the exaggerated SAT_RADIUS for visual
    // clarity (real altitude variation within one orbit is <1% of Earth radius).
    const points = orbitPoints.map(p => latLonToVec3(p[0], p[1], SAT_RADIUS));
    // Close the loop
    if (points.length > 1) {
        points.push(points[0].clone());
    }

    const geometry = new THREE.BufferGeometry().setFromPoints(points);
    const material = new THREE.LineDashedMaterial({
        color: 0xffffff,
        transparent: true,
        opacity: 0.25,
        dashSize: 0.05,
        gapSize: 0.02,
    });
    orbitLine = new THREE.Line(geometry, material);
    orbitLine.computeLineDistances();
    scene.add(orbitLine);
}

function setupSatellite() {
    // Satellite body — small box with "solar panels"
    const group = new THREE.Group();

    // Main body (small cube)
    const bodyGeo = new THREE.BoxGeometry(0.02, 0.015, 0.02);
    const bodyMat = new THREE.MeshStandardMaterial({ color: 0xcccccc, emissive: 0x444444, metalness: 0.5, roughness: 0.4 });
    const body = new THREE.Mesh(bodyGeo, bodyMat);
    group.add(body);

    // Solar panel left
    const panelGeo = new THREE.BoxGeometry(0.04, 0.001, 0.015);
    const panelMat = new THREE.MeshStandardMaterial({ color: 0x2244aa, emissive: 0x112255, metalness: 0.3, roughness: 0.6 });
    const panelL = new THREE.Mesh(panelGeo, panelMat);
    panelL.position.set(-0.03, 0, 0);
    group.add(panelL);

    // Solar panel right
    const panelR = new THREE.Mesh(panelGeo, panelMat);
    panelR.position.set(0.03, 0, 0);
    group.add(panelR);

    group.position.set(0, SAT_RADIUS, 0);
    satelliteMesh = group;
    currentSatPos.copy(group.position);
    targetSatPos.copy(group.position);
    scene.add(satelliteMesh);

    // Glow sprite around satellite
    const spriteMat = new THREE.SpriteMaterial({
        color: 0xffee88,
        transparent: true,
        opacity: 0.4,
    });
    const sprite = new THREE.Sprite(spriteMat);
    sprite.scale.set(0.08, 0.08, 1);
    satelliteMesh.add(sprite);
}

function setupBeam() {
    // Beams are created per observer in setupObservers.
}

function setupCamera() {
    const aspect = container.clientWidth / container.clientHeight;
    camera = new THREE.PerspectiveCamera(45, aspect, 0.01, 100);

    // Frame all observers when possible (dual SF+NYC otherwise hides one pin).
    let focus = observerPos.clone();
    let distance = 3.0;
    if (observers.length > 1) {
        focus = new THREE.Vector3();
        observers.forEach((o) => focus.add(o.pos));
        focus.multiplyScalar(1 / observers.length);
        let maxSep = 0;
        for (let i = 0; i < observers.length; i++) {
            for (let j = i + 1; j < observers.length; j++) {
                maxSep = Math.max(maxSep, observers[i].pos.distanceTo(observers[j].pos));
            }
        }
        distance = Math.max(3.2, 2.2 + maxSep * 1.8);
    }
    camDistance = distance;
    const camDir = focus.clone().normalize();
    const camPos = camDir.clone().multiplyScalar(distance);
    camPos.x += 0.4;
    camPos.y += 0.7;
    camera.position.copy(camPos);
    camera.lookAt(focus);
    // Keep auto-track aimed at the framing focus for multi-observer.
    observerPos.copy(focus);
}

function setupControls() {
    controls = new OrbitControls(camera, renderer.domElement);
    controls.enableDamping = true;
    controls.dampingFactor = 0.05;
    controls.minDistance = 1.5;
    controls.maxDistance = 8;
    controls.target.set(0, 0, 0);

    // Track user interaction via named handlers (for cleanup in destroy).
    renderer.domElement.addEventListener('pointerdown', onPointerDown);
    renderer.domElement.addEventListener('pointerup', onPointerUp);
    renderer.domElement.addEventListener('wheel', onWheel);
}

function onPointerDown() {
    userInteracting = true;
    lastInteractionTime = Date.now();
}

function onPointerUp() {
    lastInteractionTime = Date.now();
}

function onWheel() {
    userInteracting = true;
    lastInteractionTime = Date.now();
}

// --- Animation loop ---

function animate() {
    animFrameId = requestAnimationFrame(animate);

    // Lerp satellite toward target position
    currentSatPos.lerp(targetSatPos, LERP_FACTOR);
    satelliteMesh.position.copy(currentSatPos);

    // Update beam
    updateBeamGeometry();

    // Auto-track camera
    if (userInteracting && Date.now() - lastInteractionTime > AUTO_TRACK_RESUME_MS) {
        userInteracting = false;
    }
    if (!userInteracting) {
        autoTrackCamera();
    }

    controls.update();
    renderer.render(scene, camera);
}

function updateBeamGeometry() {
    for (const o of observers) {
        if (!o.beam) continue;
        const positions = o.beam.geometry.attributes.position;
        if (!positions) continue;
        positions.setXYZ(0, currentSatPos.x, currentSatPos.y, currentSatPos.z);
        positions.setXYZ(1, o.pos.x, o.pos.y, o.pos.z);
        positions.needsUpdate = true;
    }
}

function updateBeamVisibility() {
    for (const o of observers) {
        if (!o.beam) continue;
        o.beam.visible = !!o.inCoverage;
        o.beam.material.opacity = o.inCoverage ? 0.6 : 0;
    }
}

function autoTrackCamera() {
    // Gently move camera toward the multi-observer framing distance.
    const idealDir = observerPos.clone().normalize();
    const idealPos = idealDir.clone().multiplyScalar(camDistance);
    idealPos.y += 0.6;

    camera.position.lerp(idealPos, 0.005);
    controls.target.lerp(new THREE.Vector3(0, 0, 0), 0.01);
}

// --- Helpers ---

// latLonToVec3 maps geodetic degrees onto a Three.js Y-up sphere whose
// equirectangular Earth texture (nasa blue marble / three.js planets UV)
// has its seam at lon ±180°, not lon 0. The +180° longitude offset is
// required so California is on the Pacific coast of the texture, not the
// Middle East.
function latLonToVec3(latDeg, lonDeg, radius) {
    const phi = (90 - latDeg) * Math.PI / 180;       // polar angle from +Y
    const theta = (lonDeg + 180) * Math.PI / 180;    // match texture UV
    return new THREE.Vector3(
        -radius * Math.sin(phi) * Math.cos(theta),
         radius * Math.cos(phi),
         radius * Math.sin(phi) * Math.sin(theta)
    );
}

function onResize() {
    if (!container || !camera || !renderer) return;
    const w = container.clientWidth;
    const h = container.clientHeight;
    camera.aspect = w / h;
    camera.updateProjectionMatrix();
    renderer.setSize(w, h);
}
