package scenario

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

var ErrConflict = errors.New("scenario revision conflict")

type Store struct{ DB *pgxpool.Pool }

func (s Store) Create(ctx context.Context, v State) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.DB.Exec(ctx, `INSERT INTO ai_scenarios(id,owner_id,revision,state) VALUES($1,$2,$3,$4)`, v.ID, v.OwnerID, v.Revision, data)
	return err
}
func (s Store) Get(ctx context.Context, owner, id string) (State, error) {
	var data []byte
	err := s.DB.QueryRow(ctx, `SELECT state FROM ai_scenarios WHERE id=$1 AND owner_id=$2`, id, owner).Scan(&data)
	var v State
	if err == nil {
		err = json.Unmarshal(data, &v)
		v.OwnerID = owner
	}
	return v, err
}
func (s Store) Save(ctx context.Context, v State) (State, error) {
	previous := v.Revision
	v.Revision++
	v.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(v)
	if err != nil {
		return v, err
	}
	tag, err := s.DB.Exec(ctx, `UPDATE ai_scenarios SET state=$4,revision=$3,updated_at=now() WHERE id=$1 AND owner_id=$2 AND revision=$5`, v.ID, v.OwnerID, v.Revision, data, previous)
	if err == nil && tag.RowsAffected() != 1 {
		err = ErrConflict
	}
	return v, err
}
