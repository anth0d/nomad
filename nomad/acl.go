package nomad

import (
	"errors"
	"fmt"
	"net"
	"time"

	metrics "github.com/armon/go-metrics"
	lru "github.com/hashicorp/golang-lru"
	"github.com/hashicorp/nomad/acl"
	"github.com/hashicorp/nomad/helper"
	"github.com/hashicorp/nomad/nomad/state"
	"github.com/hashicorp/nomad/nomad/structs"
)

// Authenticate extracts an AuthenticatedIdentity from the request context or
// provided token. The caller can extract an acl.ACL, WorkloadIdentity, or other
// identifying token to use for authorization.
//
// Note: when called on the follower we'll be making stale queries, so it's
// possible if the follower is behind that the leader will get a different value
// if an ACL token or allocation's WI has just been created.
func (s *Server) Authenticate(ctx *RPCContext, secretID string) (*structs.AuthenticatedIdentity, error) {

	// Previously-connected clients will have a NodeID set and will be a large
	// number of the RPCs sent, so we can fast path this case
	if ctx != nil && ctx.NodeID != "" {
		return &structs.AuthenticatedIdentity{ClientID: ctx.NodeID}, nil
	}

	// get the user ACLToken or anonymous token
	aclToken, err := s.ResolveSecretToken(secretID)

	switch {
	case err == nil:
		// If ACLs are disabled or we have a non-anonymous token, return that.
		if aclToken == nil || aclToken != structs.AnonymousACLToken {
			return &structs.AuthenticatedIdentity{ACLToken: aclToken}, nil
		}

	case errors.Is(err, structs.ErrTokenExpired):
		return nil, err

	case errors.Is(err, structs.ErrTokenInvalid):
		// if it's not a UUID it might be an identity claim
		claims, err := s.VerifyClaim(secretID)
		if err != nil {
			// we already know the token wasn't valid for an ACL in the state
			// store, so if we get an error at this point we have an invalid
			// token and there are no other options but to bail out
			return nil, err
		}

		return &structs.AuthenticatedIdentity{Claims: claims}, nil

	case errors.Is(err, structs.ErrTokenNotFound):
		// Check if the secret ID is the leader's secret ID, in which case treat
		// it as a management token.
		leaderAcl := s.getLeaderAcl()
		if leaderAcl != "" && secretID == leaderAcl {
			aclToken = structs.LeaderACLToken
		} else {
			// Otherwise, see if the secret ID belongs to a node. We should
			// reach this point only on first connection.
			node, err := s.State().NodeBySecretID(nil, secretID)
			if err != nil {
				// this is a go-memdb error; shouldn't happen
				return nil, fmt.Errorf("could not resolve node secret: %w", err)
			}
			if node != nil {
				return &structs.AuthenticatedIdentity{ClientID: node.ID}, nil
			}
		}

	default: // any other error
		return nil, fmt.Errorf("could not resolve user: %w", err)

	}

	// If there's no context we're in a "static" handler which only happens for
	// cases where the leader is making RPCs internally (volumewatcher and
	// deploymentwatcher)
	if ctx == nil {
		return &structs.AuthenticatedIdentity{ACLToken: aclToken}, nil
	}

	// At this point we either have an anonymous token or an invalid one.
	// Unlike clients that provide their Node ID on first connection, server
	// RPCs don't include an ID for the server so we identify servers by cert
	// and IP address.
	identity := &structs.AuthenticatedIdentity{ACLToken: aclToken}
	if ctx.TLS {
		identity.TLSName = ctx.Certificate().Subject.CommonName
	}

	var remoteAddr *net.TCPAddr
	var ok bool
	if ctx.Session != nil {
		remoteAddr, ok = ctx.Session.RemoteAddr().(*net.TCPAddr)
		if !ok {
			return nil, errors.New("session address was not a TCP address")
		}
	}
	if remoteAddr == nil && ctx.Conn != nil {
		remoteAddr, ok = ctx.Conn.RemoteAddr().(*net.TCPAddr)
		if !ok {
			return nil, errors.New("session address was not a TCP address")
		}
	}
	if remoteAddr != nil {
		identity.RemoteIP = remoteAddr.IP
		return identity, nil
	}

	s.logger.Error("could not authenticate RPC request or determine remote address")
	return nil, structs.ErrPermissionDenied
}

