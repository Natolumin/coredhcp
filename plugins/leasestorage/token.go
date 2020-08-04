// Copyright 2018-present the CoreDHCP Authors. All rights reserved
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package leasestorage

import "errors"

// Token is a transaction token. It is used by the provider plugin to identify a client request and
// follow it across API calls
// A Token must not be copied after creation
type Token struct {
	// Owner defines which plugin instance created and owns this token. This is used to ensure no
	// token is mistakenly used by a different plugin than the one it is created from, especially
	// if they have the same type but are separate instances of the same plugin and thus use the
	// same type for value
	// It is also used to invalidate a token, by setting owner = nil
	owner LeaseStore
	// value is the actual value of the token. It is meant to be an opaque token and shouldn't be
	// manipulated by plugins
	Value interface{}
}

// Valid checks token validity, to avoid passing an expired token to Update()
func (t *Token) Valid() bool {
	return t != nil && t.owner != nil
}

// Invalidate makes a token unusable
func (t *Token) Invalidate() {
	if t == nil || t.owner == nil {
		return
	}
	t.owner.ReleaseToken(t)
	t.owner = nil
}

// IsOwnedBy checks whether the token was created by the given plugin
func (t *Token) IsOwnedBy(plugin LeaseStore) bool {
	// Interface values are equal if both are nil or if they're the same object. We check for nil
	// because we don't want to return true in that case (a nil token is owned by noone!)
	return t != nil && plugin != nil && t.owner == plugin
}

// NewToken creates a new token for the a plugin (passed as first argument)
// The value of the token is opaque and owned by the plugin.
func NewToken(owner LeaseStore, value interface{}) Token {
	// Require the owner plugin to pass itself when minting a token
	if owner == nil {
		return Token{}
	}
	return Token{
		owner: owner,
		Value: value,
	}
}

// Error/invalidation management for tokens

var (
	// ErrToken is the sentinel error returned when an error in a lease store requires the token
	// to be reobtained
	ErrToken = NewInvalidTokenError("Token invalidated")
	// ErrAlreadyInvalid is returned when an invalid token is passed to a plugin
	ErrAlreadyInvalid = NewInvalidTokenError("The given token was invalid for this plugin")
	// ErrConcurrentUpdate is returned when another update has invalidated the leases
	ErrConcurrentUpdate = NewInvalidTokenError("The underlying leases have changed")
)

// TokenError is an error returned when a token is invalid (or just was invalidated)
// It's also possible to just wrap ErrToken, but the message of ErrToken is useless so this is nicer
type TokenError struct {
	inner   error
	message string
}

func (tErr *TokenError) Unwrap() error {
	return tErr.inner
}
func (tErr *TokenError) Error() string {
	if tErr.message != "" {
		return tErr.message
	}
	return tErr.inner.Error()
}

// Is lets TokenError be considered equivalent to ErrToken with errors.Is
func (tErr *TokenError) Is(e error) bool {
	return e == ErrToken
}

// NewInvalidTokenError creates a new error which conveys that a passed token is invalid
func NewInvalidTokenError(msg string) error {
	return &TokenError{message: msg}
}

// WrapWithInvalidToken adds info that a token was invalidated to an existing error
func WrapWithInvalidToken(e error) error {
	return &TokenError{inner: e}
}

// InvalidateWithError invalidates the token and returns an error communicating that
func (t *Token) InvalidateWithError(e error) error {
	t.Invalidate()
	if errors.Is(e, ErrToken) {
		return e
	}
	return &TokenError{inner: e}
}
