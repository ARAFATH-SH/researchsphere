package main

import (
	"context"

	moderationdomain "github.com/ARAFATH-SH/researchsphere/backend/internal/moderation/domain"
)

// okModeration is a moderation service for which every operation succeeds.
type okModeration struct{}

func (okModeration) Warn(context.Context, string, string, moderationdomain.Input) (moderationdomain.Action, error) {
	return moderationdomain.Action{ID: okUserID, UserID: okUserID}, nil
}

func (okModeration) Restrict(context.Context, string, string, moderationdomain.RestrictInput) (moderationdomain.Action, error) {
	return moderationdomain.Action{ID: okUserID, UserID: okUserID}, nil
}

func (okModeration) Lift(context.Context, string, string, moderationdomain.ReverseInput) error {
	return nil
}

func (okModeration) Suspend(context.Context, string, string, moderationdomain.SuspendInput) (moderationdomain.Action, error) {
	return moderationdomain.Action{ID: okUserID, UserID: okUserID}, nil
}

func (okModeration) Restore(context.Context, string, string, moderationdomain.ReverseInput) (moderationdomain.Action, error) {
	return moderationdomain.Action{ID: okUserID, UserID: okUserID}, nil
}

func (okModeration) History(context.Context, string, int) (moderationdomain.History, error) {
	return moderationdomain.History{User: moderationdomain.Account{ID: okUserID}}, nil
}

func (okModeration) PendingWarnings(context.Context, string) ([]moderationdomain.Warning, error) {
	return nil, nil
}

func (okModeration) Warning(context.Context, string, string) (moderationdomain.Warning, error) {
	return moderationdomain.Warning{ID: okUserID}, nil
}

func (okModeration) Acknowledge(context.Context, string, string) error { return nil }
