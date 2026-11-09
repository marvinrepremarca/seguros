// Package app contiene los casos de uso de la gestión documental.
package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/marvinrepremarca/seguros/internal/modules/documents/domain"
	"github.com/marvinrepremarca/seguros/internal/platform/audit"
	"github.com/marvinrepremarca/seguros/internal/platform/authz"
	"github.com/marvinrepremarca/seguros/internal/platform/kernel"
	"github.com/marvinrepremarca/seguros/internal/platform/reqctx"
)

const scanBatch = 20

// Repository persiste documentos en el tenant del contexto.
type Repository interface {
	Insert(ctx context.Context, d domain.Document, key string, at time.Time) error
	InsertVersion(ctx context.Context, v domain.Version, at time.Time) error
	Bump(ctx context.Context, id uuid.UUID, number, version int32, at time.Time) error
	Get(ctx context.Context, id uuid.UUID) (domain.Document, error)
	GetByKey(ctx context.Context, key string) (domain.Document, bool, error)
	ListByOwner(ctx context.Context, t domain.OwnerType, owner uuid.UUID, sensitive bool, before uuid.NullUUID, limit int32) ([]domain.Document, error)
	ListRetentionDue(ctx context.Context, until kernel.Date, sensitive bool, owners []domain.OwnerType, before uuid.NullUUID, limit int32) ([]domain.Document, error)
	Versions(ctx context.Context, id uuid.UUID) ([]domain.Version, error)
	PendingScans(ctx context.Context, limit int32) ([]domain.Version, error)
	SetScan(ctx context.Context, id uuid.UUID, s domain.ScanStatus, at time.Time) error
}

// UnitOfWork es una transacción del tenant.
type UnitOfWork interface {
	Records() Repository
	Audit(ctx context.Context, e audit.Entry) error
}

// Store abre unidades de trabajo.
type Store interface {
	WithinTenant(ctx context.Context, fn func(ctx context.Context, uow UnitOfWork) error) error
}

// Storage guarda los archivos en un almacenamiento S3-compatible.
type Storage interface {
	Put(ctx context.Context, key string, content []byte, mime string) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Remove(ctx context.Context, key string) error
	PresignGet(ctx context.Context, key, fileName, mime string, ttl time.Duration) (string, error)
}

// Scanner revisa un archivo con el antivirus.
type Scanner interface {
	Scan(ctx context.Context, content io.Reader) (infected bool, err error)
}

// Cipher cifra datos Restringidos del tenant del contexto (lo implementa platform/crypto).
type Cipher interface {
	EncryptFor(ctx context.Context, field, record string, plaintext []byte) ([]byte, error)
	DecryptFor(ctx context.Context, field, record string, ciphertext []byte) ([]byte, error)
}

const fileField = "documents.file"

// Owners confirma que el registro dueño exista en el tenant.
type Owners interface {
	Exists(ctx context.Context, t domain.OwnerType, id uuid.UUID) (bool, error)
}

// canSeeOwner exige el permiso de lectura del registro dueño.
func canSeeOwner(ctx context.Context, t domain.OwnerType) bool {
	p := domain.OwnerPermission(t)
	return p != "" && authz.Require(ctx, authz.Permission(p)) == nil
}

// visibleOwners son los tipos de registro que el usuario puede leer.
func visibleOwners(ctx context.Context) []domain.OwnerType {
	var out []domain.OwnerType
	for _, t := range []domain.OwnerType{domain.OwnerParty, domain.OwnerPolicy, domain.OwnerClaim} {
		if canSeeOwner(ctx, t) {
			out = append(out, t)
		}
	}
	return out
}

// Service agrupa los casos de uso.
type Service struct {
	Store         Store
	Storage       Storage
	Scanner       Scanner
	Cipher        Cipher
	Owners        Owners
	Clock         kernel.Clock
	Logger        *slog.Logger
	MaxBytes      int64
	RetentionDays int32
	DownloadTTL   time.Duration
}

func entry(action, id string) audit.Entry {
	return audit.Entry{Action: action, ResourceType: "document", ResourceID: id, Outcome: audit.OutcomeSuccess}
}

func canSeeSensitive(ctx context.Context) bool {
	return authz.Require(ctx, authz.DocumentSensitive) == nil
}

func objectKey(ctx context.Context, doc, version uuid.UUID) (string, error) {
	tenant, ok := reqctx.TenantID(ctx)
	if !ok {
		return "", authz.ErrUnauthenticated
	}
	return fmt.Sprintf("tenants/%s/%s/%s", uuid.UUID(tenant), doc, version), nil
}

