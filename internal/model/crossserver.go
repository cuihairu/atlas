package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"time"
)

// 跨服协调配置的五段配置面（配置中心的托管内容）：拓扑（集群）、参与
// 分组、玩法开关、匹配域、跨服玩法类型表。全部为声明式结构，按规范化
// JSON 持久化并计算 hash。
type (
	// CrossServerCluster 跨服拓扑中的一个集群（一组共同参与跨服玩法的
	// 服务器）。Servers 为 server_id 列表，允许先建集群后补成员。
	CrossServerCluster struct {
		ID      string   `json:"id"`
		Name    string   `json:"name,omitempty"`
		Region  string   `json:"region,omitempty"`
		Status  string   `json:"status,omitempty"` // active(缺省) / disabled
		Servers []string `json:"servers"`
	}

	// CrossServerGroup 参与分组：按运营口径圈定的一批服务器
	// （如「华东跨服战场第一期」），玩法开关与匹配域可引用。
	CrossServerGroup struct {
		ID      string   `json:"id"`
		Name    string   `json:"name,omitempty"`
		Servers []string `json:"servers"`
	}

	// CrossServerMatchDomain 匹配域：同一匹配池的服务器集合。
	// Params 为游戏自定义参数（字符串值，如 mmr_range / max_team）。
	CrossServerMatchDomain struct {
		ID      string            `json:"id"`
		Name    string            `json:"name,omitempty"`
		Servers []string          `json:"servers"`
		Params  map[string]string `json:"params,omitempty"`
	}

	// CrossPlayType 跨服玩法类型表（crossplay_types）的一行：一类跨服
	// 玩法的声明式元数据（docs/config-center.md §2.2）。类型表是舰队级
	// 声明——改动即全局变更（diff targets=["*"]）。ID 是代码标识（术语
	// 契约的定名锚点），IDPrefix 是该类型运行时 ID 的类型前缀（§2.1
	// 跨服 ID 体系，全表唯一），供日志/榜单按前缀 grep 回溯。
	CrossPlayType struct {
		ID          string `json:"id"`                    // 代码标识，如 "battlefield"（[a-z0-9._-]）
		Name        string `json:"name,omitempty"`        // 中文定名，如「跨服战场」
		Summary     string `json:"summary,omitempty"`     // 一句话定义
		Lifecycle   string `json:"lifecycle,omitempty"`   // persistent(缺省) / seasonal / ephemeral
		Matchmaking bool   `json:"matchmaking,omitempty"` // 参与经 match_domains 池撮合
		Ranking     bool   `json:"ranking,omitempty"`     // 需要排行榜数据汇聚
		IDPrefix    string `json:"id_prefix,omitempty"`   // 运行时 ID 类型前缀（2-8 位小写字母，全表唯一）
	}

	// CrossServerSpec 跨服配置全文。
	CrossServerSpec struct {
		Topology       CrossServerTopology      `json:"topology"`
		Groups         []CrossServerGroup       `json:"groups"`
		Features       map[string]bool          `json:"features"`
		MatchDomains   []CrossServerMatchDomain `json:"match_domains"`
		CrossPlayTypes []CrossPlayType          `json:"crossplay_types"`
	}

	// CrossServerTopology 跨服拓扑（集群列表）。
	CrossServerTopology struct {
		Clusters []CrossServerCluster `json:"clusters"`
	}

	// CrossServerConfig 带版本号的跨服配置快照。Version 由存储层在
	// 每次保存时原子自增（单调递增），Hash 为 Spec 的规范化摘要。
	CrossServerConfig struct {
		Version   int             `json:"version"`
		Hash      string          `json:"hash"`
		Spec      CrossServerSpec `json:"spec"`
		UpdatedAt time.Time       `json:"updated_at"`
	}
)

// CrossPlayType.Lifecycle 的合法取值（缺省 persistent）：常驻玩法 /
// 赛季制（榜单与匹配池随赛季重置）/ 限时活动窗口。
const (
	CrossPlayLifecyclePersistent = "persistent"
	CrossPlayLifecycleSeasonal   = "seasonal"
	CrossPlayLifecycleEphemeral  = "ephemeral"
)

