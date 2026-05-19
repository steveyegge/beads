package fs

import (
	"github.com/steveyegge/beads/internal/storage/domain"
	domainfs "github.com/steveyegge/beads/internal/storage/domain/fs"
)

type FileSystemProvider interface {
	BeadsDirFSUseCase() domain.BeadsDirFSUseCase
}

func NewFileSystemProvider(adapters domain.BeadsDirFSAdapters) FileSystemProvider {
	return &fileSystemProviderImpl{adapters: adapters}
}

type fileSystemProviderImpl struct {
	adapters          domain.BeadsDirFSAdapters
	beadsDirFSUseCase domain.BeadsDirFSUseCase
}

var _ FileSystemProvider = (*fileSystemProviderImpl)(nil)

func (p *fileSystemProviderImpl) BeadsDirFSUseCase() domain.BeadsDirFSUseCase {
	if p.beadsDirFSUseCase == nil {
		p.beadsDirFSUseCase = domain.NewBeadsDirFSUseCase(
			domainfs.NewBeadsDirFSRepository(),
			p.adapters,
		)
	}
	return p.beadsDirFSUseCase
}
