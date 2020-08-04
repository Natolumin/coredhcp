// Copyright 2018-present the CoreDHCP Authors. All rights reserved
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

// Package leasestorage provides the interface for building lease storage plugins
// These plugins provide interfaces to store and especially retrieve leases
package leasestorage

import (
	"fmt"
	"net"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv6"
)

const (
	// CidHWAddress is for using a Hardware address as client id
	CidHWAddress = iota
	// CidOpt61 is for using dhcpv4 client information supplied by the client ("Option 61") as client ID
	CidOpt61
	// CidDUID is for using the DUID found in the DHCPv6 client-id option
	CidDUID
	// CidDUIDAndIAID is for using both the DUID of the client and the iaid; typically for IA_TA assignments
	CidDUIDAndIAID
	// CidReservedExperimentalDoNotUse is for quick experiments using arbitrary data as CID.
	// Do not use in real plugins
	CidReservedExperimentalDoNotUse = 0xff
)

// ClientID is used to find leases.
// It works as a tagged enum, holding a type and a bytestring with the actual value
type ClientID struct {
	// Variant will be one of a few constants identifying types of clientIDs
	Variant uint8
	// data here is a string only used as a read-only []byte that we can use to index maps
	// It should hold *raw bytes* and not the human-readable ascii serializations
	Data string
}

// ClientIDFromHWAddr creates a ClientID from a MAC address
func ClientIDFromHWAddr(a net.HardwareAddr) ClientID {
	return ClientID{Variant: CidHWAddress, Data: string(a)}
}

// ClientIDFromOpt61 creates a ClientID from a DHCPv4 client ID (Option 61)
func ClientIDFromOpt61(opt dhcpv4.Option) (ClientID, error) {
	if opt.Code != dhcpv4.OptionClientIdentifier {
		return ClientID{}, fmt.Errorf("Incorrect option type, expecting Client Identifier(61), got %d", opt.Code)
	}
	return ClientID{Variant: CidOpt61, Data: string(opt.Value.ToBytes())}, nil
}

// ClientIDFromDUID creates a ClientID from a DHCPv6 DUID
func ClientIDFromDUID(d dhcpv6.Duid) ClientID {
	return ClientID{Variant: CidDUID, Data: string(d.ToBytes())}
}

// ClientIDFromDUIDAndIAID creates a ClientID from both a DHCPv6 DUID and an IA_*A iaid
func ClientIDFromDUIDAndIAID(d dhcpv6.Duid, iaid [4]byte) ClientID {
	return ClientID{Variant: CidDUIDAndIAID, Data: string(iaid[:]) + string(d.ToBytes())}
}
