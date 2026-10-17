---
layout: home

hero:
  name: Atlas
  text: Game Infrastructure Directory / Control Plane
  tagline: Server registration, discovery and character directory infrastructure for online games. It answers four questions — "Who am I?", "Who is online?", "Where is my character?", "Which server should I join?" It is a control plane, not a game backend.
  image:
    src: /logo.svg
    alt: Atlas
  actions:
    - theme: brand
      text: Quick Start
      link: /api-quickstart
    - theme: alt
      text: Find Docs by Scenario
      link: #使用场景

features:
  - title: Server Registration and Heartbeat
    details: A game server registers its identity, topology and endpoints at startup, and keeps its online status through periodic heartbeats. After a heartbeat timeout it automatically moves to suspect → offline, so clients never see a dead server.
  - title: Cross-Server Character Directory
    details: An account-to-character cross-server index. Players see their character names, levels and tags on every server in a single view. This is a projection index, not authoritative data — the character database always belongs to the game server.
  - title: Server Merge and Migration Records
    details: Migration records and rollback API for server merges, transfers and migrations (status-record level). Character relocation and verification are performed by the game server; Atlas provides idempotent directory write endpoints to carry them.
  - title: Announcements and Planned Maintenance
    details: Operations declare, Atlas executes — one API call announces an emergency failure, and planned maintenance creates a window ahead of time — at start_at it automatically enters maintenance, at end_at it automatically recovers, the maintenance announcement is posted automatically for the same period, and clients see it as soon as they log in.
---

<script setup lang="ts">
// 首次进入按浏览器语言选择文档语言（vitepress 无此能力，GitHub Pages 亦不支持 _redirects）
// 优先级：localStorage 手动选择 > navigator.language > 保持当前 locale
import { onBeforeMount } from 'vue'
const KEY = 'atlas-lang-picked'
onBeforeMount(() => {
  if (typeof window === 'undefined') return
  if (window.location.pathname !== '/') return
  const picked = localStorage.getItem(KEY)
  if (picked) { if (picked !== '/') window.location.replace(picked); return }
  const langs = (navigator.languages?.length ? navigator.languages : [navigator.language]).map(l => l.toLowerCase())
  if (langs.some(l => l.startsWith('zh'))) window.location.replace('/zh/')
})
if (typeof window !== 'undefined') {
  window.addEventListener('click', (e) => {
    const a = (e.target as HTMLElement)?.closest?.('a') as HTMLAnchorElement | null
    const href = a?.getAttribute('href') || ''
    if (href.startsWith('/zh/')) localStorage.setItem(KEY, '/zh/')
    else if (href === '/' || href.startsWith('/en/')) localStorage.setItem(KEY, '/')
  }, true)
}
const showcaseSlides = [
  { image: '/screenshots/overview.png', title: 'Overview', caption: 'Overview — online servers / players / capacity and status distribution', link: '/scenarios#scenario-overview' },
  { image: '/screenshots/servers.png', title: 'Server List', caption: 'Server List — status, badges (hot/new/full) and capacity watermark', link: '/scenarios#scenario-tags' },
  { image: '/screenshots/server-detail-tags.png', title: 'Server Detail', caption: 'Server Detail — tag management (public/internal) and real-time metrics', link: '/scenarios#scenario-tags' },
  { image: '/screenshots/characters.png', title: 'Character Search', caption: 'Character Search — cross-server character index, filter by name/server/level', link: '/scenarios#scenario-characters' },
  { image: '/screenshots/migrations.png', title: 'Migration Management', caption: 'Migration Management — server merge/transfer orchestration and rollback', link: '/scenarios#scenario-migration' },
]
</script>

<ShowcaseCarousel :slides="showcaseSlides" />

Every screenshot comes from a real running Atlas + dashboard (the data is a demo cluster). Walk through it scenario by scenario in **[Scenario Tour](/scenarios)**; for the implementation-level trade-offs see **[Performance Design](/performance)**.