// 七类标准跨服玩法类型的代码标识（术语契约锚点，docs/config-center.md
// §2.2）。类型表由运营发布而非代码内置，这套常量保证文档、管理台与
// 接入方对「类型定名」引用的是同一个词。
const (
	CrossPlayBattlefield = "battlefield" // 跨服战场 Cross-Server Battlefield（xb）
	CrossPlayDungeon     = "dungeon"     // 跨服副本/BOSS Cross-Server Dungeon（xd）
	CrossPlayRanking     = "ranking"     // 跨服排行榜 Cross-Server Ranking（xr）
	CrossPlayGuildWar    = "guildwar"    // 跨服公会战/领地战 Cross-Server Guild War（xg）
	CrossPlayTrade       = "trade"       // 跨服交易行/拍卖 Cross-Server Trade（xt）
	CrossPlayChat        = "chat"        // 跨服聊天/社交 Cross-Server Chat（xc）
	CrossPlayTeam        = "team"        // 跨服组队/招募 Cross-Server Team Up（xp）
)

// Clone returns a deep copy of the spec: every slice and map is rebuilt,
// so a caller mutating its copy can never write through into a stored
// snapshot. Same zero-sharing rule the server store applies to tags —
// the SQL stores get this for free from their JSON round-trip, memory
// must do it explicitly.
func (s CrossServerSpec) Clone() CrossServerSpec {
	out := CrossServerSpec{
		Topology:       CrossServerTopology{Clusters: make([]CrossServerCluster, len(s.Topology.Clusters))},
		Groups:         make([]CrossServerGroup, len(s.Groups)),
		Features:       maps.Clone(s.Features),
		MatchDomains:   make([]CrossServerMatchDomain, len(s.MatchDomains)),
		CrossPlayTypes: slices.Clone(s.CrossPlayTypes),
	}
	for i, c := range s.Topology.Clusters {
		c.Servers = slices.Clone(c.Servers)
		out.Topology.Clusters[i] = c
	}
	for i, g := range s.Groups {
		g.Servers = slices.Clone(g.Servers)
		out.Groups[i] = g
	}
	for i, d := range s.MatchDomains {
		d.Servers = slices.Clone(d.Servers)
		d.Params = maps.Clone(d.Params)
		out.MatchDomains[i] = d
	}
	return out
}

// TargetAll 是变更目标里的通配符：任何服务器都应重新拉取
// （全局变更，如玩法开关表整体改动）。
const TargetAll = "*"

// ETag 返回该配置的 HTTP 强校验标签（引号包裹的 hash）。
func (c *CrossServerConfig) ETag() string { return `"` + c.Hash + `"` }

// NormalizeCrossServerSpec 把 nil 切片 / map 补成空容器。JSON 里 nil 与
// 空数组序列化不同，不归一化则「同一份配置」可能算出两个 hash，
// 幂等保存（内容未变则不升版本）就失效了。
func NormalizeCrossServerSpec(spec CrossServerSpec) CrossServerSpec {
	if spec.Topology.Clusters == nil {
		spec.Topology.Clusters = []CrossServerCluster{}
	}
	if spec.Groups == nil {
		spec.Groups = []CrossServerGroup{}
	}
	if spec.Features == nil {
		spec.Features = map[string]bool{}
	}
	if spec.MatchDomains == nil {
		spec.MatchDomains = []CrossServerMatchDomain{}
	}
	if spec.CrossPlayTypes == nil {
		spec.CrossPlayTypes = []CrossPlayType{}
	}
	for i := range spec.MatchDomains {
		if spec.MatchDomains[i].Servers == nil {
			spec.MatchDomains[i].Servers = []string{}
		}
		if len(spec.MatchDomains[i].Params) == 0 {
			spec.MatchDomains[i].Params = nil
		}
	}
	return spec
}

