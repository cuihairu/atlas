import { QuartzComponent, QuartzComponentConstructor, QuartzComponentProps } from "../quartz/components/types"
import { pathToRoot, joinSegments } from "../quartz/util/path"

const cards = [
  { title: "游戏服务器", href: "95-domains/game-server" },
  { title: "区块链", href: "95-domains/blockchain" },
  { title: "数据分析", href: "62-data-analytics" },
  { title: "协议与格式", href: "30-protocols-formats" },
  { title: "Kubernetes", href: "50-infrastructure/orchestration/k8s" },
  { title: "Docker", href: "50-infrastructure/containers/docker" },
  { title: "Redis", href: "60-data-systems/cache/redis" },
  { title: "MySQL", href: "60-data-systems/relational/mysql" },
  { title: "并发模型", href: "75-concurrency-parallelism" },
  { title: "分布式", href: "70-distributed-systems" },
  { title: "观测与监控", href: "85-observability" },
  { title: "性能与压测", href: "86-performance-capacity" },
]

const HomeCards: QuartzComponent = ({ fileData }: QuartzComponentProps) => {
  // Only render on home page
  if (fileData.slug !== "index") return null
  const base = pathToRoot(fileData.slug!)
  return (
    <div class="home-cards">
      {cards.map((c) => (
        <a class="home-card" href={joinSegments(base, c.href)}>
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
.home-card:hover{ border-color: var(--secondary); box-shadow: 0 2px 8px rgba(0,0,0,0.05); }
.home-card-title{ font-weight:600; }
`

export default (() => HomeCards) satisfies QuartzComponentConstructor
