package transport

import (
	"context"
	"fmt"

	generated "github.com/trip-manager-htwg/application/backend/trips/generated"
)

// ── Service ───────────────────────────────────────────────────────────────────

type Service interface {
	GetByID(ctx context.Context, id string) (*Transport, error)
	Create(ctx context.Context, req *generated.CreateTransportRequest, tripID, userID, userName, userEmail string) (*Transport, error)
	Update(ctx context.Context, req *generated.UpdateTransportRequest, transportID, userID string) (*Transport, error)
	ListByTrip(ctx context.Context, tripID string, limit, offset int) ([]*Transport, int, error)
	Delete(ctx context.Context, id, userID string) error
}

type serviceImpl struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &serviceImpl{repo: repo}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func placeFromInput(p generated.PlaceInput) Place {
	return Place{
		Name:    p.Name,
		City:    p.City,
		Country: p.Country,
		Lat:     p.Lat,
		Lng:     p.Lng,
	}
}

// ── Validation ────────────────────────────────────────────────────────────────

func validateCreate(req *generated.CreateTransportRequest) error {
	if req.From.Name == "" || req.From.Country == "" {
		return fmt.Errorf("%w: from.name and from.country are required", ErrInvalidInput)
	}
	if req.To.Name == "" || req.To.Country == "" {
		return fmt.Errorf("%w: to.name and to.country are required", ErrInvalidInput)
	}
	return nil
}

// ── Implementation ────────────────────────────────────────────────────────────

func (s *serviceImpl) GetByID(ctx context.Context, id string) (*Transport, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *serviceImpl) Create(ctx context.Context, req *generated.CreateTransportRequest, tripID, userID, userName, userEmail string) (*Transport, error) {
	if err := validateCreate(req); err != nil {
		return nil, err
	}

	notes := ""
	if req.Notes != nil {
		notes = *req.Notes
	}

	t := &Transport{
		TripID: tripID,
		CreatedBy: UserSummary{
			ID:    userID,
			Name:  userName,
			Email: userEmail,
		},
		From:          placeFromInput(req.From),
		To:            placeFromInput(req.To),
		DepartureTime: req.DepartureTime,
		ArrivalTime:   req.ArrivalTime,
		Type:          string(req.Type),
		Notes:         notes,
	}
	return s.repo.Create(ctx, t)
}

func (s *serviceImpl) Update(ctx context.Context, req *generated.UpdateTransportRequest, transportID, userID string) (*Transport, error) {
	existing, err := s.repo.GetByID(ctx, transportID)
	if err != nil {
		return nil, err
	}

	if req.From != nil {
		existing.From = placeFromInput(*req.From)
	}
	if req.To != nil {
		existing.To = placeFromInput(*req.To)
	}
	if req.DepartureTime != nil {
		existing.DepartureTime = req.DepartureTime
	}
	if req.ArrivalTime != nil {
		existing.ArrivalTime = req.ArrivalTime
	}
	if req.Type != nil {
		existing.Type = string(*req.Type)
	}
	if req.Notes != nil {
		existing.Notes = *req.Notes
	}
	existing.CreatedBy.ID = userID

	return s.repo.Update(ctx, existing)
}

func (s *serviceImpl) ListByTrip(ctx context.Context, tripID string, limit, offset int) ([]*Transport, int, error) {
	return s.repo.ListByTrip(ctx, tripID, limit, offset)
}

func (s *serviceImpl) Delete(ctx context.Context, id, userID string) error {
	return s.repo.Delete(ctx, id, userID)
}