## Run It with One Command

```bash
go run ./cmd/atlas
# …level=INFO msg="public API listening"   addr=:8080
# …level=INFO msg="registry API listening" addr=:8081 scheme=http
# …level=INFO msg="admin API listening"    addr=:8082
# …level=INFO msg="gRPC API listening"     addr=:9090
```

Once you see the four listening lines, Atlas is running (in-memory storage, zero dependencies; the gRPC port can be turned off with `ATLAS_GRPC_ADDR=""`). Next, the [Quick Start](/api-quickstart)
walks the full chain of **register online → player picks a server → character directory → admin operations → graceful shutdown** in about 5 minutes with all real commands.

## Use Cases

Atlas sits between game servers and player clients, and three kinds of people each take something different from it:

| You are | Typical questions | Where to go |
| --- | --- | --- |
| Game server developer | How does a process hook into registration, how do heartbeats work, how do I shut down gracefully? Can I skip writing registration code? | [Quick Start §1/§4](/api-quickstart) · [Server Lifecycle](/lifecycle) · [Server Info as Config](/server-config) · [Six-Language SDK](/sdk-go) |
| Client / gateway developer | Which server does the player join after login? Where is my character? Where do I pull announcements from? | [Quick Start §2](/api-quickstart) · [Server Recommendation and Sticky](/concepts) · [APISIX Integration](/apisix) |
| Operations / platform | How do I manage realms and shards? How do I post announcements and schedule maintenance? Who tells me when something drops offline at midnight? How do I orchestrate merges and transfers? | [Realm and Shard](/realms-shards) · [Announcements and Planned Maintenance](/operations) · [Health Alerting](/lifecycle#_4-1-health-alerting-delivered-in-v0-1-15) · [Server Merge / Transfer / Migration](/migration) |
| Architect / platform lead | How do I deploy multiple replicas? How do I pick a message bus? How much QPS can it take? | [High Availability](/ha) · [Deployment Topology](/topology) · [Data Sync](/sync) · [Benchmarks](/benchmarks) |

## Capabilities at a Glance

| Capability | In one sentence | Docs |
| --- | --- | --- |
| Registration and heartbeat | Game servers register identity and endpoints, heartbeats keep them alive, loss of contact is two-stage failure detection (suspect → offline) | [Lifecycle](/lifecycle) |
| Discovery and recommendation | Filter online servers by region / realm / shard; recommend "which server to join" per account, sticky if a character exists | [Quick Start §2](/api-quickstart) |
| Character directory | Account → cross-server character index, pulled in full with one login; the write endpoint is the sole entry point, optionally buffered asynchronously via Message Bus, landing at-least-once and idempotent | [Data Sync](/sync) |
| Server info as config | Static fleet declares servers in JSON, effective as soon as the server starts, no registration code; profile switches environments in one step | [Server Info as Config](/server-config) |
| Realm / Shard | Three-tier operational records: realm → shard → instance, clients pull lists by dimension | [Realm and Shard](/realms-shards) |
| Announcements and planned maintenance | Announcements become visible automatically within their effective interval; maintenance windows enter maintenance on time and recover automatically at the end | [Announcements and Planned Maintenance](/operations) |
| Health alerting | When the lost-contact / dead ratio crosses a threshold, latch and fire a webhook + Warn log, without spamming | [Health Alerting](/lifecycle#_4-1-health-alerting-delivered-in-v0-1-15) |
| Server merge / transfer / migration | Atomic character index migration: copy first, switch next, clean up last, rollback on failure | [Migration](/migration) |
| Gateway and authentication | Two APISIX plugins: player token validation + identity injection, rate limiting by endpoint group | [APISIX Integration Plugins](/apisix) |
| High availability | Atlas replicas are stateless, PG / Redis backends are shared, HAProxy fans in heartbeats | [High Availability](/ha) |
