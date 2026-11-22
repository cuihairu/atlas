package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/cuihairu/atlas/internal/model"
	"github.com/cuihairu/atlas/internal/store"
)

// PlayerSessionStore implementation (docs/presence.md §3). The state machine
// is the single source of truth shared with the memory store (pinned by the
// storetest contract): Login opens a session AND flips the character online
// in one transaction; Logout closes it AND flips it offline in one. The
// character row is locked FOR UPDATE so two concurrent reports for the same
// character serialize instead of double-booking a session.

func (s *Store) Login(ctx context.Context, accountID int64, serverID string, characterID int64, sessionID string, at time.Time) (*model.PlayerSession, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("%w: session_id is required", store.ErrInvalid)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("login character: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	var status string
	err = tx.QueryRow(ctx,
		`SELECT presence_status FROM character_index
		 WHERE account_id = $1 AND server_id = $2 AND character_id = $3 FOR UPDATE`,
		accountID, serverID, characterID,
	).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("character (account=%d, server=%s, char=%d): %w", accountID, serverID, characterID, store.ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("login character: %w", err)
	}

	if model.PresenceStatus(status) == model.PresenceOnline {
		// Re-reporting the open session is an idempotent no-op; logging in
		// while online in a different session is a conflict (close first).
		var existing model.PlayerSession
		var endedAt *time.Time
		var endReason string
		err = tx.QueryRow(ctx,
			`SELECT session_id, account_id, character_id, server_id, started_at, ended_at, end_reason
			 FROM player_sessions WHERE character_id = $1 AND ended_at IS NULL`,
			characterID,
		).Scan(&existing.SessionID, &existing.AccountID, &existing.CharacterID,
			&existing.ServerID, &existing.StartedAt, &endedAt, &endReason)
		if err == nil && existing.SessionID == sessionID {
			existing.EndedAt = endedAt
			existing.EndReason = model.SessionEndReason(endReason)
			if err := tx.Commit(ctx); err != nil {
				return nil, fmt.Errorf("login character: %w", err)
			}
			return &existing, nil
		}
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("login character: %w", err)
		}
		if err == nil {
			return nil, fmt.Errorf("character (account=%d, server=%s, char=%d): already online in a different session: %w", accountID, serverID, characterID, store.ErrConflict)
		}
		// Online but no open session (invariant break): fall through and heal.
	}

	// Not online (or healing a stray open session): close any open session as
	// crash, then open the new one and flip presence in the same transaction.
	if _, err := tx.Exec(ctx,
		`UPDATE player_sessions SET ended_at = $1, end_reason = $2
		 WHERE character_id = $3 AND ended_at IS NULL`,
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
	if _, err := tx.Exec(ctx,
		`INSERT INTO player_sessions (session_id, account_id, character_id, server_id, started_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		sess.SessionID, sess.AccountID, sess.CharacterID, sess.ServerID, sess.StartedAt,
	); err != nil {
		return nil, fmt.Errorf("login character: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE character_index SET presence_status = $1, last_login_at = $2, updated_at = $3
		 WHERE account_id = $4 AND server_id = $5 AND character_id = $6`,
		string(model.PresenceOnline), at, time.Now(), accountID, serverID, characterID,
	); err != nil {
		return nil, fmt.Errorf("login character: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("login character: %w", err)
	}
	return sess, nil
}

func (s *Store) Logout(ctx context.Context, accountID int64, serverID string, characterID int64, sessionID string, reason model.SessionEndReason, endAt time.Time) error {
	if !reason.Valid() {
		return fmt.Errorf("%w: invalid end_reason %q", store.ErrInvalid, reason)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("logout character: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit

	var status string
	err = tx.QueryRow(ctx,
		`SELECT presence_status FROM character_index
		 WHERE account_id = $1 AND server_id = $2 AND character_id = $3 FOR UPDATE`,
		accountID, serverID, characterID,
	).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("character (account=%d, server=%s, char=%d): %w", accountID, serverID, characterID, store.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("logout character: %w", err)
	}

	cur := model.PresenceStatus(status)
	if cur != model.PresenceOnline {
		if cur == model.PresenceOffline {
			// Idempotent duplicate logout.
			if err := tx.Commit(ctx); err != nil {
				return fmt.Errorf("logout character: %w", err)
			}
			return nil
		}
		return fmt.Errorf("character (account=%d, server=%s, char=%d): logout with no open session: %w", accountID, serverID, characterID, store.ErrInvalid)
	}

	var openID string
	err = tx.QueryRow(ctx,
		`SELECT session_id FROM player_sessions WHERE character_id = $1 AND ended_at IS NULL`,
		characterID,
	).Scan(&openID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("logout character: %w", err)
	}
	if err == nil && sessionID != "" && sessionID != openID {
		return fmt.Errorf("character (account=%d, server=%s, char=%d): session %s is not the open session: %w", accountID, serverID, characterID, sessionID, store.ErrConflict)
	}
	if err == nil {
		if _, err := tx.Exec(ctx,
			`UPDATE player_sessions SET ended_at = $1, end_reason = $2
			 WHERE session_id = $3`,
			endAt, string(reason), openID,
		); err != nil {
			return fmt.Errorf("logout character: %w", err)
		}
	}

	// err == pgx.ErrNoRows here is an invariant break (online but no session):
	// record the logout anyway rather than leaving the character stuck online.
	if _, err := tx.Exec(ctx,
		`UPDATE character_index SET presence_status = $1, last_logout_at = $2, updated_at = $3
		 WHERE account_id = $4 AND server_id = $5 AND character_id = $6`,
		string(model.PresenceOffline), endAt, time.Now(), accountID, serverID, characterID,
	); err != nil {
		return fmt.Errorf("logout character: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("logout character: %w", err)
	}
	return nil
}

func (s *Store) GetActivePlayerSession(ctx context.Context, characterID int64) (*model.PlayerSession, error) {
	sess, err := scanSession(s.pool.QueryRow(ctx,
		`SELECT session_id, account_id, character_id, server_id, started_at, ended_at, end_reason
		 FROM player_sessions WHERE character_id = $1 AND ended_at IS NULL`,
		characterID,
	))
	if err != nil {
		return nil, fmt.Errorf("character_id %d active session: %w", characterID, err)
	}
	return sess, nil
}

func (s *Store) GetPlayerSession(ctx context.Context, sessionID string) (*model.PlayerSession, error) {
	sess, err := scanSession(s.pool.QueryRow(ctx,
		`SELECT session_id, account_id, character_id, server_id, started_at, ended_at, end_reason
		 FROM player_sessions WHERE session_id = $1`,
		sessionID,
	))
	if err != nil {
		return nil, fmt.Errorf("session %s: %w", sessionID, err)
	}
	return sess, nil
}

func (s *Store) ListPlayerSessionsByCharacter(ctx context.Context, characterID int64, limit int) ([]*model.PlayerSession, error) {
	q := `SELECT session_id, account_id, character_id, server_id, started_at, ended_at, end_reason
	      FROM player_sessions WHERE character_id = $1 ORDER BY started_at DESC`
	args := []any{characterID}
	if limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", limit)
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list player sessions: %w", err)
	}
	defer rows.Close() //nolint:errcheck // read errors surface via rows.Err() after the loop
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
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM character_index WHERE server_id = $1 AND presence_status = $2`,
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
	      FROM character_index WHERE server_id = $1 AND presence_status = $2`
	args := []any{serverID, string(model.PresenceOnline)}
	if cursor != "" {
		q += ` AND character_id > $3`
		args = append(args, cursor)
	}
	q += fmt.Sprintf(" ORDER BY character_id LIMIT %d", limit+1)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list online by server: %w", err)
	}
	defer rows.Close()
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

// scanSession scans one player_sessions row.
func scanSession(row interface{ Scan(...any) error }) (*model.PlayerSession, error) {
	var sess model.PlayerSession
	var endReason string
	err := row.Scan(
		&sess.SessionID, &sess.AccountID, &sess.CharacterID, &sess.ServerID,
		&sess.StartedAt, &sess.EndedAt, &endReason,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sess.EndReason = model.SessionEndReason(endReason)
	return &sess, nil
}