// UploadInput son los datos de un documento nuevo.
type UploadInput struct {
	OwnerType domain.OwnerType
	OwnerID   uuid.UUID
	Title     string
	Sensitive bool
	Key       string
	Content   []byte
}

// newVersion valida el archivo y lo sube; queda pendiente del antivirus.
func (s *Service) newVersion(ctx context.Context, doc uuid.UUID, number int32, content []byte, sensitive bool) (domain.Version, error) {
	mime, _, err := domain.DetectFile(content, s.MaxBytes)
	if err != nil {
		return domain.Version{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return domain.Version{}, fmt.Errorf("documents: id: %w", err)
	}
	key, err := objectKey(ctx, doc, id)
	if err != nil {
		return domain.Version{}, err
	}
	stored := content
	if sensitive {
		// Restringido: el almacenamiento solo ve el archivo cifrado con la llave del tenant.
		if stored, err = s.Cipher.EncryptFor(ctx, fileField, id.String(), content); err != nil {
			return domain.Version{}, fmt.Errorf("documents: cifrando archivo: %w", err)
		}
	}
	if err := s.Storage.Put(ctx, key, stored, mime); err != nil {
		var known *kernel.Error
		if errors.As(err, &known) {
			return domain.Version{}, err
		}
		return domain.Version{}, fmt.Errorf("documents: guardando archivo: %w", err)
	}
	sum := sha256.Sum256(content)
	return domain.Version{ID: id, DocumentID: doc, Number: number, StorageKey: key, MimeType: mime, SizeBytes: int64(len(content)), Sha256: sum[:], Encrypted: sensitive, Scan: domain.ScanPending, CreatedAt: s.Clock.Now()}, nil
}

// discard retira un archivo subido cuya transacción falló: aún no tiene registro que lo referencie.
func (s *Service) discard(ctx context.Context, key string) {
	if err := s.Storage.Remove(context.WithoutCancel(ctx), key); err != nil {
		s.Logger.WarnContext(ctx, "archivo huérfano sin retirar", "error", err)
	}
}

// Upload crea un documento con su primera versión.
func (s *Service) Upload(ctx context.Context, in UploadInput) (domain.Document, error) {
	if err := authz.Require(ctx, authz.DocumentUpload); err != nil {
		return domain.Document{}, err
	}
	// Cargar en un registro exige poder verlo: así no se confirma que exista ni se cuelgan archivos ajenos.
	if !canSeeOwner(ctx, in.OwnerType) {
		return domain.Document{}, domain.ErrNotFound
	}
	in.Sensitive = domain.IsSensitive(in.OwnerType, in.Sensitive)
	if in.Sensitive {
		// Quien carga un sensible no necesariamente puede leerlo después (ej. Operaciones).
		if err := authz.Require(ctx, authz.DocumentUploadSensitive); err != nil {
			return domain.Document{}, err
		}
	}
	if in.Key != "" {
		var found domain.Document
		var ok bool
		err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) (err error) {
			found, ok, err = uow.Records().GetByKey(ctx, in.Key)
			return err
		})
		if err != nil {
			return domain.Document{}, err
		}
		if ok {
			if found.Sensitive && !canSeeSensitive(ctx) {
				return domain.Document{}, domain.ErrNotFound
			}
			return found, nil
		}
	}
	id, err := uuid.NewV7()
	if err != nil {
		return domain.Document{}, fmt.Errorf("documents: id: %w", err)
	}
	d := domain.Document{ID: id, OwnerType: in.OwnerType, OwnerID: in.OwnerID, Title: in.Title, Sensitive: in.Sensitive, CurrentNumber: 1, CreatedOn: kernel.Today(s.Clock), Version: 1}
	if err := d.Validate(); err != nil {
		return domain.Document{}, err
	}
	exists, err := s.Owners.Exists(ctx, d.OwnerType, d.OwnerID)
	if err != nil {
		return domain.Document{}, fmt.Errorf("documents: consultando dueño: %w", err)
	}
	if !exists {
		return domain.Document{}, domain.ErrOwnerNotFound
	}
	v, err := s.newVersion(ctx, id, 1, in.Content, in.Sensitive)
	if err != nil {
		return domain.Document{}, err
	}
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		now := s.Clock.Now()
		if err := uow.Records().Insert(ctx, d, in.Key, now); err != nil {
			return err
		}
		if err := uow.Records().InsertVersion(ctx, v, now); err != nil {
			return err
		}
		return uow.Audit(ctx, entry("documents.document.uploaded", id.String()))
	})
	if err != nil {
		s.discard(ctx, v.StorageKey)
		return domain.Document{}, err
	}
	return d, nil
}

