package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// 跨服协调配置的四段配置面（配置中心的托管内容）：
// 拓扑（集群）、参与分组、玩法开关、匹配域。全部为声明式结构，
// 按规范化 JSON 持久化并计算 hash。
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

	// CrossServerSpec 跨服配置全文。
	CrossServerSpec struct {
		Topology     CrossServerTopology      `json:"topology"`
		Groups       []CrossServerGroup       `json:"groups"`
		Features     map[string]bool          `json:"features"`
		MatchDomains []CrossServerMatchDomain `json:"match_domains"`
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
