# Standards framing

NTN-in-a-Box sits at the **app / sandbox / service API** layer: shape a path
like NTN, expose coverage and messaging hooks, and publish field-style metrics.
It is **not** a PHY/RAN stack, a certified modem, or a 5GAA product badge.

## How we map to industry language

| Phrase you may hear | What we model today | What we do **not** claim |
|---------------------|---------------------|---------------------------|
| **3GPP NTN** | Coverage windows, delay/jitter/loss/bandwidth shaped like LEO/GEO/D2C/NB-IoT-class profiles | Protocol stacks (RRC/NAS), band plans, or conformance tests |
| **5GAA / automotive NTN** | Blockage vs scheduled gaps, voice-oriented bearer presets, call-session field tallies, store-and-forward | In-vehicle integration, eCall MSD conformance, OEM certifications |
| **CAMARA** | Directionally “capability + condition APIs” for developers (`/condition`, `/capabilities`, messaging) | Published CAMARA endpoint parity or operator onboarding |
| **Voice over NTN** | Link-derived mouth-to-ear / MOS-ish estimates + call lifecycle telemetry for `--report` | Codecs, RTP, ITU E-model calibration, or a full call-control API |

## Useful mental model

- **Operators / OEMs** care about RF, modem, codec, and vehicle integration.
- **App developers** care about: “is the satellite path up?”, “what’s the
  delay/loss?”, “can I queue and flush?”, “what numbers do I publish?”
- This project targets the second list with honest, repeatable sandboxes.

## Related guides

- [Profiles](profiles.md) — LEO / GEO / NB-IoT / L-band / blockage shapes
- [Report](report.md) — coverage + messaging + voice field JSON
- [API](api.md) — condition, capabilities, call-events ingest
- [Architecture](architecture.md) — kernel vs modules

Full release-by-release / CAMARA endpoint matrices remain deferred until there
is external demand (automotive track item 11 stretch).