func (s *Server) ResolveACL(aclToken *structs.ACLToken) (*acl.ACL, error) {
	if !s.config.ACLEnabled {
		return nil, nil
	}
	snap, err := s.fsm.State().Snapshot()
	if err != nil {
		return nil, err
	}
	return resolveACLFromToken(snap, s.aclCache, aclToken)
}

// ResolveToken is used to translate an ACL Token Secret ID into
// an ACL object, nil if ACLs are disabled, or an error.
func (s *Server) ResolveToken(secretID string) (*acl.ACL, error) {
	// Fast-path if ACLs are disabled
	if !s.config.ACLEnabled {
		return nil, nil
	}
	defer metrics.MeasureSince([]string{"nomad", "acl", "resolveToken"}, time.Now())

	// Check if the secret ID is the leader secret ID, in which case treat it as
	// a management token.
	if leaderAcl := s.getLeaderAcl(); leaderAcl != "" && secretID == leaderAcl {
		return acl.ManagementACL, nil
	}

	// Snapshot the state
	snap, err := s.fsm.State().Snapshot()
	if err != nil {
		return nil, err
	}

	// Resolve the ACL
	return resolveTokenFromSnapshotCache(snap, s.aclCache, secretID)
}

// VerifyClaim asserts that the token is valid and that the resulting
// allocation ID belongs to a non-terminal allocation
func (s *Server) VerifyClaim(token string) (*structs.IdentityClaims, error) {

	claims, err := s.encrypter.VerifyClaim(token)
	if err != nil {
		return nil, err
	}
	snap, err := s.fsm.State().Snapshot()
	if err != nil {
		return nil, err
	}
	alloc, err := snap.AllocByID(nil, claims.AllocationID)
	if err != nil {
		return nil, err
	}
	if alloc == nil || alloc.Job == nil {
		return nil, fmt.Errorf("allocation does not exist")
	}

	// the claims for terminal allocs are always treated as expired
	if alloc.TerminalStatus() {
		return nil, fmt.Errorf("allocation is terminal")
	}

	return claims, nil
}

func (s *Server) ResolveClaims(claims *structs.IdentityClaims) (*acl.ACL, error) {

	policies, err := s.resolvePoliciesForClaims(claims)
	if err != nil {
		return nil, err
	}
	if len(policies) == 0 {
		return nil, nil
	}

	// Compile and cache the ACL object
	aclObj, err := structs.CompileACLObject(s.aclCache, policies)
	if err != nil {
		return nil, err
	}
	return aclObj, nil
}

// resolveTokenFromSnapshotCache is used to resolve an ACL object from a
// snapshot of state, using a cache to avoid parsing and ACL construction when
// possible. It is split from resolveToken to simplify testing.
func resolveTokenFromSnapshotCache(snap *state.StateSnapshot, cache *lru.TwoQueueCache, secretID string) (*acl.ACL, error) {
	// Lookup the ACL Token
	var token *structs.ACLToken
	var err error

	// Handle anonymous requests
	if secretID == "" {
		token = structs.AnonymousACLToken
	} else {
		token, err = snap.ACLTokenBySecretID(nil, secretID)
		if err != nil {
			return nil, err
		}
		if token == nil {
			return nil, structs.ErrTokenNotFound
		}
		if token.IsExpired(time.Now().UTC()) {
			return nil, structs.ErrTokenExpired
		}
	}

	return resolveACLFromToken(snap, cache, token)

}

