package store

import (
	"github.com/docker/swarmkit/api"
	memdb "github.com/hashicorp/go-memdb"
	"github.com/pkg/errors"
)

const tableResource = "resource"

func init() {
	register(ObjectStoreConfig{
		Table: &memdb.TableSchema{
			Name: tableResource,
			Indexes: map[string]*memdb.IndexSchema{
				indexID: {
					Name:    indexID,
					Unique:  true,
					Indexer: api.ResourceIndexerByID{},
				},
				indexName: {
					Name:    indexName,
					Unique:  true,
					Indexer: api.ResourceIndexerByName{},
				},
				indexKind: {
					Name:    indexKind,
					Indexer: resourceIndexerByKind{},
				},
				indexCustom: {
					Name:         indexCustom,
					Indexer:      api.ResourceCustomIndexer{},
					AllowMissing: true,
				},
			},
		},
		Save: func(tx ReadTx, snapshot *api.StoreSnapshot) error {
			var err error
			snapshot.Resources, err = FindResources(tx, All)
			return err
		},
		Restore: func(tx Tx, snapshot *api.StoreSnapshot) error {
			resources, err := FindResources(tx, All)
			if err != nil {
				return err
			}
			for _, r := range resources {
				if err := DeleteResource(tx, r.ID); err != nil {
					return err
				}
			}
			for _, r := range snapshot.Resources {
				if err := CreateResource(tx, r); err != nil {
					return err
				}
			}
			return nil
		},
		ApplyStoreAction: func(tx Tx, sa api.StoreAction) error {
			switch v := sa.Target.(type) {
			case *api.StoreAction_Resource:
				obj := v.Resource
				switch sa.Action {
				case api.StoreActionKindCreate:
					return CreateResource(tx, obj)
				case api.StoreActionKindUpdate:
					return UpdateResource(tx, obj)
				case api.StoreActionKindRemove:
					return DeleteResource(tx, obj.ID)
				}
			}
			return errUnknownStoreAction
		},
	})
}

type resourceEntry struct {
	*api.Resource
}

func confirmExtension(tx Tx, r *api.Resource) error {
	// There must be an extension corresponding to the Kind field.
	extensions, err := FindExtensions(tx, ByName(r.Kind))
	if err != nil {
		return errors.Wrap(err, "failed to query extensions")
	}
	if len(extensions) == 0 {
		return errors.Errorf("object kind %s is unregistered", r.Kind)
	}
	return nil
}

// CreateResource adds a new resource object to the store.
// Returns ErrExist if the ID is already taken.
func CreateResource(tx Tx, r *api.Resource) error {
	if err := confirmExtension(tx, r); err != nil {
		return err
	}
	return tx.create(tableResource, resourceEntry{r})
}

// UpdateResource updates an existing resource object in the store.
// Returns ErrNotExist if the object doesn't exist.
func UpdateResource(tx Tx, r *api.Resource) error {
	if err := confirmExtension(tx, r); err != nil {
		return err
	}
	return tx.update(tableResource, resourceEntry{r})
}

// DeleteResource removes a resource object from the store.
// Returns ErrNotExist if the object doesn't exist.
func DeleteResource(tx Tx, id string) error {
	return tx.delete(tableResource, id)
}

// GetResource looks up a resource object by ID.
// Returns nil if the object doesn't exist.
func GetResource(tx ReadTx, id string) *api.Resource {
	r := tx.get(tableResource, id)
	if r == nil {
		return nil
	}
	return r.(resourceEntry).Resource
}

// FindResources selects a set of resource objects and returns them.
func FindResources(tx ReadTx, by By) ([]*api.Resource, error) {
	checkType := func(by By) error {
		switch by.(type) {
		case byIDPrefix, byName, byKind, byCustom, byCustomPrefix:
			return nil
		default:
			return ErrInvalidFindBy
		}
	}

	resourceList := []*api.Resource{}
	appendResult := func(o api.StoreObject) {
		resourceList = append(resourceList, o.(resourceEntry).Resource)
	}

	err := tx.find(tableResource, by, checkType, appendResult)
	return resourceList, err
}

type resourceIndexerByKind struct{}

func (ri resourceIndexerByKind) FromArgs(args ...interface{}) ([]byte, error) {
	return fromArgs(args...)
}

func (ri resourceIndexerByKind) FromObject(obj interface{}) (bool, []byte, error) {
	r := obj.(resourceEntry)

	// Add the null character as a terminator
	val := r.Resource.Kind + "\x00"
	return true, []byte(val), nil
}
