package domain

import (
	"context"
	"time"
)

type Keypair struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	PublicKey      string    `json:"public_key"`
	Fingerprint    string    `json:"fingerprint"`
	OrganizationID string    `json:"organization_id"`
	CreatedAt      time.Time `json:"created_at"`
}

type KeypairRepository interface {
	Create(ctx context.Context, k *Keypair) error
	GetByID(ctx context.Context, id string) (*Keypair, error)
	GetByName(ctx context.Context, name string, orgID string) (*Keypair, error)
	List(ctx context.Context, orgID string) ([]*Keypair, error)
	Delete(ctx context.Context, id string) error
}

type CreateKeypairRequest struct {
	Name           string `json:"name"`
	PublicKey      string `json:"public_key"`
	OrganizationID string `json:"organization_id"`
}
