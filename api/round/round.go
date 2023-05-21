package round

import (
	"strconv"
	"time"

	"github.com/alarmfox/game-repository/model"
)

type Round struct {
	ID          int64     `json:"id"`
	Order       int       `json:"order"`
	TestClassId string    `json:"testClassId"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	StartedAt   time.Time `json:"startedAt"`
	ClosedAt    time.Time `json:"closedAt"`
}

type CreateRequest struct {
	GameId      int64  		`json:"gameId"`
	TestClassId string 		`json:"testClassId"`
	StartedAt   time.Time   `json:"startedAt"`
	ClosedAt    time.Time   `json:"closedAt"`
}

func (CreateRequest) Validate() error {
	return nil
}


type UpdateRequest struct {
	StartedAt  time.Time `json:"startedAt"`
	ClosedAt   time.Time `json:"closedAt"`
}

func (UpdateRequest) Validate() error {
	return nil
}

type Key int64

func (c Key) Parse(s string) (Key, error) {
	a, err := strconv.ParseInt(s, 10, 64)
	return Key(a), err
}

func (k Key) AsInt64() int64 {
	return int64(k)
}

func fromModel(r *model.Round) Round {
	return Round{
		ID:          r.ID,
		Order:       r.Order,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
		TestClassId: r.TestClassId,
		StartedAt:	 r.StartedAt,
		ClosedAt: 	 r.ClosedAt,
	}
}
