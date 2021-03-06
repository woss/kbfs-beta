// Copyright 2016 Keybase Inc. All rights reserved.
// Use of this source code is governed by a BSD
// license that can be found in the LICENSE file.

package libkbfs

import (
	"fmt"
	"sync"
)

type nodeCacheEntry struct {
	core     *nodeCore
	refCount int
}

// nodeCacheStandard implements the NodeCache interface by tracking
// the reference counts of nodeStandard Nodes, and using their member
// fields to construct paths.
type nodeCacheStandard struct {
	folderBranch FolderBranch
	nodes        map[blockRef]*nodeCacheEntry
	lock         sync.RWMutex
}

var _ NodeCache = (*nodeCacheStandard)(nil)

func newNodeCacheStandard(fb FolderBranch) *nodeCacheStandard {
	return &nodeCacheStandard{
		folderBranch: fb,
		nodes:        make(map[blockRef]*nodeCacheEntry),
	}
}

// lock must be locked for writing by the caller
func (ncs *nodeCacheStandard) forgetLocked(core *nodeCore) {
	ref := core.pathNode.ref()

	entry, ok := ncs.nodes[ref]
	if !ok {
		return
	}
	if entry.core != core {
		return
	}

	entry.refCount--
	if entry.refCount <= 0 {
		delete(ncs.nodes, ref)
	}
}

// should be called only by nodeStandardFinalizer().
func (ncs *nodeCacheStandard) forget(core *nodeCore) {
	ncs.lock.Lock()
	defer ncs.lock.Unlock()
	ncs.forgetLocked(core)
}

// lock must be held for writing by the caller
func (ncs *nodeCacheStandard) newChildForParentLocked(parent Node) (*nodeStandard, error) {
	nodeStandard, ok := parent.(*nodeStandard)
	if !ok {
		return nil, ParentNodeNotFoundError{blockRef{}}
	}

	ref := nodeStandard.core.pathNode.ref()
	entry, ok := ncs.nodes[ref]
	if !ok {
		return nil, ParentNodeNotFoundError{ref}
	}
	if nodeStandard.core != entry.core {
		return nil, ParentNodeNotFoundError{ref}
	}
	return nodeStandard, nil
}

func makeNodeStandardForEntry(entry *nodeCacheEntry) *nodeStandard {
	entry.refCount++
	return makeNodeStandard(entry.core)
}

// GetOrCreate implements the NodeCache interface for nodeCacheStandard.
func (ncs *nodeCacheStandard) GetOrCreate(
	ptr BlockPointer, name string, parent Node) (Node, error) {
	if !ptr.IsValid() {
		// Temporary code to track down bad block
		// pointers. Remove when not needed anymore.
		panic(InvalidBlockRefError{ptr.ref()})
	}

	if name == "" {
		return nil, EmptyNameError{ptr.ref()}
	}

	ncs.lock.Lock()
	defer ncs.lock.Unlock()
	entry, ok := ncs.nodes[ptr.ref()]
	if ok {
		// If the entry happens to be unlinked, we may be in a
		// situation where a node got unlinked and then recreated, but
		// someone held onto a node the whole time and so it never got
		// removed from the cache.  In that case, forcibly remove it
		// from the cache to make room for the new node.
		if parent != nil && entry.core.parent == nil {
			delete(ncs.nodes, ptr.ref())
		} else {
			return makeNodeStandardForEntry(entry), nil
		}
	}

	var parentNS *nodeStandard
	if parent != nil {
		var err error
		parentNS, err = ncs.newChildForParentLocked(parent)
		if err != nil {
			return nil, err
		}
	}

	entry = &nodeCacheEntry{
		core: newNodeCore(ptr, name, parentNS, ncs),
	}
	ncs.nodes[ptr.ref()] = entry
	return makeNodeStandardForEntry(entry), nil
}

// Get implements the NodeCache interface for nodeCacheStandard.
func (ncs *nodeCacheStandard) Get(ref blockRef) Node {
	if ref == (blockRef{}) {
		return nil
	}

	// Temporary code to track down bad block pointers. Remove (or
	// return an error) when not needed anymore.
	if !ref.IsValid() {
		panic(InvalidBlockRefError{ref})
	}

	ncs.lock.Lock()
	defer ncs.lock.Unlock()
	entry, ok := ncs.nodes[ref]
	if !ok {
		return nil
	}
	return makeNodeStandardForEntry(entry)
}

