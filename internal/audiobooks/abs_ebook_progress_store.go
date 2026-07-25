package audiobooks

import (
	"context"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Silo-Server/silo-server/internal/audiobooks/abs"
)

// ABSEbookProgressStore bridges ABS ebook state to the canonical
// ebook_reader_progress table used by Silo's native reader and recommendations.
type ABSEbookProgressStore struct{ Pool *pgxpool.Pool }

var _ abs.EbookProgressStore = (*ABSEbookProgressStore)(nil)

func (s *ABSEbookProgressStore) GetEbookProgress(ctx context.Context, userID, profileID, contentID string) (*abs.EbookProgress, error) {
	uid, err := strconv.Atoi(userID)
	if err != nil {
		return nil, fmt.Errorf("invalid user id: %w", err)
	}
	p := abs.EbookProgress{UserID: userID, ProfileID: profileID, ContentID: contentID}
	err = s.Pool.QueryRow(ctx, `SELECT file_id, location, progress FROM ebook_reader_progress WHERE user_id = $1 AND profile_id = $2 AND content_id = $3`, uid, profileID, contentID).Scan(&p.FileID, &p.Location, &p.Progress)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get ebook progress: %w", err)
	}
	return &p, nil
}

func (s *ABSEbookProgressStore) UpsertEbookProgress(ctx context.Context, p abs.EbookProgress) error {
	uid, err := strconv.Atoi(p.UserID)
	if err != nil {
		return fmt.Errorf("invalid user id: %w", err)
	}
	_, err = s.Pool.Exec(ctx, `INSERT INTO ebook_reader_progress (user_id, profile_id, content_id, file_id, location, progress, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())
		ON CONFLICT (user_id, profile_id, content_id) DO UPDATE SET
			file_id = EXCLUDED.file_id, location = EXCLUDED.location, progress = EXCLUDED.progress, updated_at = now()`, uid, p.ProfileID, p.ContentID, p.FileID, p.Location, p.Progress)
	if err != nil {
		return fmt.Errorf("upsert ebook progress: %w", err)
	}
	return nil
}

func (s *ABSEbookProgressStore) DeleteEbookProgress(ctx context.Context, userID, profileID, contentID string) error {
	uid, err := strconv.Atoi(userID)
	if err != nil {
		return fmt.Errorf("invalid user id: %w", err)
	}
	_, err = s.Pool.Exec(ctx, `DELETE FROM ebook_reader_progress WHERE user_id = $1 AND profile_id = $2 AND content_id = $3`, uid, profileID, contentID)
	if err != nil {
		return fmt.Errorf("delete ebook progress: %w", err)
	}
	return nil
}
