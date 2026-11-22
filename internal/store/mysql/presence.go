package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// PlayerSessionStore implementation (docs/presence.md §3). Schema twin of the
// PostgreSQL store (postgres/presence.go); the state machine is the single
// source of truth pinned by the storetest contract. MySQL's InnoDB row locks
// serialize concurrent reports for the same character via SELECT ... FOR
// UPDATE.

func (s *Store) Login(ctx context.Context, accountID int64, serverID string, characterID int64, sessionID string, at time.Time) (*model.PlayerSession, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("%w: session_id is required", store.ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("login character: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit

	var status string
	err = tx.QueryRowContext(ctx,
		`SELECT presence_status FROM character_index
		 WHERE account_id = ? AND server_id = ? AND character_id = ? FOR UPDATE`,
		accountID, serverID, characterID,
	).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("character (account=%d, server=%s, char=%d): %w", accountID, serverID, characterID, store.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("login character: %w", err)
	}

	if model.PresenceStatus(status) == model.PresenceOnline {
		var existing model.PlayerSession
		var endedAt *time.Time
		var endReason string
		err = tx.QueryRowContext(ctx,
			`SELECT session_id, account_id, character_id, server_id, started_at, ended_at, end_reason
			 FROM player_sessions WHERE character_id = ? AND ended_at IS NULL`,
			characterID,
		).Scan(&existing.SessionID, &existing.AccountID, &existing.CharacterID,
			&existing.ServerID, &existing.StartedAt, &endedAt, &endReason)
		if err == nil && existing.SessionID == sessionID {
			existing.EndedAt = endedAt
			existing.EndReason = model.SessionEndReason(endReason)
			if err := tx.Commit(); err != nil {
				return nil, fmt.Errorf("login character: %w", err)
			}
			return &existing, nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("login character: %w", err)
		}
		if err == nil {
			return nil, fmt.Errorf("character (account=%d, server=%s, char=%d): already online in a different session: %w", accountID, serverID, characterID, store.ErrConflict)
		}
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE player_sessions SET ended_at = ?, end_reason = ?
		 WHERE character_id = ? AND ended_at IS NULL`,
		at, string(model.SessionEndCrash), characterID,
	); err != nil {
		return nil, fmt.Errorf("login character: %w", err)
	}

	sess := &model.PlayerSession{
		SessionID:   sessionID,
		AccountID:   accountID,
		ServerID:    serverID,
		CharacterID: characterID,
		StartedAt:   at,
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO player_sessions (session_id, account_id, character_id, server_id, started_at)
		 VALUES (?, ?, ?, ?, ?)`,
		sess.SessionID, sess.AccountID, sess.CharacterID, sess.ServerID, sess.StartedAt,
	); err != nil {
		return nil, fmt.Errorf("login character: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE character_index SET presence_status = ?, last_login_at = ?, updated_at = ?
		 WHERE account_id = ? AND server_id = ? AND character_id = ?`,
		string(model.PresenceOnline), at, time.Now(), accountID, serverID, characterID,
	); err != nil {
		return nil, fmt.Errorf("login character: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("login character: %w", err)
	}
	return sess, nil
}

func (s *Store) Logout(ctx context.Context, accountID int64, serverID string, characterID int64, sessionID string, reason model.SessionEndReason, endAt time.Time) error {
	if !reason.Valid() {
		return fmt.Errorf("%w: invalid end_reason %q", store.ErrInvalid, reason)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("logout character: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit

	var status string
	err = tx.QueryRowContext(ctx,
		`SELECT presence_status FROM character_index
		 WHERE account_id = ? AND server_id = ? AND character_id = ? FOR UPDATE`,
		accountID, serverID, characterID,
	).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("character (account=%d, server=%s, char=%d): %w", accountID, serverID, characterID, store.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("logout character: %w", err)
	}

	cur := model.PresenceStatus(status)
	if cur != model.PresenceOnline {
		if cur == model.PresenceOffline {
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("logout character: %w", err)
			}
			return nil
		}
		return fmt.Errorf("character (account=%d, server=%s, char=%d): logout with no open session: %w", accountID, serverID, characterID, store.ErrInvalid)
	}

	var openID string
	err = tx.QueryRowContext(ctx,
		`SELECT session_id FROM player_sessions WHERE character_id = ? AND ended_at IS NULL`,
		characterID,
	).Scan(&openID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("logout character: %w", err)
	}
	if err == nil && sessionID != "" && sessionID != openID {
		return fmt.Errorf("character (account=%d, server=%s, char=%d): session %s is not the open session: %w", accountID, serverID, characterID, sessionID, store.ErrConflict)
	}
	if err == nil {
		if _, err := tx.ExecContext(ctx,
			`UPDATE player_sessions SET ended_at = ?, end_reason = ? WHERE session_id = ?`,
			endAt, string(reason), openID,
		); err != nil {
			return fmt.Errorf("logout character: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE character_index SET presence_status = ?, last_logout_at = ?, updated_at = ?
		 WHERE account_id = ? AND server_id = ? AND character_id = ?`,
		string(model.PresenceOffline), endAt, time.Now(), accountID, serverID, characterID,
	); err != nil {
		return fmt.Errorf("logout character: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("logout character: %w", err)
	}
	return nil
}

func (s *Store) GetActivePlayerSession(ctx context.Context, characterID int64) (*model.PlayerSession, error) {
	sess, err := scanSession(s.db.QueryRowContext(ctx,
		`SELECT session_id, account_id, character_id, server_id, started_at, ended_at, end_reason
		 FROM player_sessions WHERE character_id = ? AND ended_at IS NULL`,
		characterID,
	))
	if err != nil {
		return nil, fmt.Errorf("character_id %d active session: %w", characterID, err)
	}
	return sess, nil
}

func (s *Store) GetPlayerSession(ctx context.Context, sessionID string) (*model.PlayerSession, error) {
	sess, err := scanSession(s.db.QueryRowContext(ctx,
		`SELECT session_id, account_id, character_id, server_id, started_at, ended_at, end_reason
		 FROM player_sessions WHERE session_id = ?`,
		sessionID,
	))
	if err != nil {
		return nil, fmt.Errorf("session %s: %w", sessionID, err)
	}
	return sess, nil
}

func (s *Store) ListPlayerSessionsByCharacter(ctx context.Context, characterID int64, limit int) ([]*model.PlayerSession, error) {
	q := `SELECT session_id, account_id, character_id, server_id, started_at, ended_at, end_reason
	      FROM player_sessions WHERE character_id = ? ORDER BY started_at DESC`
	args := []any{characterID}
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list player sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var result []*model.PlayerSession
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, fmt.Errorf("list player sessions: %w", err)
		}
		result = append(result, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list player sessions: %w", err)
	}
	return result, nil
}

func (s *Store) CountOnlineByServer(ctx context.Context, serverID string) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM character_index WHERE server_id = ? AND presence_status = ?`,
		serverID, string(model.PresenceOnline),
	).Scan(&n); err != nil {
		return 0, fmt.Errorf("count online by server: %w", err)
	}
	return n, nil
}

func (s *Store) ListOnlineByServer(ctx context.Context, serverID string, limit int, cursor string) ([]*model.Character, string, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	q := `SELECT account_id, server_id, character_id, name, level, class_id, avatar, last_login_at, created_at, updated_at, metadata, presence_status, last_logout_at
	      FROM character_index WHERE server_id = ? AND presence_status = ?`
	args := []any{serverID, string(model.PresenceOnline)}
	if cursor != "" {
		q += ` AND character_id > ?`
		args = append(args, cursor)
	}
	q += " ORDER BY character_id LIMIT ?"
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list online by server: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var result []*model.Character
	for rows.Next() {
		ch, err := scanCharacter(rows)
		if err != nil {
			return nil, "", fmt.Errorf("list online by server: %w", err)
		}
		result = append(result, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("list online by server: %w", err)
	}
	nextCursor := ""
	if len(result) > limit {
		result = result[:limit]
	}
	if len(result) > 0 {
		nextCursor = fmt.Sprintf("%d", result[len(result)-1].CharacterID)
	}
	return result, nextCursor, nil
}

func scanSession(row scannable) (*model.PlayerSession, error) {
	var sess model.PlayerSession
	var endReason sql.NullString
	err := row.Scan(
		&sess.SessionID, &sess.AccountID, &sess.CharacterID, &sess.ServerID,
		&sess.StartedAt, &sess.EndedAt, &endReason,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sess.EndReason = model.SessionEndReason(endReason.String)
	return &sess, nil
}
