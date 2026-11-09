// Package s3 guarda los archivos en un almacenamiento S3-compatible (SeaweedFS o R2).
package s3

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// region evita que el cliente la consulte por red al firmar.
const region = "us-east-1"

// Config son los datos de conexión.
type Config struct {
	Endpoint       string // host:puerto que usa la API
	PublicEndpoint string // host:puerto que ve el navegador en las URL firmadas
	AccessKey      string
	SecretKey      string
	Bucket         string
	TLS            bool
}

// Storage implementa app.Storage.
type Storage struct {
	client *minio.Client
	public *minio.Client
	bucket string
}

// New construye el cliente; no abre conexiones.
func New(c Config) (*Storage, error) {
	build := func(endpoint string) (*minio.Client, error) {
		return minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(c.AccessKey, c.SecretKey, ""), Secure: c.TLS, Region: region})
	}
	client, err := build(c.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("s3: cliente: %w", err)
	}
	public, err := build(c.PublicEndpoint)
	if err != nil {
		return nil, fmt.Errorf("s3: cliente público: %w", err)
	}
	return &Storage{client: client, public: public, bucket: c.Bucket}, nil
}

// EnsureBucket crea el bucket si no existe.
func (s *Storage) EnsureBucket(ctx context.Context) error {
	ok, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("s3: consultando bucket: %w", err)
	}
	if ok {
		return nil
	}
	if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: region}); err != nil {
		return fmt.Errorf("s3: creando bucket: %w", err)
	}
	return nil
}

// Put implementa app.Storage.
func (s *Storage) Put(ctx context.Context, key string, content []byte, mime string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(content), int64(len(content)), minio.PutObjectOptions{ContentType: mime})
	if err != nil {
		return fmt.Errorf("s3: subiendo: %w", err)
	}
	return nil
}

// Open implementa app.Storage.
func (s *Storage) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("s3: abriendo: %w", err)
	}
	return obj, nil
}

// Remove implementa app.Storage.
func (s *Storage) Remove(ctx context.Context, key string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("s3: retirando: %w", err)
	}
	return nil
}

// PresignGet implementa app.Storage: fuerza descarga y tipo para que el archivo nunca se ejecute
// como contenido del navegador.
func (s *Storage) PresignGet(ctx context.Context, key, fileName, mime string, ttl time.Duration) (string, error) {
	params := url.Values{}
	params.Set("response-content-disposition", fmt.Sprintf("attachment; filename=%q", fileName))
	params.Set("response-content-type", mime)
	u, err := s.public.PresignedGetObject(ctx, s.bucket, key, ttl, params)
	if err != nil {
		return "", fmt.Errorf("s3: firmando: %w", err)
	}
	return u.String(), nil
}
