package main

import (
	"context"
	"errors"

	realtimedomain "github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/domain"
	realtimeusecase "github.com/ARAFATH-SH/researchsphere/backend/internal/realtime/usecase"
)

// okRealtime is a realtime service for the router tests: nobody is online, and no socket opens.
type okRealtime struct{}

func (okRealtime) Reserve(context.Context, string) (*realtimeusecase.Reservation, error) {
	return nil, errors.New("no sockets in this test")
}

func (okRealtime) Suspension(context.Context, string) (*realtimedomain.Suspension, error) {
	return nil, nil //nolint:nilnil // no suspension
}

func (okRealtime) Online(context.Context, string, []string) ([]realtimedomain.Status, error) {
	return nil, nil
}
