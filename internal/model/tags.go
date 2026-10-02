// Server tags: operator-set markers on a server record (docs/concepts.md).
//
// Tags are configuration, not lifecycle: the health monitor never creates or
// removes them, and a re-registering game server cannot overwrite them (the
// register upsert deliberately skips the tags column). Preset codes cover the
// common game-operations cases — 火热 / 爆满 / 禁止注册 / 维护中 / 新服 /
// 推荐 — and any custom code can be added through the admin API.
package model

import (
	"fmt"
	"regexp"
	"strings"
)

// TagTier is the presentation tier of a server tag. The frontend maps tiers
// to badge styles (hot → 火爆红, new → 新服绿, …); it is deliberately a small
// closed set so client styling stays predictable.
type TagTier string

const (
	// TierHot renders the badge in the "hot" style (火爆红).
	TierHot TagTier = "hot"
	// TierNew renders the badge in the "new" style (新服绿).
	TierNew TagTier = "new"
	// TierWarning renders the badge in the "warning" style (爆满/维护 黄).
	TierWarning TagTier = "warning"
	// TierInfo renders the badge in the "info" style (推荐 蓝).
	TierInfo TagTier = "info"
	// TierNeutral renders the badge in the default style.
	TierNeutral TagTier = "neutral"
)

// AllTagTiers lists every valid tag tier.
var AllTagTiers = []TagTier{TierHot, TierNew, TierWarning, TierInfo, TierNeutral}

// Valid reports whether t is a recognised tier.
func (t TagTier) Valid() bool {
	for _, v := range AllTagTiers {
		if t == v {
			return true
		}
	}
	return false
}

// Preset tag codes. These are stable machine codes: clients match on Code,
// never on Label (labels are operator-editable display text).
const (
	TagHot         = "hot"          // 火热
	TagFull        = "full"         // 爆满
	TagNoRegister  = "no_register"  // 禁止注册 — blocks character creation
	TagMaintenance = "maintenance"  // 维护中 — block or warn, per config
	TagNew         = "new"          // 新服
	TagRecommended = "recommended"  // 推荐
)

// ServerTag is one operator-set marker on a server.
//
// Public controls exposure: discovery/routing responses keep only Public
// tags, so internal-only markers (e.g. an ops note) never reach the player
// client. The admin API always returns the full list.
type ServerTag struct {
	// Code is the stable machine identifier: [a-z0-9][a-z0-9_-]{0,31}.
	Code string `json:"code"`
	// Label is the display text shown next to the badge (e.g. "火热").
	Label string `json:"label"`
	// Tier selects the badge style; see TagTier.
	Tier TagTier `json:"tier"`
	// Public marks the tag visible in player-facing discovery responses.
	Public bool `json:"public"`
}

// PresetTags are the built-in tags with their default label, tier and
// visibility. Operators may override Label/Tier/Public per server; the code
// stays the identity.
var PresetTags = map[string]ServerTag{
	TagHot:         {Code: TagHot, Label: "火热", Tier: TierHot, Public: true},
	TagFull:        {Code: TagFull, Label: "爆满", Tier: TierWarning, Public: true},
	TagNoRegister:  {Code: TagNoRegister, Label: "禁止注册", Tier: TierWarning, Public: true},
	TagMaintenance: {Code: TagMaintenance, Label: "维护中", Tier: TierWarning, Public: true},
	TagNew:         {Code: TagNew, Label: "新服", Tier: TierNew, Public: true},
	TagRecommended: {Code: TagRecommended, Label: "推荐", Tier: TierInfo, Public: true},
}

// tagCodeRe is the accepted shape of a tag code.
var tagCodeRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// IsPresetTag reports whether code is one of the built-in tag codes.
func IsPresetTag(code string) bool {
	_, ok := PresetTags[code]
	return ok
}

// FillTag defaults a tag against its preset (when there is one): an empty
// Label inherits the preset label, an empty Tier inherits the preset tier —
// or neutral for custom codes. A nil Public inherits the preset visibility
// (true for presets, false for custom tags, which are internal until an
// operator opts in).
func FillTag(t ServerTag, public *bool) ServerTag {
	t.Code = strings.TrimSpace(t.Code)
	preset, isPreset := PresetTags[t.Code]
	if t.Label == "" && isPreset {
		t.Label = preset.Label
	}
	if t.Tier == "" {
		if isPreset {
			t.Tier = preset.Tier
		} else {
			t.Tier = TierNeutral
		}
	}
	if public != nil {
		t.Public = *public
	} else if isPreset {
		t.Public = preset.Public
	}
	return t
}

// ValidateServerTags checks a full tag list for a server: every entry must
// carry a well-formed code, a non-empty label and a known tier, and codes
// must be unique within the list.
func ValidateServerTags(tags []ServerTag) error {
	seen := make(map[string]struct{}, len(tags))
	for i, t := range tags {
		if !tagCodeRe.MatchString(t.Code) {
			return fmt.Errorf("%w: tag[%d] code %q must match %s", ErrInvalid, i, t.Code, tagCodeRe)
		}
		if strings.TrimSpace(t.Label) == "" {
			return fmt.Errorf("%w: tag[%d] (%s) label is required", ErrInvalid, i, t.Code)
		}
		if !t.Tier.Valid() {
			return fmt.Errorf("%w: tag[%d] (%s) unknown tier %q", ErrInvalid, i, t.Code, t.Tier)
		}
		if _, dup := seen[t.Code]; dup {
			return fmt.Errorf("%w: duplicate tag code %q", ErrInvalid, t.Code)
		}
		seen[t.Code] = struct{}{}
	}
	return nil
}

// HasTag reports whether the tag list carries the given code.
func HasTag(tags []ServerTag, code string) bool {
	for _, t := range tags {
		if t.Code == code {
			return true
		}
	}
	return false
}

// RegistrationBlocked reports whether new-character requests (创角 / player
// registration) must be rejected for this server, returning the API error
// code and message to send. It returns "" when the request may proceed.
//
//   - 禁止注册 (no_register) tag: always blocked — REGISTRATION_FORBIDDEN.
//   - 维护中 (maintenance): blocked via SERVER_IN_MAINTENANCE, unless
//     warnOnly is set (ATLAS_MAINTENANCE_ENFORCE=warn) in which case the
//     request proceeds and the caller attaches a notice instead.
//
// Maintenance matches either the lifecycle status or the 维护中 tag, so an
// operator can force the gate on an otherwise-online server.
func (s *Server) RegistrationBlocked(warnOnly bool) (code, message string) {
	if HasTag(s.Tags, TagNoRegister) {
		return "REGISTRATION_FORBIDDEN",
			fmt.Sprintf("server %s does not accept new characters (禁止注册)", s.ID)
	}
	if s.InMaintenance() {
		if warnOnly {
			return "", ""
		}
		return "SERVER_IN_MAINTENANCE",
			fmt.Sprintf("server %s is under maintenance (维护中)", s.ID)
	}
	return "", ""
}

// InMaintenance reports whether the 维护中 gate applies: a maintenance
// lifecycle status or an explicit 维护中 tag (an operator can force the gate
// on an otherwise-online server).
func (s *Server) InMaintenance() bool {
	return s.Status == StatusMaintenance || HasTag(s.Tags, TagMaintenance)
}

// PublicTags returns the subset of tags marked visible to players. Discovery
// and routing responses run through this so internal tags never leak into
// player-facing JSON.
func PublicTags(tags []ServerTag) []ServerTag {
	var out []ServerTag
	for _, t := range tags {
		if t.Public {
			out = append(out, t)
		}
	}
	return out
}
