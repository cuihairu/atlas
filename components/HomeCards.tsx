import { QuartzComponent, QuartzComponentConstructor, QuartzComponentProps } from "../quartz/components/types"
import { pathToRoot, joinSegments } from "../quartz/util/path"

type Card = { title: string; href: string; icon: string }
const cards: Card[] = [
  { title: "游戏服务器", href: "domains/game-server", icon: "gamepad" },
  { title: "区块链", href: "domains/blockchain", icon: "chain" },
  { title: "数据分析", href: "data-analytics", icon: "chart" },
  { title: "协议与格式", href: "protocols-formats", icon: "layers" },
  { title: "Kubernetes", href: "infrastructure/orchestration/k8s", icon: "kube" },
  { title: "Docker", href: "infrastructure/containers/docker", icon: "docker" },
  { title: "Redis", href: "data-systems/cache/redis", icon: "db" },
  { title: "MySQL", href: "data-systems/relational/mysql", icon: "db" },
  { title: "并发模型", href: "concurrency-parallelism", icon: "threads" },
  { title: "分布式", href: "distributed-systems", icon: "network" },
  { title: "观测与监控", href: "observability", icon: "eye" },
  { title: "性能与压测", href: "performance-capacity", icon: "speed" },
]

function Icon({ name }: { name: string }) {
  // Minimal inline SVG icons; no external assets
  switch (name) {
    case "gamepad":
      return (
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M6 12h12a4 4 0 1 1-3.5 2h-5A4 4 0 1 1 6 12Z" />
          <path d="M8 12v-2M8 12h-2M16 12h.01M18 10h.01" />
        </svg>
      )
    case "chain":
      return (
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M10 13a5 5 0 0 0 7.07 0l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
          <path d="M14 11a5 5 0 0 0-7.07 0l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
        </svg>
      )
    case "chart":
      return (
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M3 3v18h18" />
          <rect x="6" y="10" width="3" height="7" />
          <rect x="11" y="6" width="3" height="11" />
          <rect x="16" y="13" width="3" height="4" />
        </svg>
      )
    case "layers":
      return (
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <polygon points="12 2 2 7 12 12 22 7 12 2" />
          <polyline points="2 12 12 17 22 12" />
          <polyline points="2 17 12 22 22 17" />
        </svg>
      )
    case "kube":
      return (
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <polygon points="12 2 3 7 3 17 12 22 21 17 21 7 12 2" />
          <path d="M12 22V12L3 7" />
          <path d="M12 12l9-5" />
        </svg>
      )
    case "docker":
      return (
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <rect x="3" y="10" width="4" height="4" />
          <rect x="8" y="10" width="4" height="4" />
          <rect x="13" y="10" width="4" height="4" />
          <path d="M2 15c1 2 3 4 7 4h6c3 0 5-2 6-4" />
        </svg>
      )
    case "db":
      return (
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <ellipse cx="12" cy="5" rx="9" ry="3" />
          <path d="M3 5v6c0 1.7 4 3 9 3s9-1.3 9-3V5" />
          <path d="M3 11v6c0 1.7 4 3 9 3s9-1.3 9-3v-6" />
        </svg>
      )
    case "threads":
      return (
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M4 6h16M4 12h16M4 18h16" />
        </svg>
      )
    case "network":
      return (
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <circle cx="12" cy="12" r="3" />
          <path d="M12 2v7M12 15v7M2 12h7M15 12h7M4.9 4.9l5 5M14.1 14.1l5 5M4.9 19.1l5-5M14.1 9.9l5-5" />
        </svg>
      )
    case "eye":
      return (
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M1 12s4-7 11-7 11 7 11 7-4 7-11 7S1 12 1 12z" />
          <circle cx="12" cy="12" r="3" />
        </svg>
      )
    case "speed":
      return (
        <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M21 14a9 9 0 1 0-18 0" />
          <path d="M10 10l4 4" />
        </svg>
      )
    default:
      return null
  }
}

const HomeCards: QuartzComponent = ({ fileData }: QuartzComponentProps) => {
  // Only render on home page
  if (fileData.slug !== "index") return null
  const base = pathToRoot(fileData.slug!)
  return (
    <div class="home-cards">
      {cards.map((c) => (
        <a class="home-card" href={joinSegments(base, c.href)}>
          <div class="home-card-icon"><Icon name={c.icon} /></div>
          <div class="home-card-title">{c.title}</div>
        </a>
      ))}
    </div>
  )
}

HomeCards.css = `
.home-cards{
  display:grid;
  grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
  gap: 0.8rem;
  margin: 0.75rem 0 1.25rem 0;
}
.home-card{
  display:block;
  border: 1px solid var(--lightgray);
  border-radius: 10px;
  padding: 0.9rem 0.8rem;
  text-decoration: none;
  background: var(--light);
}
.home-card-icon{ width:24px; height:24px; color: var(--secondary); }
.home-card:hover{ border-color: var(--secondary); box-shadow: 0 2px 8px rgba(0,0,0,0.05); }
.home-card-title{ font-weight:600; }
`

export default (() => HomeCards) satisfies QuartzComponentConstructor
