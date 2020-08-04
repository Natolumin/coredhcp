// Copyright 2018-present the CoreDHCP Authors. All rights reserved
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

// Package transient implements a lease storage plugin that keeps leasestorage in memory
// It is used as an example and for tests/experiments
package transient

import (
	"errors"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins/leasestorage"
)

var log = logger.GetLogger("plugins/leasestorage/transient")

const (
	expireGrace = time.Minute
)

type storage struct {
	// revision is a counter used to prevent concurrent modifications.
	// The special value 0 can be used when there are no leases (so that the 0-value for storage
	// is valid). Any other value MUST be sourced from LeaseStore.getRevision, to ensure that
	// non-zero revision values never go backwards and are never reused
	// ("never" meaning until 2^64 messages have been handled by the DHCP server)
	revision uint64
	leases   []leasestorage.Lease
	sync.Mutex
}

type tokenValue struct {
	// The revision at which the token was issued.
	revision uint64
	// The clientID for which we issued the token (to ensure we don't accidentally move it around)
	cid leasestorage.ClientID
}

// LeaseStore holds leasestorage in an in-memory map
type LeaseStore struct {
	// keyLock handles synchronizing the global map state, ie looking up or adding elements
	// But modifying inner elements after chasing the *storage pointer is ok without this lock
	// However, to avoid deadlocks, you must not try to take this lock while holding an element lock
	keyLock sync.RWMutex
	// This holds the actual records. Note the pointer is very rarely written to
	// Usually entries are created once then the inner value is updated. Rarely they might be cleaned
	// up and the pointer reset to nil / the entry deleted
	// This might be a good candidate to be replaced by sync.Map. Needs benchmarking
	records map[leasestorage.ClientID]*storage
	// This is a global counter for the map, which is increased on each modification of an inner
	// value. It is used to seed the revision value for individual entries on modifications,
	// to avoid rollover issues when map entries are garbage-collected
	// It must only be accessed through atomic operations (from sync.atomic) except for initial creation
	currentRev uint64
}

// reset resets a storage instance to the zero value
// except for the lock (so we don't overwrite it), which must be held
func (s *storage) reset() {
	s.leases = nil
	s.revision = 0
}

// Lookup fetches leases for a client and returns them
func (lstore *LeaseStore) Lookup(cid leasestorage.ClientID) ([]leasestorage.Lease, *leasestorage.Token, error) {
	lstore.keyLock.RLock()
	recs := lstore.records[cid]
	lstore.keyLock.RUnlock()

	var (
		outLeases []leasestorage.Lease
		revision  uint64 = 0
	)
	if recs != nil {
		// Ensure no slice modifications while we copy it, so we have a consistent view at a specific revision
		recs.Lock()
		outLeases = make([]leasestorage.Lease, len(recs.leases))
		for i := range recs.leases {
			outLeases[i] = duplicateLease(&recs.leases[i])
		}
		revision = recs.revision
		recs.Unlock()
	}

	token := leasestorage.NewToken(lstore, tokenValue{
		cid:      cid,
		revision: revision,
	})
	return outLeases, &token, nil
}

func (lstore *LeaseStore) getRevision() uint64 {
	val := atomic.AddUint64(&lstore.currentRev, 1)
	for val == 0 { // Unlikely (2^64 rollover)
		val = atomic.AddUint64(&lstore.currentRev, 1)
	}
	return val
}

// Update inserts new leases for a client
func (lstore *LeaseStore) Update(cid leasestorage.ClientID, newLeases []leasestorage.Lease, token *leasestorage.Token) error {
	if !token.Valid() {
		return leasestorage.ErrAlreadyInvalid
	} else if !token.IsOwnedBy(lstore) {
		return errors.New("The token is for another plugin")
	}

	tokVal, ok := token.Value.(tokenValue)
	if !ok {
		log.Errorf("BUG: token value issued from this plugin isn't the correct type (token: %#v)", token)
		return token.InvalidateWithError(errors.New("Corrupted token"))
	}
	if tokVal.cid != cid {
		return errors.New("The token was used for a different client than the one it was issued for")
	}

	lstore.keyLock.RLock()
	prev, already := lstore.records[cid]
	lstore.keyLock.RUnlock()

	if already {
		prev.Lock()
		defer prev.Unlock()
		// Check if we have the right revision.
		// This will fail if any other update happened inbetween as the revision will have been
		// updated. It will at the same time update the revision to invalidate any other
		// issued tokens and to make it odd (to indicate it's being updated)
		if prev.revision != tokVal.revision {
			return token.InvalidateWithError(leasestorage.ErrConcurrentUpdate)
		}

		if len(newLeases) > 0 {
			prev.leases = newLeases
			prev.revision = lstore.getRevision()
		} else {
			prev.reset()
		}
	} else { // Create the first leases
		if tokVal.revision != 0 {
			// We have a token based on existing leases, but there are none in the table
			return token.InvalidateWithError(leasestorage.ErrConcurrentUpdate)
		}

		lstore.keyLock.Lock()
		defer lstore.keyLock.Unlock()
		// Since we release the RLock to take the WLock to be able to add elements in the map
		// We need to recheck the revision
		_, already = lstore.records[cid]
		if already {
			// Since there are now leases in the map, there was a concurrent update while we had
			// released the lock. Abort
			return token.InvalidateWithError(leasestorage.ErrConcurrentUpdate)
		}

		lstore.records[cid] = &storage{revision: lstore.getRevision(), leases: newLeases}
	}

	// Discard token after a successful update
	token.Invalidate()
	return nil
}

