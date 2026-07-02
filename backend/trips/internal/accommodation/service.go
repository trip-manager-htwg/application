package accommodation

import (
	"context"
	"fmt"

	generated "github.com/trip-manager-htwg/application/backend/trips/generated"
)

// ── Service ───────────────────────────────────────────────────────────────────

type Service interface {
	GetByID(ctx context.Context, id string) (*Accommodation, error)
	Create(ctx context.Context, req *generated.CreateAccommodationRequest, tripID, userID, userName, userEmail string) (*Accommodation, error)
	Update(ctx context.Context, req *generated.UpdateAccommodationRequest, accommodationID, userID string) (*Accommodation, error)
	ListByTrip(ctx context.Context, tripID string, limit, offset int) ([]*Accommodation, int, error)
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

func validateCreate(req *generated.CreateAccommodationRequest) error {
	if req.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidInput)
	}
	if req.Location.Name == "" || req.Location.City == "" || req.Location.Country == "" {
		return fmt.Errorf("%w: location.name, location.city and location.country are required", ErrInvalidInput)
	}
	return nil
}

func validateUpdate(req *generated.UpdateAccommodationRequest) error {
	if req.Name != nil && *req.Name == "" {
		return fmt.Errorf("%w: name cannot be empty", ErrInvalidInput)
	}
	if req.Location != nil {
		if req.Location.Name == "" || req.Location.City == "" || req.Location.Country == "" {
			return fmt.Errorf("%w: location.name, location.city and location.country cannot be empty", ErrInvalidInput)
		}
	}
	return nil
}

// ── Implementation ────────────────────────────────────────────────────────────

func (s *serviceImpl) GetByID(ctx context.Context, id string) (*Accommodation, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *serviceImpl) Create(ctx context.Context, req *generated.CreateAccommodationRequest, tripID, userID, userName, userEmail string) (*Accommodation, error) {
	if err := validateCreate(req); err != nil {
		return nil, err
	}

	address := ""
	if req.Address != nil {
		address = *req.Address
	}
	notes := ""
	if req.Notes != nil {
		notes = *req.Notes
	}

	a := &Accommodation{
		TripID: tripID,
		CreatedBy: UserSummary{
			ID:    userID,
			Name:  userName,
			Email: userEmail,
		},
		Location:      placeFromInput(req.Location),
		Name:          req.Name,
		Address:       address,
		CheckIn:       req.CheckIn,
		CheckOut:      req.CheckOut,
		PricePerNight: req.PricePerNight,
		Notes:         notes,
	}
	created, err := s.repo.Create(ctx, a)
	if err != nil {
		return nil, fmt.Errorf("failed to create accommodation: %w", err)
	}
	return created, nil
}

func (s *serviceImpl) Update(ctx context.Context, req *generated.UpdateAccommodationRequest, accommodationID, userID string) (*Accommodation, error) {
	if err := validateUpdate(req); err != nil {
		return nil, err
	}

	existing, err := s.repo.GetByID(ctx, accommodationID)
	if err != nil {
		return nil, fmt.Errorf("failed to get accommodation: %w", err)
	}
	if existing.CreatedBy.ID != userID {
		return nil, ErrUnauthorized
	}

	if req.Location != nil {
		existing.Location = placeFromInput(*req.Location)
	}
	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Address != nil {
		existing.Address = *req.Address
	}
	if req.CheckIn != nil {
		existing.CheckIn = req.CheckIn
	}
	if req.CheckOut != nil {
		existing.CheckOut = req.CheckOut
	}
	if req.PricePerNight != nil {
		existing.PricePerNight = req.PricePerNight
	}
	if req.Notes != nil {
		existing.Notes = *req.Notes
	}

	result, err := s.repo.Update(ctx, existing)
	if err != nil {
		return nil, fmt.Errorf("failed to update accommodation: %w", err)
	}
	return result, nil
}

func (s *serviceImpl) ListByTrip(ctx context.Context, tripID string, limit, offset int) ([]*Accommodation, int, error) {
	accommodations, total, err := s.repo.ListByTrip(ctx, tripID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list accommodations: %w", err)
	}
	return accommodations, total, nil
}

func (s *serviceImpl) Delete(ctx context.Context, id, userID string) error {
	return s.repo.Delete(ctx, id, userID)
}
