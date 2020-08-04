// Copyright 2018-present the CoreDHCP Authors. All rights reserved
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

// Package leasestorage provides the interface for building lease storage plugins
// These plugins provide interfaces to store and especially retrieve leases
package leasestorage

import (
	"net"
	"time"

	"github.com/coredhcp/coredhcp/plugins"
)

// Lease holds data for a single lease to a client
type Lease struct {
	// Elements is a type generic enough that we can hold any known type of
	// lease (one or multiple IPs or prefixes)
	Elements []net.IPNet

	// Expire is the expiration date of the lease
	Expire time.Time

	// Owner keeps a reference to the plugin that inserted this.
	// It may be used for filtering leases and not touching those from other plugins
	Owner *plugins.Plugin

	// ExpireAction is the callback invoked on lease expiration. It receives
	// Elements and Expire as parameters
	ExpireAction func(elements []net.IPNet, expiredAt time.Time)

	// Here we may need to add something like (and pass it to ExpireAction at least)
	// AdditionalData interface{}
}

// LeaseStore is the interface offered by the plugins storing DHCP leases
type LeaseStore interface {
	// Lookup obtains the leases for a client and prepares an update to them
	Lookup(ClientID) ([]Lease, *Token, error)

	// Update attempts to update the leases for ClientID.
	// It may fail and invalidate the Token, after which Lookup() needs to be
	// performed again (and in general the whole operation restarted)
	// On success, the Token is usually invalidated
	// It may also fail without invalidating the token and be retried
	Update(ClientID, []Lease, *Token) error

	// Possibly, if especially useful, a read that only reads and doesn't create a token:
	// ReadOnlyLookup(ClientID) ([]Lease, error)

	// ReleaseToken cleans up any resource associated with an issued token.
	// It must handle being called multiple times (possibly concurrently) for the same token so it
	// must handle its own synchronization.
	// It must handle being called from Update() or Lookup(). It is called when the token is invalidated
	ReleaseToken(*Token)
}
