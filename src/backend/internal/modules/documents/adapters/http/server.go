// Package documentshttp expone documentos. Solo mapea HTTP ↔ casos de uso.
package documentshttp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/documents/app"
	"github.com/marvinrepremarca/seguros/internal/modules/documents/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/httpx"
)

// Server implementa StrictServerInterface.
type Server struct {
	svc    *app.Service
	policy *authz.Policy
	logger *slog.Logger
}

var _ StrictServerInterface = (*Server)(nil)

// NewServer construye el servidor.
func NewServer(svc *app.Service, policy *authz.Policy, logger *slog.Logger) *Server {
	return &Server{svc: svc, policy: policy, logger: logger}
}

// Mount registra las rutas con el contrato.
func (s *Server) Mount(r chi.Router) error {
	spec, err := GetSpec()
	if err != nil {
		return err
	}
	guard, err := authz.NewOperationGuard(spec, s.policy)
	if err != nil {
		return err
	}
	contract, err := httpx.Contract(spec, guard.Check, s.logger)
	if err != nil {
		return err
	}
	strictGuard := func(f StrictHandlerFunc, operationID string) StrictHandlerFunc {
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
			if err := guard.Check(ctx, operationID); err != nil {
				return nil, err
			}
			return f(ctx, w, r, req)
		}
	}
	h := NewStrictHandlerWithOptions(s, []StrictMiddlewareFunc{strictGuard}, StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
			httpx.WriteProblem(w, r, http.StatusBadRequest, httpx.CodeBadRequest, "La solicitud no es válida")
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) { httpx.WriteError(w, r, s.logger, err) },
	})
	r.Group(func(g chi.Router) {
		g.Use(contract)
		HandlerWithOptions(h, ChiServerOptions{BaseRouter: g, ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, _ error) {
			httpx.WriteProblem(w, r, http.StatusBadRequest, httpx.CodeBadRequest, "La solicitud no es válida")
		}})
	})
	return nil
}

// envelopeBytes es lo que pueden ocupar los campos de texto del formulario.
const envelopeBytes = 4 << 10

// formFields son las partes que admite el formulario; cualquier otra se rechaza. El validador del
// contrato no revisa este cuerpo (x-stream-body), así que lo hace esta función.
var formFields = map[string]bool{"file": true, "ownerType": true, "ownerId": true, "title": true, "sensitive": true} //nolint:gochecknoglobals // tabla inmutable

const maxParts = 8

func readForm(r *multipart.Reader, maxBytes int64) (fields map[string]string, file []byte, err error) {
	fields = map[string]string{}
	for parts := 0; ; parts++ {
		part, err := r.NextPart()
		if errors.Is(err, io.EOF) {
			if file == nil {
				return nil, nil, domain.ErrInvalid
			}
			return fields, file, nil
		}
		if err != nil {
			return nil, nil, formError(err)
		}
		name := part.FormName()
		if parts >= maxParts || !formFields[name] {
			return nil, nil, domain.ErrInvalid
		}
		limit := int64(envelopeBytes)
		if name == "file" {
			limit = maxBytes + 1 // un byte más para que el dominio detecte el exceso
		}
		content, err := io.ReadAll(io.LimitReader(part, limit))
		if err != nil {
			return nil, nil, formError(err)
		}
		if name == "file" {
			file = content
		} else {
			fields[name] = string(content)
		}
	}
}

// formError conserva el exceso de tamaño (413) y trata lo demás como formulario inválido.
func formError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return err
	}
	return domain.ErrInvalid
}

func (s *Server) toDocument(d domain.Document) Document {
	return Document{Id: d.ID, OwnerType: OwnerType(d.OwnerType), OwnerId: d.OwnerID, Title: d.Title, Sensitive: d.Sensitive, CurrentNumber: d.CurrentNumber, CreatedOn: d.CreatedOn, RetentionUntil: d.RetentionUntil(s.svc.RetentionDays), Version: d.Version}
}

func toVersion(v domain.Version) Version {
	return Version{Id: v.ID, Number: v.Number, MimeType: v.MimeType, SizeBytes: v.SizeBytes, ScanStatus: ScanStatus(v.Scan), CreatedAt: v.CreatedAt}
}

func cursorOf(c *string) (uuid.NullUUID, error) {
	if c == nil || *c == "" {
		return uuid.NullUUID{}, nil
	}
	id, err := uuid.Parse(*c)
	if err != nil {
		return uuid.NullUUID{}, domain.ErrInvalid
	}
	return uuid.NullUUID{UUID: id, Valid: true}, nil
}

