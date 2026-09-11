// Copyright The Linux Foundation and each contributor to LFX.
// SPDX-License-Identifier: MIT

package constants

const (
	// AccessCheckSubject is the subject used for access control checks
	AccessCheckSubject = "lfx.access_check.request"
	// ReadTuplesSubject is the subject used for reading FGA tuples
	ReadTuplesSubject = "lfx.access_check.read_tuples"
	// AnonymousPrincipal is the identifier for anonymous users
	AnonymousPrincipal = `_anonymous`
	// PrincipalAttribute is the attribute used to indicate the principal in the logging context
	PrincipalAttribute = "principal"
	// NonceSize is the size of the number used for nonce generation
	NonceSize = 24
)