func resolveACLFromToken(snap *state.StateSnapshot, cache *lru.TwoQueueCache, token *structs.ACLToken) (*acl.ACL, error) {

	// Check if this is a management token
	if token.Type == structs.ACLManagementToken {
		return acl.ManagementACL, nil
	}

	// Store all policies detailed in the token request, this includes the
	// named policies and those referenced within the role link.
	policies := make([]*structs.ACLPolicy, 0, len(token.Policies)+len(token.Roles))

	// Iterate all the token policies and add these to our policy tracking
	// array.
	for _, policyName := range token.Policies {
		policy, err := snap.ACLPolicyByName(nil, policyName)
		if err != nil {
			return nil, err
		}
		if policy == nil {
			// Ignore policies that don't exist, since they don't grant any
			// more privilege.
			continue
		}

		// Add the policy to the tracking array.
		policies = append(policies, policy)
	}

	// Iterate all the token role links, so we can unpack these and identify
	// the ACL policies.
	for _, roleLink := range token.Roles {

		// Any error reading the role means we cannot move forward. We just
		// ignore any roles that have been detailed but are not within our
		// state.
		role, err := snap.GetACLRoleByID(nil, roleLink.ID)
		if err != nil {
			return nil, err
		}
		if role == nil {
			continue
		}

		// Unpack the policies held within the ACL role to form a single list
		// of ACL policies that this token has available.
		for _, policyLink := range role.Policies {
			policy, err := snap.ACLPolicyByName(nil, policyLink.Name)
			if err != nil {
				return nil, err
			}

			// Ignore policies that don't exist, since they don't grant any
			// more privilege.
			if policy == nil {
				continue
			}

			// Add the policy to the tracking array.
			policies = append(policies, policy)
		}
	}

	// Compile and cache the ACL object
	aclObj, err := structs.CompileACLObject(cache, policies)
	if err != nil {
		return nil, err
	}
	return aclObj, nil
}

// ResolveSecretToken is used to translate an ACL Token Secret ID into
// an ACLToken object, nil if ACLs are disabled, or an error.
func (s *Server) ResolveSecretToken(secretID string) (*structs.ACLToken, error) {
	// TODO(Drew) Look into using ACLObject cache or create a separate cache

	// Fast-path if ACLs are disabled
	if !s.config.ACLEnabled {
		return nil, nil
	}
	defer metrics.MeasureSince([]string{"nomad", "acl", "resolveSecretToken"}, time.Now())

	if secretID == "" {
		return structs.AnonymousACLToken, nil
	}
	if !helper.IsUUID(secretID) {
		return nil, structs.ErrTokenInvalid
	}

	snap, err := s.fsm.State().Snapshot()
	if err != nil {
		return nil, err
	}

	// Lookup the ACL Token
	token, err := snap.ACLTokenBySecretID(nil, secretID)
	if err != nil {
		return nil, err
	}
	if token == nil {
		return nil, structs.ErrTokenNotFound
	}
	if token.IsExpired(time.Now().UTC()) {
		return nil, structs.ErrTokenExpired
	}

	return token, nil
}

func (s *Server) resolvePoliciesForClaims(claims *structs.IdentityClaims) ([]*structs.ACLPolicy, error) {

	snap, err := s.fsm.State().Snapshot()
	if err != nil {
		return nil, err
	}
	alloc, err := snap.AllocByID(nil, claims.AllocationID)
	if err != nil {
		return nil, err
	}
	if alloc == nil || alloc.Job == nil {
		return nil, fmt.Errorf("allocation does not exist")
	}

	// Find any policies attached to the job
	iter, err := snap.ACLPolicyByJob(nil, alloc.Namespace, alloc.Job.ID)
	if err != nil {
		return nil, err
	}
	policies := []*structs.ACLPolicy{}
	for {
		raw := iter.Next()
		if raw == nil {
			break
		}
		policy := raw.(*structs.ACLPolicy)
		if policy.JobACL == nil {
			continue
		}

		switch {
		case policy.JobACL.Group == "":
			policies = append(policies, policy)
		case policy.JobACL.Group != alloc.TaskGroup:
			continue // don't bother checking task
		case policy.JobACL.Task == "":
			policies = append(policies, policy)
		case policy.JobACL.Task == claims.TaskName:
			policies = append(policies, policy)
		}
	}

	return policies, nil
}