// CloneCrossServerSpec 返回 spec 的深拷贝。配置文档按值存取、按前后
// 两个版本做 diff：store 若与调用方共享底层数组，调用方之后的原地
// 改写会连同已存的历史一起改掉（SQL store 走 JSON 序列化天然无别名，
// 内存 store 必须显式克隆——三库契约一致）。
func CloneCrossServerSpec(spec CrossServerSpec) CrossServerSpec {
	out := spec
	out.Topology.Clusters = make([]CrossServerCluster, len(spec.Topology.Clusters))
	for i, c := range spec.Topology.Clusters {
		c.Servers = slices.Clone(c.Servers)
		out.Topology.Clusters[i] = c
	}
	out.Groups = make([]CrossServerGroup, len(spec.Groups))
	for i, g := range spec.Groups {
		g.Servers = slices.Clone(g.Servers)
		out.Groups[i] = g
	}
	out.Features = maps.Clone(spec.Features)
	out.MatchDomains = make([]CrossServerMatchDomain, len(spec.MatchDomains))
	for i, d := range spec.MatchDomains {
		d.Servers = slices.Clone(d.Servers)
		d.Params = maps.Clone(d.Params)
		out.MatchDomains[i] = d
	}
	// 类型表行是纯值类型（string/bool），切片克隆即完全独立。
	out.CrossPlayTypes = slices.Clone(spec.CrossPlayTypes)
	return out
}

// EmptyCrossServerConfig 是 version=0 的空快照：配置中心还没被写过。
// 游戏服务器启动拉取拿到它即为「暂无跨服协调配置」，可照常起服
// （不断服），而不是把它当成拉取失败。
func EmptyCrossServerConfig() *CrossServerConfig {
	return &CrossServerConfig{
		Version: 0,
		Hash:    HashCrossServerSpec(NormalizeCrossServerSpec(CrossServerSpec{})),
		Spec:    NormalizeCrossServerSpec(CrossServerSpec{}),
	}
}

// HashCrossServerSpec 计算 Spec 的确定性摘要：encoding/json 对 map
// 按键排序、struct 字段序固定，因此同一 Spec 的序列化字节稳定；
// 取 sha256 前 16 个十六进制字符（64 bit）作版本指纹。
func HashCrossServerSpec(spec CrossServerSpec) string {
	spec = NormalizeCrossServerSpec(spec)
	payload, err := json.Marshal(spec)
	if err != nil {
		// Spec 只含 string/bool/slice/map，Marshal 不可能失败；
		// 防御性兜底：内容不变则摘要恒定。
		payload = []byte(fmt.Sprintf("%v", spec))
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])[:16]
}