func (s *Service) visible(ctx context.Context, r Repository, id uuid.UUID) (domain.Document, error) {
	d, err := r.Get(ctx, id)
	if err != nil {
		return domain.Document{}, err
	}
	// Quien no puede ver documentos sensibles, o el registro al que pertenecen, no sabe que existen.
	if (d.Sensitive && !canSeeSensitive(ctx)) || !canSeeOwner(ctx, d.OwnerType) {
		return domain.Document{}, domain.ErrNotFound
	}
	return d, nil
}

// AddVersion agrega un archivo a un documento; las versiones anteriores se conservan.
func (s *Service) AddVersion(ctx context.Context, id uuid.UUID, content []byte) (domain.Version, error) {
	if err := authz.Require(ctx, authz.DocumentUpload); err != nil {
		return domain.Version{}, err
	}
	var d domain.Document
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) (err error) {
		d, err = s.visible(ctx, uow.Records(), id)
		return err
	})
	if err != nil {
		return domain.Version{}, err
	}
	v, err := s.newVersion(ctx, id, d.CurrentNumber+1, content, d.Sensitive)
	if err != nil {
		return domain.Version{}, err
	}
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		now := s.Clock.Now()
		if err := uow.Records().Bump(ctx, id, v.Number, d.Version, now); err != nil {
			return err
		}
		if err := uow.Records().InsertVersion(ctx, v, now); err != nil {
			return err
		}
		return uow.Audit(ctx, entry("documents.version.added", id.String()))
	})
	if err != nil {
		s.discard(ctx, v.StorageKey)
		return domain.Version{}, err
	}
	return v, nil
}

// Get devuelve un documento con sus versiones.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (d domain.Document, versions []domain.Version, err error) {
	if err := authz.Require(ctx, authz.DocumentRead); err != nil {
		return d, nil, err
	}
	err = s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		var err error
		if d, err = s.visible(ctx, uow.Records(), id); err != nil {
			return err
		}
		versions, err = uow.Records().Versions(ctx, id)
		return err
	})
	return d, versions, err
}

// Page es una página de documentos.
type Page struct {
	Items []domain.Document
	Next  uuid.NullUUID
}

func pageSize(limit int32) int32 {
	if limit <= 0 || limit > 200 {
		return 50
	}
	return limit
}

func paginate(rows []domain.Document, limit int32) Page {
	if int32(len(rows)) <= limit { //nolint:gosec // acotado por limit
		return Page{Items: rows}
	}
	rows = rows[:limit]
	return Page{Items: rows, Next: uuid.NullUUID{UUID: rows[limit-1].ID, Valid: true}}
}

// List devuelve los documentos de un registro.
func (s *Service) List(ctx context.Context, t domain.OwnerType, owner uuid.UUID, before uuid.NullUUID, limit int32) (Page, error) {
	if err := authz.Require(ctx, authz.DocumentRead); err != nil {
		return Page{}, err
	}
	if !canSeeOwner(ctx, t) {
		return Page{}, nil
	}
	limit = pageSize(limit)
	var rows []domain.Document
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) (err error) {
		rows, err = uow.Records().ListByOwner(ctx, t, owner, canSeeSensitive(ctx), before, limit+1)
		return err
	})
	return paginate(rows, limit), err
}

// RetentionDue lista lo que cumplió el plazo, para revisión: nada se borra de forma automática.
func (s *Service) RetentionDue(ctx context.Context, before uuid.NullUUID, limit int32) (Page, error) {
	if err := authz.Require(ctx, authz.DocumentRetention); err != nil {
		return Page{}, err
	}
	limit = pageSize(limit)
	until := kernel.Today(s.Clock).AddDays(-int(s.RetentionDays))
	var rows []domain.Document
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) (err error) {
		rows, err = uow.Records().ListRetentionDue(ctx, until, canSeeSensitive(ctx), visibleOwners(ctx), before, limit+1)
		return err
	})
	return paginate(rows, limit), err
}

