package httpapi

import (
	"context"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// failingStore forwards nothing: every method answers the configured
// error, so handler error branches (the 5xx paths callers depend on) can
// be driven without taking real storage down. The embedded real store
// keeps compile-time completeness; any method a handler reaches without
// an override would silently succeed and fail the sweep's 5xx assertion.
type failingStore struct {
	store.Store
	err error
}

func (f *failingStore) fail() error { return f.err }

// Ping is overridden too: readyz reports storage health through it, and an
// inherited success would keep a broken deployment looking ready.
func (f *failingStore) Ping(ctx context.Context) error {
	_ = ctx
	return f.fail()
}

func (f *failingStore) CreateAnnouncement(ctx context.Context, a *model.Announcement) error {
	_ = ctx
	return f.fail()
}

func (f *failingStore) CreateMaintenanceWindow(ctx context.Context, w *model.MaintenanceWindow) error {
	_ = ctx
	return f.fail()
}

func (f *failingStore) CreateMigration(ctx context.Context, m *model.Migration) error {
	_ = ctx
	return f.fail()
}

func (f *failingStore) CreateRealm(ctx context.Context, r *model.Realm) error {
	_ = ctx
	return f.fail()
}

func (f *failingStore) CreateShard(ctx context.Context, s *model.Shard) error {
	_ = ctx
	return f.fail()
}

func (f *failingStore) DeleteAnnouncement(ctx context.Context, id string) error {
	_ = ctx
	return f.fail()
}

func (f *failingStore) DeleteCharacter(ctx context.Context, accountID int64, serverID string, characterID int64) error {
	_ = ctx
	return f.fail()
}

func (f *failingStore) DeleteMaintenanceWindow(ctx context.Context, id string) error {
	_ = ctx
	return f.fail()
}

func (f *failingStore) DeleteRuntime(ctx context.Context, id string) error {
	_ = ctx
	return f.fail()
}

func (f *failingStore) DeleteServer(ctx context.Context, id string) error {
	_ = ctx
	return f.fail()
}

func (f *failingStore) GetAnnouncement(ctx context.Context, id string) (*model.Announcement, error) {
	_ = ctx
	return nil, f.fail()
}

func (f *failingStore) GetCharacter(ctx context.Context, accountID int64, serverID string, characterID int64) (*model.Character, error) {
	_ = ctx
	return nil, f.fail()
}

func (f *failingStore) GetCharacterByCharacterID(ctx context.Context, characterID int64) (*model.Character, error) {
	_ = ctx
	return nil, f.fail()
}

func (f *failingStore) GetCrossServerConfig(ctx context.Context) (*model.CrossServerConfig, error) {
	_ = ctx
	return nil, f.fail()
}

func (f *failingStore) GetMaintenanceWindow(ctx context.Context, id string) (*model.MaintenanceWindow, error) {
	_ = ctx
	return nil, f.fail()
}

func (f *failingStore) GetMigration(ctx context.Context, id string) (*model.Migration, error) {
	_ = ctx
	return nil, f.fail()
}

func (f *failingStore) GetRealm(ctx context.Context, id string) (*model.Realm, error) {
	_ = ctx
	return nil, f.fail()
}

func (f *failingStore) GetRuntime(ctx context.Context, id string) (*model.Runtime, error) {
	_ = ctx
	return nil, f.fail()
}

func (f *failingStore) GetRuntimes(ctx context.Context, ids []string) (map[string]model.Runtime, error) {
	_ = ctx
	return nil, f.fail()
}

func (f *failingStore) GetServer(ctx context.Context, id string) (*model.Server, error) {
	_ = ctx
	return nil, f.fail()
}

func (f *failingStore) GetShard(ctx context.Context, id string) (*model.Shard, error) {
	_ = ctx
	return nil, f.fail()
}

func (f *failingStore) GetStats(ctx context.Context) (*model.Stats, error) {
	_ = ctx
	return nil, f.fail()
}

func (f *failingStore) ListAnnouncements(ctx context.Context, filter store.AnnouncementFilter) ([]*model.Announcement, error) {
	_ = ctx
	return nil, f.fail()
}

func (f *failingStore) ListCharactersByAccount(ctx context.Context, accountID int64) ([]*model.Character, error) {
	_ = ctx
	return nil, f.fail()
}

func (f *failingStore) ListCharactersByServer(ctx context.Context, serverID string, limit int, cursor string) ([]*model.Character, error) {
	_ = ctx
	return nil, f.fail()
}

func (f *failingStore) ListMaintenanceWindows(ctx context.Context, serverID string, limit int) ([]*model.MaintenanceWindow, error) {
	_ = ctx
	return nil, f.fail()
}

func (f *failingStore) ListMigrations(ctx context.Context, limit int) ([]*model.Migration, error) {
	_ = ctx
	return nil, f.fail()
}

func (f *failingStore) ListRealms(ctx context.Context, limit int) ([]*model.Realm, error) {
	_ = ctx
	return nil, f.fail()
}

func (f *failingStore) ListRuntimes(ctx context.Context) (map[string]model.Runtime, error) {
	_ = ctx
	return nil, f.fail()
}

func (f *failingStore) ListServers(ctx context.Context, filter store.ServerFilter) ([]*model.Server, error) {
	_ = ctx
	return nil, f.fail()
}

func (f *failingStore) ListShards(ctx context.Context, realmID string, limit int) ([]*model.Shard, error) {
	_ = ctx
	return nil, f.fail()
}

func (f *failingStore) MarkMaintenanceWindowApplied(ctx context.Context, id string, previous model.ServerStatus) error {
	_ = ctx
	return f.fail()
}

func (f *failingStore) QueueStats() store.QueueStats {
	return store.QueueStats{}
}

func (f *failingStore) RecordHeartbeat(ctx context.Context, id string, hb model.Heartbeat) error {
	_ = ctx
	return f.fail()
}

func (f *failingStore) RegisterServer(ctx context.Context, s *model.Server) error {
	_ = ctx
	return f.fail()
}

func (f *failingStore) SaveCrossServerConfig(ctx context.Context, cfg *model.CrossServerConfig) (*model.CrossServerConfig, error) {
	_ = ctx
	return nil, f.fail()
}

func (f *failingStore) SearchCharacters(ctx context.Context, filter store.CharacterSearchFilter) ([]*model.Character, string, error) {
	_ = ctx
	return nil, "", f.fail()
}

func (f *failingStore) UpdateCharacter(ctx context.Context, accountID int64, serverID string, characterID int64, patch store.CharacterPatch) error {
	_ = ctx
	return f.fail()
}

func (f *failingStore) UpdateMigrationStatus(ctx context.Context, id string, status model.MigrationStatus, completedAt *time.Time) error {
	_ = ctx
	return f.fail()
}

func (f *failingStore) UpdateServerStatus(ctx context.Context, id string, status model.ServerStatus) error {
	_ = ctx
	return f.fail()
}

func (f *failingStore) UpdateServerStatusIf(ctx context.Context, id string, fromStatus, toStatus model.ServerStatus) (applied bool, err error) {
	_ = ctx
	return false, f.fail()
}

func (f *failingStore) UpdateServerTags(ctx context.Context, id string, tags []model.ServerTag) error {
	_ = ctx
	return f.fail()
}

func (f *failingStore) UpdateServerWeight(ctx context.Context, id string, weight int) error {
	_ = ctx
	return f.fail()
}

func (f *failingStore) UpsertCharacter(ctx context.Context, ch *model.Character) error {
	_ = ctx
	return f.fail()
}