// ValidateCrossServerSpec 结构校验：ID 唯一、玩法开关 key 合法。
// 不校验 server 存在性——引用的服务器可后注册（拓扑先行的规划期）。
func ValidateCrossServerSpec(spec CrossServerSpec) error {
	clusterIDs := make(map[string]bool, len(spec.Topology.Clusters))
	for i, c := range spec.Topology.Clusters {
		if c.ID == "" {
			return fmt.Errorf("topology.clusters[%d].id: required", i)
		}
		if clusterIDs[c.ID] {
			return fmt.Errorf("topology.clusters[%d].id %q: duplicate", i, c.ID)
		}
		clusterIDs[c.ID] = true
		if c.Status != "" && c.Status != "active" && c.Status != "disabled" {
			return fmt.Errorf("topology.clusters[%d].status %q: must be active or disabled", i, c.Status)
		}
		if err := validateServerIDs(fmt.Sprintf("topology.clusters[%d].servers", i), c.Servers); err != nil {
			return err
		}
	}
	groupIDs := make(map[string]bool, len(spec.Groups))
	for i, g := range spec.Groups {
		if g.ID == "" {
			return fmt.Errorf("groups[%d].id: required", i)
		}
		if groupIDs[g.ID] {
			return fmt.Errorf("groups[%d].id %q: duplicate", i, g.ID)
		}
		groupIDs[g.ID] = true
		if err := validateServerIDs(fmt.Sprintf("groups[%d].servers", i), g.Servers); err != nil {
			return err
		}
	}
	for key := range spec.Features {
		if err := validateConfigKey(key); err != nil {
			return fmt.Errorf("features.%s: %w", key, err)
		}
	}
	domainIDs := make(map[string]bool, len(spec.MatchDomains))
	for i, d := range spec.MatchDomains {
		if d.ID == "" {
			return fmt.Errorf("match_domains[%d].id: required", i)
		}
		if domainIDs[d.ID] {
			return fmt.Errorf("match_domains[%d].id %q: duplicate", i, d.ID)
		}
		domainIDs[d.ID] = true
		if err := validateServerIDs(fmt.Sprintf("match_domains[%d].servers", i), d.Servers); err != nil {
			return err
		}
	}
	typeIDs := make(map[string]bool, len(spec.CrossPlayTypes))
	prefixes := make(map[string]bool, len(spec.CrossPlayTypes))
	for i, tp := range spec.CrossPlayTypes {
		if tp.ID == "" {
			return fmt.Errorf("crossplay_types[%d].id: required", i)
		}
		if err := validateConfigKey(tp.ID); err != nil {
			return fmt.Errorf("crossplay_types[%d].id: %w", i, err)
		}
		if typeIDs[tp.ID] {
			return fmt.Errorf("crossplay_types[%d].id %q: duplicate", i, tp.ID)
		}
		typeIDs[tp.ID] = true
		switch tp.Lifecycle {
		case "", CrossPlayLifecyclePersistent, CrossPlayLifecycleSeasonal, CrossPlayLifecycleEphemeral:
		default:
			return fmt.Errorf("crossplay_types[%d].lifecycle %q: must be persistent, seasonal or ephemeral", i, tp.Lifecycle)
		}
		if tp.IDPrefix == "" {
			continue
		}
		if err := validateIDPrefix(tp.IDPrefix); err != nil {
			return fmt.Errorf("crossplay_types[%d].id_prefix: %w", i, err)
		}
		// Prefixes must be unique so a runtime ID greps back to exactly
		// one type (ambiguous prefixes break log/bboard traceability).
		if prefixes[tp.IDPrefix] {
			return fmt.Errorf("crossplay_types[%d].id_prefix %q: duplicate", i, tp.IDPrefix)
		}
		prefixes[tp.IDPrefix] = true
	}
	return nil
}

// validateIDPrefix checks a runtime-ID type prefix: 2-8 lowercase ASCII
// letters (docs/config-center.md §2.1), e.g. "xb" / "xd". Letters-only
// keeps prefixes unambiguous against the "-" segment separator.
func validateIDPrefix(p string) error {
	if len(p) < 2 || len(p) > 8 {
		return fmt.Errorf("%q: must be 2-8 lowercase letters", p)
	}
	for i := 0; i < len(p); i++ {
		if c := p[i]; c < 'a' || c > 'z' {
			return fmt.Errorf("%q: must be 2-8 lowercase letters", p)
		}
	}
	return nil
}

func validateServerIDs(field string, ids []string) error {
	seen := make(map[string]bool, len(ids))
	for i, id := range ids {
		if id == "" {
			return fmt.Errorf("%s[%d]: empty server id", field, i)
		}
		if seen[id] {
			return fmt.Errorf("%s[%d] %q: duplicate server id", field, i, id)
		}
		seen[id] = true
	}
	return nil
}

func validateConfigKey(key string) error {
	if key == "" {
		return fmt.Errorf("key required")
	}
	if len(key) > 64 {
		return fmt.Errorf("key too long (max 64)")
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case (c == '_' || c == '-' || c == '.') && i > 0:
		default:
			return fmt.Errorf("key %q: only [a-z0-9._-] allowed", key)
		}
	}
	if c := key[0]; (c < 'a' || c > 'z') && (c < '0' || c > '9') {
		return fmt.Errorf("key %q: must start with [a-z0-9]", key)
	}
	return nil
}