func duplicateLease(l *leasestorage.Lease) leasestorage.Lease {
	dupLeases := make([]net.IPNet, len(l.Elements))
	for i := range l.Elements {
		dupLeases[i].IP = make(net.IP, len(l.Elements[i].IP))
		dupLeases[i].Mask = make(net.IPMask, len(l.Elements[i].Mask))
		copy(dupLeases[i].IP, l.Elements[i].IP)
		copy(dupLeases[i].Mask, l.Elements[i].Mask)
	}
	return leasestorage.Lease{
		Elements:     dupLeases,
		Expire:       l.Expire,
		Owner:        l.Owner,
		ExpireAction: l.ExpireAction,
	}
}

// Dump outputs the entire map
// The output map may not have existed in that exact state at any point in time, however each entry
// will be internally consistent
func (lstore *LeaseStore) Dump() map[leasestorage.ClientID][]leasestorage.Lease {
	out := make(map[leasestorage.ClientID][]leasestorage.Lease)
	lstore.keyLock.RLock()
	for k, v := range lstore.records {
		v.Lock()
		out[k] = make([]leasestorage.Lease, len(v.leases))
		for i := range v.leases {
			out[k][i] = duplicateLease(&v.leases[i])
		}
		v.Unlock()
	}
	lstore.keyLock.RUnlock()

	return out
}

// Expire garbage-collects expired leases
// It takes a target number of leases to expire before returning
// This allows using it in 2 situations, both during immediate pressure, to release a few leases and
// allow a plugin to allocate some new ones to its clients and continue its work, and as a scheduled
// task for regular cleanup
// workAmount is the number of leases that Expire should free. It may not be
// exactly respected, it could free a few more leases when multiple leases are
// assigned to the same client, and it could free fewer leases if there are not
// enough expired leases to free
func (lstore *LeaseStore) Expire(workAmount int) (cleaned int, deferred *sync.WaitGroup) {
	cutoff := time.Now().Add(-expireGrace)
	cleanupCandidates := []leasestorage.ClientID{}

	lstore.keyLock.RLock()
	callbacks := &sync.WaitGroup{}
	for cid, v := range lstore.records {
		var cleanedLeases []leasestorage.Lease
		v.Lock()
		if v.revision == 0 {
			// Immediately mark clients with 0 leases as cleanable
			cleanupCandidates = append(cleanupCandidates, cid)
			v.Unlock()
			continue
		}
		for i, lease := range v.leases {
			// Here we have a fastpath where no lease is expired, in which case we go through
			// all the leases and check them, but don't allocate or copy anything
			// Or a slowpath when at least one lease expired, where we have to copy all the
			// non-expired leases to a new slice
			if lease.Expire.Before(cutoff) {
				if lease.ExpireAction != nil {
					// TODO: probably a workqueue here I guess. Anyway this has to not block
					callbacks.Add(1)
					go func() {
						lease.ExpireAction(lease.Elements, lease.Expire)
						callbacks.Done()
					}()
				}
				if cleanedLeases == nil {
					// At least one lease expired, we need to rewrite the array
					// XXX: The heuristic for the size here is probably stupid, just let it be resized ?
					// XXX: Alternatively update in-place and eat the cost of leaked memory
					// at the end of the slice
					cleanedLeases = make([]leasestorage.Lease, i, len(v.leases)-(len(v.leases)/(i+1)))
					copy(cleanedLeases, v.leases[:i])
				}

				cleaned++
			} else if cleanedLeases != nil {
				// if we've started copying still-valid leases because at least one expired
				// we need to copy all the remaining non-expired leases
				cleanedLeases = append(cleanedLeases, v.leases[i])
			}
		}
		if cleanedLeases != nil {
			if len(cleanedLeases) > 0 {
				v.leases = cleanedLeases
				v.revision = lstore.getRevision()
			} else {
				// Reset leases to zero state and mark this entry for deletion
				v.reset()
				cleanupCandidates = append(cleanupCandidates, cid)
			}
		}
		v.Unlock()

		if cleaned >= workAmount {
			// We've done enough
			break
		}
	}
	lstore.keyLock.RUnlock()
	log.Printf("Expired %d leases", cleaned)

	// Now schedule cleanup of the orphaned entries
	// this can block for a while (need to lock the whole map), so we punt it
	// to a goroutine and return before it's done. The WaitGroup can be use to track completion
	deferred = &sync.WaitGroup{}
	deferred.Add(1)
	go lstore.cleanup(cleanupCandidates, deferred)

	// Wait until all pending callbacks are done so we can guarantee the leases we said we freed
	// are actually freed
	callbacks.Wait()
	return
}

func (lstore *LeaseStore) cleanup(candidates []leasestorage.ClientID, wg *sync.WaitGroup) {
	lstore.keyLock.Lock()
	for _, c := range candidates {
		stored := lstore.records[c]
		if stored != nil {
			stored.Lock()
			// Only delete actually empty records
			if stored.revision != 0 {
				stored.Unlock()
				continue
			}
			delete(lstore.records, c)
			// The inner record stays locked. Since it is not referenced in the map anymore
			// nothing should try to lock it again, and it will be gc'd soon
		}
	}
	lstore.keyLock.Unlock()
	wg.Done()
}

func (lstore *LeaseStore) expireTask(expirePeriod time.Duration) {
	expireSchedule := time.NewTicker(expirePeriod)
	for {
		<-expireSchedule.C
		lstore.Expire(math.MaxInt32)
	}
}

// ReleaseToken frees resources associated with the token
// For this storage there are none so this is a noop
func (lstore *LeaseStore) ReleaseToken(_ *leasestorage.Token) {}

// New initializes a new instance of the LeaseStore plugin
func New(expirePeriod time.Duration) *LeaseStore {
	ls := LeaseStore{
		records:    make(map[leasestorage.ClientID]*storage),
		currentRev: 1,
	}
	go ls.expireTask(expirePeriod)

	return &ls
}
