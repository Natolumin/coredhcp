// Copyright 2018-present the CoreDHCP Authors. All rights reserved
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

// Package leasestorage provides the interface for building lease storage plugins
// These plugins provide interfaces to store and especially retrieve leases
package leasestorage

import (
	"errors"
	"fmt"
)

const maxRetries = 5

// Mutate attempts to update a lease multiple times, until it succeeds.
// If the leasestore supports Mutate, then it can use that directly
func Mutate(ls LeaseStore, client ClientID, mutator func([]Lease) ([]Lease, error)) error {
	if als, ok := ls.(AtomicLeaseStore); ok {
		return als.Mutate(client, mutator)
	}

	for i := 0; i < maxRetries; i++ {
		leases, token, err := ls.Lookup(client)
		if err != nil {
			continue
		}

		leases, err = mutator(leases)
		if err != nil {
			return fmt.Errorf("Could not mutate leases: %w", err)
		}

		err = ls.Update(client, leases, token)
		if err != nil {
			if errors.Is(err, ErrConcurrentUpdate) {
				continue
			}
			return fmt.Errorf("Error inserting updated leases: %w", err)
		}
		return nil
	}
	return fmt.Errorf("Could not update records after %d tries", maxRetries)
}
