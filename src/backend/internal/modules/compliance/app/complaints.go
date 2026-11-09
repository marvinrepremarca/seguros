package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/compliance/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
)

// CreateComplaint registra una PQR.
func (s *Service) CreateComplaint(ctx context.Context, d domain.ComplaintData) (domain.ComplaintData, error) {
	if err := authz.Require(ctx, authz.ComplaintManage); err != nil {
		return domain.ComplaintData{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return domain.ComplaintData{}, fmt.Errorf("compliance: %w", err)
	}
	d.ID = id
	c, err := domain.NewComplaint(d, kernel.Today(s.Clock))
	if err != nil {
		return domain.ComplaintData{}, err
	}
	snap := c.Data()
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := uow.Records().InsertComplaint(ctx, snap, s.Clock.Now().UTC()); err != nil {
			return err
		}
		return uow.Audit(ctx, entry("compliance.complaint.created", "compliance.complaint", id))
	})
	return snap, err
}

func (s *Service) changeComplaint(ctx context.Context, id uuid.UUID, action string, fn func(*domain.Complaint) error) (domain.ComplaintData, error) {
	if err := authz.Require(ctx, authz.ComplaintManage); err != nil {
		return domain.ComplaintData{}, err
	}
	var snap domain.ComplaintData
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		c, err := uow.Records().GetComplaint(ctx, id)
		if err != nil {
			return err
		}
		if err := fn(c); err != nil {
			return err
		}
		if err := uow.Records().SaveComplaint(ctx, c, s.Clock.Now().UTC()); err != nil {
			return err
		}
		snap = c.Data()
		return uow.Audit(ctx, entry(action, "compliance.complaint", id))
	})
	return snap, err
}

// StartComplaint pasa la queja a gestión.
func (s *Service) StartComplaint(ctx context.Context, id uuid.UUID) (domain.ComplaintData, error) {
	return s.changeComplaint(ctx, id, "compliance.complaint.started", func(c *domain.Complaint) error { return c.Start() })
}

// ResolveComplaint cierra la queja.
func (s *Service) ResolveComplaint(ctx context.Context, id uuid.UUID, resolution string) (domain.ComplaintData, error) {
	return s.changeComplaint(ctx, id, "compliance.complaint.resolved", func(c *domain.Complaint) error { return c.Resolve(resolution) })
}

// ListComplaints pagina las quejas.
func (s *Service) ListComplaints(ctx context.Context, cursor string, limit int32) ([]domain.ComplaintData, string, error) {
	if err := authz.Require(ctx, authz.ComplaintManage); err != nil {
		return nil, "", err
	}
	before, err := kernel.DecodeCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	size := kernel.PageSize(limit)
	var rows []domain.ComplaintData
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		rows, err = uow.Records().Complaints(ctx, before, size+1)
		return err
	})
	if err != nil {
		return nil, "", err
	}
	next := ""
	if int32(len(rows)) > size { //nolint:gosec // acotado por el límite
		rows = rows[:size]
		next = kernel.EncodeCursor(rows[size-1].ID)
	}
	return rows, next, nil
}