// Download entrega una URL de corta vida de un archivo limpio y deja la lectura en la bitácora.
func (s *Service) Download(ctx context.Context, id, version uuid.UUID) (string, time.Duration, error) {
	if err := authz.Require(ctx, authz.DocumentRead); err != nil {
		return "", 0, err
	}
	var url string
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if _, err := s.visible(ctx, uow.Records(), id); err != nil {
			return err
		}
		versions, err := uow.Records().Versions(ctx, id)
		if err != nil {
			return err
		}
		for _, v := range versions {
			if v.ID != version {
				continue
			}
			if err := v.Downloadable(); err != nil {
				return err
			}
			if v.Encrypted {
				// Un archivo cifrado no admite URL firmada: se sirve por la API, que audita al entregar.
				url = fmt.Sprintf("/api/v1/documents/documents/%s/versions/%s/content", id, version)
				return nil
			}
			name := fileName(id, v)
			if url, err = s.Storage.PresignGet(ctx, v.StorageKey, name, v.MimeType, s.DownloadTTL); err != nil {
				return fmt.Errorf("documents: firmando descarga: %w", err)
			}
			return uow.Audit(ctx, entry("documents.version.downloaded", id.String()))
		}
		return domain.ErrNotFound
	})
	return url, s.DownloadTTL, err
}

func fileName(id uuid.UUID, v domain.Version) string {
	return fmt.Sprintf("documento-%s-v%d.%s", id, v.Number, domain.Extension(v.MimeType))
}

// File es un archivo descifrado listo para entregar.
type File struct {
	Name    string
	Content []byte
}

// Content descifra y entrega una versión cifrada, y deja la lectura en la bitácora.
func (s *Service) Content(ctx context.Context, id, version uuid.UUID) (File, error) {
	if err := authz.Require(ctx, authz.DocumentRead); err != nil {
		return File{}, err
	}
	var found domain.Version
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if _, err := s.visible(ctx, uow.Records(), id); err != nil {
			return err
		}
		versions, err := uow.Records().Versions(ctx, id)
		if err != nil {
			return err
		}
		for _, v := range versions {
			if v.ID == version && v.Encrypted {
				if err := v.Downloadable(); err != nil {
					return err
				}
				found = v
				return uow.Audit(ctx, entry("documents.version.downloaded", id.String()))
			}
		}
		return domain.ErrNotFound
	})
	if err != nil {
		return File{}, err
	}
	plain, err := s.readPlain(ctx, found)
	if err != nil {
		return File{}, err
	}
	return File{Name: fileName(id, found), Content: plain}, nil
}

func (s *Service) readPlain(ctx context.Context, v domain.Version) ([]byte, error) {
	obj, err := s.Storage.Open(ctx, v.StorageKey)
	if err != nil {
		return nil, fmt.Errorf("documents: abriendo archivo: %w", err)
	}
	raw, err := io.ReadAll(obj)
	if err = errors.Join(err, obj.Close()); err != nil {
		return nil, fmt.Errorf("documents: leyendo archivo: %w", err)
	}
	if !v.Encrypted {
		return raw, nil
	}
	plain, err := s.Cipher.DecryptFor(ctx, fileField, v.ID.String(), raw)
	if err != nil {
		return nil, fmt.Errorf("documents: descifrando archivo: %w", err)
	}
	return plain, nil
}

// ScanPending revisa los archivos pendientes del tenant. Si el antivirus falla, el archivo sigue
// pendiente y bloqueado hasta un reintento.
func (s *Service) ScanPending(ctx context.Context) error {
	var pending []domain.Version
	err := s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) (err error) {
		pending, err = uow.Records().PendingScans(ctx, scanBatch)
		return err
	})
	if err != nil {
		return err
	}
	var errs []error
	for _, v := range pending {
		errs = append(errs, s.scanOne(ctx, v))
	}
	return errors.Join(errs...)
}

func (s *Service) scanOne(ctx context.Context, v domain.Version) error {
	plain, err := s.readPlain(ctx, v)
	if err != nil {
		return err
	}
	infected, err := s.Scanner.Scan(ctx, bytes.NewReader(plain))
	if err != nil {
		return fmt.Errorf("documents: antivirus: %w", err)
	}
	status, err := v.ScanResult(infected)
	if err != nil {
		return err
	}
	action := "documents.scan.clean"
	if status == domain.ScanInfected {
		action = "documents.scan.infected"
	}
	return s.Store.WithinTenant(ctx, func(ctx context.Context, uow UnitOfWork) error {
		if err := uow.Records().SetScan(ctx, v.ID, status, s.Clock.Now()); err != nil {
			return err
		}
		return uow.Audit(ctx, entry(action, v.DocumentID.String()))
	})
}