func (s *Server) toPage(p app.Page) DocumentPage {
	out := DocumentPage{Items: make([]Document, 0, len(p.Items))}
	for _, d := range p.Items {
		out.Items = append(out.Items, s.toDocument(d))
	}
	if p.Next.Valid {
		next := p.Next.UUID.String()
		out.NextCursor = &next
	}
	return out
}

func limitOf(p *int32) int32 {
	if p == nil {
		return 0
	}
	return *p
}

// ListDocuments implementa StrictServerInterface.
func (s *Server) ListDocuments(ctx context.Context, req ListDocumentsRequestObject) (ListDocumentsResponseObject, error) {
	before, err := cursorOf(req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	p, err := s.svc.List(ctx, domain.OwnerType(req.Params.OwnerType), req.Params.OwnerId, before, limitOf(req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return ListDocuments200JSONResponse(s.toPage(p)), nil
}

// ListRetentionDue implementa StrictServerInterface.
func (s *Server) ListRetentionDue(ctx context.Context, req ListRetentionDueRequestObject) (ListRetentionDueResponseObject, error) {
	before, err := cursorOf(req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	p, err := s.svc.RetentionDue(ctx, before, limitOf(req.Params.Limit))
	if err != nil {
		return nil, err
	}
	return ListRetentionDue200JSONResponse(s.toPage(p)), nil
}

// UploadDocument implementa StrictServerInterface.
func (s *Server) UploadDocument(ctx context.Context, req UploadDocumentRequestObject) (UploadDocumentResponseObject, error) {
	fields, file, err := readForm(req.Body, s.svc.MaxBytes)
	if err != nil {
		return nil, err
	}
	owner, err := uuid.Parse(fields["ownerId"])
	if err != nil {
		return nil, domain.ErrInvalid
	}
	in := app.UploadInput{OwnerType: domain.OwnerType(fields["ownerType"]), OwnerID: owner, Title: fields["title"], Sensitive: fields["sensitive"] == "true", Content: file}
	if req.Params.IdempotencyKey != nil {
		in.Key = *req.Params.IdempotencyKey
	}
	d, err := s.svc.Upload(ctx, in)
	if err != nil {
		return nil, err
	}
	return UploadDocument201JSONResponse(s.toDocument(d)), nil
}

// GetDocument implementa StrictServerInterface.
func (s *Server) GetDocument(ctx context.Context, req GetDocumentRequestObject) (GetDocumentResponseObject, error) {
	d, versions, err := s.svc.Get(ctx, req.DocumentId)
	if err != nil {
		return nil, err
	}
	out := GetDocument200JSONResponse{Document: s.toDocument(d), Versions: make([]Version, 0, len(versions))}
	for _, v := range versions {
		out.Versions = append(out.Versions, toVersion(v))
	}
	return out, nil
}

// AddDocumentVersion implementa StrictServerInterface.
func (s *Server) AddDocumentVersion(ctx context.Context, req AddDocumentVersionRequestObject) (AddDocumentVersionResponseObject, error) {
	_, file, err := readForm(req.Body, s.svc.MaxBytes)
	if err != nil {
		return nil, err
	}
	v, err := s.svc.AddVersion(ctx, req.DocumentId, file)
	if err != nil {
		return nil, err
	}
	return AddDocumentVersion201JSONResponse(toVersion(v)), nil
}

// DownloadDocumentVersion implementa StrictServerInterface.
func (s *Server) DownloadDocumentVersion(ctx context.Context, req DownloadDocumentVersionRequestObject) (DownloadDocumentVersionResponseObject, error) {
	url, ttl, err := s.svc.Download(ctx, req.DocumentId, req.VersionId)
	if err != nil {
		return nil, err
	}
	return DownloadDocumentVersion200JSONResponse{Url: url, ExpiresInSeconds: int32(ttl.Seconds())}, nil
}

// GetDocumentContent implementa StrictServerInterface.
func (s *Server) GetDocumentContent(ctx context.Context, req GetDocumentContentRequestObject) (GetDocumentContentResponseObject, error) {
	f, err := s.svc.Content(ctx, req.DocumentId, req.VersionId)
	if err != nil {
		return nil, err
	}
	disposition := fmt.Sprintf("attachment; filename=%q", f.Name)
	return GetDocumentContent200ApplicationoctetStreamResponse{
		Body: bytes.NewReader(f.Content), ContentLength: int64(len(f.Content)),
		Headers: GetDocumentContent200ResponseHeaders{ContentDisposition: &disposition},
	}, nil
}