// UpdatePointer implements the NodeCache interface for nodeCacheStandard.
func (ncs *nodeCacheStandard) UpdatePointer(
	oldRef blockRef, newPtr BlockPointer) {
	if oldRef == (blockRef{}) && newPtr == (BlockPointer{}) {
		return
	}

	if !oldRef.IsValid() {
		panic(fmt.Sprintf("invalid oldRef %s with newPtr %s", oldRef, newPtr))
	}

	if !newPtr.IsValid() {
		panic(fmt.Sprintf("invalid newPtr %s with oldRef %s", newPtr, oldRef))
	}

	ncs.lock.Lock()
	defer ncs.lock.Unlock()
	entry, ok := ncs.nodes[oldRef]
	if !ok {
		return
	}

	// Cannot update the pointer for an unlinked node
	if len(entry.core.cachedPath.path) > 0 {
		return
	}

	entry.core.pathNode.BlockPointer = newPtr
	delete(ncs.nodes, oldRef)
	ncs.nodes[newPtr.ref()] = entry
}

// Move implements the NodeCache interface for nodeCacheStandard.
func (ncs *nodeCacheStandard) Move(
	ref blockRef, newParent Node, newName string) error {
	if ref == (blockRef{}) {
		return nil
	}

	// Temporary code to track down bad block pointers. Remove (or
	// return an error) when not needed anymore.
	if !ref.IsValid() {
		panic(InvalidBlockRefError{ref})
	}

	if newName == "" {
		return EmptyNameError{ref}
	}

	ncs.lock.Lock()
	defer ncs.lock.Unlock()
	entry, ok := ncs.nodes[ref]
	if !ok {
		return nil
	}

	newParentNS, err := ncs.newChildForParentLocked(newParent)
	if err != nil {
		return err
	}

	entry.core.parent = newParentNS
	entry.core.pathNode.Name = newName
	return nil
}

// Unlink implements the NodeCache interface for nodeCacheStandard.
func (ncs *nodeCacheStandard) Unlink(ref blockRef, oldPath path) {
	if ref == (blockRef{}) {
		return
	}

	// Temporary code to track down bad block pointers. Remove (or
	// return an error) when not needed anymore.
	if !ref.IsValid() {
		panic(InvalidBlockRefError{ref})
	}

	ncs.lock.Lock()
	defer ncs.lock.Unlock()
	entry, ok := ncs.nodes[ref]
	if !ok {
		return
	}

	entry.core.cachedPath = oldPath
	entry.core.parent = nil
	entry.core.pathNode.Name = ""
	return
}

// PathFromNode implements the NodeCache interface for nodeCacheStandard.
func (ncs *nodeCacheStandard) PathFromNode(node Node) (p path) {
	ncs.lock.RLock()
	defer ncs.lock.RUnlock()

	ns, ok := node.(*nodeStandard)
	if !ok {
		p.path = nil
		return
	}

	for ns != nil {
		core := ns.core
		if core.parent == nil && len(core.cachedPath.path) > 0 {
			// The node was unlinked, but is still in use, so use its
			// cached path.  The path is already reversed, so append
			// it backwards one-by-one to the existing path.  If this
			// is the first node, we can just optimize by returning
			// the complete cached path.
			if len(p.path) == 0 {
				return core.cachedPath
			}
			for i := len(core.cachedPath.path) - 1; i >= 0; i-- {
				p.path = append(p.path, core.cachedPath.path[i])
			}
			break
		}

		p.path = append(p.path, *core.pathNode)
		ns = core.parent
	}

	// need to reverse the path nodes
	for i := len(p.path)/2 - 1; i >= 0; i-- {
		opp := len(p.path) - 1 - i
		p.path[i], p.path[opp] = p.path[opp], p.path[i]
	}

	// TODO: would it make any sense to cache the constructed path?
	p.FolderBranch = ncs.folderBranch
	return
}
