package storage

import "context"

// FederationStore provides federation peer management.
type FederationStore interface {
	AddFederationPeer(ctx context.Context, peer *FederationPeer) error
	GetFederationPeer(ctx context.Context, name string) (*FederationPeer, error)
	ListFederationPeers(ctx context.Context) ([]*FederationPeer, error)
	RemoveFederationPeer(ctx context.Context, name string) error
}
