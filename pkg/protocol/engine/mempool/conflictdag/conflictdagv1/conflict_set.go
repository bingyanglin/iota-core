package conflictdagv1

import (
	"sync"

	"golang.org/x/xerrors"

	"github.com/iotaledger/hive.go/ds/advancedset"
	"github.com/iotaledger/iota-core/pkg/core/promise"
	"github.com/iotaledger/iota-core/pkg/protocol/engine/mempool/conflictdag"
)

// ConflictSet represents a set of Conflicts that are conflicting with each other over a common Resource.
type ConflictSet[ConflictID, ResourceID conflictdag.IDType, VotePower conflictdag.VotePowerType[VotePower]] struct {
	// ID is the ID of the Resource that the Conflicts in this ConflictSet are conflicting over.
	ID ResourceID

	// members is the set of Conflicts that are conflicting over the shared resource.
	members *advancedset.AdvancedSet[*Conflict[ConflictID, ResourceID, VotePower]]

	allMembersEvicted *promise.Value[bool]

	mutex sync.RWMutex
}

// NewConflictSet creates a new ConflictSet of Conflicts that are conflicting with each other over the given Resource.
func NewConflictSet[ConflictID, ResourceID conflictdag.IDType, VotePower conflictdag.VotePowerType[VotePower]](id ResourceID) *ConflictSet[ConflictID, ResourceID, VotePower] {
	return &ConflictSet[ConflictID, ResourceID, VotePower]{
		ID:                id,
		allMembersEvicted: promise.NewValue[bool](),
		members:           advancedset.New[*Conflict[ConflictID, ResourceID, VotePower]](),
	}
}

// Add adds a Conflict to the ConflictSet and returns all other members of the set.
func (c *ConflictSet[ConflictID, ResourceID, VotePower]) Add(addedConflict *Conflict[ConflictID, ResourceID, VotePower]) (otherMembers *advancedset.AdvancedSet[*Conflict[ConflictID, ResourceID, VotePower]], err error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.allMembersEvicted.Get() {
		return nil, xerrors.Errorf("cannot join a ConflictSet whose all members are evicted")
	}

	if otherMembers = c.members.Clone(); !c.members.Add(addedConflict) {
		return nil, conflictdag.ErrAlreadyPartOfConflictSet
	}

	return otherMembers, nil

}

// Remove removes a Conflict from the ConflictSet and returns all remaining members of the set.
func (c *ConflictSet[ConflictID, ResourceID, VotePower]) Remove(removedConflict *Conflict[ConflictID, ResourceID, VotePower]) (removed bool) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if removed = c.members.Delete(removedConflict); removed && c.members.IsEmpty() {
		c.allMembersEvicted.Set(true)
	}

	return removed
}

func (c *ConflictSet[ConflictID, ResourceID, VotePower]) ForEach(callback func(parent *Conflict[ConflictID, ResourceID, VotePower]) error) error {
	c.mutex.RLock()
	defer c.mutex.RUnlock()

	return c.members.ForEach(callback)
}

// OnAllMembersEvicted executes a callback when all members of the ConflictSet are evicted and the ConflictSet itself can be evicted.
func (c *ConflictSet[ConflictID, ResourceID, VotePower]) OnAllMembersEvicted(callback func(prevValue, newValue bool)) {
	c.allMembersEvicted.OnUpdate(callback)
}