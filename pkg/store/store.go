package store

import (
	"github.com/replicatedhq/kots/pkg/store/kotsstore"
)

var (
	hasStore    = false
	globalStore Store
)

var _ Store = (*kotsstore.KOTSStore)(nil)

func GetStore() Store {
	if !hasStore {
		globalStore = storeFromEnv()
	}

	return globalStore
}

func storeFromEnv() Store {
	return kotsstore.KOTSStore{}
}